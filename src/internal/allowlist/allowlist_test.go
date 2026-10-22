package allowlist

import "testing"

func TestCacheableCommands(t *testing.T) {
	c := NewClassifier(nil)

	tests := []struct {
		args     []string
		wantType CommandType
		wantRes  ResourceType
	}{
		{[]string{"pr", "list"}, Cacheable, ResourcePR},
		{[]string{"pr", "view", "42"}, Cacheable, ResourcePR},
		{[]string{"pr", "status"}, Cacheable, ResourcePR},
		{[]string{"issue", "list"}, Cacheable, ResourceIssue},
		{[]string{"issue", "view", "1"}, Cacheable, ResourceIssue},
		{[]string{"repo", "view"}, Cacheable, ResourceRepo},
		{[]string{"repo", "list"}, Cacheable, ResourceRepo},
		{[]string{"run", "list"}, Cacheable, ResourceRun},
		{[]string{"release", "list"}, Cacheable, ResourceRelease},
		{[]string{"label", "list"}, Cacheable, ResourceLabel},
		{[]string{"search", "repos", "go"}, Cacheable, ResourceSearch},
		{[]string{"search", "commits", "fix"}, Cacheable, ResourceSearch},
		{[]string{"search", "code", "main"}, Cacheable, ResourceSearch},
		{[]string{"gist", "list"}, Cacheable, ResourceGist},
		{[]string{"gist", "view", "abc123"}, Cacheable, ResourceGist},
		{[]string{"project", "list"}, Cacheable, ResourceProject},
		{[]string{"project", "view", "1"}, Cacheable, ResourceProject},
		{[]string{"project", "field-list"}, Cacheable, ResourceProject},
		{[]string{"project", "item-list", "1"}, Cacheable, ResourceProject},
		{[]string{"cache", "list"}, Cacheable, ResourceCache},
		{[]string{"ruleset", "list"}, Cacheable, ResourceRuleset},
		{[]string{"ruleset", "view", "1"}, Cacheable, ResourceRuleset},
		{[]string{"ruleset", "check"}, Cacheable, ResourceRuleset},
		{[]string{"org", "list"}, Cacheable, ResourceOrg},
		{[]string{"secret", "list"}, Cacheable, ResourceSecret},
		{[]string{"variable", "list"}, Cacheable, ResourceVariable},
		{[]string{"variable", "get", "MY_VAR"}, Cacheable, ResourceVariable},
	}

	for _, tt := range tests {
		cl := c.Classify(tt.args)
		if cl.Type != tt.wantType {
			t.Errorf("Classify(%v): type=%d, want %d", tt.args, cl.Type, tt.wantType)
		}
		if cl.Resource != tt.wantRes {
			t.Errorf("Classify(%v): resource=%s, want %s", tt.args, cl.Resource, tt.wantRes)
		}
	}
}

func TestMutations(t *testing.T) {
	c := NewClassifier(nil)

	tests := []struct {
		args     []string
		wantType CommandType
	}{
		{[]string{"pr", "create"}, Mutation},
		{[]string{"pr", "merge", "42"}, Mutation},
		{[]string{"pr", "close", "42"}, Mutation},
		{[]string{"issue", "create"}, Mutation},
		{[]string{"issue", "edit", "1"}, Mutation},
		{[]string{"issue", "delete", "1"}, Mutation},
		{[]string{"secret", "set", "MY_SECRET"}, Mutation},
		{[]string{"secret", "delete", "MY_SECRET"}, Mutation},
		{[]string{"variable", "set", "MY_VAR"}, Mutation},
		{[]string{"variable", "delete", "MY_VAR"}, Mutation},
		{[]string{"run", "cancel", "123"}, Mutation},
		{[]string{"run", "rerun", "123", "--failed"}, Mutation},
		{[]string{"workflow", "run", "ci.yml"}, Mutation},
		{[]string{"workflow", "enable", "ci.yml"}, Mutation},
		{[]string{"release", "upload", "v1", "a.tgz"}, Mutation},
		{[]string{"repo", "sync"}, Mutation},
		{[]string{"pr", "update-branch", "1"}, Mutation},
		{[]string{"project", "item-add", "1"}, Mutation},
	}

	for _, tt := range tests {
		cl := c.Classify(tt.args)
		if cl.Type != tt.wantType {
			t.Errorf("Classify(%v): type=%d, want %d", tt.args, cl.Type, tt.wantType)
		}
	}
}

