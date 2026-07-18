package authenv

import (
	"os"
	"strings"
)

// Environment contains the GitHub CLI environment that affects authentication.
// Values are sent only over the local IPC connection and must not be logged or persisted.
type Environment map[string]string

var names = []string{
	"GH_TOKEN",
	"GITHUB_TOKEN",
	"GH_ENTERPRISE_TOKEN",
	"GITHUB_ENTERPRISE_TOKEN",
	"GH_HOST",
	"GH_CONFIG_DIR",
	"XDG_CONFIG_HOME",
}

// Capture reads the current invocation's GitHub CLI authentication environment.
func Capture() Environment {
	env := make(Environment)
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			env[name] = value
		}
	}
	return env
}

// Apply removes inherited GitHub authentication variables and applies the
// invoking client's values. Unknown keys are ignored.
func Apply(base []string, values Environment) []string {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
	}

	result := make([]string, 0, len(base)+len(values))
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if _, ok := allowed[name]; !ok {
			result = append(result, entry)
		}
	}
	for _, name := range names {
		if value := values[name]; value != "" {
			result = append(result, name+"="+value)
		}
	}
	return result
}
