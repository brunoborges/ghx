package metrics

import (
	"reflect"
	"strings"
	"testing"
)

func TestRedactArgs(t *testing.T) {
	const tok = "ghp_abcdefghijklmnopqrstuvwxyz0123"
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"plain command untouched",
			[]string{"pr", "view", "42", "--json", "title", "--jq", ".title"},
			[]string{"pr", "view", "42", "--json", "title", "--jq", ".title"}},
		{"header value",
			[]string{"api", "user", "-H", "Authorization: token " + tok},
			[]string{"api", "user", "-H", "Authorization: [REDACTED]"}},
		{"attached headers",
			[]string{"api", "--header=X-Key: abc", "-HAuthorization:x"},
			[]string{"api", "--header=X-Key: [REDACTED]", "-HAuthorization: [REDACTED]"}},
		{"header without colon",
			[]string{"api", "-H", "garbage"},
			[]string{"api", "-H", "[REDACTED]"}},
		{"benign fields kept",
			[]string{"api", "search/issues", "-X", "GET", "-f", "q=repo:o/r is:open", "-F", "per_page=100"},
			[]string{"api", "search/issues", "-X", "GET", "-f", "q=repo:o/r is:open", "-F", "per_page=100"}},
		{"sensitive and file fields",
			[]string{"api", "-f", "client_secret=s3", "--field=api_key=k", "-Fbody=@notes.md"},
			[]string{"api", "-f", "client_secret=[REDACTED]", "--field=api_key=[REDACTED]", "-Fbody=[REDACTED]"}},
		{"bodies",
			[]string{"pr", "create", "-b", "private text", "--body=more", "--body-file", "/tmp/x"},
			[]string{"pr", "create", "-b", "[REDACTED]", "--body=[REDACTED]", "--body-file", "[REDACTED]"}},
		{"stray tokens",
			[]string{"api", "user?access_token=" + tok, "github_pat_11ABCDEFG0123456789_abcdef", "Bearer abc.def"},
			[]string{"api", "user?access_token=[REDACTED]", "[REDACTED]", "Bearer [REDACTED]"}},
		{"trailing value flag", []string{"api", "-H"}, []string{"api", "-H"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactArgs(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("RedactArgs(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
			for _, a := range got {
				if strings.Contains(a, tok) {
					t.Errorf("token leaked: %q", got)
				}
			}
		})
	}
	if RedactArgs(nil) != nil {
		t.Error("nil args should stay nil")
	}
}

func TestRecordDetailedKeepsArgs(t *testing.T) {
	s := New()
	s.Record("pr_view", "k1", ResultMiss, 1)
	s.RecordDetailed("pr_view", "k2", ResultHit, 1, []string{"pr", "view", "1"}, []string{"pr", "view", "1", "--json", "a"})
	log := s.Log(0)
	if len(log) != 2 || log[0].Args != nil || strings.Join(log[1].Args, " ") != "pr view 1" || len(log[1].Fetch) != 5 {
		t.Fatalf("log = %+v", log)
	}
}
