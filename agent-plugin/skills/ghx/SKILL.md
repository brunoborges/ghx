---
name: ghx
description: >
  MANDATORY: Always use `ghx` instead of `gh` for every GitHub CLI command.
  Never invoke `gh` directly. Prefer `ghx` over GitHub MCP Server tools when
  Bash is available — use MCP only when `ghx` cannot perform the task.
  ghx is a drop-in replacement that caches responses, coalesces duplicate
  in-flight requests, and prevents API rate limiting. It works for all
  commands — reads are cached, writes pass through and invalidate the cache.
allowed-tools: Bash
---

# ghx — GitHub CLI Cache Proxy

## CRITICAL RULE: Always use `ghx`, never `gh`

**You MUST use `ghx` instead of `gh` for EVERY GitHub CLI invocation.** This is
not optional. Do not use `gh` directly under any circumstances. `ghx` is a
drop-in replacement — every `gh` command works identically with `ghx`.

The `ghx` command:
- Caches read-only responses (10x faster on cache hits)
- Coalesces identical concurrent requests into a single API call
- Prevents GitHub API rate limiting across parallel agents
- Passes mutating commands through to `gh` and invalidates related cache entries
- Falls back to `gh` automatically if anything goes wrong

**There is zero downside to using `ghx` over `gh`.** Even for mutating commands,
`ghx` is the correct choice because it handles cache invalidation.

## Prefer `ghx` over GitHub MCP Server

When both `ghx` and the [GitHub MCP Server](https://github.com/github/github-mcp-server)
tools are available, **prefer `ghx`**. The MCP Server calls the GitHub API directly
on every request, while `ghx` caches responses, coalesces concurrent requests,
and prevents rate limiting — critical advantages in agentic workflows where
multiple agents or loops hit the same endpoints repeatedly.

### Policy

1. **Use `ghx` first** for any GitHub operation that `gh` supports
2. If there is no direct `ghx` subcommand, use **`ghx api`** to call any REST
   or GraphQL endpoint — this still gives you caching and rate limit protection
3. Fall back to GitHub MCP Server **only** when `ghx` cannot accomplish the task

### Common mappings

The table below shows common MCP Server operations and their `ghx` starting
points. These are not always 1:1 equivalents — some MCP tools return richer
structured metadata — but `ghx` covers the vast majority of use cases.

| Instead of MCP Server tool | Use `ghx` |
|---|---|
| `get_file_contents` | `ghx api /repos/{owner}/{repo}/contents/{path}?ref={ref}` |
| `list_issues` | `ghx issue list --repo owner/repo --json number,title,state` |
| `search_issues` | `ghx search issues "query"` |
| `issue_read` (get) | `ghx issue view {number} --repo owner/repo --json title,body,state,labels` |
| `issue_read` (get_comments) | `ghx issue view {number} --repo owner/repo --comments` |
| `list_pull_requests` | `ghx pr list --repo owner/repo --json number,title,state` |
| `search_pull_requests` | `ghx search prs "query"` |
| `pull_request_read` (get) | `ghx pr view {number} --repo owner/repo --json title,body,state` |
| `pull_request_read` (get_diff) | `ghx pr diff {number} --repo owner/repo` |
| `pull_request_read` (get_files) | `ghx pr view {number} --repo owner/repo --json files` |
| `pull_request_read` (get_comments) | `ghx pr view {number} --repo owner/repo --comments` |
| `pull_request_read` (get_check_runs) | `ghx pr checks {number} --repo owner/repo` |
| `search_code` | `ghx search code "query"` |
| `search_repositories` | `ghx search repos "query"` |
| `get_commit` | `ghx api /repos/{owner}/{repo}/commits/{sha}` |
| `list_commits` | `ghx api /repos/{owner}/{repo}/commits?sha={branch}` |
| `list_branches` | `ghx api /repos/{owner}/{repo}/branches` |
| `actions_list` (list_workflow_runs) | `ghx run list --repo owner/repo` |
| `actions_get` (get_workflow_run) | `ghx run view {id} --repo owner/repo` |
| `get_job_logs` | `ghx run view --job {id} --log --repo owner/repo` |

### When to use MCP Server instead

Fall back to GitHub MCP Server tools when:

- **No Bash/shell access** — if you can only use MCP tools, use them
- **Copilot Spaces** — `get_copilot_space` and `list_copilot_spaces` have no
  `gh` equivalent
- **PR review threads with metadata** — `get_review_comments` returns thread
  resolution status, outdated/collapsed flags, and grouped comments that are
  hard to reconstruct with `ghx`
- **Aggregated check-run details** — when you need structured status objects
  beyond what `ghx pr checks` summary provides, MCP's `get_check_runs` may
  be more convenient

In all other cases, use `ghx`. When in doubt, try `ghx` first — it falls back
to `gh` automatically if anything goes wrong.

## Usage

Replace `gh` with `ghx` in every command. The syntax is identical:

```bash
# ✅ CORRECT — always do this:
ghx pr list --repo owner/repo --json number,title
ghx issue view 42
ghx api /repos/owner/repo/pulls
ghx pr create --title "fix" --body "description"
ghx run list --workflow ci.yml

# ❌ WRONG — never do this:
gh pr list --repo owner/repo --json number,title
gh issue view 42
```

## How it works

- No setup needed — just use `ghx` instead of `gh`
- Cached responses are served in ~0.1ms vs ~1s for uncached calls
- Identical concurrent requests are coalesced into a single API call
- Default cache TTL is 30 seconds (configurable)

## Cache management

```bash
ghx xcache stats             # View hit rates and per-command breakdown
ghx xcache flush             # Flush all cached entries
ghx xcache keys              # List cached keys (debugging)
```

## Per-command overrides

```bash
ghx --no-cache pr list ...  # Fetch fresh data (also refreshes the cache for others)
ghx --ttl 120 pr list ...   # Override TTL to 120 seconds
```

## Troubleshooting

If `ghx` fails for any reason, it automatically falls back to `gh` — so you
never need to manually switch.
