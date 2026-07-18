package context

import (
	"strings"
	"testing"
)

func TestCacheKeyIsolatesAuthIdentity(t *testing.T) {
	args := []string{"api", "user"}
	first := CacheKey(ExecContext{
		Host:      "github.com",
		Repo:      "owner/repo",
		Branch:    "main",
		TokenHash: tokenHash("account-one-token"),
	}, args)
	second := CacheKey(ExecContext{
		Host:      "github.com",
		Repo:      "owner/repo",
		Branch:    "main",
		TokenHash: tokenHash("account-two-token"),
	}, args)

	if first == second {
		t.Fatal("cache keys for different auth identities must differ")
	}
}

func TestTokenHashIsFullSHA256Fingerprint(t *testing.T) {
	got := tokenHash("secret-token")
	if len(got) != 64 {
		t.Fatalf("tokenHash() length = %d, want 64", len(got))
	}
	if strings.Contains(got, "secret-token") {
		t.Fatal("tokenHash() exposed the raw token")
	}
	if got != tokenHash("secret-token") {
		t.Fatal("tokenHash() is not deterministic")
	}
}
