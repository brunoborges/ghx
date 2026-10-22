package allowlist

import "strings"

// CommandType classifies a gh command.
type CommandType int

const (
	Cacheable   CommandType = iota // Safe to cache
	Passthrough                    // Not cacheable, pass directly to gh
	Mutation                       // Mutating command, triggers cache invalidation
)

// ResourceType identifies the resource namespace for cache invalidation.
type ResourceType string

const (
	ResourcePR       ResourceType = "pr"
	ResourceIssue    ResourceType = "issue"
	ResourceRun      ResourceType = "run"
	ResourceWorkflow ResourceType = "workflow"
	ResourceRelease  ResourceType = "release"
	ResourceLabel    ResourceType = "label"
	ResourceRepo     ResourceType = "repo"
	ResourceAPI      ResourceType = "api"
	ResourceSearch   ResourceType = "search"
	ResourceGist     ResourceType = "gist"
	ResourceProject  ResourceType = "project"
	ResourceCache    ResourceType = "cache"
	ResourceRuleset  ResourceType = "ruleset"
	ResourceOrg      ResourceType = "org"
	ResourceSecret   ResourceType = "secret"
	ResourceVariable ResourceType = "variable"
	ResourceUnknown  ResourceType = ""
)

type Classification struct {
	Type     CommandType
	Resource ResourceType
	// CmdKey is a normalized identifier like "pr_list" for metrics/TTL overrides.
	CmdKey string

	// Scope hints for cache keys and invalidation. Zero values are the
	// conservative default: the key includes the caller's repo and branch.
	IgnoreBranch bool   // output does not depend on the current git branch
	IgnoreRepo   bool   // output does not depend on the current git repo
	TargetHost   string // host named explicitly by the command, if any
	TargetRepo   string // owner/repo named explicitly by the command, if any
}

// cacheableCommands maps "subcommand action" to resource type.
var cacheableCommands = map[string]ResourceType{
	"pr list":            ResourcePR,
	"pr view":            ResourcePR,
	"pr status":          ResourcePR,
	"pr checks":          ResourcePR,
	"pr diff":            ResourcePR,
	"issue list":         ResourceIssue,
	"issue view":         ResourceIssue,
	"issue status":       ResourceIssue,
	"repo view":          ResourceRepo,
	"repo list":          ResourceRepo,
	"run list":           ResourceRun,
	"run view":           ResourceRun,
	"workflow list":      ResourceWorkflow,
	"workflow view":      ResourceWorkflow,
	"release list":       ResourceRelease,
	"release view":       ResourceRelease,
	"search repos":       ResourceSearch,
	"search issues":      ResourceSearch,
	"search prs":         ResourceSearch,
	"search commits":     ResourceSearch,
	"search code":        ResourceSearch,
	"label list":         ResourceLabel,
	"gist list":          ResourceGist,
	"gist view":          ResourceGist,
	"project list":       ResourceProject,
	"project view":       ResourceProject,
	"project field-list": ResourceProject,
	"project item-list":  ResourceProject,
	"cache list":         ResourceCache,
	"ruleset list":       ResourceRuleset,
	"ruleset view":       ResourceRuleset,
	"ruleset check":      ResourceRuleset,
	"org list":           ResourceOrg,
	"secret list":        ResourceSecret,
	"variable list":      ResourceVariable,
	"variable get":       ResourceVariable,
}

