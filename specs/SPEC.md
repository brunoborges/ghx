# ghx — GitHub CLI Cache Proxy

## Problem

The GitHub CLI (`gh`) has no client-side caching. When multiple AI agents (Copilot CLI, coding agents, MCP servers) make frequent `gh` calls, they quickly exhaust GitHub API rate limits. Identical requests for the same PR list, issue details, or repo metadata are repeated seconds apart with no reuse.

## Solution

**ghx** is a caching proxy for the `gh` CLI, consisting of:

- **`ghxd`** — a background daemon that executes `gh` commands, caches results, and serves cached responses
- **`ghx`** — a thin CLI client that forwards commands to the daemon and returns results

The system uses an **allowlist** of known-safe read-only commands, **request coalescing** to prevent duplicate in-flight calls, and a **configurable TTL** (default: 30s). Commands not on the allowlist are passed through directly to `gh` without caching.

## Architecture

```
┌─────────┐  ┌─────────┐  ┌─────────┐
│ Agent 1 │  │ Agent 2 │  │ Agent 3 │
│ (ghx)   │  │ (ghx)   │  │ (ghx)   │
└────┬────┘  └────┬────┘  └────┬────┘
     │            │            │
     └────────────┼────────────┘
                  │ IPC (Unix socket / Windows named pipe)
           ┌──────┴──────┐
           │    ghxd     │
           │  (daemon)   │
           ├─────────────┤
           │ Cache Store │  (in-memory, LRU)
           │ Singleflight│  (request coalescing)
           │ Metrics     │
           │ Allowlist   │
           └──────┬──────┘
                  │ exec
              ┌───┴───┐
              │  gh   │
              └───────┘
```

## Language

**Go**. Same language as `gh` itself. Excellent for CLI tools, daemon processes, and HTTP servers. Standard library covers Unix sockets, HTTP, JSON, and concurrency primitives. Easy cross-compilation for macOS, Linux, and Windows.

## Core Concepts

### Cache Key

A cache key is a composite of the **full execution context**, not just command arguments. This prevents wrong cache hits across repos, users, or output formats.

```
CacheKey = hash(
    gh_command,          // normalized: sorted flags, resolved args
    resolved_host,       // github.com or GHE hostname
    resolved_repo,       // owner/name (from CWD or GH_REPO)
    resolved_branch,     // current branch (when command depends on it)
    auth_token_hash,     // SHA256 of token (not the token itself)
    output_format,       // --json fields, --jq, --template, or default
)
```

**Context resolution**: The client (`ghx`) resolves the execution context before sending to the daemon:
- Repo: `git remote get-url origin` or `GH_REPO` env var
- Branch: `git symbolic-ref --short HEAD`
- Host: `GH_HOST` env var or default from `gh` config
- Auth: token fingerprint from `gh auth token`

If the context cannot be reliably resolved for a given command, **the command is not cached**.

### Cacheable Command Allowlist

Only explicitly allowlisted commands are cached. Everything else passes through directly to `gh`.

**Phase 1 allowlist:**

| Command Pattern              | Notes                                    |
|------------------------------|------------------------------------------|
| `gh pr list`                 | With any filter/format flags             |
| `gh pr view <number>`        | Including `--json`, `--comments`         |
| `gh pr status`               |                                          |
| `gh pr checks <number>`      |                                          |
| `gh pr diff <number>`        |                                          |
| `gh issue list`              | With any filter/format flags             |
| `gh issue view <number>`     | Including `--json`, `--comments`         |
| `gh issue status`            |                                          |
| `gh repo view [repo]`        |                                          |
| `gh run list`                |                                          |
| `gh run view <id>`           |                                          |
| `gh workflow list`           |                                          |
| `gh workflow view <id>`      |                                          |
| `gh release list`            |                                          |
| `gh release view <tag>`      |                                          |
| `gh search repos <query>`    |                                          |
| `gh search issues <query>`   |                                          |
| `gh search prs <query>`      |                                          |
| `gh search commits <query>`  |                                          |
| `gh search code <query>`     |                                          |
| `gh api <GET endpoint>`      | REST GET only; GraphQL via opt-in config |
| `gh label list`              |                                          |
| `gh gist list`               |                                          |
| `gh gist view <id>`          |                                          |
| `gh project list`            |                                          |
| `gh project view <number>`   |                                          |
| `gh cache list`              | Actions cache                            |
| `gh ruleset list`            |                                          |
| `gh ruleset view <id>`       |                                          |
| `gh ruleset check`           |                                          |
| `gh repo list`               |                                          |
| `gh org list`                |                                          |

