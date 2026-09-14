package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brunoborges/ghx/src/internal/allowlist"
	"github.com/brunoborges/ghx/src/internal/authenv"
	"github.com/brunoborges/ghx/src/internal/cache"
	"github.com/brunoborges/ghx/src/internal/config"
	execctx "github.com/brunoborges/ghx/src/internal/context"
	"github.com/brunoborges/ghx/src/internal/executor"
	"github.com/brunoborges/ghx/src/internal/metrics"
	"github.com/brunoborges/ghx/src/internal/protocol"
)

func TestSanitizeCmdKey(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "two args", args: []string{"pr", "list"}, want: "pr_list"},
		{name: "many args", args: []string{"api", "-H", "Authorization: token secret", "/repos"}, want: "api_-H"},
		{name: "single arg", args: []string{"auth"}, want: "auth"},
		{name: "empty args", args: nil, want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeCmdKey(tt.args)
			if got != tt.want {
				t.Errorf("sanitizeCmdKey(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestHandler_GHPath_AtomicAccess(t *testing.T) {
	cfg := &config.Config{GHPath: "/usr/bin/gh"}
	c := cache.New(100)
	cl := allowlist.NewClassifier(nil)
	s := metrics.New()

	h := NewHandler(cfg, c, cl, s)

	// Initial value from config
	if got := h.GHPath(); got != "/usr/bin/gh" {
		t.Errorf("GHPath() = %q, want %q", got, "/usr/bin/gh")
	}

	// Update via SetGHPath
	h.SetGHPath("/opt/homebrew/bin/gh")
	if got := h.GHPath(); got != "/opt/homebrew/bin/gh" {
		t.Errorf("GHPath() after set = %q, want %q", got, "/opt/homebrew/bin/gh")
	}

	// Concurrent reads and writes — verifies no data race under -race
	paths := []string{
		"/usr/local/bin/gh",
		"/opt/homebrew/bin/gh",
		"/home/user/.ghx/bin/gh",
		"/snap/bin/gh",
	}

	const goroutines = 20
	const iterations = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			if id%2 == 0 {
				// Writer
				for j := range iterations {
					h.SetGHPath(paths[j%len(paths)])
				}
			} else {
				// Reader
				for range iterations {
					got := h.GHPath()
					// Value must always be a valid path we've set
					valid := false
					for _, p := range paths {
						if got == p {
							valid = true
							break
						}
					}
					if !valid && got != "/opt/homebrew/bin/gh" {
						t.Errorf("GHPath() returned unexpected value: %q", got)
						return
					}
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestHandler_NoCacheUsesEachClientsAuthEnvironment(t *testing.T) {
	h := newTestHandler()
	h.execute = func(_ context.Context, _ string, _ []string, _ string, env authenv.Environment) *executor.Result {
		login := "configured-account"
		if token := env["GH_TOKEN"]; token != "" {
			login = token
		}
		return &executor.Result{Stdout: []byte(login)}
	}

	tests := []struct {
		name string
		env  authenv.Environment
		want string
	}{
		{name: "first token", env: authenv.Environment{"GH_TOKEN": "account-one"}, want: "account-one"},
		{name: "second token", env: authenv.Environment{"GH_TOKEN": "account-two"}, want: "account-two"},
		{name: "configured account without token", want: "configured-account"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.Handle(&protocol.Request{
				Type:    protocol.TypeExec,
				Args:    []string{"api", "user"},
				AuthEnv: tt.env,
				NoCache: true,
			})
			if got := string(resp.Stdout); got != tt.want {
				t.Fatalf("stdout = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHandler_CacheKeysIsolateAuthIdentity(t *testing.T) {
	h := newTestHandler()
	var calls int
	h.execute = func(_ context.Context, _ string, _ []string, _ string, env authenv.Environment) *executor.Result {
		calls++
		return &executor.Result{Stdout: []byte(env["GH_TOKEN"])}
	}

	request := func(token, tokenHash string) *protocol.Response {
		return h.Handle(&protocol.Request{
			Type:    protocol.TypeExec,
			Args:    []string{"api", "user"},
			AuthEnv: authenv.Environment{"GH_TOKEN": token},
			Context: execctx.ExecContext{Host: "github.com", TokenHash: tokenHash},
		})
	}

	first := request("account-one", "hash-one")
	second := request("account-two", "hash-two")
	firstAgain := request("account-one", "hash-one")

	if got := string(first.Stdout); got != "account-one" {
		t.Fatalf("first stdout = %q", got)
	}
	if got := string(second.Stdout); got != "account-two" {
		t.Fatalf("second stdout = %q", got)
	}
	if got := string(firstAgain.Stdout); got != "account-one" || !firstAgain.Cached {
		t.Fatalf("repeated first response = %q, cached = %v", got, firstAgain.Cached)
	}
	if calls != 2 {
		t.Fatalf("executions = %d, want 2 isolated identities", calls)
	}
}

func TestHandler_SingleflightIsolatesAuthIdentity(t *testing.T) {
	h := newTestHandler()
	started := make(chan string, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	h.execute = func(_ context.Context, _ string, _ []string, _ string, env authenv.Environment) *executor.Result {
		calls.Add(1)
		token := env["GH_TOKEN"]
		started <- token
		<-release
		return &executor.Result{Stdout: []byte(token)}
	}

	requests := []*protocol.Request{
		{
			Type:    protocol.TypeExec,
			Args:    []string{"api", "user"},
			AuthEnv: authenv.Environment{"GH_TOKEN": "account-one"},
			Context: execctx.ExecContext{Host: "github.com", TokenHash: "hash-one"},
		},
		{
			Type:    protocol.TypeExec,
			Args:    []string{"api", "user"},
			AuthEnv: authenv.Environment{"GH_TOKEN": "account-two"},
			Context: execctx.ExecContext{Host: "github.com", TokenHash: "hash-two"},
		},
	}

	var wg sync.WaitGroup
	responses := make([]*protocol.Response, len(requests))
	for i, req := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses[i] = h.Handle(req)
		}()
	}

	seen := make(map[string]bool)
	for range requests {
		select {
		case token := <-started:
			seen[token] = true
		case <-time.After(time.Second):
			close(release)
			wg.Wait()
			t.Fatal("requests with different auth identities were coalesced")
		}
	}
	close(release)
	wg.Wait()

	if !seen["account-one"] || !seen["account-two"] {
		t.Fatalf("executed identities = %v", seen)
	}
	if calls.Load() != 2 {
		t.Fatalf("executions = %d, want 2", calls.Load())
	}
	for i, want := range []string{"account-one", "account-two"} {
		if got := string(responses[i].Stdout); got != want {
			t.Fatalf("response %d = %q, want %q", i, got, want)
		}
	}
}

func newTestHandler() *Handler {
	cfg := &config.Config{GHPath: "gh", MaxCacheEntries: 100, TTL: 30 * time.Second}
	return NewHandler(cfg, cache.New(100), allowlist.NewClassifier(nil), metrics.New())
}

func TestHandlerNoCacheMutationInvalidates(t *testing.T) {
	for _, noCache := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "no-cache"}[noCache], func(t *testing.T) {
			h := newTestHandler()
			value := "before"
			h.execute = func(_ context.Context, _ string, args []string, _ string, _ authenv.Environment) *executor.Result {
				if args[1] == "edit" {
					value = "after"
				}
				return &executor.Result{Stdout: []byte(value)}
			}
			ctx := execctx.ExecContext{Host: "github.com", Repo: "owner/repo"}
			read := &protocol.Request{Type: protocol.TypeExec, Args: []string{"pr", "view", "1"}, Context: ctx}
			h.Handle(read)
			if !h.Handle(read).Cached {
				t.Fatal("read did not warm the cache")
			}
			h.Handle(&protocol.Request{Type: protocol.TypeExec, Args: []string{"pr", "edit", "1"}, Context: ctx, NoCache: noCache})
			got := h.Handle(read)
			if got.Cached || string(got.Stdout) != "after" {
				t.Fatalf("read after mutation = %+v", got)
			}
		})
	}
}

func TestHandlerAPIMutationsAreNeverCached(t *testing.T) {
	h := newTestHandler()
	calls := 0
	h.execute = func(_ context.Context, _ string, _ []string, _ string, _ authenv.Environment) *executor.Result {
		calls++
		return &executor.Result{}
	}
	req := &protocol.Request{Type: protocol.TypeExec, Args: []string{"api", "repos/owner/repo/issues", "-f", "title=hello"}}
	h.Handle(req)
	h.Handle(req)
	if calls != 2 || h.cache.Size() != 0 {
		t.Fatalf("calls = %d, cache size = %d", calls, h.cache.Size())
	}
}

func TestHandlerScopedFlush(t *testing.T) {
	h := newTestHandler()
	for _, entry := range []cache.Entry{
		{Key: "target", Host: "github.com", Repo: "owner/repo", Resource: allowlist.ResourcePR},
		{Key: "other resource", Host: "github.com", Repo: "owner/repo", Resource: allowlist.ResourceIssue},
		{Key: "other repo", Host: "github.com", Repo: "owner/other", Resource: allowlist.ResourcePR},
	} {
		entry.TTL, entry.CachedAt = time.Minute, time.Now()
		h.cache.Set(&entry)
	}
	h.Handle(&protocol.Request{Type: protocol.TypeFlush, Resource: allowlist.ResourcePR,
		Context: execctx.ExecContext{Host: "github.com", Repo: "owner/repo"}})
	if h.cache.Get("target") != nil || h.cache.Size() != 2 {
		t.Fatalf("scoped flush left keys %v", h.cache.Keys())
	}
	h.Handle(&protocol.Request{Type: protocol.TypeFlush})
	if h.cache.Size() != 0 {
		t.Fatal("legacy/global flush did not clear cache")
	}
}
