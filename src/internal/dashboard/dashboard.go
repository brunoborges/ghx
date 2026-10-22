package dashboard

import (
	"net/http"
)

// Handler returns an HTTP handler that serves the dashboard HTML page.
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(dashboardHTML))
	}
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ghx Dashboard</title>
<style>
  :root {
    --bg: #0d1117; --surface: #161b22; --border: #30363d;
    --text: #e6edf3; --text-dim: #8b949e; --accent: #58a6ff;
    --green: #3fb950; --red: #f85149; --yellow: #d29922; --blue: #58a6ff;
  }
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif;
    background: var(--bg); color: var(--text); padding: 24px; line-height: 1.5;
  }
  h1 { font-size: 24px; margin-bottom: 4px; }
  h1 span { color: var(--accent); }
  h1 .version-badge { font-size: 12px; font-weight: 500; color: var(--text-dim); border: 1px solid var(--border); border-radius: 10px; padding: 2px 8px; margin-left: 8px; vertical-align: middle; }
  .subtitle { color: var(--text-dim); font-size: 14px; margin-bottom: 24px; }
  .cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 16px; margin-bottom: 24px; }
  .card {
    background: var(--surface); border: 1px solid var(--border); border-radius: 8px;
    padding: 16px; text-align: center;
  }
  .card .value { font-size: 28px; font-weight: 700; }
  .card .label { font-size: 12px; color: var(--text-dim); text-transform: uppercase; letter-spacing: 0.05em; }
  .card .value.green { color: var(--green); }
  .card .value.red { color: var(--red); }
  .card .value.blue { color: var(--blue); }
  .card .value.yellow { color: var(--yellow); }

  .section { margin-bottom: 24px; }
  .section h2 { font-size: 16px; margin-bottom: 12px; color: var(--text-dim); }

  table { width: 100%; border-collapse: collapse; background: var(--surface); border: 1px solid var(--border); border-radius: 8px; overflow: hidden; }
  th, td { padding: 10px 14px; text-align: left; border-bottom: 1px solid var(--border); font-size: 13px; }
  th { background: var(--bg); color: var(--text-dim); font-weight: 600; cursor: pointer; user-select: none; }
  th:hover { color: var(--accent); }
  tr:last-child td { border-bottom: none; }
  tr:hover td { background: rgba(88,166,255,0.04); }

  .hit { color: var(--green); } .miss { color: var(--red); } .passthrough { color: var(--yellow); } .coalesced { color: var(--blue); }
  .bar-bg { background: var(--border); border-radius: 4px; height: 8px; width: 100%; overflow: hidden; }
  .bar-fill { height: 100%; border-radius: 4px; transition: width 0.3s; }

  .log-container { max-height: 400px; overflow-y: auto; background: var(--surface); border: 1px solid var(--border); border-radius: 8px; }
  .log-entry { padding: 6px 14px; font-size: 12px; font-family: monospace; border-bottom: 1px solid var(--border); display: flex; gap: 12px; }
  .log-entry:last-child { border-bottom: none; }
  .log-entry { flex-wrap: wrap; }
  .log-args { flex-basis: 100%; color: var(--text-dimmer); white-space: pre-wrap; word-break: break-all; padding-left: 4px; }
  .log-args .fetch { display: block; opacity: 0.8; }
  .log-time { color: var(--text-dim); min-width: 80px; }
  .log-cmd { flex: 1; }
  .log-key { color: var(--text-dimmer); min-width: 90px; max-width: 120px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; cursor: default; }
  .log-latency { color: var(--text-dim); min-width: 70px; text-align: right; }

  .refresh-info { color: var(--text-dim); font-size: 12px; text-align: right; margin-bottom: 8px; }
  .badge { display: inline-block; padding: 2px 8px; border-radius: 12px; font-size: 11px; font-weight: 600; }
  .badge.hit { background: rgba(63,185,80,0.15); color: var(--green); }
  .badge.miss { background: rgba(248,81,73,0.15); color: var(--red); }
  .badge.passthrough { background: rgba(210,153,34,0.15); color: var(--yellow); }
  .badge.coalesced { background: rgba(88,166,255,0.15); color: var(--blue); }

  footer { padding: 24px 0; text-align: center; color: var(--text-dimmer); font-size: 12px; margin-top: 32px; border-top: 1px solid var(--border); }
  footer a { color: var(--text-dim); text-decoration: none; }
  footer a:hover { color: var(--text); }

  .actions { display: flex; gap: 10px; margin-bottom: 20px; align-items: center; flex-wrap: wrap; }
  .actions button {
    font-family: var(--mono); font-size: 13px; padding: 6px 14px; border-radius: 6px; cursor: pointer; border: 1px solid var(--border); color: var(--text);
  }
  .actions button { background: var(--surface); transition: background 0.15s; }
  .actions button:hover { background: var(--border); }
  .actions button.danger { border-color: var(--red); color: var(--red); }
  .actions button.danger:hover { background: rgba(248,81,73,0.15); }
  .actions .spacer { flex: 1; }
  .actions .status-msg { font-size: 12px; color: var(--green); opacity: 0; transition: opacity 0.3s; }
  .actions .status-msg.show { opacity: 1; }
