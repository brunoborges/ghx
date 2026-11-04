package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/brunoborges/ghx/src/internal/authenv"
	"github.com/brunoborges/ghx/src/internal/executor"
	"github.com/brunoborges/ghx/src/internal/metrics"
	"github.com/brunoborges/ghx/src/internal/protocol"
)

func TestHandlerAggregateMissReasonsWithoutArgumentLogging(t *testing.T) {
	h := newTestHandler()
	h.execute = func(context.Context, string, []string, string, authenv.Environment) *executor.Result {
		return &executor.Result{Stdout: []byte(`{"number":7}`)}
	}
	req := &protocol.Request{Type: protocol.TypeExec, Args: []string{"pr", "view", "7", "--repo", "o/private-repo", "--json", "number"}}
	req.Context.Host = "github.com"
	req.Context.Repo = "o/private-repo"
	h.Handle(req) // absent
	h.Handle(req) // hit
	req.NoCache = true
	h.Handle(req) // deliberate bypass, even with a cached value
	req.NoCache = false
	key := h.cache.Keys()[0]
	expired := *h.cache.Get(key)
	expired.CachedAt = time.Now().Add(-time.Minute)
	expired.TTL = time.Second
	h.cache.Set(&expired)
	h.Handle(req) // expired
	mutation := *req
	mutation.Args = []string{"pr", "close", "7", "--repo", "o/private-repo"}
	h.Handle(&mutation) // fake executor only; mutation invalidates the prior read
	h.Handle(req)       // absent, not asserted to be cold
	snap := h.stats.Snapshot(h.cache.Size(), 100)
	want := metrics.MissReasons{Absent: 2, Expired: 1, Bypass: 1}
	if snap.MissReasons != want || snap.Misses != 4 || snap.Hits != 1 || snap.Passthrough != 1 {
		t.Fatalf("snapshot = %+v", snap)
	}
	if h.cache.Usage().RemovalReasons.Invalidated != 1 {
		t.Fatal("mutation invalidation must remain enabled")
	}
	data, err := json.Marshal(h.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-repo") || strings.Contains(string(data), `"args"`) {
		t.Fatal("aggregate diagnostics must not expose arguments or repository names")
	}
}
