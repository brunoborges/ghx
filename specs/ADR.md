# Architecture Decision Records

## ADR-000: Daemon/client architecture with allowlist-based caching

**Date:** 2026-04-15
**Status:** Accepted

### Context

Multiple AI agents (Copilot CLI, Claude Code, etc.) running in parallel frequently invoke the GitHub CLI (`gh`) to inspect PRs, issues, runs, and repository state. Each invocation makes one or more HTTP requests to the GitHub API, which enforces rate limits (5,000 requests/hour for authenticated users). With 5+ agents polling the same endpoints, rate limits are exhausted within minutes, causing failures and degraded workflows.

### Decision

Build a **two-binary daemon/client system** (`ghx` + `ghxd`) that acts as a caching proxy for `gh`:

- **`ghx`** (client) — Drop-in replacement for `gh`. Resolves execution context (repo, branch, token), classifies the command, and sends it to the daemon over IPC. Falls back to direct `gh` execution if the daemon is unavailable.
- **`ghxd`** (daemon) — Long-lived background process that receives commands, checks a local cache, executes `gh` on cache misses, and stores results. Serves a web dashboard for observability.

### Key design choices

**Allowlist, not denylist:** Only explicitly listed read-only commands are cached. Unknown commands pass through to `gh` unmodified. This is safer than trying to detect mutations — a missed mutation in a denylist would serve stale data after a state change.

**Context-aware cache keys:** Keys are SHA-256 hashes of `host + repo + branch + token_hash + args`. The same command in different repos, branches, or auth contexts gets separate cache entries. This prevents cross-contamination in multi-repo workflows.

**Singleflight coalescing:** When multiple agents request the same command simultaneously and it's a cache miss, only one `gh` execution occurs. All waiting clients receive the same result. This is the highest-value optimization for the multi-agent scenario.

**Mutation-triggered invalidation:** Mutating commands (`pr create`, `issue close`, etc.) are detected and passed through to `gh`. They also flush all cache entries for the corresponding resource type and repository, ensuring subsequent reads see fresh data.

**Auto-start daemon:** The client automatically starts the daemon on first invocation if it's not running. No manual setup required — `ghx` is truly a drop-in replacement.

**Graceful degradation:** If the daemon crashes or is unavailable, `ghx` falls back to executing `gh` directly. The user never sees a failure caused by the caching layer itself.

### Alternatives considered

- **In-process caching (no daemon):** Each `ghx` invocation would maintain its own cache. Rejected because there's no shared state between agents — every agent would still make its own API calls, defeating the purpose.
- **Shared file-based cache:** Write cached responses to disk, share via filesystem. Rejected due to file locking complexity, no singleflight coalescing, and race conditions between concurrent writers.
- **HTTP-layer proxy:** Intercept at the network level. Rejected — see ADR-001.

### Consequences

- Requires two binaries (`ghx` and `ghxd`) instead of one
- IPC transport is platform-specific (Unix sockets on macOS/Linux, named pipes on Windows)
- New `gh` commands require allowlist updates to be cached (mitigated by `additional_cacheable` config)
- Cache observability is built-in via the web dashboard, not bolted on after the fact

## ADR-001: Command-layer caching over HTTP-layer caching

**Date:** 2026-04-15
**Status:** Accepted

### Context

ghx needs to cache GitHub API responses to prevent rate limiting when multiple AI agents run concurrent `gh` commands. Two architectural approaches were evaluated:

1. **Command-layer proxy** — Wrap the `gh` binary, intercept at the CLI argument level, cache stdout/stderr by command signature.
2. **HTTP-layer proxy** — Intercept GitHub API traffic at the network level, cache HTTP responses by URL/headers.

### Analysis of the HTTP-layer approach

All `gh` CLI traffic goes to a single domain (`api.github.com`) over HTTPS (port 443), using both REST and GraphQL endpoints. An HTTP-layer cache would need to inspect request and response bodies to be useful.

**TLS interception (MITM proxy):**
- Requires generating a local CA certificate and installing it in the OS trust store
- Trust store installation differs across macOS (Keychain), Linux (ca-certificates/update-ca-trust), and Windows (certutil)
- `gh` uses Go's `net/http` which respects system roots, but other tools or hardened environments may pin certificates
- Introduces a real security surface — a local CA that can sign arbitrary certificates
- Users and security teams are rightly suspicious of MITM proxies

**Transparent reverse proxy (rewrite host):**
- Run a local HTTP server and redirect `gh` to it via config or env vars
- Avoids TLS interception since the local hop is plaintext
- However, `gh` forces HTTPS when constructing API URLs — there is no clean mechanism to override the scheme to HTTP for a local endpoint
- `GH_HOST` changes the hostname but not the protocol
- Would require patching `gh` or maintaining a fork

**`HTTPS_PROXY` env var:**
- `gh` respects `HTTPS_PROXY`, but Go's HTTP client uses HTTP CONNECT tunneling for HTTPS targets
- The proxy sees only the destination hostname, not request/response bodies
- Useless for caching without TLS interception

### Decision

Use the **command-layer proxy** approach.

### Rationale

- **No TLS complexity** — Caching happens above the network layer. No certificates, no trust stores, no MITM.
- **Cross-platform simplicity** — Works identically on macOS, Linux, and Windows. The only platform-specific concern is IPC transport (Unix sockets vs named pipes), which is far simpler than managing OS trust stores.
- **Richer cache semantics** — Command-layer classification enables allowlisting by command type, mutation detection with targeted invalidation, and singleflight coalescing by command signature. An HTTP proxy would need to reverse-engineer GraphQL query bodies to achieve the same granularity.
- **Drop-in replacement UX** — Users replace `gh` with `ghx`. No proxy configuration, no environment variables, no certificate installation.
- **Graceful fallback** — If the daemon is down, `ghx` falls back to direct `gh` execution. An HTTP proxy failure would break all API traffic.

### Consequences

- ghx only caches `gh` CLI invocations, not arbitrary HTTP calls to the GitHub API from other tools (curl, libraries, etc.)
- Adding new cacheable commands requires updating the allowlist (though `additional_cacheable` config provides an escape hatch)
- The cache key is based on CLI arguments, not the underlying HTTP request — two different `gh` commands that produce the same API call are cached separately
