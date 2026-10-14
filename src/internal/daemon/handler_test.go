package daemon

import (
	"context"
	"os"
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

func TestHandlerCacheByteBudget(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name     string
		stdout   string
		stderr   string
		exitCode int
		retained bool
	}{
		{name: "exact limit", stdout: "out", stderr: "err", retained: true},
		{name: "oversized stdout", stdout: "toolong", exitCode: 1},
		{name: "oversized stderr", stderr: "toolong", exitCode: 2},
		{name: "combined overflow", stdout: "data", stderr: "err"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.MaxCacheBytes = 6
			s := NewServer(cfg, "test", executable)
			var calls int
			s.handler.execute = func(context.Context, string, []string, string, authenv.Environment) *executor.Result {
				calls++
				return &executor.Result{
					Stdout:   []byte(tt.stdout),
					Stderr:   []byte(tt.stderr),
					ExitCode: tt.exitCode,
				}
			}
			req := &protocol.Request{
				Type:    protocol.TypeExec,
				Args:    []string{"pr", "list"},
				Context: execctx.ExecContext{Host: "github.com", Repo: "o/r"},
			}
			for i := range 2 {
				resp := s.handler.Handle(req)
				if string(resp.Stdout) != tt.stdout || string(resp.Stderr) != tt.stderr ||
					resp.ExitCode != tt.exitCode || resp.Error != "" {
					t.Fatalf("request %d: stdout = %q, stderr = %q, exit code = %d, error = %q; want %q, %q, %d, no error",
						i+1, resp.Stdout, resp.Stderr, resp.ExitCode, resp.Error, tt.stdout, tt.stderr, tt.exitCode)
				}
				if wantCached := tt.retained && i == 1; resp.Cached != wantCached {
					t.Fatalf("request %d: cached = %v, want %v", i+1, resp.Cached, wantCached)
				}
			}
			wantCalls, wantSize := 2, 0
			wantUsage := cache.Usage{MaxBytes: 6, Rejected: 2}
			if tt.retained {
				wantCalls, wantSize = 1, 1
				wantUsage.Bytes = int64(len(tt.stdout) + len(tt.stderr))
				wantUsage.Rejected = 0
			}
			if calls != wantCalls {
				t.Errorf("executions = %d, want %d", calls, wantCalls)
			}
			if s.cache.Size() != wantSize || s.cache.Usage() != wantUsage {
				t.Fatalf("size = %d, usage = %+v; want size %d, usage %+v",
					s.cache.Size(), s.cache.Usage(), wantSize, wantUsage)
			}
		})
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