// mutatingSubcommands trigger cache invalidation for their resource type.
var mutatingActions = map[string]bool{
	"create":   true,
	"edit":     true,
	"delete":   true,
	"merge":    true,
	"close":    true,
	"reopen":   true,
	"comment":  true,
	"review":   true,
	"approve":  true,
	"ready":    true,
	"lock":     true,
	"unlock":   true,
	"pin":      true,
	"unpin":    true,
	"transfer": true,
	"set":      true,
	// Workflow/run state changes.
	"run":     true, // gh workflow run
	"rerun":   true,
	"cancel":  true,
	"enable":  true,
	"disable": true,
	// Other remote writes.
	"upload":        true,
	"sync":          true,
	"rename":        true,
	"archive":       true,
	"unarchive":     true,
	"fork":          true,
	"update-branch": true,
	"develop":       true,
	"item-add":      true,
	"item-edit":     true,
	"item-delete":   true,
	"item-archive":  true,
	"item-create":   true,
	"field-create":  true,
	"field-delete":  true,
	"link":          true,
	"unlink":        true,
	"copy":          true,
	"mark-template": true,
}

// mutationResourceOverrides route invalidation for mutations whose effect
// lands in a different namespace than their subcommand.
var mutationResourceOverrides = map[string]ResourceType{
	"workflow run": ResourceRun,
	"label clone":  ResourceLabel,
}

var subcommandResourceMap = map[string]ResourceType{
	"pr":       ResourcePR,
	"issue":    ResourceIssue,
	"run":      ResourceRun,
	"workflow": ResourceWorkflow,
	"release":  ResourceRelease,
	"label":    ResourceLabel,
	"repo":     ResourceRepo,
	"gist":     ResourceGist,
	"project":  ResourceProject,
	"cache":    ResourceCache,
	"ruleset":  ResourceRuleset,
	"org":      ResourceOrg,
	"secret":   ResourceSecret,
	"variable": ResourceVariable,
}

// neverCacheSubcommands are always passed through regardless.
var neverCacheSubcommands = map[string]bool{
	"auth":      true,
	"codespace": true,
	"config":    true,
	"ssh-key":   true,
	"gpg-key":   true,
	"extension": true,
}

// Target returns the host and repo a command operates on, preferring explicit
// selectors over the caller's context.
func (c Classification) Target(host, repo string) (string, string) {
	if c.TargetHost != "" {
		host = c.TargetHost
	}
	if c.TargetRepo != "" {
		repo = c.TargetRepo
	}
	return host, repo
}

// Classifier classifies gh commands.
type Classifier struct {
	additionalCacheable map[string]ResourceType
}

// NewClassifier creates a classifier with optional additional cacheable commands.
func NewClassifier(additional []string) *Classifier {
	ac := make(map[string]ResourceType)
	for _, cmd := range additional {
		// Strip "gh " prefix if present
		cmd = strings.TrimPrefix(cmd, "gh ")
		ac[cmd] = ResourceUnknown
	}
	return &Classifier{additionalCacheable: ac}
}

// Classify determines how to handle a gh command based on its arguments.
// args should be the arguments after "gh", e.g., ["pr", "list", "--json", "number"].
func (c *Classifier) Classify(args []string) Classification {
	if len(args) == 0 {
		return Classification{Type: Passthrough}
	}

	sub := args[0]

	// Check additional cacheable first — user overrides take priority.
	// Single-word commands (e.g., "status")
	if res, ok := c.additionalCacheable[sub]; ok {
		return Classification{
			Type:     Cacheable,
			Resource: res,
			CmdKey:   sub,
		}
	}
	// Two-word additional cacheable (e.g., "variable list")
	if len(args) >= 2 {
		key := sub + " " + args[1]
		if res, ok := c.additionalCacheable[key]; ok {
			return Classification{
				Type:     Cacheable,
				Resource: res,
				CmdKey:   strings.ReplaceAll(key, " ", "_"),
			}
		}
	}

	// Never-cache subcommands (after user overrides)
	if neverCacheSubcommands[sub] {
		return Classification{Type: Passthrough}
	}

	// Special handling for "api" subcommand
	if sub == "api" {
		return c.classifyAPI(args)
	}

	// Need at least subcommand + action
	if len(args) < 2 {
		return Classification{Type: Passthrough, Resource: subcommandResourceMap[sub]}
	}

	action := args[1]
	key := sub + " " + action

	// Check built-in cacheable list
	if res, ok := cacheableCommands[key]; ok {
		cl := Classification{
			Type:     Cacheable,
			Resource: res,
			CmdKey:   strings.ReplaceAll(key, " ", "_"),
		}
		applyScope(&cl, key, args[2:])
		return cl
	}

	// Check if it's a known mutation
	if res, ok := mutationResourceOverrides[key]; ok {
		cl := Classification{Type: Mutation, Resource: res, CmdKey: strings.ReplaceAll(key, " ", "_")}
		applyScope(&cl, key, args[2:])
		return cl
	}
	if mutatingActions[action] {
		cl := Classification{
			Type:     Mutation,
			Resource: subcommandResourceMap[sub],
			CmdKey:   strings.ReplaceAll(key, " ", "_"),
		}
		applyScope(&cl, key, args[2:])
		return cl
	}

	return Classification{Type: Passthrough, Resource: subcommandResourceMap[sub]}
}

