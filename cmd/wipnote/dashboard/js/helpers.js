/* ── Shared helpers ─────────────────────────────────────────── */

// buildProjectUrl constructs an API URL that scopes to the currently
// active project via the /p/<id>/ base path.
//
// When the dashboard loads under /p/<id>/, every API call must go to
// /p/<id>/api/<endpoint> so the parent server routes the request to the
// correct child process via the reverse proxy. When the dashboard loads
// at root (the global doorway landing page), API calls stay at
// /api/<endpoint> — which is the tiny doorway API (/api/mode and
// /api/projects only, no per-project data).
//
// The old ?project=<id> query-parameter approach is gone. Per-project
// data routing is now 100% path-based.
function buildProjectUrl(endpoint, extraQuery) {
  var prefix = '';
  if (window.location.pathname.indexOf('/p/') === 0) {
    // Extract /p/<id> from the path. location.pathname starts with
    // /p/<id>/ so split on / and take the first two non-empty parts.
    var segs = window.location.pathname.split('/').filter(function(s) { return s !== ''; });
    if (segs.length >= 2 && segs[0] === 'p') {
      prefix = '/p/' + segs[1];
    }
  }
  var base = prefix + '/api/' + endpoint;
  if (extraQuery) return base + '?' + extraQuery;
  return base;
}

function esc(s) {
  if (!s) return '';
  var div = document.createElement('div');
  div.appendChild(document.createTextNode(s));
  return div.innerHTML;
}

function relTime(ts) {
  if (!ts) return '--';
  var d = new Date(ts.indexOf('T') >= 0 ? ts : ts.replace(' ', 'T') + 'Z');
  var now = Date.now();
  var diffSec = Math.floor((now - d.getTime()) / 1000);
  if (isNaN(diffSec) || diffSec < 0) return fmtTime(ts);
  if (diffSec < 5) return 'just now';
  if (diffSec < 60) return diffSec + 's ago';
  var m = Math.floor(diffSec / 60);
  if (m < 60) return m + 'm ago';
  var h = Math.floor(m / 60);
  if (h < 24) return h + 'h ago';
  var dy = Math.floor(h / 24);
  return dy + 'd ago';
}

function fmtTime(ts) {
  if (!ts) return '--';
  try {
    var d = new Date(ts.indexOf('T') >= 0 ? ts : ts.replace(' ', 'T') + 'Z');
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  } catch(e) { return ts.slice(11, 19) || ts; }
}

function formatTime(ts) { return fmtTime(ts); }

function statusBadgeClass(s) {
  var map = { 'todo': 'badge-todo', 'in-progress': 'badge-ip', 'done': 'badge-done',
    'blocked': 'badge-blocked', 'active': 'badge-active', 'ended': 'badge-ended',
    'completed': 'badge-completed', 'started': 'badge-ip', 'error': 'badge-error' };
  return map[s] || 'badge-todo';
}

function priorityBadgeClass(p) {
  var map = { 'low': 'badge-pri-low', 'medium': 'badge-pri-medium',
    'high': 'badge-pri-high', 'critical': 'badge-pri-critical' };
  return map[p] || 'badge-pri-medium';
}

function summaryText(evt) {
  return evt.input_summary || evt.output_summary || evt.summary || '';
}

// Extract line range from Read input_summary for display in tool chip.
// Returns e.g. " [100:150]" or "" if no range present.
function toolChipRange(evt) {
  if (evt.tool_name !== 'Read' || !evt.input_summary) return '';
  var m = evt.input_summary.match(/\s(\[\d*:\d*\])$/);
  return m ? ' ' + m[1] : '';
}

function truncId(id) {
  if (!id) return '--';
  if (id.length <= 16) return id;
  return id.slice(0, 8) + '..' + id.slice(-6);
}

function cleanSessionTitle(text) {
  if (!text) return '';
  var cmdMatch = text.match(/<command-message>([^<]+)<\/command-message>/);
  if (cmdMatch) {
    var cmd = cmdMatch[1];
    var cmdNames = {
      'wipnote:execute': 'Parallel Execution',
      'wipnote:recommend': 'Work Recommendations',
      'wipnote:plan': 'Planning Session',
      'wipnote:status': 'Status Check',
      'wipnote:diagnose': 'Diagnostics',
      'wipnote:cleanup': 'Cleanup',
      'wipnote:git-commit': 'Git Commit'
    };
    return cmdNames[cmd] || '/' + cmd;
  }
  text = text.replace(/^#{1,4}\s+/, '');
  text = text.replace(/<[^>]+>/g, '').trim();
  text = text.replace(/\[Image #\d+\]/g, '').trim();
  return text;
}

function sessionDisplayTitle(s) {
  if (s.title && s.title !== '--') return s.title;
  if (s.first_message) {
    var title = cleanSessionTitle(s.first_message);
    if (title.length > 60) title = title.substring(0, 57) + '...';
    return title;
  }
  return sessionTimeLabel(s);
}

function sessionTimeLabel(s) {
  if (!s.created_at) return 'Untitled Session';
  try {
    var d = new Date(s.created_at);
    return d.toLocaleDateString([], { month: 'short', day: 'numeric' })
      + ' ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  } catch(e) { return 'Untitled Session'; }
}

function agentClass(agentId) {
  if (!agentId) return 'default';
  var lower = agentId.toLowerCase();
  if (lower.includes('researcher')) return 'researcher';
  if (lower.includes('haiku')) return 'haiku';
  if (lower.includes('opus')) return 'opus';
  if (lower.includes('test')) return 'test';
  if (lower.includes('sonnet')) return 'sonnet';
  if (lower.includes('debug')) return 'debug';
  return 'default';
}

function fmtTokens(n) {
  if (!n || n === 0) return '0';
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
  if (n >= 1000) return (n / 1000).toFixed(1) + 'k';
  return String(n);
}

/* ── Safe DOM builders ─────────────────────────────────────── */

function createBadge(text, cls) {
  var span = document.createElement('span');
  span.className = 'badge ' + cls;
  span.textContent = text || 'unknown';
  return span;
}

function createStatusBadge(status) {
  return createBadge(status, statusBadgeClass(status));
}

function createPriorityBadge(priority) {
  return createBadge(priority || 'medium', priorityBadgeClass(priority));
}

function td(text, opts) {
  var cell = document.createElement('td');
  if (opts && opts.className) cell.className = opts.className;
  if (opts && opts.style) cell.setAttribute('style', opts.style);
  if (opts && opts.title) cell.title = text || '';
  cell.textContent = text || '';
  return cell;
}

function tdWithChild(child, opts) {
  var cell = document.createElement('td');
  if (opts && opts.className) cell.className = opts.className;
  if (opts && opts.style) cell.setAttribute('style', opts.style);
  cell.appendChild(child);
  return cell;
}

function setVal(id, val) {
  var el = document.getElementById(id);
  if (!el) return;
  var prev = el.textContent;
  var next = val != null ? String(val) : '--';
  if (prev !== next) {
    el.textContent = next;
    var pill = el.closest('.stat-pill');
    if (pill) { pill.classList.add('pulse'); setTimeout(function() { pill.classList.remove('pulse'); }, 400); }
  }
}
