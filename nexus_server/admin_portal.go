package main

// adminPortalHTML is the single-page admin dashboard served at /admin.
// Vanilla JS + fetch to the existing /api/v1/admin/* endpoints. Kept
// separate from the React SPA so the admin surface has no shared bundle,
// no shared dependencies, and no shared cookie context with users.
const adminPortalHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Phaze · Command Center</title>
<style>
  * { box-sizing: border-box; margin: 0; }
  body { font-family: 'Plus Jakarta Sans', 'Inter', system-ui, -apple-system, sans-serif; background: #000; color: #f5f5f7; }
  :root { --brand: #a677ff; --brand-dim: rgba(166,119,255,0.12); --panel: #111; --edge: #1c1c1e; --chrome: #0a0a0a; --muted: #636366; --text: #f5f5f7; --danger: #ef4444; --success: #34d399; --warn: #fbbf24; }

  /* ── Login ──────────────────────────────── */
  .login-wrap { display: grid; place-items: center; min-height: 100vh; padding: 2rem; }
  .login { width: 380px; max-width: 100%; background: var(--panel); border: 1px solid var(--edge); border-radius: 20px; padding: 2.5rem 2rem; }
  .login h2 { font-size: 1.5rem; font-weight: 800; letter-spacing: -0.03em; margin-bottom: 0.25rem; }
  .login .sub { color: var(--muted); font-size: 0.85rem; margin-bottom: 1.5rem; }
  .login input { display: block; width: 100%; padding: 0.7rem 0.9rem; margin-bottom: 0.65rem; border-radius: 10px; border: 1px solid var(--edge); background: var(--chrome); color: var(--text); font-size: 0.95rem; font-family: inherit; }
  .login input:focus { outline: none; border-color: var(--brand); box-shadow: 0 0 0 3px rgba(166,119,255,0.2); }
  .login button { width: 100%; padding: 0.75rem; background: var(--brand); color: #fff; border: none; border-radius: 10px; font-weight: 700; font-size: 0.95rem; cursor: pointer; margin-top: 0.5rem; }
  .login button:hover { background: #9261f0; }
  .err { color: var(--danger); font-size: 0.82rem; margin-top: 0.5rem; }

  /* ── Shell ──────────────────────────────── */
  .shell { display: grid; grid-template-columns: 220px 1fr; min-height: 100vh; }
  @media (max-width: 768px) { .shell { grid-template-columns: 1fr; } .sidebar { display: none; } }
  .sidebar { background: var(--chrome); border-right: 1px solid var(--edge); padding: 1.25rem 0; display: flex; flex-direction: column; }
  .sidebar-brand { padding: 0 1.25rem 1.25rem; font-size: 1.1rem; font-weight: 800; letter-spacing: -0.03em; display: flex; align-items: center; gap: 0.5rem; }
  .sidebar-brand .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--success); box-shadow: 0 0 8px rgba(52,211,153,0.5); }
  .sidebar nav { flex: 1; display: flex; flex-direction: column; gap: 2px; padding: 0 0.5rem; }
  .sidebar nav button { width: 100%; text-align: left; background: transparent; border: none; color: var(--muted); padding: 0.6rem 0.75rem; border-radius: 10px; cursor: pointer; font-size: 0.85rem; font-weight: 600; font-family: inherit; display: flex; align-items: center; gap: 0.5rem; transition: all 150ms ease; }
  .sidebar nav button:hover { background: rgba(255,255,255,0.04); color: var(--text); }
  .sidebar nav button.on { background: var(--brand-dim); color: var(--brand); }
  .sidebar-foot { padding: 1rem 1.25rem 0; border-top: 1px solid var(--edge); margin-top: auto; }
  .sidebar-foot .user { font-size: 0.82rem; color: var(--muted); margin-bottom: 0.5rem; }
  .sidebar-foot button { background: transparent; border: 1px solid var(--edge); color: var(--muted); padding: 0.4rem 0.8rem; border-radius: 8px; cursor: pointer; font-size: 0.78rem; font-family: inherit; }
  .sidebar-foot button:hover { color: var(--text); border-color: var(--muted); }

  /* ── Content ────────────────────────────── */
  .content { padding: 1.5rem 2rem; overflow-y: auto; max-height: 100vh; }
  .page-title { font-size: 1.4rem; font-weight: 800; letter-spacing: -0.03em; margin-bottom: 0.25rem; }
  .page-sub { color: var(--muted); font-size: 0.85rem; margin-bottom: 1.5rem; }

  /* ── Stats cards ────────────────────────── */
  .stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(160px, 1fr)); gap: 0.75rem; margin-bottom: 1.5rem; }
  .stat { background: var(--panel); border: 1px solid var(--edge); border-radius: 14px; padding: 1rem 1.25rem; }
  .stat-label { font-size: 0.68rem; font-weight: 700; text-transform: uppercase; letter-spacing: 0.08em; color: var(--muted); margin-bottom: 0.25rem; }
  .stat-value { font-size: 1.8rem; font-weight: 800; letter-spacing: -0.03em; }
  .stat-value.brand { color: var(--brand); }
  .stat-value.green { color: var(--success); }

  /* ── Search ─────────────────────────────── */
  .search-bar { display: flex; gap: 0.5rem; margin-bottom: 1rem; }
  .search-bar input { flex: 1; padding: 0.6rem 0.85rem; border-radius: 10px; border: 1px solid var(--edge); background: var(--chrome); color: var(--text); font-size: 0.9rem; font-family: inherit; }
  .search-bar input:focus { outline: none; border-color: var(--brand); }

  /* ── Table ──────────────────────────────── */
  .tbl { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
  .tbl th, .tbl td { padding: 0.55rem 0.65rem; text-align: left; border-bottom: 1px solid var(--edge); }
  .tbl th { color: var(--muted); font-weight: 700; text-transform: uppercase; font-size: 0.68rem; letter-spacing: 0.06em; position: sticky; top: 0; background: var(--chrome); }
  .tbl tr:hover td { background: rgba(255,255,255,0.02); }
  .tbl .mono { font-family: 'JetBrains Mono', ui-monospace, monospace; font-size: 0.78rem; }
  .tbl .actions { display: flex; gap: 0.3rem; flex-wrap: wrap; }
  .tbl .ip-cell { display: flex; flex-direction: column; gap: 1px; }
  .tbl .ip-cell .geo { font-size: 0.7rem; color: var(--muted); }

  /* ── Badges ─────────────────────────────── */
  .badge { display: inline-block; padding: 2px 8px; border-radius: 999px; font-size: 0.68rem; font-weight: 700; }
  .badge.online { background: rgba(52,211,153,0.15); color: var(--success); }
  .badge.offline { background: rgba(255,255,255,0.04); color: var(--muted); }
  .badge.banned { background: rgba(239,68,68,0.15); color: #fca5a5; }
  .badge.verified { background: rgba(52,211,153,0.12); color: #86efac; }
  .badge.unverified { background: rgba(251,191,36,0.12); color: #fde047; }
  .badge.role-user { background: rgba(255,255,255,0.04); color: var(--muted); }
  .badge.role-helper { background: rgba(59,130,246,0.15); color: #93c5fd; }
  .badge.role-moderator { background: rgba(168,85,247,0.15); color: #d8b4fe; }
  .badge.role-admin { background: rgba(166,119,255,0.15); color: #cab1ff; }
  .badge.role-super_admin { background: rgba(239,68,68,0.15); color: #fca5a5; }

  /* ── Buttons ────────────────────────────── */
  .btn { padding: 4px 10px; border-radius: 8px; border: 1px solid var(--edge); background: var(--panel); color: var(--text); cursor: pointer; font-size: 0.78rem; font-family: inherit; font-weight: 600; transition: all 150ms ease; }
  .btn:hover { background: var(--edge); }
  .btn.danger { color: #fca5a5; border-color: rgba(239,68,68,0.25); }
  .btn.danger:hover { background: rgba(239,68,68,0.1); }
  .btn.brand { background: var(--brand); color: #fff; border-color: var(--brand); }
  .btn.brand:hover { background: #9261f0; }
  select.role-sel { background: var(--panel); color: var(--text); border: 1px solid var(--edge); border-radius: 8px; padding: 4px 8px; font-size: 0.78rem; cursor: pointer; font-family: inherit; }

  /* ── Broadcast ──────────────────────────── */
  .broadcast textarea { width: 100%; min-height: 100px; padding: 0.7rem; border-radius: 10px; border: 1px solid var(--edge); background: var(--chrome); color: var(--text); font-family: inherit; resize: vertical; font-size: 0.9rem; }
  .broadcast textarea:focus { outline: none; border-color: var(--brand); }
  .broadcast .send-row { display: flex; gap: 0.5rem; margin-top: 0.75rem; align-items: center; }
  .ok { color: var(--success); font-size: 0.82rem; }
  .empty { padding: 2.5rem; text-align: center; color: var(--muted); font-size: 0.9rem; }
</style>
</head>
<body>
<div id="root"></div>
<script>
const $ = (sel) => document.querySelector(sel);
const html = (s) => { const t = document.createElement('template'); t.innerHTML = s.trim(); return t.content.firstChild; };
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));

const STATE = {
  token: localStorage.getItem('phaze_admin_token') || '',
  username: localStorage.getItem('phaze_admin_user') || '',
  tab: 'dashboard',
  users: [], reports: [], pending: [], geoCache: {},
  search: '',
};

async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (STATE.token) opts.headers['Authorization'] = 'Bearer ' + STATE.token;
  if (body !== undefined) { opts.headers['Content-Type'] = 'application/json'; opts.body = JSON.stringify(body); }
  const r = await fetch(path, opts);
  if (!r.ok) {
    const txt = await r.text();
    if (r.status === 401 || r.status === 403) { logout(); }
    throw new Error(r.status + ': ' + txt);
  }
  const ct = r.headers.get('content-type') || '';
  return ct.includes('json') ? r.json() : r.text();
}

async function login(u, p) {
  const res = await api('POST', '/api/v1/admin/login', { username: u, password: p });
  STATE.token = res.token; STATE.username = res.username;
  localStorage.setItem('phaze_admin_token', res.token);
  localStorage.setItem('phaze_admin_user', res.username);
  render();
}
function logout() {
  STATE.token = ''; STATE.username = '';
  localStorage.removeItem('phaze_admin_token');
  localStorage.removeItem('phaze_admin_user');
  render();
}

async function loadUsers() { STATE.users = await api('GET', '/api/v1/admin/users'); renderContent(); }
async function loadReports() { STATE.reports = await api('GET', '/api/v1/admin/reports'); renderContent(); }
async function loadPending() { STATE.pending = await api('GET', '/api/v1/admin/pending-verifications'); renderContent(); }
async function banUser(u) { const r = prompt('Reason:', 'TOS violation'); if (r === null) return; await api('POST', '/api/v1/admin/users/' + encodeURIComponent(u) + '/ban', { reason: r }); loadUsers(); }
async function unbanUser(u) { if (!confirm('Unban ' + u + '?')) return; await api('POST', '/api/v1/admin/users/' + encodeURIComponent(u) + '/unban'); loadUsers(); }
async function setRole(u, role) { if (!confirm('Set ' + u + ' → ' + role + '?')) return; await api('POST', '/api/v1/admin/users/' + encodeURIComponent(u) + '/role', { role }); loadUsers(); }
async function deleteUser(u) { if (!confirm('DELETE ' + u + '? Cannot undo.')) return; if (prompt('Type username:') !== u) return; await api('POST', '/api/v1/admin/users/' + encodeURIComponent(u) + '/delete'); loadUsers(); }
async function resolveReport(id) { if (!confirm('Resolve #' + id + '?')) return; await api('POST', '/api/v1/admin/reports/' + id + '/resolve'); loadReports(); }
async function broadcast(text) { if (!text.trim()) return; await api('POST', '/api/v1/admin/broadcast', { message: text.trim() }); }

async function geoLookup(ip) {
  if (!ip) return '';
  if (STATE.geoCache[ip]) return STATE.geoCache[ip];
  try {
    const r = await fetch('/api/v1/admin/geo?ip=' + encodeURIComponent(ip));
    if (!r.ok) return '';
    const d = await r.json();
    const s = (d.city || '') + (d.city && d.country ? ', ' : '') + (d.country || '') + (d.isp ? ' · ' + d.isp : '');
    STATE.geoCache[ip] = s;
    return s;
  } catch { return ''; }
}

function fmtDate(s) { return (s || '').replace('T', ' ').slice(0, 16); }
function relTime(s) {
  if (!s) return '';
  const ms = Date.now() - new Date(s).getTime();
  if (ms < 60000) return 'just now';
  if (ms < 3600000) return Math.floor(ms/60000) + 'm ago';
  if (ms < 86400000) return Math.floor(ms/3600000) + 'h ago';
  return Math.floor(ms/86400000) + 'd ago';
}

function renderLogin() {
  const root = $('#root');
  root.innerHTML = '';
  const wrap = html('<div class="login-wrap"></div>');
  const card = html('<form class="login"><h2>Command Center</h2><p class="sub">Phaze admin access only.</p><input name="u" placeholder="Username" autocomplete="username"><input name="p" type="password" placeholder="Password" autocomplete="current-password"><button type="submit">Sign in</button><div class="err" hidden></div></form>');
  card.addEventListener('submit', async (e) => {
    e.preventDefault();
    const errEl = card.querySelector('.err');
    errEl.hidden = true;
    try { await login(card.querySelector('[name=u]').value.trim(), card.querySelector('[name=p]').value); }
    catch (err) { errEl.textContent = err.message; errEl.hidden = false; }
  });
  wrap.appendChild(card);
  root.appendChild(wrap);
}

function setTab(t) { STATE.tab = t; STATE.search = ''; renderNav(); renderContent(); }

function renderNav() {
  document.querySelectorAll('.sidebar nav button').forEach(b => {
    b.classList.toggle('on', b.dataset.t === STATE.tab);
  });
}

function renderShell() {
  const root = $('#root');
  root.innerHTML = '';
  const shell = html('<div class="shell"></div>');
  const sidebar = html('<aside class="sidebar"><div class="sidebar-brand"><span class="dot"></span> Phaze Admin</div><nav></nav><div class="sidebar-foot"><div class="user"></div><button class="logout-btn">Sign out</button></div></aside>');
  const tabs = [
    { id: 'dashboard', icon: '📊', label: 'Dashboard' },
    { id: 'users', icon: '👥', label: 'Users' },
    { id: 'reports', icon: '⚑', label: 'Reports' },
    { id: 'pending', icon: '⏳', label: 'Pending' },
    { id: 'logs', icon: '📋', label: 'Activity Log' },
    { id: 'broadcast', icon: '📢', label: 'Broadcast' },
    { id: 'notice', icon: '🔔', label: 'Global Notice' },
    { id: 'ipblock', icon: '🛡', label: 'IP Block' },
  ];
  const nav = sidebar.querySelector('nav');
  tabs.forEach(t => {
    const btn = html('<button data-t="' + t.id + '">' + t.icon + ' ' + t.label + '</button>');
    btn.addEventListener('click', () => setTab(t.id));
    if (t.id === STATE.tab) btn.classList.add('on');
    nav.appendChild(btn);
  });
  sidebar.querySelector('.user').textContent = '@' + STATE.username;
  sidebar.querySelector('.logout-btn').addEventListener('click', logout);
  shell.appendChild(sidebar);
  const content = html('<main class="content" id="content"></main>');
  shell.appendChild(content);
  root.appendChild(shell);
  renderContent();
}

function renderContent() {
  const el = $('#content');
  if (!el) return;

  if (STATE.tab === 'dashboard') {
    if (!STATE.users.length) { el.innerHTML = '<div class="empty">Loading…</div>'; loadUsers(); return; }
    const total = STATE.users.length;
    const online = STATE.users.filter(u => u.online).length;
    const verified = STATE.users.filter(u => u.verified).length;
    const banned = STATE.users.filter(u => u.banned).length;
    el.innerHTML = '<h1 class="page-title">Dashboard</h1><p class="page-sub">Real-time overview of Phaze.</p>' +
      '<div class="stats">' +
      '<div class="stat"><div class="stat-label">Total users</div><div class="stat-value brand">' + total + '</div></div>' +
      '<div class="stat"><div class="stat-label">Online now</div><div class="stat-value green">' + online + '</div></div>' +
      '<div class="stat"><div class="stat-label">Verified</div><div class="stat-value">' + verified + '</div></div>' +
      '<div class="stat"><div class="stat-label">Banned</div><div class="stat-value">' + banned + '</div></div>' +
      '</div>' +
      '<h2 style="font-size:1rem;font-weight:700;margin-bottom:0.75rem">Recently active</h2>' +
      '<table class="tbl"><thead><tr><th>User</th><th>Status</th><th>Last IP</th><th>Last seen</th></tr></thead><tbody>' +
      STATE.users.filter(u => u.last_login_at).sort((a,b) => (b.last_login_at||'').localeCompare(a.last_login_at||'')).slice(0,15).map(u =>
        '<tr><td><strong>' + esc(u.username) + '</strong></td>' +
        '<td><span class="badge ' + (u.online ? 'online' : 'offline') + '">' + (u.online ? 'online' : 'offline') + '</span></td>' +
        '<td class="mono">' + esc(u.last_ip) + '</td>' +
        '<td>' + relTime(u.last_login_at) + '</td></tr>'
      ).join('') + '</tbody></table>';
    return;
  }

  if (STATE.tab === 'users') {
    if (!STATE.users.length) { el.innerHTML = '<div class="empty">Loading…</div>'; loadUsers(); return; }
    const q = STATE.search.toLowerCase();
    const filtered = q ? STATE.users.filter(u => u.username.toLowerCase().includes(q) || (u.email||'').toLowerCase().includes(q) || (u.last_ip||'').includes(q) || (u.signup_ip||'').includes(q)) : STATE.users;
    el.innerHTML = '<h1 class="page-title">Users</h1><p class="page-sub">' + STATE.users.length + ' total · ' + filtered.length + ' shown</p>' +
      '<div class="search-bar"><input placeholder="Search by username, email, or IP…" value="' + esc(STATE.search) + '"></div>' +
      '<table class="tbl"><thead><tr><th>User</th><th>Email</th><th>Status</th><th>Role</th><th>IP</th><th>Joined</th><th>Last seen</th><th>Actions</th></tr></thead><tbody>' +
      filtered.map(u =>
        '<tr><td><strong>' + esc(u.username) + '</strong></td>' +
        '<td style="font-size:0.78rem">' + esc(u.email) + '</td>' +
        '<td>' +
          '<span class="badge ' + (u.online ? 'online' : 'offline') + '">' + (u.online ? 'online' : 'offline') + '</span> ' +
          (u.banned ? '<span class="badge banned">banned</span> ' : '') +
          '<span class="badge ' + (u.verified ? 'verified' : 'unverified') + '">' + (u.verified ? 'verified' : 'unverified') + '</span>' +
        '</td>' +
        '<td><span class="badge role-' + esc(u.role||'user') + '">' + esc(u.role||'user') + '</span></td>' +
        '<td class="ip-cell"><span class="mono" data-ip="' + esc(u.last_ip) + '">' + esc(u.last_ip || '—') + '</span>' +
          (u.signup_ip && u.signup_ip !== u.last_ip ? '<span class="mono" style="font-size:0.7rem;color:var(--muted)" title="Signup IP">' + esc(u.signup_ip) + '</span>' : '') +
          '<span class="geo" data-geo-ip="' + esc(u.last_ip) + '"></span></td>' +
        '<td>' + fmtDate(u.created_at) + '</td>' +
        '<td>' + relTime(u.last_login_at) + '</td>' +
        '<td><div class="actions">' +
          (!u.verified ? '<button class="btn" data-act="verify" data-u="' + esc(u.username) + '">Verify</button> ' : '') +
          (u.banned ? '<button class="btn" data-act="unban" data-u="' + esc(u.username) + '">Unban</button>' : '<button class="btn danger" data-act="ban" data-u="' + esc(u.username) + '">Ban</button>') +
          ' <select class="role-sel" data-role-u="' + esc(u.username) + '"><option value="">Role…</option><option value="user">user</option><option value="helper">helper</option><option value="moderator">mod</option><option value="admin">admin</option></select>' +
          ' <button class="btn danger" data-act="delete" data-u="' + esc(u.username) + '">Delete</button>' +
        '</div></td></tr>'
      ).join('') + '</tbody></table>';

    el.querySelector('.search-bar input').addEventListener('input', (e) => { STATE.search = e.target.value; renderContent(); });
    el.querySelectorAll('button[data-act]').forEach(b => b.addEventListener('click', () => {
      if (b.dataset.act === 'ban') banUser(b.dataset.u);
      else if (b.dataset.act === 'unban') unbanUser(b.dataset.u);
      else if (b.dataset.act === 'delete') deleteUser(b.dataset.u);
      else if (b.dataset.act === 'verify') { api('POST', '/api/v1/admin/verify-user', { username: b.dataset.u }).then(() => loadUsers()); }
    }));
    el.querySelectorAll('select[data-role-u]').forEach(sel => sel.addEventListener('change', (e) => {
      if (e.target.value) { setRole(sel.dataset.roleU, e.target.value); e.target.value = ''; }
    }));
    // Async geo lookups
    el.querySelectorAll('[data-geo-ip]').forEach(async (span) => {
      const ip = span.dataset.geoIp;
      if (!ip) return;
      const geo = await geoLookup(ip);
      if (geo) span.textContent = geo;
    });
    return;
  }

  if (STATE.tab === 'reports') {
    if (!STATE.reports.length && STATE.reports !== null) { el.innerHTML = '<div class="empty">Loading…</div>'; loadReports(); return; }
    if (!STATE.reports.length) { el.innerHTML = '<h1 class="page-title">Reports</h1><div class="empty">No reports.</div>'; return; }
    el.innerHTML = '<h1 class="page-title">Reports</h1><p class="page-sub">' + STATE.reports.length + ' total</p>' +
      '<table class="tbl"><thead><tr><th>ID</th><th>Reporter</th><th>Subject</th><th>Reason</th><th>Status</th><th></th></tr></thead><tbody>' +
      STATE.reports.map(r => '<tr><td>#' + r.id + '</td><td>' + esc(r.reporter) + '</td><td><strong>' + esc(r.subject) + '</strong></td><td>' + esc(r.reason) + (r.body ? '<br><span style="color:var(--muted);font-size:0.78rem">' + esc(r.body) + '</span>' : '') + '</td><td>' + esc(r.status) +
        (r.resolved_by ? '<br><span style="color:var(--muted);font-size:0.78rem">by ' + esc(r.resolved_by) + '</span>' : '') +
        '</td><td>' + (r.status === 'open' ? '<button class="btn brand" data-id="' + r.id + '">Resolve</button>' : '<span style="color:var(--success)">✓</span>') + '</td></tr>').join('') + '</tbody></table>';
    el.querySelectorAll('button[data-id]').forEach(b => b.addEventListener('click', () => resolveReport(+b.dataset.id)));
    return;
  }

  if (STATE.tab === 'pending') {
    if (!STATE.pending.length && STATE.pending !== null) { el.innerHTML = '<div class="empty">Loading…</div>'; loadPending(); return; }
    if (!STATE.pending.length) { el.innerHTML = '<h1 class="page-title">Pending Verifications</h1><div class="empty">None in the last 24h.</div>'; return; }
    el.innerHTML = '<h1 class="page-title">Pending Verifications</h1><p class="page-sub">' + STATE.pending.length + ' waiting</p>' +
      '<table class="tbl"><thead><tr><th>Username</th><th>Email</th><th>Code</th><th>Created</th></tr></thead><tbody>' +
      STATE.pending.map(u => '<tr><td><strong>' + esc(u.username) + '</strong></td><td>' + esc(u.email) + '</td><td class="mono">' + esc(u.verification_code) + '</td><td>' + fmtDate(u.created_at) + '</td></tr>').join('') + '</tbody></table>';
    return;
  }

  if (STATE.tab === 'broadcast') {
    el.innerHTML = '<h1 class="page-title">Broadcast</h1><p class="page-sub">Post to #announcements in the global Phaze Hub.</p>' +
      '<div class="broadcast"><textarea placeholder="Write your announcement…"></textarea><div class="send-row"><button class="btn brand">Send broadcast</button><span class="ok" hidden></span></div></div>';
    el.querySelector('.btn.brand').addEventListener('click', async () => {
      const ta = el.querySelector('textarea');
      const ok = el.querySelector('.ok');
      try { await broadcast(ta.value); ta.value = ''; ok.textContent = 'Posted.'; ok.hidden = false; setTimeout(() => ok.hidden = true, 3000); }
      catch (e) { alert('Failed: ' + e.message); }
    });
    return;
  }

  if (STATE.tab === 'notice') {
    el.innerHTML = '<h1 class="page-title">Global Notice</h1><p class="page-sub">Send a popup notification to ALL connected users right now.</p>' +
      '<div class="broadcast"><textarea placeholder="Your notice (shows as a popup on every screen)…"></textarea><div class="send-row"><button class="btn brand">Send to everyone</button><span class="ok" hidden></span></div></div>';
    el.querySelector('.btn.brand').addEventListener('click', async () => {
      const ta = el.querySelector('textarea');
      const ok = el.querySelector('.ok');
      if (!ta.value.trim()) return;
      try { await api('POST', '/api/v1/admin/global-notice', { message: ta.value.trim() }); ta.value = ''; ok.textContent = 'Sent to all online users.'; ok.hidden = false; setTimeout(() => ok.hidden = true, 3000); }
      catch (e) { alert('Failed: ' + e.message); }
    });
    return;
  }

  if (STATE.tab === 'logs') {
    el.innerHTML = '<h1 class="page-title">Activity Log</h1><p class="page-sub">Recent login activity.</p><div class="empty">Loading…</div>';
    api('GET', '/api/v1/admin/logs').then((logs) => {
      el.innerHTML = '<h1 class="page-title">Activity Log</h1><p class="page-sub">' + logs.length + ' recent logins</p>' +
        '<table class="tbl"><thead><tr><th>User</th><th>IP</th><th>Location</th><th>Login time</th></tr></thead><tbody>' +
        logs.map(l => '<tr><td><strong>' + esc(l.username) + '</strong></td><td class="mono">' + esc(l.ip) + '</td><td class="geo-cell" data-geo="' + esc(l.ip) + '">…</td><td>' + fmtDate(l.login_at) + '</td></tr>').join('') +
        '</tbody></table>';
      el.querySelectorAll('.geo-cell').forEach(async (td) => {
        const geo = await geoLookup(td.dataset.geo);
        td.textContent = geo || '—';
      });
    });
    return;
  }

  if (STATE.tab === 'ipblock') {
    el.innerHTML = '<h1 class="page-title">IP Block</h1><p class="page-sub">Block IPs from connecting. Active connections from blocked IPs are kicked immediately.</p>' +
      '<div class="search-bar"><input placeholder="Enter IP to block…" id="block-ip"><button class="btn danger" id="block-btn">Block IP</button></div>' +
      '<div id="blocked-list"><div class="empty">Loading…</div></div>';
    el.querySelector('#block-btn').addEventListener('click', async () => {
      const ip = el.querySelector('#block-ip').value.trim();
      if (!ip) return;
      await api('POST', '/api/v1/admin/ip-block', { ip, action: 'block' });
      el.querySelector('#block-ip').value = '';
      loadBlockedIPs();
    });
    async function loadBlockedIPs() {
      const ips = await api('GET', '/api/v1/admin/ip-block');
      const container = el.querySelector('#blocked-list');
      if (!ips.length) { container.innerHTML = '<div class="empty">No IPs blocked.</div>'; return; }
      container.innerHTML = '<table class="tbl"><thead><tr><th>IP</th><th>Location</th><th></th></tr></thead><tbody>' +
        ips.map(ip => '<tr><td class="mono">' + esc(ip) + '</td><td class="geo-cell" data-geo="' + esc(ip) + '">…</td><td><button class="btn" data-unblock="' + esc(ip) + '">Unblock</button></td></tr>').join('') +
        '</tbody></table>';
      container.querySelectorAll('[data-unblock]').forEach(b => b.addEventListener('click', async () => {
        await api('POST', '/api/v1/admin/ip-block', { ip: b.dataset.unblock, action: 'unblock' });
        loadBlockedIPs();
      }));
      container.querySelectorAll('.geo-cell').forEach(async (td) => {
        const geo = await geoLookup(td.dataset.geo);
        td.textContent = geo || '—';
      });
    }
    loadBlockedIPs();
    return;
  }
}

function render() {
  if (!STATE.token) renderLogin();
  else renderShell();
}

render();
</script>
</body>
</html>
` + ""
