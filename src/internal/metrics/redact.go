package metrics

import (
	"regexp"
	"strings"
)

const redacted = "[REDACTED]"

var (
	// GitHub token formats, JWTs, and bearer/basic credentials.
	tokenPattern = regexp.MustCompile(`(?i)\b(gh[pousr]_[A-Za-z0-9_]{16,}|github_pat_[A-Za-z0-9_]{16,}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]*)|((?:bearer|token|basic)\s+)\S+`)
	// Field names whose values are likely sensitive.
	sensitiveKey = regexp.MustCompile(`(?i)token|secret|passw|auth|key|cred|session|cookie`)
)

// valueFlags maps flags whose values are always redacted. Header values may
// carry credentials; bodies may carry private content.
var valueFlags = map[string]bool{
	"-H": true, "--header": true,
	"-b": true, "--body": true, "--body-file": true,
	"--input": true,
}

// fieldFlags take key=value pairs; values are redacted for sensitive keys.
var fieldFlags = map[string]bool{
	"-f": true, "--raw-field": true, "-F": true, "--field": true,
}

// RedactArgs returns a copy of args with credentials and request bodies
// replaced, suitable for the dashboard's request log.
func RedactArgs(args []string) []string {
	if args == nil {
		return nil
	}
	out := make([]string, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		var name, value string
		var attached bool
		switch {
		case len(a) > 2 && a[0] == '-' && a[1] != '-' && (valueFlags[a[:2]] || fieldFlags[a[:2]]):
			name, value, attached = a[:2], a[2:], true // e.g. -HAuthorization:x
		case strings.HasPrefix(a, "--"):
			name, value, attached = strings.Cut(a, "=")
		default:
			name = a
		}
		redact := redactField
		if valueFlags[name] {
			redact = func(v string) string { return redactValue(name, v) }
		}
		switch {
		case !valueFlags[name] && !fieldFlags[name]:
			out[i] = redactTokens(a)
		case attached:
			out[i] = a[:len(a)-len(value)] + redact(value)
		default:
			out[i] = a
			if i+1 < len(args) {
				i++
				out[i] = redact(args[i])
			}
		}
	}
	return out
}

func redactValue(flag, v string) string {
	if flag == "-H" || flag == "--header" {
		if name, _, ok := strings.Cut(v, ":"); ok {
			return name + ": " + redacted
		}
	}
	return redacted
}

func redactField(kv string) string {
	k, v, ok := strings.Cut(kv, "=")
	if !ok {
		return redactTokens(kv)
	}
	if sensitiveKey.MatchString(k) || strings.HasPrefix(v, "@") {
		return k + "=" + redacted
	}
	return k + "=" + redactTokens(v)
}

func redactTokens(s string) string {
	return tokenPattern.ReplaceAllStringFunc(s, func(m string) string {
		sub := tokenPattern.FindStringSubmatch(m)
		if sub[2] != "" {
			return sub[2] + redacted
		}
		return redacted
	})
}