The allowlist is configurable — users can add custom commands via `additional_cacheable` in `~/.ghx/config.yaml`.

### Commands That Are NEVER Cached

- Any mutating command (`create`, `edit`, `delete`, `merge`, `close`, `reopen`, `comment`, `review`)
- Interactive commands (anything that prompts for input or launches a browser)
- Streaming commands (`gh run watch`, `gh codespace ssh`)
- Auth commands (`gh auth`)
- `gh api` with methods other than GET (unless explicitly opted in for read-only GraphQL)

These commands are **passed through directly** to `gh` with no daemon involvement, preserving TTY, stdin, and interactive behavior.

### Request Coalescing (Singleflight)

When multiple clients request the same cache key simultaneously and it's a miss, only **one** `gh` execution occurs. All waiting clients receive the same result. This is critical for the multi-agent scenario where 5+ agents may request the same PR list within milliseconds.

```
Agent1 → ghxd: "gh pr list" (cache miss, starts gh execution)
Agent2 → ghxd: "gh pr list" (same key, joins in-flight request)
Agent3 → ghxd: "gh pr list" (same key, joins in-flight request)
           ↓
       gh pr list  ← single execution
           ↓
All three agents get the same response
```

### Cache Invalidation

**Coarse-grained, namespace-based invalidation.** After any mutating command passes through the daemon, all cached entries for the same `{host, repo, resource_type}` namespace are invalidated.

Resource types: `pr`, `issue`, `run`, `workflow`, `release`, `label`, `repo`, `api`.

Example: After `gh pr merge #42`, all cached entries tagged `{github.com, owner/repo, pr}` are evicted.

This is intentionally conservative — it may over-invalidate, but it will never serve stale data after mutations.

### Cached Response Format

Each cached entry stores:

```go
type CachedResponse struct {
    Stdout   []byte
    Stderr   []byte
    ExitCode int
    CachedAt time.Time
    TTL      time.Duration
    Key      string
    Context  ExecutionContext
}
```

The client reproduces the exact observed behavior: writing stdout, stderr, and returning the original exit code.

## CLI Interface

### Drop-in Usage

```bash
# Instead of:
gh pr list --repo owner/repo --json number,title

# Use:
ghx pr list --repo owner/repo --json number,title
```

### Daemon Management

```bash
ghx xdaemon start          # Start daemon (foreground, for debugging)
ghx xdaemon start -d       # Start daemon (background, detached)
ghx xdaemon stop           # Graceful shutdown
ghx xdaemon status         # Show PID, uptime, cache stats summary
ghx xdaemon restart        # Stop + start
```

### Cache Management

```bash
ghx xcache flush           # Flush all cached entries
ghx xcache stats           # Show hit rate, per-command breakdown
ghx xcache keys            # List currently cached keys (for debugging)
```

### GitHub CLI Management

```bash
ghx ghcli status             # Show which gh binary is in use, version, and age
ghx ghcli upgrade            # Download the latest GitHub CLI to ~/.ghx/bin/gh
```

### Per-Command Overrides

```bash
ghx --no-cache pr list            # Bypass cache for this call
ghx --ttl 120 pr list             # Override TTL for this call
GHX_TTL=120 ghx pr list           # Same, via env var
GHX_NO_CACHE=1 ghx pr list        # Same as --no-cache, via env var
```

## Daemon Details

### Auto-Start

When `ghx` is invoked and no daemon is running, it **automatically starts one** in the background. This makes `ghx` a true drop-in replacement — no setup required.

### IPC Transport

- **Unix (macOS/Linux)**: Unix domain socket at `~/.ghx/ghxd.sock`, permissions `0600` (owner-only)
- **Windows**: Named pipe at `\\.\pipe\ghxd` (via `go-winio`)
- **Protocol**: Length-prefixed JSON messages (4-byte big-endian size header, 10MB max)

The client sends a `Request` containing the command args, resolved execution context, and the client's working directory (`WorkDir`). The daemon responds with the cached or fresh result.

### PID File

