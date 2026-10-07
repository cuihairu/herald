package api

import (
	"crypto/sha1"
	"crypto/subtle"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/cuihairu/herald/core/audience"
)

// BotSourceConfig is the §8 telegram entry: the webhook secret token
// (Telegram's X-Telegram-Bot-Api-Secret-Token, empty disables the
// endpoint), the surface channel the events bind, and the default
// subscription group a successful /start <token> lays down.
type BotSourceConfig struct {
	Secret   string
	Channel  string
	Defaults []string
}

// WeChatMPSourceConfig is the §8 公众号 entry: the server-verification
// token signatures are checked against (empty disables the endpoint)
// and the default subscription group a follow lays down.
type WeChatMPSourceConfig struct {
	Token    string
	Defaults []string
}

// SetSources wires the §8 来源适配器 into the handler: the adapter does
// the registry work, the two configs gate the platform endpoints, and
// the surface registry resolves platform identities (openid, chat id)
// back to audiences.
func (h *Handler) SetSources(adapter *audience.SourceAdapter, surfaces *audience.SurfaceRegistry, bot BotSourceConfig, mp WeChatMPSourceConfig) {
	h.sourceAdapter = adapter
	h.sourceSurfaces = surfaces
	h.sourceBot = bot
	h.sourceMP = mp
}

// botUpdate is the slice of a Telegram update the entry cares about.
type botUpdate struct {
	Message *struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

// HandleBotCallback receives Telegram webhook updates (POST
// /api/v1/callbacks/bot): the /start <token> binding redemption of §3.1
// and the /stop 取关回流 of §8. Not behind the API-token middleware —
// Telegram authenticates with the webhook secret token instead (§8
// trust shape: the platform is the caller). Unconfigured, it 404s.
//
// Every well-formed update answers 200: Telegram retries non-2xx
// deliveries, and re-delivering a /stop or an expired /start changes
// nothing worth retrying. Malformed JSON is a caller bug (400), a wrong
// secret is an intruder (403).
func (h *Handler) HandleBotCallback(w http.ResponseWriter, r *http.Request) {
	if h.sourceAdapter == nil || h.sourceSurfaces == nil || h.sourceBot.Secret == "" {
		h.respondError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Telegram-Bot-Api-Secret-Token")), []byte(h.sourceBot.Secret)) != 1 {
		h.respondError(w, http.StatusForbidden, "bad webhook secret")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "cannot read request body")
		return
	}
	var update botUpdate
	if err := json.Unmarshal(body, &update); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if update.Message == nil || update.Message.Chat.ID == 0 {
		// Edited messages, callbacks, other update kinds: nothing to do.
		w.WriteHeader(http.StatusOK)
		return
	}

	chatID := fmt.Sprintf("%d", update.Message.Chat.ID)
	channel := h.sourceBot.Channel
	if channel == "" {
		channel = "telegram"
	}
	fields := strings.Fields(strings.TrimSpace(update.Message.Text))
	switch {
	case len(fields) == 2 && fields[0] == "/start":
		// §3.1: one-time token redemption binds the chat as the active
		// surface; then the default group (§8 follow) lands under bot.
		result, err := h.sourceSurfaces.RedeemBinding(fields[1], chatID)
		if err != nil {
			// Unknown/expired token: answer 200 with nothing bound —
			// retrying cannot mint a fresh token.
			w.WriteHeader(http.StatusOK)
			return
		}
		if result.Outcome == audience.OutcomeActivated {
			applyDefaults(h.sourceAdapter, result.Surface.AudienceID, channel, audience.SourceBot, h.sourceBot.Defaults)
		}
		w.WriteHeader(http.StatusOK)
	case len(fields) == 1 && fields[0] == "/stop":
		if audienceID, ok := h.sourceSurfaces.FindByTarget(channel, chatID); ok {
			// 取关回流全停 — surface invalid + subscriptions swept.
			_, _ = h.sourceAdapter.Unfollow(audienceID, channel, audience.SourceBot)
		}
		w.WriteHeader(http.StatusOK)
	default:
		// Plain /start, help, chatter: no token, no stop word.
		w.WriteHeader(http.StatusOK)
	}
}

// mpEvent is the WeChat 公众号 event push body (subset §8 reads).
type mpEvent struct {
	XMLName      xml.Name `xml:"xml"`
	FromUserName string   `xml:"FromUserName"`
	Event        string   `xml:"Event"`
}

