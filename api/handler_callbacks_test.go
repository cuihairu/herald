package api

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/cuihairu/herald/core/ack"
	"github.com/cuihairu/herald/core/escalation"
	"github.com/cuihairu/herald/core/incident"
)

const testEncryptKey = "herald-test-encrypt-key"

// feishuSeal encrypts a payload the way Feishu encrypts callbacks: the AES
// key is SHA-256 of the encrypt key, the IV its first block, PKCS7 padding.
func feishuSeal(t *testing.T, key string, payload any) string {
	t.Helper()
	k := sha256.Sum256([]byte(key))
	plain, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(plain, bytes.Repeat([]byte{byte(pad)}, pad)...)
	block, err := aes.NewCipher(k[:])
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, k[:aes.BlockSize]).CryptBlocks(out, padded)
	return base64.StdEncoding.EncodeToString(out)
}

func TestFeishuCallbackChallenge(t *testing.T) {
	env := newTestEnv(t, withAckStore(ack.NewMemoryStore()))

	code, resp := env.do(t, http.MethodPost, "/api/v1/callbacks/feishu",
		`{"type":"url_verification","challenge":"tok-123"}`, nil)
	if code != http.StatusOK {
		t.Fatalf("challenge failed: %d", code)
	}
	if got := dataOf(t, resp)["challenge"]; got != "tok-123" {
		t.Fatalf("challenge must be echoed, got %v", got)
	}
}

func TestFeishuCallbackPlaintextAck(t *testing.T) {
	ledger := incident.New(0)
	ledger.Open(incident.Opening{RuleID: "r-up", GroupKey: "g1", AlertID: "inc-7", Title: "one"})
	store := ack.NewMemoryStore()
	env := newTestEnv(t, withAckStore(store), withIncidentStore(ledger))

	body := `{
		"schema": "2.0",
		"header": {"event_type": "card.action.trigger"},
		"event": {
			"operator": {"open_id": "ou_alice"},
			"action": {"tag": "button", "value": {"alert_id": "inc-7"}}
		}
	}`
	if code, resp := env.do(t, http.MethodPost, "/api/v1/callbacks/feishu", body, nil); code != http.StatusOK {
		t.Fatalf("callback failed: %d (%v)", code, resp)
	}

	rec, err := store.Get(t.Context(), "inc-7")
	if err != nil || rec == nil {
		t.Fatalf("the button must ack the alert, got %v / %v", rec, err)
	}
	if rec.AckedBy != "ou_alice" || rec.Source != "feishu_card" {
		t.Fatalf("unexpected ack record: %+v", rec)
	}
	inc := ledger.List(&incident.Filter{AlertID: "inc-7"})[0]
	if inc.Status() != incident.StatusAcked || inc.AckedBy != "ou_alice" {
		t.Fatalf("ack must land on the ledger, got %+v", inc)
	}
}

