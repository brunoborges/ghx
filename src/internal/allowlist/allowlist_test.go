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
