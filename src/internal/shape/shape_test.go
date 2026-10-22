package shape

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		fetch  []string
		jq     string
		fields []string
		base   []string
	}{
		{
			name:   "json and jq stripped and sorted",
			args:   []string{"pr", "view", "1", "--json", "title,number,title", "--jq", ".title"},
			fetch:  []string{"pr", "view", "1", "--json", "number,title"},
			jq:     ".title",
			fields: []string{"number", "title"},
			base:   []string{"pr", "view", "1", "--json", fieldsWildcard},
		},
		{
			name:   "attached forms",
			args:   []string{"pr", "list", "--json=state,id", "-q", ".[]"},
			fetch:  []string{"pr", "list", "--json", "id,state"},
			jq:     ".[]",
			fields: []string{"id", "state"},
			base:   []string{"pr", "list", "--json", fieldsWildcard},
		},
		{
			name:  "no shaping flags",
			args:  []string{"pr", "view", "1"},
			fetch: []string{"pr", "view", "1"},
		},
		{
			name:  "jq without json is left alone",
			args:  []string{"pr", "view", "--jq", ".x"},
			fetch: []string{"pr", "view", "--jq", ".x"},
		},
		{
			name:  "template is left alone",
			args:  []string{"pr", "view", "--json", "a", "--template", "{{.a}}"},
			fetch: []string{"pr", "view", "--json", "a", "--template", "{{.a}}"},
		},
		{
			name:  "attached short jq is left alone",
			args:  []string{"pr", "view", "--json", "a", "-q.a"},
			fetch: []string{"pr", "view", "--json", "a", "-q.a"},
		},
		{
			name:  "empty fields are left alone",
			args:  []string{"pr", "view", "--json"},
			fetch: []string{"pr", "view", "--json"},
		},
		{
			name:  "env-dependent filter is left alone",
			args:  []string{"pr", "view", "--json", "a", "--jq", "$ENV.HOME"},
			fetch: []string{"pr", "view", "--json", "a", "--jq", "$ENV.HOME"},
		},
		{
			name:  "api jq stripped",
			args:  []string{"api", "repos/o/r/pulls", "-H", "Accept: x", "--jq", ".[].id"},
			fetch: []string{"api", "repos/o/r/pulls", "-H", "Accept: x"},
			jq:    ".[].id",
		},
		{
			name:  "api attached jq stripped",
			args:  []string{"api", "--jq=.id", "user"},
			fetch: []string{"api", "user"},
			jq:    ".id",
		},
		{
			name:  "api paginate is left alone",
			args:  []string{"api", "--paginate", "repos/o/r/pulls", "--jq", ".[]"},
			fetch: []string{"api", "--paginate", "repos/o/r/pulls", "--jq", ".[]"},
		},
		{
			name:  "api include is left alone",
			args:  []string{"api", "-i", "user", "-q", ".id"},
			fetch: []string{"api", "-i", "user", "-q", ".id"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Normalize(tt.args)
			if !reflect.DeepEqual(p.Fetch, tt.fetch) {
				t.Errorf("Fetch = %q, want %q", p.Fetch, tt.fetch)
			}
			if p.JQ != tt.jq {
				t.Errorf("JQ = %q, want %q", p.JQ, tt.jq)
			}
			if !reflect.DeepEqual(p.Fields, tt.fields) {
				t.Errorf("Fields = %q, want %q", p.Fields, tt.fields)
			}
			if !reflect.DeepEqual(p.Base, tt.base) {
				t.Errorf("Base = %q, want %q", p.Base, tt.base)
			}
		})
	}
}

func TestApplyJQ(t *testing.T) {
	input := []byte(`{"n":3,"f":1.5,"s":"a<b","b":true,"z":null,"o":{"k":"<v>"},"l":[1,2]}`)
	tests := []struct {
		expr string
		want string
	}{
		{".n", "3\n"},
		{".f", "1.50\n"},
		{".s", "a<b\n"},
		{".b", "true\n"},
		{".z", "\n"},
		{".o", "{\"k\":\"\\u003cv\\u003e\"}\n"},
		{".l[]", "1\n2\n"},
		{".l", "[1,2]\n"},
		{"empty", ""},
		{"halt", ""},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := ApplyJQ(input, tt.expr)
			if err != nil {
				t.Fatalf("ApplyJQ: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
	for _, expr := range []string{".[", `error("boom")`, ".n | keys"} {
		if _, err := ApplyJQ(input, expr); err == nil {
			t.Errorf("ApplyJQ(%q) expected error", expr)
		}
	}
	if _, err := ApplyJQ([]byte("not json"), "."); err == nil {
		t.Error("expected error for invalid input")
	}
}

func TestProject(t *testing.T) {
	obj := []byte("{\"a\":1,\"b\":\"<x>\",\"c\":[1]}\n")
	got, ok := Project(obj, []string{"a", "c"})
	if !ok || string(got) != "{\"a\":1,\"c\":[1]}\n" {
		t.Errorf("object projection = %q, %v", got, ok)
	}

	arr := []byte("[{\"a\":1,\"b\":2},{\"a\":3,\"b\":4}]\n")
	got, ok = Project(arr, []string{"b"})
	if !ok || string(got) != "[{\"b\":2},{\"b\":4}]\n" {
		t.Errorf("array projection = %q, %v", got, ok)
	}

	for name, in := range map[string]string{
		"indented":        "{\n  \"a\": 1\n}\n",
		"unsorted keys":   "{\"b\":1,\"a\":2}\n",
		"missing newline": "{\"a\":1}",
		"scalar":          "3\n",
		"empty":           "",
		"not json":        "hello",
	} {
		if _, ok := Project([]byte(in), []string{"a"}); ok {
			t.Errorf("%s: expected projection to be refused", name)
		}
	}
}

func TestCovers(t *testing.T) {
	if !Covers([]string{"a", "b", "c"}, []string{"c", "a"}) {
		t.Error("superset should cover")
	}
	if Covers([]string{"a"}, []string{"a", "b"}) {
		t.Error("subset should not cover")
	}
}

func TestNormalizeDoesNotMutateInput(t *testing.T) {
	args := []string{"pr", "view", "--json", "b,a", "--jq", ".a"}
	orig := strings.Join(args, " ")
	Normalize(args)
	if strings.Join(args, " ") != orig {
		t.Errorf("args mutated: %q", args)
	}
}