func TestFeishuCallbackPlaintextCancelsUpgrade(t *testing.T) {
	path := t.TempDir() + "/pendings.json"
	store := ack.NewMemoryStore()
	esc := escalation.NewManager(store, nil, path)
	env := newTestEnv(t, withAckStore(store), func(c *Config) { c.Escalation = esc })

	if err := esc.Schedule(t.Context(), escalation.Pending{
		RuleID: "r-up", AlertID: "inc-9", To: []string{"phone"},
		Timeout: time.Minute, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	body := `{"schema":"2.0","event":{"operator":{"open_id":"ou_b"},"action":{"value":{"alert_id":"inc-9"}}}}`
	if code, _ := env.do(t, http.MethodPost, "/api/v1/callbacks/feishu", body, nil); code != http.StatusOK {
		t.Fatalf("callback failed: %d", code)
	}
	// The ack cancelled the pending upgrade: the persisted table is empty.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pendings: %v", err)
	}
	var f struct {
		Pendings []escalation.Pending `json:"pendings"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse pendings: %v", err)
	}
	if len(f.Pendings) != 0 {
		t.Fatalf("the button ack must cancel the pending upgrade, got %+v", f.Pendings)
	}
}

func TestFeishuCallbackEncryptedAck(t *testing.T) {
	store := ack.NewMemoryStore()
	env := newTestEnv(t, withAckStore(store), func(c *Config) { c.CardCallbackEncryptKey = testEncryptKey })

	inner := map[string]any{
		"schema": "2.0",
		"event": map[string]any{
			"operator": map[string]any{"open_id": "ou_enc"},
			"action":   map[string]any{"value": map[string]any{"alert_id": "inc-enc"}},
		},
	}
	body, err := json.Marshal(map[string]any{"encrypt": feishuSeal(t, testEncryptKey, inner)})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	if code, _ := env.do(t, http.MethodPost, "/api/v1/callbacks/feishu", string(body), nil); code != http.StatusOK {
		t.Fatalf("encrypted callback failed: %d", code)
	}
	rec, _ := store.Get(t.Context(), "inc-enc")
	if rec == nil || rec.AckedBy != "ou_enc" {
		t.Fatalf("the decrypted action must ack the alert, got %+v", rec)
	}
}

func TestFeishuCallbackEncryptedWithoutKey(t *testing.T) {
	store := ack.NewMemoryStore()
	env := newTestEnv(t, withAckStore(store))

	body := `{"encrypt":"AAAA"}`
	if code, _ := env.do(t, http.MethodPost, "/api/v1/callbacks/feishu", body, nil); code != http.StatusNotImplemented {
		t.Fatalf("encrypted callback without a key must be 501, got %d", code)
	}
}

func TestFeishuCallbackV1Fallback(t *testing.T) {
	store := ack.NewMemoryStore()
	env := newTestEnv(t, withAckStore(store))

	// Legacy payloads carry the action and operator on the top level.
	body := `{"action":{"value":{"alert_id":"inc-v1"}},"open_id":"ou_old"}`
	if code, _ := env.do(t, http.MethodPost, "/api/v1/callbacks/feishu", body, nil); code != http.StatusOK {
		t.Fatalf("v1 callback failed: %d", code)
	}
	rec, _ := store.Get(t.Context(), "inc-v1")
	if rec == nil || rec.AckedBy != "ou_old" {
		t.Fatalf("v1 action must ack the alert, got %+v", rec)
	}
}

func TestFeishuCallbackNoAlertID(t *testing.T) {
	store := ack.NewMemoryStore()
	env := newTestEnv(t, withAckStore(store))

	if code, _ := env.do(t, http.MethodPost, "/api/v1/callbacks/feishu",
		`{"schema":"2.0","event":{"operator":{"open_id":"ou_x"},"action":{"value":{"other":"v"}}}}`, nil); code != http.StatusBadRequest {
		t.Fatalf("action without alert_id must be 400, got %d", code)
	}
}

func TestFeishuCallbackGuards(t *testing.T) {
	// No ack store configured.
	env := newTestEnv(t)
	if code, _ := env.do(t, http.MethodPost, "/api/v1/callbacks/feishu", `{}`, nil); code != http.StatusServiceUnavailable {
		t.Fatalf("no ack store must be 503, got %d", code)
	}
	// GET is not a callback.
	env = newTestEnv(t, withAckStore(ack.NewMemoryStore()))
	if code, _ := env.do(t, http.MethodGet, "/api/v1/callbacks/feishu", "", nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("GET must be 405, got %d", code)
	}
	// A body that is not JSON.
	if code, _ := env.do(t, http.MethodPost, "/api/v1/callbacks/feishu", `not-json`, nil); code != http.StatusBadRequest {
		t.Fatalf("malformed body must be 400, got %d", code)
	}
}

// failingReader is a request body that errors on read.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestFeishuCallbackBodyReadError(t *testing.T) {
	env := newTestEnv(t, withAckStore(ack.NewMemoryStore()))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/callbacks/feishu", failingReader{})
	rec := httptest.NewRecorder()
	env.server.handler.HandleFeishuCallback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an unreadable body must be 400, got %d", rec.Code)
	}
}

// rawSeal encrypts already-block-aligned plaintext without adding padding,
// letting tests craft deliberately broken PKCS7 trailers.
func rawSeal(t *testing.T, key string, plain []byte) string {
	t.Helper()
	if len(plain) == 0 || len(plain)%aes.BlockSize != 0 {
		t.Fatalf("rawSeal plaintext must be block aligned, got %d bytes", len(plain))
	}
	k := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(k[:])
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, k[:aes.BlockSize]).CryptBlocks(out, plain)
	return base64.StdEncoding.EncodeToString(out)
}

func TestFeishuCallbackRejectsCorruptCiphertext(t *testing.T) {
	env := newTestEnv(t, withAckStore(ack.NewMemoryStore()),
		func(c *Config) { c.CardCallbackEncryptKey = testEncryptKey })

	inner := map[string]any{"schema": "2.0"}
	cases := map[string]string{
		// Not base64 at all.
		"not base64": "not-base64!!",
		// Base64 but not a whole number of blocks.
		"unaligned length": base64.StdEncoding.EncodeToString([]byte("short")),
		// A padding byte of zero.
		"zero padding": rawSeal(t, testEncryptKey, make([]byte, 16)),
		// A padding byte beyond the block size.
		"oversized padding": rawSeal(t, testEncryptKey,
			append(append([]byte(`{"x":1}`), make([]byte, 8)...), 0x11)),
		// Padding bytes that do not match the declared pad count.
		"inconsistent padding": rawSeal(t, testEncryptKey,
			append(append([]byte(`{"x":1}`), make([]byte, 6)...), 0x03, 0x05, 0x03)),
	}
	for name, ct := range cases {
		body, err := json.Marshal(map[string]any{"encrypt": ct})
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		if code, _ := env.do(t, http.MethodPost, "/api/v1/callbacks/feishu", string(body), nil); code != http.StatusBadRequest {
			t.Fatalf("%s: corrupt ciphertext must be 400, got %d", name, code)
		}
	}

	// A body sealed under a different key decrypts to garbage padding.
	encrypted := feishuSeal(t, "some-other-key", inner)
	body, _ := json.Marshal(map[string]any{"encrypt": encrypted})
	if code, _ := env.do(t, http.MethodPost, "/api/v1/callbacks/feishu", string(body), nil); code != http.StatusBadRequest {
		t.Fatalf("wrong-key ciphertext must be 400, got %d", code)
	}
}

func TestFeishuCallbackDecryptedGarbage(t *testing.T) {
	env := newTestEnv(t, withAckStore(ack.NewMemoryStore()),
		func(c *Config) { c.CardCallbackEncryptKey = testEncryptKey })

	// Decrypts cleanly but is not the JSON object the handler expects.
	body, _ := json.Marshal(map[string]any{"encrypt": feishuSeal(t, testEncryptKey, "not-json")})
	if code, _ := env.do(t, http.MethodPost, "/api/v1/callbacks/feishu", string(body), nil); code != http.StatusBadRequest {
		t.Fatalf("decrypted garbage must be 400, got %d", code)
	}
}

// failingAckStore always errors, exercising the handlers' failure paths.
type failingAckStore struct{}

func (failingAckStore) Ack(context.Context, string, string, string) (*ack.Record, error) {
	return nil, errors.New("store down")
}
func (failingAckStore) Get(context.Context, string) (*ack.Record, error) {
	return nil, errors.New("store down")
}
func (failingAckStore) Delete(context.Context, string) error { return nil }
func (failingAckStore) Close() error                         { return nil }

func TestFeishuCallbackAckStoreFailure(t *testing.T) {
	env := newTestEnv(t, withAckStore(failingAckStore{}))

	body := `{"schema":"2.0","event":{"operator":{"open_id":"ou_a"},"action":{"value":{"alert_id":"inc-x"}}}}`
	if code, _ := env.do(t, http.MethodPost, "/api/v1/callbacks/feishu", body, nil); code != http.StatusInternalServerError {
		t.Fatalf("a failing store must be 500, got %d", code)
	}
}

func TestFeishuCallbackAnonymizesMissingOperator(t *testing.T) {
	store := ack.NewMemoryStore()
	env := newTestEnv(t, withAckStore(store))

	// V2 action without an operator block: still ack, with a placeholder.
	body := `{"schema":"2.0","event":{"action":{"value":{"alert_id":"inc-anon"}}}}`
	if code, _ := env.do(t, http.MethodPost, "/api/v1/callbacks/feishu", body, nil); code != http.StatusOK {
		t.Fatalf("callback failed: %d", code)
	}
	rec, _ := store.Get(t.Context(), "inc-anon")
	if rec == nil || rec.AckedBy != "feishu-user" {
		t.Fatalf("missing operator must fall back to a placeholder, got %+v", rec)
	}
}

func TestServerSetCardCallbackKey(t *testing.T) {
	// The daemon wires the key after construction: the server wrapper must
	// arm encrypted callbacks the same way the config field does.
	store := ack.NewMemoryStore()
	env := newTestEnv(t, withAckStore(store))
	env.server.SetCardCallbackKey(testEncryptKey)

	inner := map[string]any{
		"schema": "2.0",
		"event": map[string]any{
			"operator": map[string]any{"open_id": "ou_late"},
			"action":   map[string]any{"value": map[string]any{"alert_id": "inc-late"}},
		},
	}
	body, err := json.Marshal(map[string]any{"encrypt": feishuSeal(t, testEncryptKey, inner)})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	if code, _ := env.do(t, http.MethodPost, "/api/v1/callbacks/feishu", string(body), nil); code != http.StatusOK {
		t.Fatalf("late-wired encrypted callback failed: %d", code)
	}
	rec, _ := store.Get(t.Context(), "inc-late")
	if rec == nil || rec.AckedBy != "ou_late" {
		t.Fatalf("the late-wired key must accept callbacks, got %+v", rec)
	}
}