- Path: `~/.ghx/ghxd.pid`
- Used for daemon lifecycle management and stale process detection

### Graceful Shutdown

On SIGTERM/SIGINT: stop accepting new connections, wait for in-flight requests (up to 5s), then exit. Metrics are flushed to disk before exit.

### In-Memory Cache

Cache is in-memory only. Lost on daemon restart. This is acceptable because:
- TTLs are short (30s default)
- Daemon restarts are rare
- Disk-based caching adds complexity without proportional benefit for this use case

### LRU Eviction

Default max entries: 1000 (configurable). When exceeded, least-recently-used entries are evicted. Each entry is relatively small (a few KB of JSON output).

## Metrics & Observability

### Tracked Metrics

The daemon tracks the following internally, exposed via `ghx xcache stats` and the web dashboard:

- **Total requests** (and per-command)
- **Cache hits / misses / passthrough** (counts and percentages)
- **Coalesced requests** (served via singleflight)
- **Cache size** (current entries vs max)
- **Cache evictions and invalidations**
- **Response latency** (cached vs uncached, per-command)
- **Inter-request intervals** (per cache key, for TTL analysis)

### CLI Stats

```bash
$ ghx xcache stats
Uptime:          2h 34m
Total Requests:  1,247
Cache Hits:      891 (71.4%)
Cache Misses:    203 (16.3%)
Passthrough:     153 (12.3%)
Coalesced:       87
Cache Size:      142 / 1000 entries

Top Commands:
  gh pr list           412 hits / 48 misses  (89.6%)
  gh issue view        198 hits / 32 misses  (86.1%)
  gh pr view           143 hits / 67 misses  (68.1%)
  gh api /repos/...     88 hits / 31 misses  (73.9%)
  gh run list           50 hits / 25 misses  (66.7%)
```

### Web Dashboard

Served by the daemon at `http://localhost:9847/` — always available when the daemon is running.

A single self-contained HTML page (embedded in the Go binary via `embed`) with no external dependencies. Uses vanilla JS and inline CSS. Auto-refreshes metrics via a simple JSON endpoint (`GET /api/stats`).

**Dashboard sections:**

1. **Overview** — real-time hit rate %, total requests, cache size, daemon uptime
2. **Per-Command Breakdown** — sortable table showing each command with hit count, miss count, hit rate, and average latency
3. **Request Log** — scrollable live tail of recent requests (last 200) showing timestamp, command, cache hit/miss, and latency
4. **TTL Analysis** — for each command, shows the median time between repeated identical requests, helping users pick the right TTL

**JSON API** (consumed by the dashboard, also useful for scripting):

```
GET /api/stats          → { uptime, total, hits, misses, passthrough, coalesced, cache_size, commands: [...] }
GET /api/log?limit=200  → [ { timestamp, command, cache_result, latency_ms }, ... ]
```

## Configuration File

Location: `~/.ghx/config.yaml` (Unix) or `%LOCALAPPDATA%\ghx\config.yaml` (Windows)

```yaml
# Default TTL for all cached commands
ttl: 30s

# Per-command TTL overrides
ttl_overrides:
  pr_list: 60s
  pr_view: 30s
  issue_list: 60s
  run_list: 15s
  api_get: 30s

# Cache limits
max_cache_entries: 1000

# Daemon settings
socket_path: ~/.ghx/ghxd.sock
pid_file: ~/.ghx/ghxd.pid
auto_start: true

# Custom allowlist additions
additional_cacheable:
  - "gh status"

# Dashboard port (0 to disable)
dashboard_port: 9847

# gh binary path (auto-resolved if not set)
# Resolution: this setting → PATH → ~/.ghx/bin/gh → auto-download
gh_path: /opt/homebrew/bin/gh

# Logging
log_file: ~/.ghx/ghxd.log
```

## GitHub CLI Resolution and Auto-Download

`ghx` does not require the GitHub CLI (`gh`) to be pre-installed. When `gh` is needed, it is resolved using the `src/internal/ghcli` package with the following priority:

### Resolution Order

1. **User override** — `gh_path` in config or `GHX_GH_PATH` env var (if not the default `"gh"`)
2. **PATH scan** — search `PATH` for a real `gh` binary, skipping ghx shims
3. **Managed location** — `~/.ghx/bin/gh` (previously auto-downloaded)
4. **Auto-download** — download from `cli/cli` GitHub releases to `~/.ghx/bin/gh`

