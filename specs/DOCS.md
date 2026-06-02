# ghx Documentation

> Full reference for installing, configuring, and using ghx.  
> For a quick overview, see the [README](README.md).

## Table of Contents

- [Install](#install)
  - [Homebrew (macOS)](#homebrew-macos--recommended)
  - [Quick install script (macOS / Linux)](#quick-install-script-macos--linux)
  - [PowerShell (Windows)](#powershell-windows)
  - [Manual download](#manual-download)
  - [Build from source](#build-from-source)
  - [Agents Plugin](#agents-plugin-claude-code--copilot-cli)
- [Using ghx Without Installing GitHub CLI](#using-ghx-without-installing-github-cli)
  - [How it works](#how-it-works)
  - [The `gh` shim](#the-gh-shim)
  - [Example first-run experience](#example-first-run-experience)
  - [Overriding the `gh` binary path](#overriding-the-gh-binary-path)
  - [Updating the managed `gh` binary](#updating-the-managed-gh-binary)
- [Usage](#usage)
  - [Cache behavior](#cache-behavior)
  - [Per-command options](#per-command-options)
- [Daemon Management](#daemon-management)
- [Cache Management](#cache-management)
- [Web Dashboard](#web-dashboard)
  - [JSON API](#json-api)
- [What Gets Cached](#what-gets-cached)
  - [Custom Commands](#custom-commands)
- [Configuration](#configuration)
- [Architecture](#architecture)
- [Security](#security)
- [Development](#development)

---

## Install

### Homebrew (macOS — recommended)

```bash
brew tap brunoborges/tap
brew install ghx
```

### Quick install script (macOS / Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/brunoborges/ghx/main/install.sh | bash
```

This detects your OS and architecture, downloads the latest release, and installs `ghx` and `ghxd` to `/usr/local/bin`. If no real `gh` binary is found on the system, a lightweight `gh` shim is also installed that routes all `gh` commands through `ghx` for automatic caching. To install elsewhere:

```bash
curl -fsSL https://raw.githubusercontent.com/brunoborges/ghx/main/install.sh | INSTALL_DIR=~/.local/bin bash
```

### PowerShell (Windows)

```powershell
irm https://raw.githubusercontent.com/brunoborges/ghx/main/install.ps1 | iex
```

This installs `ghx.exe` and `ghxd.exe` to `%LOCALAPPDATA%\ghx\bin` and adds it to your user PATH. If no real `gh.exe` is found, a `gh.cmd` shim is also installed.

### Manual download

Download the latest release for your platform from [GitHub Releases](https://github.com/brunoborges/ghx/releases):

```bash
# macOS (Apple Silicon)
curl -fsSL https://github.com/brunoborges/ghx/releases/latest/download/ghx-darwin-arm64.tar.gz | tar xz
sudo cp ghx ghxd /usr/local/bin/

# Linux (x64)
curl -fsSL https://github.com/brunoborges/ghx/releases/latest/download/ghx-linux-amd64.tar.gz | tar xz
sudo cp ghx ghxd /usr/local/bin/

# Linux (arm64)
curl -fsSL https://github.com/brunoborges/ghx/releases/latest/download/ghx-linux-arm64.tar.gz | tar xz
sudo cp ghx ghxd /usr/local/bin/
```

On Windows, download and extract with PowerShell:

```powershell
# Windows (x64)
Invoke-WebRequest https://github.com/brunoborges/ghx/releases/latest/download/ghx-windows-amd64.zip -OutFile ghx.zip
Expand-Archive ghx.zip -DestinationPath ghx
Copy-Item ghx\ghx.exe, ghx\ghxd.exe -Destination "$env:LOCALAPPDATA\Microsoft\WinGet\Packages"

# Windows (arm64)
Invoke-WebRequest https://github.com/brunoborges/ghx/releases/latest/download/ghx-windows-arm64.zip -OutFile ghx.zip
Expand-Archive ghx.zip -DestinationPath ghx
Copy-Item ghx\ghx.exe, ghx\ghxd.exe -Destination "$env:LOCALAPPDATA\Microsoft\WinGet\Packages"
```

Make sure the destination directory is on your `PATH`, or copy the binaries to any directory that is.

### Build from source

```bash
git clone https://github.com/brunoborges/ghx.git
cd ghxd
make build
# Binaries are in bin/ghx and bin/ghxd
sudo cp bin/ghx bin/ghxd /usr/local/bin/
```

### Agents Plugin (Claude Code & Copilot CLI)

If you use [Claude Code](https://code.claude.com/docs/en/plugins) or [GitHub Copilot CLI](https://docs.github.com/en/copilot/concepts/agents/copilot-cli/about-cli-plugins), install the plugin and your agent will automatically prefer `ghx` over `gh`:

```bash
# Add the marketplace (one-time)
/plugin marketplace add brunoborges/agent-plugins

# Install the plugin
/plugin install ghx@agent-plugins
```

> **Local development / testing:** `claude --plugin-dir ./agent-plugin`

The plugin:
- **Lazy-installs** `ghx` and `ghxd` binaries on first use
- **Adds `ghx` to PATH** so agents use it automatically
- **Includes a skill** that teaches agents to prefer `ghx` for all GitHub CLI calls

See the [plugin README](agent-plugin/README.md) for details. Plugin releases are available on the [Releases page](https://github.com/brunoborges/ghx/releases) with the `plugin-v*` tag.

## Using ghx Without Installing GitHub CLI

You don't need to install the GitHub CLI (`gh`) separately. When `ghx` needs the real `gh` binary and can't find one, it **automatically downloads** the latest release from [cli/cli](https://github.com/cli/cli/releases) to `~/.ghx/bin/gh`.

### How it works

When `ghx` or `ghxd` needs to execute a `gh` command, it resolves the real binary using this order:

1. **User override** — if `gh_path` is set in `~/.ghx/config.yaml` or via `GHX_GH_PATH` env var, use that path directly
2. **PATH scan** — search `PATH` for a real `gh` binary (skipping ghx shims)
3. **Managed location** — check `~/.ghx/bin/gh` (previously auto-downloaded)
4. **Auto-download** — download the latest GitHub CLI from [cli/cli releases](https://github.com/cli/cli/releases) to `~/.ghx/bin/gh`

The download includes **SHA-256 checksum verification** against the official release checksums.

### The `gh` shim

When no real GitHub CLI (`gh`) binary is found on the system, every installation method places a lightweight `gh` shim script alongside the `ghx` binary. This shim redirects all `gh` commands through `ghx`:

```sh
#!/bin/sh
# ghx-shim: this script redirects gh commands through ghx for caching
exec ghx "$@"
```

This means existing tools, scripts, and CI workflows that call `gh` will automatically benefit from caching — no changes needed. If a real `gh` binary is already available, the shim is skipped and you can use `ghx` directly.

> **Already have the GitHub CLI installed?** When a real `gh` binary is detected, the shim is not installed — so agents will keep calling `gh` directly, bypassing the cache. You have two options:
>
> - **Option A:** Remove the GitHub CLI and reinstall ghx. The shim will be created and agents automatically get caching via `gh`. ghx will download the real `gh` binary on first use.
> - **Option B:** Keep the GitHub CLI and install the [agent plugin](#agents-plugin-claude-code--copilot-cli) instead. The plugin teaches agents to call `ghx` directly instead of `gh`.

| Install method | Shim location | Notes |
|---|---|---|
| **install.sh** | `$INSTALL_DIR/gh` (default `/usr/local/bin/gh`) | Skipped if a real `gh` binary exists anywhere on the system |
| **install.ps1** | `%LOCALAPPDATA%\ghx\bin\gh.cmd` | Skipped if a real `gh.exe` exists on the system |
| **Homebrew** | `$(brew --prefix)/bin/gh` | Skipped if `gh` is already installed (via Homebrew or otherwise) |
| **Agent plugin** | Plugin `bin/` directory (on PATH) | Skipped if a real `gh` binary exists on the system |

### Example first-run experience

```
$ ghx pr list
ghx: GitHub CLI (gh) not found, downloading...
ghx: downloading GitHub CLI v2.74.0...
ghx: GitHub CLI v2.74.0 installed to /Users/you/.ghx/bin/gh
#1  My first PR  (main <- feature-branch)
```

Subsequent runs use the cached `gh` binary at `~/.ghx/bin/gh` — no re-download.

### Overriding the `gh` binary path

If you have `gh` installed in a non-standard location, point `ghx` at it:

```yaml
# ~/.ghx/config.yaml
gh_path: /opt/homebrew/bin/gh
```

Or via environment variable:

```bash
export GHX_GH_PATH=/opt/homebrew/bin/gh
```

### Updating the managed `gh` binary

When `ghx` auto-downloads `gh`, the binary is stored at `~/.ghx/bin/gh`. To upgrade it to the latest release:

```bash
ghx ghcli upgrade
```

If the managed binary is more than 30 days old, `ghx` prints a one-line reminder:

```
ghx: managed gh binary is 45 days old — run 'ghx ghcli upgrade' to update
```

To check which `gh` binary is in use:

```bash
$ ghx ghcli status
gh binary:  /Users/you/.ghx/bin/gh
source:     managed (~/.ghx/bin/gh)
version:    2.74.0
installed:  3 days ago
```

## Usage

Use `ghx` exactly like `gh` — the daemon starts automatically on first use:

```bash
# These are cached (read-only commands)
ghx pr list --repo owner/repo --json number,title
ghx issue view 42 --json title,state
ghx api /repos/owner/repo --jq '.stargazers_count'
ghx run list --repo owner/repo

# These pass through directly to gh (mutations)
ghx pr create --title "My PR" --body "Description"
ghx issue close 42
```

### Cache behavior

```
First call:   ghx pr list ...   → 1.1s (cache miss, calls gh)
Second call:  ghx pr list ...   → 0.1s (cache hit, instant)
After 30s:    ghx pr list ...   → 1.0s (TTL expired, fresh call)
```

### How caching works

#### Cache keys

Each cached response is stored under a SHA-256 key computed from:

- **Host** — the GitHub host (e.g., `github.com` or your GHES instance)
- **Repository** — `owner/repo`
- **Branch** — current Git branch
- **Auth token hash** — SHA-256 fingerprint of your token (the token itself is never stored)
- **Full command arguments** — every argument and flag, in order

This means the **same command with different flags produces different cache entries**:

```bash
ghx pr list --json number        # cache key A
ghx pr list --json number,title  # cache key B (different flags)
ghx pr list --json number        # cache hit on key A
```

And the same command in **different repos or branches** also gets separate entries — no cross-context collisions.

#### Singleflight coalescing

When multiple agents run the exact same command at the same time (same repo, branch, auth, and flags), ghx **coalesces them into a single `gh` execution**. Only one API call is made; all agents receive the same response.

```
Agent 1:  ghx pr list --json number  ─┐
Agent 2:  ghx pr list --json number  ─┼── 1 API call → response shared by all 3
Agent 3:  ghx pr list --json number  ─┘
```

This happens automatically — no configuration needed. The coalesced count is visible in `ghx xcache stats` and the web dashboard.

### Per-command options

```bash
ghx --no-cache pr list ...     # Bypass cache for this call
ghx --ttl 120 pr list ...      # Override TTL to 120 seconds
GHX_NO_CACHE=1 ghx pr list ... # Same via env var
GHX_TTL=60 ghx pr list ...     # Same via env var
```

## Daemon Management

```bash
ghx xdaemon start          # Start in foreground
ghx xdaemon start -d       # Start detached (background)
ghx xdaemon stop           # Graceful shutdown
ghx xdaemon status         # Show uptime and cache stats
ghx xdaemon restart        # Stop + start
```

The daemon auto-starts on first `ghx` call. If the daemon can't start, `ghx` falls back to running `gh` directly — it never blocks you.

## Cache Management

```bash
ghx xcache stats           # Show hit rates and per-command breakdown
ghx xcache flush           # Flush all entries
ghx xcache keys            # List cached keys (debugging)
```

### Example stats output

```
Uptime:          2h 34m
Total Requests:  1,247
Cache Hits:      891 (71.4%)
Cache Misses:    203 (16.3%)
Passthrough:     153 (12.3%)
Coalesced:       87
Cache Size:      142 / 1000 entries

Top Commands:
  pr list                  412 hits / 48 misses  (89.6%)
  issue view               198 hits / 32 misses  (86.1%)
  pr view                  143 hits / 67 misses  (68.1%)
  api get                   88 hits / 31 misses  (73.9%)
```

## Web Dashboard

When the daemon is running, a live dashboard is available at:

```
http://localhost:9847/
```

It shows:
- **Real-time hit rate** and request counters
- **Per-command breakdown** with hit/miss rates and average latency
- **Request log** — live tail of recent requests with cache result and timing

The dashboard auto-refreshes every 2 seconds. No external dependencies — it's a single HTML page embedded in the binary.

### JSON API

The dashboard data is also available as JSON for scripting:

```bash
curl http://localhost:9847/api/stats | jq .
curl http://localhost:9847/api/log?limit=50 | jq .
curl http://localhost:9847/api/ttl-analysis | jq .
```

## What Gets Cached

Only explicitly allowlisted read-only commands are cached:

| Command | Cached |
|---------|--------|
| `gh pr list/view/status/checks/diff` | ✅ |
| `gh issue list/view/status` | ✅ |
| `gh repo view/list` | ✅ |
| `gh run list/view` | ✅ |
| `gh workflow list/view` | ✅ |
| `gh release list/view` | ✅ |
| `gh search repos/issues/prs/commits/code` | ✅ |
| `gh api` (GET only) | ✅ |
| `gh label list` | ✅ |
| `gh gist list/view` | ✅ |
| `gh project list/view` | ✅ |
| `gh cache list` | ✅ |
| `gh ruleset list/view/check` | ✅ |
| `gh org list` | ✅ |
| `gh pr create/merge/close/edit` | ❌ (mutation → invalidates PR cache) |
| `gh issue create/edit/delete/close` | ❌ (mutation → invalidates issue cache) |
| `gh auth/config/codespace/secret` | ❌ (always passthrough) |

Mutations automatically invalidate related cache entries. For example, `gh pr merge 42` flushes all cached PR entries for that repo.

### Custom Commands

You can add your own commands to the allowlist via `~/.ghx/config.yaml`:

```yaml
additional_cacheable:
  - "gh status"
  - "gh variable list"
  - "gh secret list"
```

Each entry should be the full command prefix (e.g., `"gh status"` for a single-word subcommand, or `"gh variable list"` for two-word). Custom commands are classified with `ResourceUnknown` — they participate in caching but won't be invalidated by mutation detection. To apply changes, restart the daemon: `ghxd --restart`.

### TTL Override Keys

The `ttl_overrides` map in config uses **command keys** — normalized identifiers where spaces become underscores. For example, `gh pr list` → `pr_list`.

#### Built-in command keys

| Key | Command |
|-----|---------|
| `pr_list` | `gh pr list` |
| `pr_view` | `gh pr view` |
| `pr_status` | `gh pr status` |
| `pr_checks` | `gh pr checks` |
| `pr_diff` | `gh pr diff` |
| `issue_list` | `gh issue list` |
| `issue_view` | `gh issue view` |
| `issue_status` | `gh issue status` |
| `repo_view` | `gh repo view` |
| `repo_list` | `gh repo list` |
| `run_list` | `gh run list` |
| `run_view` | `gh run view` |
| `workflow_list` | `gh workflow list` |
| `workflow_view` | `gh workflow view` |
| `release_list` | `gh release list` |
| `release_view` | `gh release view` |
| `search_repos` | `gh search repos` |
| `search_issues` | `gh search issues` |
| `search_prs` | `gh search prs` |
| `search_commits` | `gh search commits` |
| `search_code` | `gh search code` |
| `label_list` | `gh label list` |
| `gist_list` | `gh gist list` |
| `gist_view` | `gh gist view` |
| `project_list` | `gh project list` |
| `project_view` | `gh project view` |
| `project_field-list` | `gh project field-list` |
| `project_item-list` | `gh project item-list` |
| `cache_list` | `gh cache list` |
| `ruleset_list` | `gh ruleset list` |
| `ruleset_view` | `gh ruleset view` |
| `ruleset_check` | `gh ruleset check` |
| `org_list` | `gh org list` |
| `secret_list` | `gh secret list` |
| `variable_list` | `gh variable list` |
| `variable_get` | `gh variable get` |
| `api_get` | `gh api` (GET requests) |

#### Custom command keys

Commands added via `additional_cacheable` follow the same rule — strip the `gh` prefix and replace spaces with underscores:

| `additional_cacheable` entry | TTL override key |
|------------------------------|-----------------|
| `"gh status"` | `status` |
| `"gh variable list"` | `variable_list` |
| `"gh codespace list"` | `codespace_list` |

Example combining both:

```yaml
additional_cacheable:
  - "gh status"
  - "gh codespace list"

ttl_overrides:
  status: 120s
  codespace_list: 60s
  pr_list: 90s
```

## Configuration

Configuration file: `~/.ghx/config.yaml`

```yaml
# Default TTL for all cached commands (default: 30s)
ttl: 30s

# Per-command TTL overrides (see full key list below)
ttl_overrides:
  pr_list: 60s
  pr_view: 30s
  issue_list: 60s
  run_list: 15s

# Max cached entries before LRU eviction (default: 1000)
max_cache_entries: 1000

# Dashboard HTTP port (default: 9847)
dashboard_port: 9847

# Auto-start daemon on first ghx call (default: true)
auto_start: true

# Additional commands to cache
additional_cacheable:
  - "gh status"

# Path to gh binary (default: auto-resolved)
# Resolution order: this setting → PATH → ~/.ghx/bin/gh → auto-download
# gh_path: /usr/local/bin/gh
```

## Architecture

```
┌─────────┐  ┌─────────┐  ┌─────────┐
│ Agent 1 │  │ Agent 2 │  │ Agent 3 │
│ (ghx)   │  │ (ghx)   │  │ (ghx)   │
└────┬────┘  └────┬────┘  └────┬────┘
     │            │            │
     └────────────┼────────────┘
                  │ Unix Domain Socket
           ┌──────┴──────┐
           │    ghxd     │
           │  (daemon)   │
           ├─────────────┤
           │ Cache (LRU) │
           │ Singleflight│
           │ Metrics     │
           │ Dashboard   │
           └──────┬──────┘
                  │ exec
              ┌───┴───┐
              │  gh   │
              └───────┘
```

**Key design decisions:**
- **Allowlist, not denylist** — only known-safe commands are cached
- **Context-aware cache keys** — includes repo, branch, host, and auth token hash to prevent cross-context collisions
- **Singleflight** — concurrent identical requests share a single `gh` execution
- **Coarse invalidation** — mutations flush the entire resource namespace (all PR cache for that repo)
- **Graceful fallback** — if daemon is down or fails, `ghx` runs `gh` directly
- **Self-contained** — auto-downloads `gh` if not installed; no external dependencies beyond a network connection

## Security

- Unix socket with `0600` permissions (owner-only access)
- Auth tokens are never stored — only a SHA256 fingerprint is used in cache keys
- Dashboard binds to `127.0.0.1` only (not accessible from network)
- In-memory cache only (lost on daemon restart)
- Each user runs their own isolated daemon

## Development

```bash
# Build
make build

# Run tests
make test

# Clean
make clean
```

## License

[MIT](LICENSE)
