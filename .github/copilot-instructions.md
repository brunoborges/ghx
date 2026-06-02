# Copilot Instructions for ghxd

## Build & Test

```bash
make build          # Builds bin/ghx and bin/ghxd
make test           # go test ./...
make clean          # rm -rf bin/

# Single package test
go test ./src/internal/allowlist/ -v
go test ./src/internal/cache/ -v -run TestTTLExpiry

# Lint (matches CI)
go vet ./...
gofmt -l .

# Race detector (matches CI)
go test -v -race -coverprofile=coverage.out ./...
```

## Architecture

**Two-binary daemon/client model** for caching `gh` CLI calls across concurrent AI agents:

- **`ghx`** (client, `src/cmd/ghx/`) — Drop-in `gh` replacement. Resolves git context (host, repo, branch, token), classifies the command, and sends an IPC request to the daemon. Auto-starts the daemon if needed. Falls back to direct `gh` execution on any daemon failure.
- **`ghxd`** (daemon, `src/cmd/ghxd/`) — Long-lived daemon. Accepts requests over a Unix domain socket (`~/.ghx/ghxd.sock`), serves cached responses, and runs a web dashboard on port 9847.

**Request flow:** `ghx` → Unix socket IPC → `ghxd` handler → allowlist classifier → cache lookup → (on miss) `executor.Execute("gh", ...)` → cache store → response back to client.

### Internal packages

| Package | Role |
|---------|------|
| `allowlist` | Classifies commands as `Cacheable`, `Mutation`, or `Passthrough` |
| `cache` | Thread-safe LRU cache with per-entry TTL and namespace invalidation |
| `client` | Thin Unix socket client with health-check support |
| `config` | Loads `~/.ghx/config.yaml` with env var overrides (`GHX_TTL`, `GHX_SOCKET`, `GHX_GH_PATH`) |
| `context` | Resolves git execution context (host, repo, branch, token hash) for cache key generation |
| `daemon` | Server, connection handler, and request dispatcher |
| `dashboard` | Embedded single-file HTML dashboard with JSON API endpoints |
| `executor` | Runs `gh` subprocesses and captures stdout/stderr/exit code |
| `metrics` | Hit/miss counters, per-command stats, ring-buffer request log, TTL analysis |
| `protocol` | Length-prefixed JSON IPC protocol (4-byte big-endian size header, 10MB max) |

### IPC Protocol

Communication uses length-prefixed JSON over Unix sockets. `Request` carries `Args`, `Context`, `NoCache`, `TTLOverride`, and `Type` (one of: `exec`, `flush`, `stats`, `keys`, `shutdown`). `Response` returns `Stdout`, `Stderr`, `ExitCode`, `Cached`, `Error`.

### Cache Key Design

Keys are SHA-256 hashes of `host + repo + branch + tokenHash + args` joined with null separators. This ensures context-aware caching — the same command in different repos or branches gets different cache entries.

### Command Classification

The `Classifier` in `allowlist` determines handling:

1. **User overrides first** — `additional_cacheable` from config takes priority over everything, including the never-cache list
2. **Never-cache subcommands** — `auth`, `codespace`, `config`, `secret`, `variable`, `extension`, etc.
3. **`gh api`** — Cacheable only for GET; any other HTTP method is treated as a mutation
4. **Built-in cacheable map** — Maps `"sub action"` strings (e.g., `"pr list"`) to `ResourceType` values
5. **Mutations** — Actions like `create`, `merge`, `close`, `edit`, `delete` trigger cache invalidation for the corresponding resource namespace

### Namespace Invalidation

Mutations flush all cache entries matching `host/repo/resourceType`. For example, `gh pr merge 42` invalidates all cached PR entries for that repository.

## Key Conventions

- **Constructors**: `NewX(...)` pattern (e.g., `NewClassifier`, `NewCache`, `NewStats`)
- **Error handling**: Internals wrap errors with `fmt.Errorf("...: %w", err)`. CLI entry points log warnings and fall back gracefully rather than failing hard.
- **Cache/command keys**: Normalized with underscores (`pr_list`, `api_get`, `repo_view`)
- **Resource types**: String constants like `"pr"`, `"issue"`, `"repo"` — used for both metrics grouping and cache invalidation namespaces
- **Tests**: Table-driven, package-local, stdlib `testing` only (no testify). See `allowlist_test.go` for the canonical pattern.
- **No external dependencies** beyond `gopkg.in/yaml.v3` — dashboard is embedded HTML, no frameworks
- **Config is forgiving**: Missing config files return safe defaults; callers log warnings, never panic

## Agent Plugin

The `agent-plugin/` directory contains a Claude Code / Copilot CLI plugin:

- `bin/ghc` and `bin/ghxd` are shell wrapper scripts that lazy-install the real binaries on first run
- `scripts/install.sh` auto-detects OS/arch and downloads from GitHub releases (filters out `plugin-v*` tags)
- `skills/ghxd/SKILL.md` teaches the agent to prefer `ghx` over `gh`

## Release Process

- **App release**: Push a `v*` tag → `release.yml` builds multi-arch tarballs, creates GitHub release, updates Homebrew tap
- **Plugin release**: Push a `plugin-v*` tag → `release-plugin.yml` validates structure and packages plugin tarball
- Install scripts must filter out `plugin-v*` tags when resolving "latest version" since GitHub's `/releases/latest` API doesn't distinguish tag prefixes
