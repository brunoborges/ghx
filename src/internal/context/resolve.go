package context

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ExecContext holds resolved execution context for building cache keys.
type ExecContext struct {
	Host      string `json:"host"`
	Repo      string `json:"repo"`
	Branch    string `json:"branch"`
	TokenHash string `json:"token_hash"`
}

// Resolve gathers execution context from the current working directory and environment.
func Resolve(ghPath string) ExecContext {
	ctx := ExecContext{}

	// Host: GH_HOST env var or default
	if host := os.Getenv("GH_HOST"); host != "" {
		ctx.Host = host
	} else {
		ctx.Host = "github.com"
	}

	// Repo: GH_REPO env var or from git remote
	if repo := os.Getenv("GH_REPO"); repo != "" {
		ctx.Repo = repo
	} else {
		ctx.Repo = resolveRepoFromGit()
	}

	// Branch: current git branch
	ctx.Branch = resolveCurrentBranch()

	// Auth: hash of the current auth token
	ctx.TokenHash = resolveTokenHash(ghPath, ctx.Host)

	return ctx
}

func resolveRepoFromGit() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return parseRepoFromURL(strings.TrimSpace(string(out)))
}

// parseRepoFromURL extracts "owner/repo" from a git remote URL.
func parseRepoFromURL(url string) string {
	// Handle SSH: git@github.com:owner/repo.git
	if strings.HasPrefix(url, "git@") {
		if idx := strings.Index(url, ":"); idx != -1 {
			path := url[idx+1:]
			path = strings.TrimSuffix(path, ".git")
			return path
		}
	}

	// Handle HTTPS: https://github.com/owner/repo.git
	url = strings.TrimSuffix(url, ".git")
	parts := strings.Split(url, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}

	return ""
}

func resolveCurrentBranch() string {
	out, err := exec.Command("git", "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func resolveTokenHash(ghPath, host string) string {
	cmd := exec.Command(ghPath, "auth", "token", "--hostname", host)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return ""
	}
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h[:8]) // first 8 bytes is enough for keying
}

// CacheKey builds a deterministic cache key from the execution context and command args.
func CacheKey(ctx ExecContext, args []string) string {
	h := sha256.New()
	fmt.Fprintf(h, "host=%s\n", ctx.Host)
	fmt.Fprintf(h, "repo=%s\n", ctx.Repo)
	fmt.Fprintf(h, "branch=%s\n", ctx.Branch)
	fmt.Fprintf(h, "token=%s\n", ctx.TokenHash)
	fmt.Fprintf(h, "args=%s\n", strings.Join(args, "\x00"))
	return fmt.Sprintf("%x", h.Sum(nil))
}
