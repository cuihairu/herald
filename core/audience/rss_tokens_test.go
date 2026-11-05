package audience

import "testing"

func TestRSSTokensListsMintedTokensOnly(t *testing.T) {
	s := NewSurfaceRegistry()
	if tokens := s.RSSTokens(); len(tokens) != 0 {
		t.Fatalf("expected no tokens before minting, got %v", tokens)
	}

	alice, err := s.RSSToken("alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := s.RSSToken("bob")
	if err != nil {
		t.Fatal(err)
	}
	tokens := s.RSSTokens()
	if len(tokens) != 2 || tokens["alice"] != alice || tokens["bob"] != bob {
		t.Fatalf("unexpected token map: %v (want alice=%q bob=%q)", tokens, alice, bob)
	}

	// 重置后旧 token 不再出现。
	if err := s.ResetRSSToken("alice"); err != nil {
		t.Fatal(err)
	}
	tokens = s.RSSTokens()
	if _, ok := tokens["alice"]; ok {
		t.Fatalf("expected alice's token to be gone after reset, got %v", tokens)
	}
}