### `gh` Shim

When no real GitHub CLI (`gh`) binary is found on the system, every installation method places a `gh` shim alongside the `ghx` binary. The shim contains a `# ghx-shim` marker comment for detection:

```sh
#!/bin/sh
# ghx-shim: this script redirects gh commands through ghx for caching
exec ghx "$@"
```

**Distribution across install channels:**

| Channel | Behavior |
|---|---|
| **Release tarball** | Shim included in the tarball alongside `ghx` and `ghxd` |
| **install.sh** | Installs the shim only if no real `gh` binary is found anywhere on the system PATH |
| **install.ps1** | Windows installer; installs the shim only if no real `gh` binary is found |
| **Homebrew formula** | Installs the shim during `install` (not `post_install`) only if no `gh` binary is found on the system |
| **Agent plugin** | `bin/gh` wrapper delegates to the co-located `ghx` wrapper; plugin install script installs the shim only if no real `gh` binary is found on the system |

**Shim detection** uses three strategies to prevent infinite recursion:
1. **Symlink resolution** — `filepath.EvalSymlinks` to see if `gh` resolves to the same file as `ghx`
2. **Inode comparison** — `os.SameFile` to catch hardlinks
3. **Header marker** — read first 512 bytes and check for `# ghx-shim` (only for text files; binary magic bytes are checked to skip compiled executables)

### Auto-Download