func TestPassthrough(t *testing.T) {
	c := NewClassifier(nil)

	tests := [][]string{
		{"auth", "login"},
		{"config", "set"},
		{"codespace", "ssh"},
		{},
	}

	for _, args := range tests {
		cl := c.Classify(args)
		if cl.Type != Passthrough {
			t.Errorf("Classify(%v): type=%d, want Passthrough", args, cl.Type)
		}
	}
}

func TestAPIClassification(t *testing.T) {
	c := NewClassifier(nil)

	// GET (default) → cacheable
	cl := c.Classify([]string{"api", "/repos/cli/cli"})
	if cl.Type != Cacheable {
		t.Errorf("GET api: type=%d, want Cacheable", cl.Type)
	}

	// POST → mutation
	cl = c.Classify([]string{"api", "-X", "POST", "/repos/cli/cli/issues"})
	if cl.Type != Mutation {
		t.Errorf("POST api: type=%d, want Mutation", cl.Type)
	}

	// --method DELETE → mutation
	cl = c.Classify([]string{"api", "--method", "DELETE", "/repos/cli/cli/issues/1"})
	if cl.Type != Mutation {
		t.Errorf("DELETE api: type=%d, want Mutation", cl.Type)
	}
}

func TestAdditionalCacheable(t *testing.T) {
	c := NewClassifier([]string{"gh status", "gh variable list"})
	// Single-word additional command
	cl := c.Classify([]string{"status"})
	if cl.Type != Cacheable {
		t.Errorf("additional cacheable 'status': type=%d, want Cacheable", cl.Type)
	}
	// Two-word additional command
	cl = c.Classify([]string{"variable", "list"})
	if cl.Type != Cacheable {
		t.Errorf("additional cacheable 'variable list': type=%d, want Cacheable", cl.Type)
	}
	if cl.CmdKey != "variable_list" {
		t.Errorf("additional cacheable 'variable list': cmdKey=%s, want variable_list", cl.CmdKey)
	}
}

