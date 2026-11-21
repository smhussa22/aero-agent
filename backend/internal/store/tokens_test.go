package store

import "testing"

func TestHashTokenStableAndDistinct(t *testing.T) {
	if hashToken("a") != hashToken("a") || hashToken("a") == hashToken("b") {
		t.Fatal("hashToken must be deterministic and collision-free for distinct inputs")
	}
	if len(hashToken("a")) != 64 {
		t.Fatalf("want 64 hex chars, got %d", len(hashToken("a")))
	}
}