When no `gh` is available, `ghx` downloads it from [cli/cli releases](https://github.com/cli/cli/releases):

1. Fetch latest version from the GitHub Releases API
2. Determine the correct asset name based on OS (`macOS`, `linux`, `windows`) and architecture (`amd64`, `arm64`)
3. Download the release checksums file and verify SHA-256 of the archive
4. Extract the `gh` binary from the archive (`.tar.gz` for Linux, `.zip` for macOS and Windows)
5. Atomically install to `~/.ghx/bin/gh` (temp file + rename)

**Safety measures:**
- **Checksum verification** — SHA-256 hash is verified against the official release checksums
- **Lock file** — prevents concurrent download from multiple `ghx` processes
- **Archive validation** — rejects symlinks and path traversal entries during extraction
- **Stale lock detection** — lock files older than 5 minutes are considered stale and removed

### Lazy Resolution

Resolution is **lazy** — it only happens on code paths that actually need `gh`:
- In `ghx`: triggered before `execDirect()` or `execctx.Resolve()` calls (not for `xversion`, `xhelp`, `xdaemon`, `xcache`)
- In `ghxd`: triggered once at daemon startup before accepting connections

This ensures commands like `ghx xversion` work instantly even offline.

### Staleness Warning and Upgrade

The managed `gh` binary at `~/.ghx/bin/gh` does not auto-update. Instead:

- **Staleness warning**: When `ghx` resolves the managed binary and it is older than 30 days (by file modification time), a one-line warning is printed to stderr: `ghx: managed gh binary is N days old — run 'ghx ghcli upgrade' to update`. This does not block execution.
- **Explicit upgrade**: `ghx ghcli upgrade` force-downloads the latest release, replacing the existing managed binary. It uses the same checksum-verified, lock-protected download path as the initial install.
- **Status**: `ghx ghcli status` shows the resolved `gh` path, source (config override / PATH / managed), version, and age.

Staleness checks only apply to the managed binary. If `gh` was found in PATH or via an explicit config override, no staleness warning is shown.

## Security Considerations

1. **IPC permissions**: Unix socket is created with `0600` — only the owning user can connect. On Windows, named pipe uses default security descriptors
2. **No token storage**: The daemon never stores auth tokens; it delegates to `gh` which manages its own auth. Only a SHA256 fingerprint of the token is used in cache keys
3. **No cross-user sharing**: Each user runs their own daemon with their own cache
4. **Cached data**: Responses may contain private repo data. The in-memory cache is ephemeral and protected by socket permissions
5. **Dashboard binding**: Metrics/dashboard HTTP server binds to `127.0.0.1` only
6. **Log hygiene**: Command arguments are logged, but response bodies are not logged by default

## Error Handling

- **Daemon unavailable**: Auto-start the daemon, then retry. If auto-start itself fails (e.g., socket conflict, permissions), fall back to executing `gh` directly (never block the user)
- **`gh` execution error**: Cache the error response too (exit code, stderr) for the TTL duration to avoid hammering a failing endpoint
- **Socket timeout**: 5-second timeout on client→daemon communication; fall back to direct `gh` on timeout
- **Cache corruption**: In-memory only, so restart the daemon to clear

## Project Structure

```
ghx/
├── src/
│   ├── cmd/
│   │   ├── ghx/                # CLI client entry point
│   │   │   ├── main.go
│   │   │   ├── proc_unix.go    # Unix process management (build-tagged)
│   │   │   └── proc_windows.go # Windows process management (build-tagged)
│   │   └── ghxd/               # Daemon entry point
│   │       └── main.go
│   └── internal/
│       ├── allowlist/           # Command classification
│       │   ├── allowlist.go
│       │   └── allowlist_test.go
│       ├── cache/               # LRU cache with TTL
│       │   ├── cache.go
│       │   └── cache_test.go
│       ├── client/              # IPC client
│       │   └── client.go
│       ├── config/              # Configuration loading
│       │   ├── config.go
│       │   ├── config_test.go
│       │   ├── dir_unix.go      # Unix default paths (build-tagged)
│       │   └── dir_windows.go   # Windows default paths (build-tagged)
│       ├── context/             # Execution context resolution
│       │   └── resolve.go
│       ├── daemon/              # Daemon server, request handling
│       │   ├── server.go
│       │   ├── handler.go       # Includes inline singleflight coalescing
│       │   ├── platform_unix.go
│       │   └── platform_windows.go
│       ├── dashboard/           # Web dashboard (embedded HTML)
│       │   ├── dashboard.go
│       │   └── static/
│       ├── executor/            # gh command execution
│       │   ├── executor.go
│       │   └── executor_test.go
│       ├── ghcli/               # gh binary resolution and auto-download
│       │   ├── resolve.go
│       │   ├── resolve_test.go
│       │   ├── shim.go
│       │   ├── shim_test.go
│       │   ├── download.go
│       │   └── download_test.go
│       ├── ipc/                 # Platform-specific IPC transport
│       │   ├── ipc_unix.go      # Unix domain sockets (build-tagged)
│       │   └── ipc_windows.go   # Named pipes via go-winio (build-tagged)
│       ├── metrics/             # Counters, stats, JSON API
│       │   ├── metrics.go
│       │   └── metrics_test.go
│       └── protocol/            # Length-prefixed JSON IPC protocol
│           └── protocol.go
├── agent-plugin/            # Claude Code / Copilot CLI plugin
│   ├── bin/                 # Shell wrapper scripts (lazy-install)
│   ├── scripts/             # Install scripts (OS/arch auto-detect)
│   └── skills/              # Agent skill definitions
├── docs/                    # GitHub Pages site
│   └── index.html
├── specs/                   # Project specs & docs
│   ├── ADR.md
│   ├── DOCS.md
│   └── SPEC.md
├── install.sh               # Unix installer (curl | sh)
├── install.ps1              # Windows installer (irm | iex)
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## Phased Delivery

### Phase 1 — Core Caching + Dashboard ✅

- `ghx` client and `ghxd` daemon
- IPC via Unix domain sockets (macOS/Linux) and named pipes (Windows)
- Allowlisted command caching with context-aware keys
- Configurable TTL (default 30s)
- Singleflight request coalescing (inline in handler)
- Coarse-grained invalidation after mutations
- Auto-start daemon, fallback to direct `gh` on failure
- `ghx xcache stats` for CLI metrics
- `ghx xdaemon start/stop/status`
- Config file support
- Web dashboard with per-command stats, request log, and TTL analysis
- JSON API for scripting
- Windows support (named pipes, PowerShell installer)
- `gh` shim for systems without GitHub CLI
- `gh` auto-download and resolution
- Agent plugin for Claude Code / Copilot CLI
- Client working directory forwarding to daemon

### Phase 2 — Advanced Features

- Negative caching configuration (cache 404s, rate limit responses)
- Cache warming (pre-fetch commonly used commands on daemon start)
- `gh` extension integration (`gh cache` as a native extension)
- Version pinning for auto-downloaded `gh` binary (`gh_version` config option)