func (c *Classifier) classifyAPI(args []string) Classification {
	mutation := Classification{Type: Mutation, Resource: ResourceAPI}
	method, endpoint, hostname := "GET", "", ""
	explicitMethod, hasFields := false, false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if endpoint != "" || i+2 != len(args) {
				return mutation
			}
			endpoint = args[i+1]
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			if endpoint != "" {
				return mutation
			}
			endpoint = arg
			continue
		}

		// Consume flag values so a header/filter/body containing "-X" cannot
		// change the inferred method. Unknown options stay out of the cache.
		long := strings.HasPrefix(arg, "--")
		options := strings.TrimPrefix(arg, "-")
		if long {
			options = strings.TrimPrefix(options, "-")
		}
		for options != "" {
			var name, value string
			var attached bool
			if long {
				name, value, attached = strings.Cut(options, "=")
				options = ""
			} else {
				name, options = options[:1], options[1:]
			}
			switch name {
			case "i", "include", "paginate", "silent", "slurp", "verbose", "allow-escape-sequences":
				continue
			case "X", "method", "f", "raw-field", "F", "field", "input",
				"H", "header", "hostname", "p", "preview", "q", "jq", "t", "template", "cache":
				if !long && options != "" {
					value, attached = strings.TrimPrefix(options, "="), true
					options = ""
				}
				if !attached {
					i++
					if i == len(args) {
						return mutation
					}
					value = args[i]
				}
			default:
				return mutation
			}
			switch name {
			case "X", "method":
				method, explicitMethod = value, true
			case "f", "raw-field", "F", "field":
				hasFields = true
				if name == "F" || name == "field" {
					_, fieldValue, _ := strings.Cut(value, "=")
					if strings.HasPrefix(fieldValue, "@") {
						return mutation // File/stdin contents are not part of the cache key.
					}
				}
			case "input":
				return mutation // Body contents can change without changing argv.
			case "hostname":
				hostname = value
			}
		}
	}
	if endpoint == "" || endpoint == "graphql" {
		return mutation // GraphQL requires operation-aware classification before caching.
	}
	mutation = apiScope(mutation, args, endpoint, hostname)
	if hasFields && !explicitMethod {
		method = "POST"
	}
	if !strings.EqualFold(method, "GET") {
		return mutation
	}
	return apiScope(Classification{Type: Cacheable, Resource: ResourceAPI, CmdKey: "api_get"}, args, endpoint, hostname)
}

// branchDefaultCommands fall back to the current branch's pull request (or the
// current branch itself) when no explicit selector is given.
var branchDefaultCommands = map[string]bool{
	"pr view":   true,
	"pr checks": true,
	"pr diff":   true,
}

// branchAlwaysCommands always report on the current branch.
var branchAlwaysCommands = map[string]bool{
	"pr status":     true,
	"ruleset check": true,
}

