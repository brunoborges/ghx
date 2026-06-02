# ghx — GitHub CLI Cache Proxy

[![CI](https://github.com/brunoborges/ghx/actions/workflows/ci.yml/badge.svg)](https://github.com/brunoborges/ghx/actions/workflows/ci.yml)
[![Release](https://github.com/brunoborges/ghx/actions/workflows/release.yml/badge.svg)](https://github.com/brunoborges/ghx/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/brunoborges/ghx)](https://goreportcard.com/report/github.com/brunoborges/ghx)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

<p align="center">
  <img src="ghx-dashboard.png" alt="ghx Dashboard" width="700">
</p>

A caching proxy for the [GitHub CLI (`gh`)](https://cli.github.com/) that eliminates redundant API calls, prevents rate limiting, and dramatically speeds up repeated commands.

**Built for AI agent workflows** where multiple agents (Copilot CLI, coding agents, MCP servers) hammer the same `gh` commands simultaneously.

## Highlights

- 🚀 **10x faster** cached responses (~0.1s vs ~1s)
- 🔄 **Singleflight coalescing** — 5 agents asking the same thing = 1 API call
- 🎯 **Allowlist-based** — only caches known-safe read-only commands
- 🧹 **Auto-invalidation** — mutations flush related cache entries
- 📊 **Web dashboard** — real-time hit rates, per-command stats, request log
- 🔌 **Drop-in replacement** — just use `ghx` instead of `gh`
- 📦 **No `gh` required** — auto-downloads GitHub CLI on first use if not installed

## Quick Start

### Install

```bash
# macOS (Homebrew)
brew tap brunoborges/tap && brew install ghx

# macOS / Linux (script)
curl -fsSL https://raw.githubusercontent.com/brunoborges/ghx/main/install.sh | bash

# Windows (PowerShell)
irm https://raw.githubusercontent.com/brunoborges/ghx/main/install.ps1 | iex
```

See the [full documentation](specs/DOCS.md#install) for manual download, building from source, and agent plugin setup.

### Usage

Use `ghx` exactly like `gh` — the daemon starts automatically on first use:

```bash
ghx pr list --repo owner/repo --json number,title   # cached
ghx issue view 42 --json title,state                 # cached
ghx pr create --title "My PR" --body "Description"   # mutation — passes through
```

```
First call:   ghx pr list ...   → 1.1s (cache miss, calls gh)
Second call:  ghx pr list ...   → 0.1s (cache hit, instant)
```

## Documentation

📖 **[Full documentation](specs/DOCS.md)** — install options, configuration, daemon & cache management, what gets cached, architecture, and more.

## License

[MIT](LICENSE)
