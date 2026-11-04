package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cuihairu/herald/internal/store"
)

func newKeysHandler(t *testing.T) (*Handler, *store.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "herald.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	h := NewHandler(nil, nil, nil)
	h.SetKeyStore(db)
	return h, db
}

func TestHandleKeysWithoutStoreIs503(t *testing.T) {
	h := NewHandler(nil, nil, nil)
	for _, tc := range []struct {
		method, path string
		body         string
	}{
		{"GET", "/api/v1/keys", ""},
		{"POST", "/api/v1/keys", `{"name":"x"}`},
		{"DELETE", "/api/v1/keys/abc", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		if tc.path == "/api/v1/keys" {
			h.HandleKeys(w, req)
		} else {
			h.HandleKeyByID(w, req)
		}
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s without store: expected 503, got %d", tc.method, w.Code)
		}
	}
}

func TestHandleKeysMethodNotAllowed(t *testing.T) {
	h, _ := newKeysHandler(t)
	req := httptest.NewRequest("PUT", "/api/v1/keys", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.HandleKeys(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/v1/keys/abc", nil)
	w = httptest.NewRecorder()
	req.SetPathValue("id", "abc")
	h.HandleKeyByID(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 on by-id GET, got %d", w.Code)
	}
}

func TestCreateKeyReturnsPlainOnceAndListsMasked(t *testing.T) {
	h, _ := newKeysHandler(t)

	req := httptest.NewRequest("POST", "/api/v1/keys", strings.NewReader(`{"name":"CI 发布机"}`))
	w := httptest.NewRecorder()
	h.HandleKeys(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, _ := resp.Data.(map[string]interface{})
	if data["key"] == "" || !strings.HasPrefix(data["key"].(string), "wm_") {
		t.Fatalf("expected a plain wm_ key in the create response, got %v", data)
	}
	info, _ := data["info"].(map[string]interface{})
	if info["prefix"] == data["key"] {
		t.Fatal("the masked prefix must not equal the plain key")
	}
	if info["name"] != "CI 发布机" || info["revoked"] != false {
		t.Fatalf("unexpected info: %v", info)
	}

	// 列表只有掩码记录，不含明文。
	req = httptest.NewRequest("GET", "/api/v1/keys", nil)
	w = httptest.NewRecorder()
	h.HandleKeys(w, req)
	body := w.Body.String()
	if strings.Contains(body, data["key"].(string)) {
		t.Fatal("the plain key must not appear in the list")
	}
	if !strings.Contains(body, info["prefix"].(string)) {
		t.Fatalf("expected the masked prefix in the list: %s", body)
	}
	if !strings.Contains(body, `"count":1`) {
		t.Fatalf("expected count 1: %s", body)
	}
}

func TestCreateKeyRejectsBadRequests(t *testing.T) {
	h, _ := newKeysHandler(t)
	for _, body := range []string{"not json", `{"name":""}` + "", `{}`} {
		req := httptest.NewRequest("POST", "/api/v1/keys", strings.NewReader(body))
		w := httptest.NewRecorder()
		h.HandleKeys(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body %q: expected 400, got %d", body, w.Code)
		}
	}
}

func TestRevokeKeyRemovesItFromAuth(t *testing.T) {
	h, db := newKeysHandler(t)
	created, err := db.CreateAPIKey("ops")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("DELETE", "/api/v1/keys/"+created.Info.ID, nil)
	w := httptest.NewRecorder()
	req.SetPathValue("id", created.Info.ID)
	h.HandleKeyByID(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	keys, _ := db.ListAPIKeys()
	if !keys[0].Revoked {
		t.Fatal("expected the key to be revoked")
	}

	// 未知 id 与空 id。
	req = httptest.NewRequest("DELETE", "/api/v1/keys/missing", nil)
	w = httptest.NewRecorder()
	req.SetPathValue("id", "missing")
	h.HandleKeyByID(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown id, got %d", w.Code)
	}
	req = httptest.NewRequest("DELETE", "/api/v1/keys/", nil)
	w = httptest.NewRecorder()
	req.SetPathValue("id", "")
	h.HandleKeyByID(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an empty id, got %d", w.Code)
	}
}

func TestHandleKeysReportsStoreFailureAs500(t *testing.T) {
	h, db := newKeysHandler(t)
	_ = db.Close()

	req := httptest.NewRequest("GET", "/api/v1/keys", nil)
	w := httptest.NewRecorder()
	h.HandleKeys(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 listing on a closed store, got %d", w.Code)
	}

	req = httptest.NewRequest("POST", "/api/v1/keys", strings.NewReader(`{"name":"x"}`))
	w = httptest.NewRecorder()
	h.HandleKeys(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 creating on a closed store, got %d", w.Code)
	}

	req = httptest.NewRequest("DELETE", "/api/v1/keys/abc", nil)
	w = httptest.NewRecorder()
	req.SetPathValue("id", "abc")
	h.HandleKeyByID(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 revoking on a closed store, got %d", w.Code)
	}
}
