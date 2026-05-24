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
<title>Phaze · Admin</title>
<style>
  * { box-sizing: border-box; }
  body { margin: 0; font-family: 'Inter', system-ui, -apple-system, Segoe UI, sans-serif; background: #0b0b0d; color: #fafafa; }
  header { background: #131316; padding: 1rem 1.5rem; border-bottom: 1px solid #232328; display: flex; align-items: center; gap: 1rem; }
  header h1 { margin: 0; font-size: 1.1rem; font-weight: 700; }
  header .me { margin-left: auto; opacity: 0.65; font-size: 0.85rem; }
  header button.logout { background: transparent; border: 1px solid #2a2a30; color: #fafafa; padding: 6px 12px; border-radius: 6px; cursor: pointer; }
  main { max-width: 1100px; margin: 0 auto; padding: 1.5rem; }
  .login { max-width: 360px; margin: 4rem auto; padding: 2rem; background: #16161a; border-radius: 14px; border: 1px solid #232328; }
  .login h2 { margin: 0 0 1rem; font-size: 1.25rem; }
  .login input { display: block; width: 100%; padding: 0.6rem 0.85rem; margin-bottom: 0.6rem; border-radius: 8px; border: 1px solid #2a2a30; background: #0b0b0d; color: #fafafa; font-size: 0.95rem; font-family: inherit; }
  .login button { width: 100%; padding: 0.7rem; background: #863bff; color: #fff; border: none; border-radius: 8px; font-weight: 700; cursor: pointer; }
  .login button:hover { background: #6f1ee0; }
  .err { color: #ef4444; font-size: 0.85rem; margin-top: 0.5rem; }
  .tabs { display: flex; gap: 0.25rem; margin-bottom: 1rem; border-bottom: 1px solid #232328; }
  .tabs button { background: transparent; border: none; color: #a1a1aa; padding: 0.6rem 1rem; cursor: pointer; font-weight: 600; border-bottom: 2px solid transparent; }
  .tabs button.on { color: #fafafa; border-color: #863bff; }
  table { width: 100%; border-collapse: collapse; font-size: 0.9rem; }
  th, td { padding: 0.5rem 0.6rem; text-align: left; border-bottom: 1px solid #1c1c21; }
  th { color: #a1a1aa; font-weight: 600; text-transform: uppercase; font-size: 0.75rem; letter-spacing: 0.05em; }
  td .actions { display: flex; gap: 0.35rem; }
  td button { padding: 4px 10px; border-radius: 6px; border: 1px solid #2a2a30; background: #1a1a1f; color: #fafafa; cursor: pointer; font-size: 0.85rem; }
  td button:hover { background: #232328; }
  td button.danger { color: #fca5a5; border-color: rgba(239,68,68,0.3); }
  td button.danger:hover { background: rgba(239,68,68,0.12); }
  .badge { display: inline-block; padding: 2px 8px; border-radius: 999px; font-size: 0.7rem; font-weight: 600; }
  .badge.banned { background: rgba(239,68,68,0.15); color: #fca5a5; }
  .badge.admin { background: rgba(134,59,255,0.18); color: #cab1ff; }
  .badge.verified { background: rgba(34,197,94,0.15); color: #86efac; }
  .badge.unverified { background: rgba(234,179,8,0.15); color: #fde047; }
  .badge.role-user { background: rgba(255,255,255,0.06); color: #a1a1aa; }
  .badge.role-helper { background: rgba(59,130,246,0.18); color: #93c5fd; }
  .badge.role-moderator { background: rgba(168,85,247,0.18); color: #d8b4fe; }
  .badge.role-admin { background: rgba(134,59,255,0.18); color: #cab1ff; }
  .badge.role-super_admin { background: rgba(239,68,68,0.18); color: #fca5a5; }
  td select { background: #1a1a1f; color: #fafafa; border: 1px solid #2a2a30; border-radius: 6px; padding: 4px 8px; font-size: 0.85rem; cursor: pointer; }
  .broadcast textarea { width: 100%; min-height: 90px; padding: 0.6rem; border-radius: 8px; border: 1px solid #2a2a30; background: #0b0b0d; color: #fafafa; font-family: inherit; resize: vertical; }
  .broadcast button { margin-top: 0.5rem; padding: 0.6rem 1.2rem; background: #863bff; color: #fff; border: none; border-radius: 8px; font-weight: 600; cursor: pointer; }
  .empty { padding: 2rem; text-align: center; color: #71717a; }
  .ok { color: #86efac; font-size: 0.85rem; }
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
  tab: 'users',
  users: [], reports: [], pending: [],
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
  STATE.token = res.token;
  STATE.username = res.username;
  localStorage.setItem('phaze_admin_token', res.token);
  localStorage.setItem('phaze_admin_user', res.username);
  render();
}

function logout() {
  STATE.token = '';
  STATE.username = '';
  localStorage.removeItem('phaze_admin_token');
  localStorage.removeItem('phaze_admin_user');
  render();
}

async function loadUsers() {
  STATE.users = await api('GET', '/api/v1/admin/users');
  renderTab();
}
async function loadReports() {
  STATE.reports = await api('GET', '/api/v1/admin/reports');
  renderTab();
}
async function loadPending() {
  STATE.pending = await api('GET', '/api/v1/admin/pending-verifications');
  renderTab();
}

async function banUser(u) {
  const reason = prompt('Reason for ban (shown to user):', 'TOS violation');
  if (reason === null) return;
  await api('POST', '/api/v1/admin/users/' + encodeURIComponent(u) + '/ban', { reason });
  loadUsers();
}
async function unbanUser(u) {
  if (!confirm('Unban ' + u + '?')) return;
  await api('POST', '/api/v1/admin/users/' + encodeURIComponent(u) + '/unban');
  loadUsers();
}
async function setRole(u, role) {
  if (!confirm('Set ' + u + "'s role to " + role + '?')) return;
  await api('POST', '/api/v1/admin/users/' + encodeURIComponent(u) + '/role', { role });
  loadUsers();
}
async function deleteUser(u) {
  if (!confirm('PERMANENTLY DELETE ' + u + '? This cannot be undone.')) return;
  if (prompt('Type the username to confirm:') !== u) return;
  await api('POST', '/api/v1/admin/users/' + encodeURIComponent(u) + '/delete');
  loadUsers();
}
async function resolveReport(id) {
  if (!confirm('Mark report ' + id + ' resolved?')) return;
  await api('POST', '/api/v1/admin/reports/' + id + '/resolve');
  loadReports();
}
async function broadcast(text) {
  if (!text.trim()) return;
  await api('POST', '/api/v1/admin/broadcast', { message: text.trim() });
  alert('Broadcast posted to #announcements');
}

function renderLogin() {
  const root = $('#root');
  root.innerHTML = '';
  const card = html('<form class="login"><h2>Phaze Admin</h2><input name="u" placeholder="Username" autocomplete="username"><input name="p" type="password" placeholder="Password" autocomplete="current-password"><button type="submit">Sign in</button><div class="err" hidden></div></form>');
  card.addEventListener('submit', async (e) => {
    e.preventDefault();
    const u = card.querySelector('[name=u]').value.trim();
    const p = card.querySelector('[name=p]').value;
    const errEl = card.querySelector('.err');
    errEl.hidden = true;
    try { await login(u, p); }
    catch (err) { errEl.textContent = err.message; errEl.hidden = false; }
  });
  root.appendChild(card);
}

function setTab(t) { STATE.tab = t; renderTab(); }

function renderShell() {
  const root = $('#root');
  root.innerHTML = '';
  const hdr = html('<header><h1>🛡 Phaze Admin</h1><div class="tabs"><button data-t="users" class="on">Users</button><button data-t="reports">Reports</button><button data-t="pending">Pending verifications</button><button data-t="broadcast">Broadcast</button></div><span class="me">' + esc(STATE.username) + '</span><button class="logout">Sign out</button></header>');
  hdr.querySelectorAll('.tabs button').forEach((b) => b.addEventListener('click', () => {
    hdr.querySelectorAll('.tabs button').forEach(x => x.classList.remove('on'));
    b.classList.add('on');
    setTab(b.dataset.t);
  }));
  hdr.querySelector('.logout').addEventListener('click', logout);
  root.appendChild(hdr);
  const main = html('<main id="main"></main>');
  root.appendChild(main);
  renderTab();
}

function renderTab() {
  const main = $('#main');
  if (!main) return;
  if (STATE.tab === 'users') {
    if (!STATE.users.length) { main.innerHTML = '<div class="empty">Loading users…</div>'; loadUsers(); return; }
    main.innerHTML = '<table><thead><tr><th>Username</th><th>Email</th><th>Role</th><th>Status</th><th>Created</th><th></th></tr></thead><tbody>' +
      STATE.users.map((u) => '<tr><td>' + esc(u.username) + '</td><td>' + esc(u.email) + '</td>' +
        '<td><span class="badge role-' + esc(u.role || 'user') + '">' + esc(u.role || 'user') + '</span></td>' +
        '<td>' +
        (u.banned ? '<span class="badge banned">banned</span>' : (u.verified ? '<span class="badge verified">verified</span>' : '<span class="badge unverified">unverified</span>')) +
        (u.ban_reason ? '<br><small>' + esc(u.ban_reason) + '</small>' : '') +
        '</td><td>' + esc((u.created_at || '').replace('T', ' ').slice(0, 16)) + '</td><td><div class="actions">' +
        (u.banned
          ? '<button data-act="unban" data-u="' + esc(u.username) + '">Unban</button>'
          : '<button class="danger" data-act="ban" data-u="' + esc(u.username) + '">Ban</button>') +
        '<select data-role-u="' + esc(u.username) + '"><option value="">Set role…</option><option value="user">user</option><option value="helper">helper</option><option value="moderator">moderator</option><option value="admin">admin</option></select>' +
        '<button class="danger" data-act="delete" data-u="' + esc(u.username) + '" style="margin-left:8px">Delete</button>' +
        '</div></td></tr>').join('') + '</tbody></table>';
    main.querySelectorAll('button[data-act]').forEach((b) => b.addEventListener('click', () => {
      const u = b.dataset.u;
      const act = b.dataset.act;
      if (act === 'ban') banUser(u);
      else if (act === 'unban') unbanUser(u);
      else if (act === 'delete') deleteUser(u);
    }));
    main.querySelectorAll('select[data-role-u]').forEach((sel) => sel.addEventListener('change', (e) => {
      const role = e.target.value;
      if (!role) return;
      setRole(sel.dataset.roleU, role);
      e.target.value = '';
    }));
  } else if (STATE.tab === 'reports') {
    if (!STATE.reports.length && STATE.reports !== null) { main.innerHTML = '<div class="empty">Loading reports…</div>'; loadReports(); return; }
    if (!STATE.reports.length) { main.innerHTML = '<div class="empty">No reports.</div>'; return; }
    main.innerHTML = '<table><thead><tr><th>ID</th><th>Reporter</th><th>Subject</th><th>Reason</th><th>Status</th><th></th></tr></thead><tbody>' +
      STATE.reports.map((r) => '<tr><td>' + r.id + '</td><td>' + esc(r.reporter) + '</td><td>' + esc(r.subject) + '</td><td>' + esc(r.reason) + '<br><small>' + esc(r.body) + '</small></td><td>' + esc(r.status) +
        (r.resolved_by ? '<br><small>by ' + esc(r.resolved_by) + '</small>' : '') +
        '</td><td>' + (r.status === 'open' ? '<button data-id="' + r.id + '">Resolve</button>' : '') + '</td></tr>').join('') + '</tbody></table>';
    main.querySelectorAll('button[data-id]').forEach((b) => b.addEventListener('click', () => resolveReport(+b.dataset.id)));
  } else if (STATE.tab === 'pending') {
    if (!STATE.pending.length && STATE.pending !== null) { main.innerHTML = '<div class="empty">Loading pending verifications…</div>'; loadPending(); return; }
    if (!STATE.pending.length) { main.innerHTML = '<div class="empty">No pending verifications in the last 24h.</div>'; return; }
    main.innerHTML = '<table><thead><tr><th>Username</th><th>Email</th><th>Code</th><th>Created</th></tr></thead><tbody>' +
      STATE.pending.map((u) => '<tr><td>' + esc(u.username) + '</td><td>' + esc(u.email) + '</td><td><code>' + esc(u.verification_code) + '</code></td><td>' + esc((u.created_at || '').replace('T', ' ').slice(0, 16)) + '</td></tr>').join('') + '</tbody></table>';
  } else if (STATE.tab === 'broadcast') {
    main.innerHTML = '<div class="broadcast"><h3>Broadcast to #announcements</h3><p style="opacity:0.6;font-size:0.85rem">Posts as ' + esc(STATE.username) + ' into the global Phaze Hub.</p><textarea placeholder="Your announcement…"></textarea><button>Send</button><div class="ok" hidden></div></div>';
    const btn = main.querySelector('button');
    btn.addEventListener('click', async () => {
      const ta = main.querySelector('textarea');
      const ok = main.querySelector('.ok');
      btn.disabled = true;
      try { await broadcast(ta.value); ta.value = ''; ok.textContent = 'Posted.'; ok.hidden = false; }
      catch (e) { alert('Broadcast failed: ' + e.message); }
      finally { btn.disabled = false; }
    });
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
`
