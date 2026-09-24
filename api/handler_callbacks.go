package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ackSourceCard marks acknowledgements arriving through a provider card
// button rather than the HTTP API.
const ackSourceCard = "feishu_card"

// HandleFeishuCallback receives Feishu interactive-card actions
// (POST /api/v1/callbacks/feishu): the acknowledge button on a card
// acknowledges its alert through the same identity and stores the ack API
// uses. The endpoint is deliberately not behind the API-token middleware —
// it is called by Feishu's servers, which authenticate with the callback
// encryption key instead.
//
// It answers Feishu's URL verification challenge and accepts both the
// encrypted (encrypt field, AES-256-CBC under SHA-256 of the encrypt key)
// and the plaintext callback body.
func (h *Handler) HandleFeishuCallback(w http.ResponseWriter, r *http.Request) {
	if h.ackStore == nil {
		h.respondError(w, http.StatusServiceUnavailable, "ack store is not configured")
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

	var payload struct {
		// URL verification carries the challenge directly.
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		// Encrypted callbacks carry the whole event under encrypt.
		Encrypt string `json:"encrypt"`
		// Plaintext V2 callbacks carry schema/header/event; V1 carries the
		// action on the top level.
		Schema string         `json:"schema"`
		Event  *feishuCardAct `json:"event"`
		Action *feishuCardBtn `json:"action"`
		OpenID string         `json:"open_id"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		h.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// One-time URL verification when the callback address is configured in
	// the Feishu console.
	if payload.Type == "url_verification" {
		h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: map[string]string{"challenge": payload.Challenge}})
		return
	}

	if payload.Encrypt != "" {
		if h.cardEncryptKey == "" {
			h.respondError(w, http.StatusNotImplemented, "callback encryption key is not configured")
			return
		}
		plain, err := decryptFeishuCallback(h.cardEncryptKey, payload.Encrypt)
		if err != nil {
			h.respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		payload.Encrypt = ""
		if err := json.Unmarshal(plain, &payload); err != nil {
			h.respondError(w, http.StatusBadRequest, "invalid decrypted payload")
			return
		}
	}

	alertID, ackedBy := extractFeishuCardAck(payload.Schema, payload.Event, payload.Action, payload.OpenID)
	if alertID == "" {
		h.respondError(w, http.StatusBadRequest, "card action carries no alert_id")
		return
	}

	// The same pairing the ack API performs: first record wins, the ack
	// cancels pending upgrades, and the incident ledger is marked.
	rec, err := h.ackStore.Ack(r.Context(), alertID, ackedBy, ackSourceCard)
	if err != nil {
		h.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if h.escalation != nil {
		_ = h.escalation.Cancel(r.Context(), alertID)
	}
	if h.incidents != nil {
		h.incidents.Ack(alertID, ackedBy)
	}
	h.respondJSON(w, &Response{Code: 0, Message: "ok", Data: map[string]any{
		"alert_id":     alertID,
		"acknowledged": rec != nil,
		"acked_by":     ackedBy,
	}})
}

// feishuCardAct is the V2 (schema 2.0) card action event.
type feishuCardAct struct {
	Operator *struct {
		OpenID string `json:"open_id"`
	} `json:"operator"`
	Action *feishuCardBtn `json:"action"`
}

// feishuCardBtn is the button the user pressed; Value is the button's
// custom payload (the alert_id the card was rendered with).
type feishuCardBtn struct {
	Value map[string]any `json:"value"`
}

// extractFeishuCardAck pulls the acknowledgement identity and the operator
// out of a V2 or V1 card action payload.
func extractFeishuCardAck(schema string, event *feishuCardAct, action *feishuCardBtn, openID string) (alertID, ackedBy string) {
	var value map[string]any
	if event != nil && event.Action != nil {
		value = event.Action.Value
		if event.Operator != nil {
			ackedBy = event.Operator.OpenID
		}
	} else if action != nil {
		value = action.Value
		ackedBy = openID
	}
	if v, ok := value["alert_id"].(string); ok {
		alertID = v
	}
	if ackedBy == "" {
		ackedBy = "feishu-user"
	}
	return alertID, ackedBy
}

// decryptFeishuCallback decrypts the encrypt field of an encrypted Feishu
// callback: the AES key is SHA-256 of the configured encrypt key, the IV is
// the key's first block, and the plaintext is PKCS7-padded JSON.
func decryptFeishuCallback(encryptKey, encoded string) ([]byte, error) {
	key := sha256.Sum256([]byte(encryptKey))
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("ciphertext is not base64")
	}
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext has invalid length")
	}
	// aes.NewCipher only fails on key lengths other than 16/24/32 bytes;
	// the key here is always a SHA-256 digest (32 bytes), so the error
	// return exists only to satisfy the API.
	block, _ := aes.NewCipher(key[:])
	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, key[:aes.BlockSize]).CryptBlocks(plain, ciphertext)
	plain, err = pkcs7Unpad(plain, aes.BlockSize)
	if err != nil {
		return nil, err
	}
	return plain, nil
}

// pkcs7Unpad removes the padding of a PKCS7-padded block. The length
// guard below is unreachable from decryptFeishuCallback — that caller
// already rejects empty or non-block-aligned ciphertexts, and CBC output
// preserves input length — but the check keeps this helper self-contained
// should it ever gain another caller.
func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("invalid padded length")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > blockSize || pad > len(data) {
		return nil, fmt.Errorf("invalid padding")
	}
	for _, b := range data[len(data)-pad:] {
		if int(b) != pad {
			return nil, fmt.Errorf("invalid padding")
		}
	}
	return data[:len(data)-pad], nil
}
