// Package shape normalizes output-shaping flags (--jq, --json) so requests that
// differ only in how they format the same data share one cached response.
// Filters are re-applied in the daemon with the same engine and formatting gh
// uses, so callers receive byte-identical output.
package shape

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/itchyny/gojq"
)

// Plan describes how to fetch and post-process a cacheable command.
type Plan struct {
	// Fetch is the command to execute and key the cache on: the original args
	// with --jq removed and --json fields sorted.
	Fetch []string
	// JQ is the filter to apply to Fetch's stdout, if any.
	JQ string
	// Fields is the sorted --json field list, if any.
	Fields []string
	// Base is Fetch with the --json field list replaced by a wildcard. Entries
	// with the same Base differ only in the fields they selected.
	Base []string
}

const fieldsWildcard = "\x00*"

// Normalize builds a plan for args (the arguments after "gh"). When args use
// a form it does not fully understand, it returns a plan that runs args as-is.
func Normalize(args []string) Plan {
	unchanged := Plan{Fetch: args}
	if len(args) < 2 {
		return unchanged
	}
	var p *Plan
	if args[0] == "api" {
		p = normalizeAPI(args)
	} else {
		p = normalizeCommand(args)
	}
	if p == nil || strings.Contains(p.JQ, "env") || strings.Contains(p.JQ, "ENV") {
		return unchanged // the daemon's environment differs from the caller's
	}
	return *p
}

// normalizeCommand handles commands such as "pr view 1 --json a,b --jq .a".
// --jq is only stripped alongside --json, since gh rejects --jq without it.
func normalizeCommand(args []string) *Plan {
	var fetch, fields []string
	var jq string
	fieldsAt := -1
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, value, attached := strings.Cut(a, "=")
		switch {
		case a == "--":
			return nil
		case name == "--jq" || a == "-q":
			if !attached {
				if i+1 >= len(args) {
					return nil
				}
				i++
				value = args[i]
			}
			jq = value
		case name == "--json":
			if !attached {
				if i+1 >= len(args) {
					return nil
				}
				i++
				value = args[i]
			}
			if value == "" || strings.HasPrefix(value, "-") || fieldsAt >= 0 {
				return nil
			}
			fields = sortedFields(value)
			fetch = append(fetch, "--json", strings.Join(fields, ","))
			fieldsAt = len(fetch) - 1
		case name == "--template" || strings.HasPrefix(a, "-t") || strings.HasPrefix(a, "-q"):
			return nil // templates, or short flags with attached values
		default:
			fetch = append(fetch, a)
		}
	}
	if fieldsAt < 0 {
		if jq != "" {
			return nil
		}
		return &Plan{Fetch: args}
	}
	base := append([]string(nil), fetch...)
	base[fieldsAt] = fieldsWildcard
	return &Plan{Fetch: fetch, JQ: jq, Fields: fields, Base: base}
}

// normalizeAPI strips --jq from gh api calls whose output is the raw response
// body. Pagination, headers in output, and templates are left untouched.
func normalizeAPI(args []string) *Plan {
	fetch := []string{"api"}
	var jq string
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			fetch = append(fetch, args[i:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			fetch = append(fetch, a)
			continue
		}
		name, value, attached := strings.Cut(a, "=")
		if !strings.HasPrefix(name, "--") && len(name) != 2 {
			return nil // combined or attached short flags
		}
		switch strings.TrimLeft(name, "-") {
		case "q", "jq":
			if !attached {
				if i+1 >= len(args) {
					return nil
				}
				i++
				value = args[i]
			}
			jq = value
		case "X", "method", "f", "raw-field", "F", "field", "H", "header", "hostname", "p", "preview", "cache":
			fetch = append(fetch, a)
			if !attached {
				if i+1 >= len(args) {
					return nil
				}
				i++
				fetch = append(fetch, args[i])
			}
		default:
			return nil // include, paginate, slurp, silent, verbose, template, input, ...
		}
	}
	return &Plan{Fetch: fetch, JQ: jq}
}

func sortedFields(value string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range strings.Split(value, ",") {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// Covers reports whether a cached field set includes every requested field.
func Covers(have, want []string) bool {
	set := make(map[string]bool, len(have))
	for _, f := range have {
		set[f] = true
	}
	for _, f := range want {
		if !set[f] {
			return false
		}
	}
	return true
}

// Project narrows gh --json output (an object or array of objects) produced
// for a superset of fields down to fields. It reports false unless re-encoding
// the input reproduces it exactly, which guarantees the projection matches
// what gh would print for the narrower field list.
func Project(stdout []byte, fields []string) ([]byte, bool) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return nil, false
	}
	if trimmed[0] == '[' {
		var rows []map[string]json.RawMessage
		if json.Unmarshal(trimmed, &rows) != nil || !roundTrips(rows, stdout) {
			return nil, false
		}
		out := make([]map[string]json.RawMessage, len(rows))
		for i, row := range rows {
			out[i] = pick(row, fields)
		}
		return encode(out)
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(trimmed, &obj) != nil || obj == nil || !roundTrips(obj, stdout) {
		return nil, false
	}
	return encode(pick(obj, fields))
}

func pick(row map[string]json.RawMessage, fields []string) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(fields))
	for _, f := range fields {
		if v, ok := row[f]; ok {
			out[f] = v
		}
	}
	return out
}

// encode matches gh's --json exporter: json.Encoder without HTML escaping.
func encode(v any) ([]byte, bool) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if enc.Encode(v) != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

func roundTrips(v any, original []byte) bool {
	b, ok := encode(v)
	return ok && bytes.Equal(b, original)
}

// ApplyJQ evaluates expr against stdout as gh does for non-TTY output
// (github.com/cli/go-gh/v2/pkg/jq.Evaluate, MIT): top-level scalars are
// printed raw, other values as compact JSON, one result per line.
func ApplyJQ(stdout []byte, expr string) ([]byte, error) {
	query, err := gojq.Parse(expr)
	if err != nil {
		return nil, err
	}
	code, err := gojq.Compile(query, gojq.WithEnvironLoader(os.Environ))
	if err != nil {
		return nil, err
	}
	var data any
	if err := json.Unmarshal(stdout, &data); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	iter := code.Run(data)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := v.(error); isErr {
			var halt *gojq.HaltError
			if errors.As(err, &halt) && halt.Value() == nil {
				break
			}
			return nil, err
		}
		if text, ok := scalarString(v); ok {
			out.WriteString(text)
		} else {
			b, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			out.Write(b)
		}
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

func scalarString(v any) (string, bool) {
	switch tt := v.(type) {
	case string:
		return tt, true
	case float64:
		if math.Trunc(tt) == tt {
			return strconv.FormatFloat(tt, 'f', 0, 64), true
		}
		return strconv.FormatFloat(tt, 'f', 2, 64), true
	case nil:
		return "", true
	case bool:
		return fmt.Sprintf("%v", tt), true
	}
	return "", false
}
