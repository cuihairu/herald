package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubKeyHashes 替代 store.DB 的 ActiveKeyHashes 面。
type stubKeyHashes struct {
	hashes map[string]bool
	err    error
}

func (s *stubKeyHashes) ActiveKeyHashes() (map[string]bool, error) {
	return s.hashes, s.err
}

// mintedHash is the sha-256 hex the store persists for a plain key.
func mintedHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// runAuthed 在启用 auth 的中间件上以 X-API-Key 发起一次请求，返回响应码
// 与 next 是否被调用。
func runAuthed(t *testing.T, a *Auth, key string) (int, bool) {
	t.Helper()
	called := false
	handler := a.Middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w.Code, called
}

func TestMiddlewareAcceptsStoreMintedKeys(t *testing.T) {
	a := New(&Config{Enabled: true, APIKeys: map[string]string{"static-key": "bootstrap"}})
	minted := "wm_mintedkey123"
	a.SetAPIKeyHashes(&stubKeyHashes{hashes: map[string]bool{mintedHash(minted): true}})

	if code, called := runAuthed(t, a, minted); code != http.StatusOK || !called {
		t.Fatalf("expected the minted key to authenticate, got code=%d called=%v", code, called)
	}
	// config 静态 key 在同一实例上仍然有效。
	if code, called := runAuthed(t, a, "static-key"); code != http.StatusOK || !called {
		t.Fatalf("expected the static key to keep working, got code=%d called=%v", code, called)
	}
}

func TestMiddlewareRejectsUnknownAndRevokedStoreKeys(t *testing.T) {
	a := New(&Config{Enabled: true})
	minted := "wm_revokedkey"
	a.SetAPIKeyHashes(&stubKeyHashes{hashes: map[string]bool{mintedHash(minted): true}})

	if code, called := runAuthed(t, a, "wm_unknown"); code != http.StatusUnauthorized || called {
		t.Fatalf("expected 401 for an unknown key, got code=%d called=%v", code, called)
	}
	// 吊销 = hash 从活跃集合里消失。
	a.SetAPIKeyHashes(&stubKeyHashes{hashes: map[string]bool{}})
	if code, called := runAuthed(t, a, minted); code != http.StatusUnauthorized || called {
		t.Fatalf("expected 401 after revocation, got code=%d called=%v", code, called)
	}
}

func TestMiddlewareFailsClosedOnKeySourceError(t *testing.T) {
	a := New(&Config{Enabled: true})
	a.SetAPIKeyHashes(&stubKeyHashes{err: errBoom{}})

	if code, called := runAuthed(t, a, "wm_anything"); code != http.StatusUnauthorized || called {
		t.Fatalf("expected 401 when the key source fails, got code=%d called=%v", code, called)
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "store down" }