func TestAPIEffectiveMethod(t *testing.T) {
	c := NewClassifier(nil)
	tests := []struct {
		name string
		args []string
		want CommandType
	}{
		{"implicit raw field POST", []string{"repos/owner/repo/issues", "-f", "title=hello"}, Mutation},
		{"implicit typed field POST", []string{"repos/owner/repo/issues", "--field=title=hello"}, Mutation},
		{"attached method", []string{"repos/owner/repo/issues", "--method=POST"}, Mutation},
		{"short attached method", []string{"repos/owner/repo/issues", "-XPOST"}, Mutation},
		{"short equals method", []string{"repos/owner/repo/issues", "-X=POST"}, Mutation},
		{"combined short flags", []string{"-iXPOST", "repos/owner/repo/issues"}, Mutation},
		{"input file", []string{"repos/owner/repo/issues", "--input", "body.json"}, Mutation},
		{"GET input file", []string{"repos/owner/repo/issues", "-XGET", "--input=body.json"}, Mutation},
		{"GET file field", []string{"repos/owner/repo/issues", "-XGET", "-Fquery=@query.txt"}, Mutation},
		{"GET stdin field", []string{"repos/owner/repo/issues", "-XGET", "-Fquery=@-"}, Mutation},
		{"GraphQL mutation", []string{"graphql", "-f", "query=mutation { example }"}, Mutation},
		{"GraphQL explicit GET", []string{"graphql", "-XGET"}, Mutation},
		{"explicit GET fields", []string{"search/issues", "-f", "q=hello", "--method=GET"}, Cacheable},
		{"typed GET fields", []string{"search/issues", "-X", "GET", "-F", "page=1"}, Cacheable},
		{"raw at-sign field", []string{"search/issues", "-XGET", "-f", "q=@name"}, Cacheable},
		{"combined GET flags", []string{"-iXGET", "repos/owner/repo"}, Cacheable},
		{"last method wins", []string{"repos/owner/repo", "-XPOST", "--method", "GET"}, Cacheable},
		{"method-like header", []string{"repos/owner/repo", "-H", "-X", "--jq", "POST"}, Cacheable},
		{"method-like input value", []string{"repos/owner/repo", "--input", "-X"}, Mutation},
		{"missing value", []string{"repos/owner/repo", "--method"}, Mutation},
		{"unknown option", []string{"repos/owner/repo", "--future-option"}, Mutation},
		{"terminator", []string{"-XGET", "--", "repos/owner/repo"}, Cacheable},
		{"endpoint after terminator", []string{"--", "--method=POST"}, Cacheable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.Classify(append([]string{"api"}, tt.args...))
			if got.Type != tt.want {
				t.Fatalf("Classify(%q) = %+v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestMutationResourceRouting(t *testing.T) {
	c := NewClassifier(nil)
	tests := []struct {
		args []string
		want ResourceType
	}{
		{[]string{"workflow", "run", "ci.yml"}, ResourceRun},
		{[]string{"run", "cancel", "1"}, ResourceRun},
		{[]string{"workflow", "disable", "ci.yml"}, ResourceWorkflow},
		{[]string{"label", "clone", "o/r"}, ResourceLabel},
	}
	for _, tt := range tests {
		if got := c.Classify(tt.args); got.Type != Mutation || got.Resource != tt.want {
			t.Errorf("Classify(%v) = %+v, want Mutation/%s", tt.args, got, tt.want)
		}
	}
}

func TestReadOnlyRunCommandsStayPassthrough(t *testing.T) {
	c := NewClassifier(nil)
	for _, args := range [][]string{{"run", "watch", "1"}, {"run", "download", "1"}, {"repo", "clone", "o/r"}, {"pr", "checkout", "1"}} {
		if got := c.Classify(args); got.Type != Passthrough {
			t.Errorf("Classify(%v) = %+v, want Passthrough", args, got)
		}
	}
}

func TestScopeHints(t *testing.T) {
	c := NewClassifier(nil)
	tests := []struct {
		name                     string
		args                     []string
		ignoreBranch, ignoreRepo bool
		host, repo               string
	}{
		{"pr view current branch", []string{"pr", "view"}, false, false, "", ""},
		{"pr view json only", []string{"pr", "view", "--json", "number"}, false, false, "", ""},
		{"pr view number", []string{"pr", "view", "42", "--json", "number"}, true, false, "", ""},
		{"pr view after flags", []string{"pr", "view", "--json=title", "-c", "feature"}, true, false, "", ""},
		{"pr checks unknown flag", []string{"pr", "checks", "--future", "x"}, false, false, "", ""},
		{"pr status", []string{"pr", "status"}, false, false, "", ""},
		{"run view", []string{"run", "view", "7"}, true, false, "", ""},
		{"run list", []string{"run", "list", "--limit", "5"}, true, false, "", ""},
		{"issue view -R", []string{"issue", "view", "1", "-R", "o/r"}, true, true, "", "o/r"},
		{"repo flag with host", []string{"run", "list", "--repo=ghe.io/o/r"}, true, true, "ghe.io", "o/r"},
		{"search", []string{"search", "repos", "ghx"}, true, true, "", ""},
		{"api user", []string{"api", "user"}, true, true, "", ""},
		{"api explicit repo", []string{"api", "/repos/o/r/actions/runs?per_page=5"}, true, true, "", "o/r"},
		{"api repo with query", []string{"api", "repos/o/r?x=1"}, true, true, "", "o/r"},
		{"api placeholders", []string{"api", "repos/{owner}/{repo}/pulls"}, true, false, "", ""},
		{"api branch placeholder", []string{"api", "repos/o/r/branches/{branch}"}, false, false, "", "o/r"},
		{"api hostname", []string{"api", "--hostname", "ghe.io", "user"}, true, true, "ghe.io", ""},
		{"additional cacheable is conservative", []string{"status"}, false, false, "", ""},
	}
	c = NewClassifier([]string{"status"})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.Classify(tt.args)
			if got.IgnoreBranch != tt.ignoreBranch || got.IgnoreRepo != tt.ignoreRepo || got.TargetHost != tt.host || got.TargetRepo != tt.repo {
				t.Fatalf("Classify(%q) = %+v", tt.args, got)
			}
		})
	}
}

func TestMutationTargetsExplicitRepo(t *testing.T) {
	c := NewClassifier(nil)
	cl := c.Classify([]string{"pr", "merge", "1", "-R", "o/r"})
	if host, repo := cl.Target("github.com", "cwd/repo"); host != "github.com" || repo != "o/r" {
		t.Fatalf("Target = %s %s", host, repo)
	}
	cl = c.Classify([]string{"api", "-X", "POST", "repos/o/r/issues"})
	if _, repo := cl.Target("github.com", "cwd/repo"); repo != "o/r" {
		t.Fatalf("api Target repo = %s", repo)
	}
}