</style>
</head>
<body>
<h1><span>ghx</span> Dashboard <span class="version-badge" id="versionBadge" title="ghxd version">-</span></h1>
<p class="subtitle">GitHub CLI Cache Proxy — <span id="uptime">-</span></p>

<div class="actions">
  <button id="flushBtn" onclick="doFlush()">Flush Cache</button>
  <span id="actionStatus" class="status-msg"></span>
  <span class="spacer"></span>
  <button class="danger" onclick="doShutdown()">Shutdown Daemon</button>
</div>

<div class="cards">
  <div class="card"><div class="value blue" id="total">-</div><div class="label">Total Requests</div></div>
  <div class="card"><div class="value green" id="hits">-</div><div class="label">Cache Hits</div></div>
  <div class="card"><div class="value red" id="misses">-</div><div class="label">Cache Misses</div></div>
  <div class="card"><div class="value yellow" id="passthrough">-</div><div class="label">Passthrough</div></div>
  <div class="card"><div class="value blue" id="coalesced">-</div><div class="label">Coalesced</div></div>
  <div class="card">
    <div class="value green" id="hitrate">-</div><div class="label">Hit Rate</div>
    <div class="bar-bg" style="margin-top:8px"><div class="bar-fill" id="hitbar" style="width:0%;background:var(--green)"></div></div>
  </div>
  <div class="card"><div class="value" id="cachesize">-</div><div class="label">Cache Entries</div></div>
</div>

<div class="section">
  <h2>Per-Command Breakdown</h2>
  <table id="cmdtable">
    <thead><tr>
      <th data-sort="cmd">Command</th>
      <th data-sort="hits">Hits</th>
      <th data-sort="misses">Misses</th>
      <th data-sort="rate">Hit Rate</th>
      <th data-sort="avg">Avg Latency</th>
    </tr></thead>
    <tbody></tbody>
  </table>
</div>

<div class="section">
  <h2>Request Log</h2>
  <div class="refresh-info">Auto-refreshes every 2s<span id="logargsHint" hidden> · Set <code>log_args: true</code> in <code>~/.ghx/config.yaml</code> (or <code>GHX_LOG_ARGS=1</code>) and restart the daemon to see full, redacted commands</span></div>
  <div class="log-container" id="logcontainer"></div>
</div>

<footer>
  <a href="https://github.com/brunoborges/ghx">ghx</a> <span id="version"></span> — MIT License —
  Made by <a href="https://github.com/brunoborges">Bruno Borges</a>
</footer>

<script>
const $ = s => document.querySelector(s);

function fmtNum(n) { return n == null ? '-' : n.toLocaleString(); }
function fmtPct(n) { return n == null ? '-' : n.toFixed(1) + '%'; }
function fmtMs(n) { return n == null ? '-' : n.toFixed(1) + 'ms'; }
function fmtTime(ts) {
  const d = new Date(ts);
  return d.toLocaleTimeString();
}

async function refresh() {
  try {
    const [statsRes, logRes] = await Promise.all([
      fetch('/api/stats'), fetch('/api/log?limit=200')
    ]);
    const stats = await statsRes.json();
    const log = await logRes.json();
    renderStats(stats);
    renderLog(log);
  } catch(e) { console.error('refresh failed:', e); }
}

