package telegram

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cuihairu/herald/core/httpclient"
)

// Probe is the §8 telegram 对账 probe: it answers whether the Bot API
// still knows a chat — the "TG chat 有效性探测" half of external-state
// reconciliation. A chat the bot can no longer reach (bot blocked, chat
// deleted, kicked from the group) is reported false so the reconciler
// stops delivery; a probe that cannot ask (auth failure, transport
// error) errors instead of guessing.
type Probe struct {
	token  string
	apiURL string
	client *httpclient.Client
}

// getChatMethod is the Bot API method used for validity: getChat
// answers ok for any chat the bot may see, with an error description
// (chat not found / bot was kicked) otherwise.
const getChatMethod = "getChat"

// NewProbe builds the telegram probe from the provider configuration map
// (same `token`/`api_url` shape as the sender; chat_id is irrelevant
// here — a probe verifies any chat the events hand it).
func NewProbe(config map[string]interface{}) (*Probe, error) {
	token, _ := config["token"].(string)
	if token == "" {
		return nil, fmt.Errorf("telegram: probe needs token")
	}
	apiURL, _ := config["api_url"].(string)
	if apiURL == "" {
		apiURL = defaultAPIBase
	}
	return &Probe{token: token, apiURL: apiURL, client: httpclient.NewClient(nil)}, nil
}

// Channel names the surface vocabulary this probe verifies.
func (p *Probe) Channel() string { return "telegram" }

// getChatResponse is the Bot API envelope for getChat.
type getChatResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
}

// Valid reports whether Telegram still recognizes chatID.
func (p *Probe) Valid(ctx context.Context, chatID string) (bool, error) {
	if chatID == "" {
		return false, fmt.Errorf("telegram: probe needs a chat id")
	}
	url := fmt.Sprintf("%s/bot%s/%s?chat_id=%s", p.apiURL, p.token, getChatMethod, chatID)
	resp, err := p.client.Get(ctx, url)

	// The Bot API answers "chat not found / bot was blocked" with HTTP
	// 400/403 carrying a well-formed ok:false envelope — that is the
	// platform's definite no, not a probe failure. Everything else that
	// went wrong errors so the reconciler skips rather than guesses:
	// transport failures come back as err, every non-2xx status comes
	// back as err too (httpclient's contract), and an unparseable 200
	// fails the unmarshal below.
	if resp != nil && (resp.StatusCode == 400 || resp.StatusCode == 403) {
		var refused getChatResponse
		if jerr := json.Unmarshal(resp.Body, &refused); jerr == nil && !refused.OK {
			return false, nil
		}
	}
	if err != nil {
		return false, fmt.Errorf("telegram: getChat: %w", err)
	}
	var parsed getChatResponse
	if err := json.Unmarshal(resp.Body, &parsed); err != nil {
		return false, fmt.Errorf("telegram: getChat response: %w", err)
	}
	return parsed.OK, nil
}