// HandleWeChatMPCallback receives 公众号 server events (POST
// /api/v1/callbacks/wechat-mp): follow/unfollow of §8 converged into
// registry changes. Not behind the API-token middleware — the platform
// authenticates with the same shared-token signature the console URL
// verification uses. Unconfigured, it 404s.
//
// GET answers the console's echostr challenge (the one-time URL check),
// POST validates the signature on every event and answers 200 always —
// MP retries non-2xx and re-running an unfollow changes nothing.
func (h *Handler) HandleWeChatMPCallback(w http.ResponseWriter, r *http.Request) {
	if h.sourceAdapter == nil || h.sourceSurfaces == nil || h.sourceMP.Token == "" {
		h.respondError(w, http.StatusNotFound, "not found")
		return
	}
	q := r.URL.Query()
	sig, ts, nonce := q.Get("signature"), q.Get("timestamp"), q.Get("nonce")
	if sig == "" || ts == "" || nonce == "" {
		h.respondError(w, http.StatusForbidden, "missing signature")
		return
	}
	if subtle.ConstantTimeCompare([]byte(mpSignature(h.sourceMP.Token, ts, nonce)), []byte(sig)) != 1 {
		h.respondError(w, http.StatusForbidden, "bad signature")
		return
	}
	if r.Method == http.MethodGet {
		// Console URL verification: echo the challenge back verbatim.
		_, _ = io.WriteString(w, q.Get("echostr"))
		return
	}
	if r.Method != http.MethodPost {
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.respondError(w, http.StatusBadRequest, "cannot read request body")
		return
	}
	var event mpEvent
	if err := xml.Unmarshal(body, &event); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid event body")
		return
	}
	openid := event.FromUserName
	if openid == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	// The event carries the platform identity; §8 resolves it to the
	// audience the registry already bound it to. An identity with no
	// binding has no audience yet — MP follow before binding is the
	// normal order for new followers, and there is nothing to converge.
	audienceID, found := h.sourceSurfaces.FindByTarget("wechat_mp", openid)
	if !found {
		w.WriteHeader(http.StatusOK)
		return
	}
	switch strings.ToLower(strings.TrimSpace(event.Event)) {
	case "subscribe":
		_, _ = h.sourceAdapter.Follow(audienceID, "wechat_mp", openid, audience.SourceWeChatMP, h.sourceMP.Defaults)
	case "unsubscribe":
		_, _ = h.sourceAdapter.Unfollow(audienceID, "wechat_mp", audience.SourceWeChatMP)
	}
	w.WriteHeader(http.StatusOK)
}

// subscriptionRequest is the in-app preference-centre checkbox body.
type subscriptionRequest struct {
	Category string `json:"category"`
	Channel  string `json:"channel"`
}

// HandleSubscriptions is the 应用内勾选 API of §8 (check 动作): POST
// ticks a category×channel box on for the audience, DELETE ticks it
// off. Gated behind the API-token middleware — the caller is the
// application acting for its user, and batch 11's app-scoped tokens
// narrow that further. Every relation lands under source
// preference_center, so the audit answers where the opt-in came from.
func (h *Handler) HandleSubscriptions(w http.ResponseWriter, r *http.Request) {
	if h.sourceAdapter == nil {
		h.respondError(w, http.StatusNotFound, "not found")
		return
	}
	audienceID := r.PathValue("id")
	switch r.Method {
	case http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			h.respondError(w, http.StatusBadRequest, "cannot read request body")
			return
		}
		var req subscriptionRequest
		if err := json.Unmarshal(body, &req); err != nil {
			h.respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if msg, bad := checkSourceFields(audienceID, req.Category, req.Channel); bad {
			h.respondError(w, http.StatusUnprocessableEntity, msg)
			return
		}
		rel, err := h.sourceAdapter.Toggle(audienceID, req.Category, req.Channel, audience.SourcePreferenceCenter, true)
		if err != nil {
			h.respondError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: rel})
	case http.MethodDelete:
		category, channel := r.URL.Query().Get("category"), r.URL.Query().Get("channel")
		if msg, bad := checkSourceFields(audienceID, category, channel); bad {
			h.respondError(w, http.StatusUnprocessableEntity, msg)
			return
		}
		rel, err := h.sourceAdapter.Toggle(audienceID, category, channel, audience.SourcePreferenceCenter, false)
		switch {
		case errors.Is(err, audience.ErrRelationNotFound):
			h.respondError(w, http.StatusNotFound, "subscription not found")
		case err != nil:
			// Field checks passed above; the only refusal left is the
			// §4 must-deliver bottom line.
			h.respondError(w, http.StatusConflict, err.Error())
		default:
			h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: rel})
		}
	default:
		h.respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// checkSourceFields validates the three fields every source-adapter
// entry names, with the same bounds the registry enforces (id pattern
// for the audience, 1-64 chars for category/channel).
func checkSourceFields(audienceID, category, channel string) (string, bool) {
	if !audience.ValidID(audienceID) {
		return "invalid audience id", true
	}
	for label, v := range map[string]string{"category": category, "channel": channel} {
		if len(v) == 0 || len(v) > 64 {
			return fmt.Sprintf("invalid %s", label), true
		}
	}
	return "", false
}

// applyDefaults lays the default subscription group down after a
// successful bind, ignoring per-category refusals the registry refuses
// (the surface bound either way).
func applyDefaults(adapter *audience.SourceAdapter, audienceID, channel, source string, categories []string) {
	for _, category := range categories {
		_, _ = adapter.Toggle(audienceID, category, channel, source, true)
	}
}

// mpSignature computes the shared-token signature of a 公众号 request:
// sha1 of the three parameters sorted ascending, hex encoded.
func mpSignature(token, timestamp, nonce string) string {
	parts := []string{timestamp, nonce, token}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return fmt.Sprintf("%x", sum)
}