function renderStats(s) {
  $('#uptime').textContent = 'Uptime: ' + s.uptime;
  if (s.version) {
    $('#version').textContent = s.version;
    $('#versionBadge').textContent = s.version;
  }
  $('#total').textContent = fmtNum(s.total);
  $('#hits').textContent = fmtNum(s.hits);
  $('#misses').textContent = fmtNum(s.misses);
  $('#passthrough').textContent = fmtNum(s.passthrough);
  $('#coalesced').textContent = fmtNum(s.coalesced);
  $('#hitrate').textContent = fmtPct(s.hit_rate);
  $('#hitbar').style.width = (s.hit_rate || 0) + '%';
  $('#cachesize').textContent = s.cache_size + ' / ' + s.max_cache_size;
  $('#logargsHint').hidden = !!s.log_args;

  const tbody = $('#cmdtable tbody');
  tbody.innerHTML = '';
  if (s.commands) {
    const cmds = Object.entries(s.commands).sort((a,b) => (b[1].hits + b[1].misses) - (a[1].hits + a[1].misses));
    for (const [name, c] of cmds) {
      const total = c.hits + c.misses;
      const rate = total > 0 ? (c.hits / total * 100) : 0;
      const avg = c.request_count > 0 ? c.total_latency_ms / c.request_count : 0;
      const tr = document.createElement('tr');
      const code = el('code', '', name.replace(/_/g, ' '));
      const first = el('td');
      first.appendChild(code);
      tr.append(first, el('td', 'hit', fmtNum(c.hits)), el('td', 'miss', fmtNum(c.misses)),
        el('td', '', fmtPct(rate)), el('td', '', fmtMs(avg)));
      tbody.appendChild(tr);
    }
  }
}

function el(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text != null) n.textContent = text;
  return n;
}

function shellJoin(args) {
  return args.map(a => /^[\w@%+=:,.\/-]+$/.test(a) ? a : "'" + a.replace(/'/g, "'\\''") + "'").join(' ');
}

function renderLog(entries) {
  const container = $('#logcontainer');
  container.replaceChildren();
  if (!entries) return;
  for (const e of entries) {
    const div = el('div', 'log-entry');
    const key = el('span', 'log-key', e.cache_key ? e.cache_key.slice(0, 12) + '…' : '—');
    key.title = e.cache_key || '';
    div.append(
      el('span', 'log-time', fmtTime(e.timestamp)),
      el('span', 'log-cmd', e.command.replace(/_/g, ' ')),
      key,
      el('span', 'badge ' + e.result, e.result),
      el('span', 'log-latency', fmtMs(e.latency_ms)));
    if (e.args) {
      const args = el('span', 'log-args', 'gh ' + shellJoin(e.args));
      if (e.fetch) args.appendChild(el('span', 'fetch', '↳ fetched as: gh ' + shellJoin(e.fetch)));
      div.appendChild(args);
    }
    container.appendChild(div);
  }
  container.scrollTop = container.scrollHeight;
}

// Column sorting
let sortCol = 'rate', sortDir = -1;
$('#cmdtable thead').addEventListener('click', e => {
  const th = e.target.closest('th');
  if (!th) return;
  const col = th.dataset.sort;
  if (sortCol === col) sortDir *= -1; else { sortCol = col; sortDir = -1; }
  refresh();
});

refresh();
setInterval(refresh, 2000);

function showStatus(msg, isError) {
  const el = $('#actionStatus');
  el.textContent = msg;
  el.style.color = isError ? 'var(--red)' : 'var(--green)';
  el.classList.add('show');
  setTimeout(() => el.classList.remove('show'), 3000);
}

function doFlush() {
  fetch('/api/flush', { method: 'POST' })
    .then(r => r.json())
    .then(d => { showStatus('Flushed ' + d.flushed + ' entries'); refresh(); })
    .catch(() => showStatus('Flush failed', true));
}

function doShutdown() {
  if (!confirm('Shut down the ghxd daemon? The dashboard will become unavailable.')) return;
  fetch('/api/shutdown', { method: 'POST' })
    .then(() => {
      document.body.innerHTML = '<div style="text-align:center;margin-top:20vh;color:var(--text-dim);font-family:monospace"><h2>Daemon stopped</h2><p>ghxd has been shut down. Restart with: <code>ghx xdaemon start -d</code></p></div>';
    })
    .catch(() => showStatus('Shutdown failed', true));
}
</script>
</body>
</html>`