// repoIndependentCommands never read the current repository.
var repoIndependentCommands = map[string]bool{
	"search repos":   true,
	"search issues":  true,
	"search prs":     true,
	"search commits": true,
	"search code":    true,
	"gist list":      true,
	"gist view":      true,
	"org list":       true,
	"repo list":      true,
}

// selectorValueFlags take a value on commands in branchDefaultCommands.
var selectorValueFlags = map[string]bool{
	"-R": true, "--repo": true, "--json": true, "-q": true, "--jq": true,
	"-t": true, "--template": true, "--color": true, "-i": true, "--interval": true,
}

// selectorBoolFlags are value-less flags on commands in branchDefaultCommands.
var selectorBoolFlags = map[string]bool{
	"-w": true, "--web": true, "-c": true, "--comments": true, "--watch": true,
	"--fail-fast": true, "--required": true, "--name-only": true, "--patch": true,
}

func applyScope(cl *Classification, key string, rest []string) {
	cl.TargetHost, cl.TargetRepo = repoFlag(rest)
	if cl.TargetRepo != "" || repoIndependentCommands[key] {
		cl.IgnoreRepo = true
	}
	switch {
	case branchAlwaysCommands[key]:
	case branchDefaultCommands[key]:
		cl.IgnoreBranch = hasSelector(rest)
	default:
		cl.IgnoreBranch = true
	}
}

// hasSelector reports whether args contain a positional argument. Unknown
// flags make the answer uncertain, so they report false (branch-scoped).
func hasSelector(args []string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return i+1 < len(args)
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			return true
		}
		name, _, attached := strings.Cut(a, "=")
		switch {
		case selectorValueFlags[name]:
			if !attached {
				i++
			}
		case selectorBoolFlags[name] && !attached:
		default:
			return false
		}
	}
	return false
}

// repoFlag extracts an explicit -R/--repo target as host and owner/repo.
func repoFlag(args []string) (host, repo string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		var v string
		switch {
		case a == "-R" || a == "--repo":
			if i+1 < len(args) {
				v = args[i+1]
			}
			i++
		case strings.HasPrefix(a, "--repo="):
			v = strings.TrimPrefix(a, "--repo=")
		case strings.HasPrefix(a, "-R") && len(a) > 2:
			v = strings.TrimPrefix(strings.TrimPrefix(a, "-R"), "=")
		default:
			continue
		}
		host, repo = parseRepoSelector(v)
	}
	return host, repo
}

func parseRepoSelector(v string) (host, repo string) {
	v = strings.TrimSuffix(v, ".git")
	if i := strings.Index(v, "://"); i >= 0 {
		v = v[i+3:]
	}
	parts := strings.Split(strings.Trim(v, "/"), "/")
	for _, p := range parts {
		if p == "" {
			return "", ""
		}
	}
	switch len(parts) {
	case 2:
		return "", parts[0] + "/" + parts[1]
	case 3:
		return parts[0], parts[1] + "/" + parts[2]
	}
	return "", ""
}

// apiScope derives key scope for gh api. gh fills {owner}, {repo}, and
// {branch} placeholders from the current repository, so any placeholder keeps
// the corresponding context in the key.
func apiScope(cl Classification, args []string, endpoint, hostname string) Classification {
	joined := strings.Join(args[1:], "\x00")
	usesRepo := strings.Contains(joined, "{owner}") || strings.Contains(joined, "{repo}")
	usesBranch := strings.Contains(joined, "{branch}")
	cl.TargetHost = hostname
	cl.IgnoreBranch = !usesBranch
	cl.IgnoreRepo = !usesRepo && !usesBranch
	if !usesRepo {
		path := strings.TrimPrefix(endpoint, "/")
		if rest, ok := strings.CutPrefix(path, "repos/"); ok {
			parts := strings.SplitN(rest, "/", 3)
			if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
				name, _, _ := strings.Cut(parts[1], "?")
				if name != "" {
					cl.TargetRepo = parts[0] + "/" + name
				}
			}
		}
	}
	return cl
}
