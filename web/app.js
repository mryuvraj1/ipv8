const appRoot = document.querySelector('#app');
const state = { branding: { name: 'IPv8' }, user: null, installs: [], admin: null, users: [], token: null, message: '', messageType: 'success' };

async function api(path, options = {}) {
  const response = await fetch(path, { credentials: 'same-origin', ...options, headers: { ...(options.body ? { 'Content-Type': 'application/json' } : {}), ...(options.headers || {}) } });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || `Request failed (${response.status})`);
  return body;
}
const json = value => JSON.stringify(value);
const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
const logo = () => state.branding.logo ? `<img src="${esc(state.branding.logo)}" alt="">` : '<span class="brand-mark">8</span>';
const brand = () => `<div class="brand">${logo()}<span>${esc(state.branding.name)}</span></div>`;
function toast(message, type = 'success') {
  state.message = message; state.messageType = type;
  let el = document.querySelector('.toast');
  if (!el) { el = document.createElement('div'); el.className = 'toast'; document.body.append(el); }
  el.textContent = message; el.style.color = type === 'error' ? '#ffafb8' : '#a3f2bf';
  clearTimeout(toast.timer); toast.timer = setTimeout(() => el.remove(), 3200);
}
function setTitle(page) { document.title = `${page} · ${state.branding.name}`; }
function currentRoute() { return location.hash.replace(/^#\/?/, '') || 'login'; }
function go(route) { location.hash = `#/${route}`; }

function renderAuth(route) {
  const isRegister = route === 'register';
  const isAdmin = route === 'admin/login';
  const title = isRegister ? 'Create your account' : isAdmin ? 'Administrator access' : 'Welcome back';
  setTitle(isRegister ? 'Register' : isAdmin ? 'Admin login' : 'Login');
  appRoot.innerHTML = `<div class="auth-page"><section class="auth-intro">${brand()}<span class="eyebrow">Experimental networking</span><h1>Explore a different shape for network identity.</h1><p>Generate and manage IPv8 experimental addresses in a contained prototype. Addresses are unique in this installation and have no public internet routing.</p></section><section class="auth-box-wrap"><form class="auth-box" id="auth-form"><span class="eyebrow">${isAdmin ? 'Private control plane' : 'IPv8 control panel'}</span><h2>${title}</h2><p>${isRegister ? 'Start with an email and a strong password.' : isAdmin ? 'Admin accounts are created from the server command line.' : 'Sign in to view your experimental network workspace.'}</p>${isRegister ? `<div class="input-wrap"><label for="displayName">Display name</label><input class="input" id="displayName" name="displayName" required maxlength="60" autocomplete="name"></div>` : ''}<div class="input-wrap"><label for="email">Email</label><input class="input" id="email" name="email" type="email" required maxlength="254" autocomplete="email"></div><div class="input-wrap"><label for="password">Password</label><input class="input" id="password" name="password" type="password" required minlength="${isRegister ? 10 : 1}" maxlength="256" autocomplete="${isRegister ? 'new-password' : 'current-password'}"></div><button class="button" type="submit" style="width:100%;margin-top:6px">${isRegister ? 'Create account' : 'Sign in'}</button><div id="form-error" class="error" role="alert"></div><div class="auth-links">${isRegister ? '<a class="link" href="#/login">Already have an account? Sign in</a>' : !isAdmin ? '<a class="link" href="#/register">Create an account</a>' : '<a class="link" href="#/login">User login</a>'}${!isAdmin ? '<a class="link" href="#/admin/login">Admin login</a>' : ''}</div></form></section></div>`;
  document.querySelector('#auth-form').addEventListener('submit', async e => {
    e.preventDefault();
    const form = new FormData(e.currentTarget);
    const payload = { email: form.get('email'), password: form.get('password') };
    if (isRegister) payload.displayName = form.get('displayName');
    const button = e.currentTarget.querySelector('button'); button.disabled = true;
    try {
      const result = await api(isRegister ? '/api/register' : isAdmin ? '/api/admin/login' : '/api/login', { method: 'POST', body: json(payload) });
      state.user = result.user; state.token = null; go('dashboard');
    } catch (error) { document.querySelector('#form-error').textContent = error.message; }
    finally { button.disabled = false; }
  });
}

function shell(page, content) {
  setTitle(page);
  const admin = state.user.role === 'admin';
  appRoot.innerHTML = `<div class="shell"><aside class="sidebar">${brand()}<nav class="nav"><a href="#/dashboard" class="${currentRoute() === 'dashboard' ? 'active' : ''}"><span class="nav-icon">◈</span>${admin ? 'Overview' : 'Dashboard'}</a>${admin ? '<a href="#/settings" class="' + (currentRoute() === 'settings' ? 'active' : '') + '"><span class="nav-icon">⚙</span>Brand settings</a>' : '<a href="#/settings" class="' + (currentRoute() === 'settings' ? 'active' : '') + '"><span class="nav-icon">◎</span>Account settings</a>'}</nav><div class="side-note">IPv8 is a local experimental prototype. It does not provide global routing or replace IP.</div></aside><main class="main"><header class="topbar"><h1>${esc(page)}</h1><div class="profile-pill">${state.user.avatar ? `<img class="avatar" src="${esc(state.user.avatar)}" alt="">` : `<span class="avatar">${esc((state.user.displayName || state.user.email).slice(0, 1).toUpperCase())}</span>`}<span>${esc(state.user.displayName || state.user.email)}</span><button class="button secondary small" id="logout">Sign out</button></div></header>${content}</main></div>`;
  document.querySelector('#logout').addEventListener('click', async () => {
    try { await api('/api/logout', { method: 'POST', body: '{}' }); } catch (_) { /* Clear the local view even if the session already expired. */ }
    state.user = null; state.installs = []; state.token = null; go('login');
  });
}

async function renderDashboard() {
  const isAdmin = state.user.role === 'admin';
  if (isAdmin) {
    const [overview, userResult] = await Promise.all([api('/api/admin/overview'), api('/api/admin/users')]);
    state.admin = overview; state.users = userResult.users || [];
    shell('Admin overview', `<div class="eyebrow">Control plane</div><h2 class="page-title">Platform overview</h2><p class="sub">Manage persistent platform branding and review account activity.</p><div class="grid"><article class="card third"><span class="eyebrow">Accounts</span><h2 style="font-size:30px;margin-top:10px">${overview.users}</h2><p>Registered user accounts</p></article><article class="card third"><span class="eyebrow">Installations</span><h2 style="font-size:30px;margin-top:10px">${overview.installations}</h2><p>Enrolled local agents</p></article><article class="card third"><span class="eyebrow">Online now</span><h2 style="font-size:30px;margin-top:10px">${overview.online}</h2><p>Heartbeat seen in the last 90 seconds</p></article><article class="card wide"><h2>Registered users</h2><p>Private account directory for administrators.</p><div class="table-scroll"><table class="table"><thead><tr><th>Display name</th><th>Email</th><th>Address</th><th>Created</th></tr></thead><tbody>${state.users.length ? state.users.map(u => `<tr><td>${esc(u.displayName)}</td><td>${esc(u.email)}</td><td class="mono">${esc(u.address || 'Not generated')}</td><td>${esc(new Date(u.createdAt).toLocaleDateString())}</td></tr>`).join('') : '<tr><td colspan="4">No registered accounts.</td></tr>'}</tbody></table></div></article></div>`);
    return;
  }
  const [config, installs] = await Promise.all([api('/api/config'), api('/api/installations')]);
  state.installs = installs.installations || [];
  const address = config.address || '';
  shell('Dashboard', `<div class="eyebrow">Your experimental network workspace</div><h2 class="page-title">A new identity for local experiments.</h2><p class="sub">Generate a stable IPv8 identifier, inspect its config, or enroll an agent for an authenticated heartbeat.</p><div class="notice"><strong>Experimental scope:</strong> this address is not routed on the public internet. Packet delivery currently works only in the local loopback simulator.</div><div class="grid"><article class="card wide"><div class="row" style="justify-content:space-between"><div><span class="eyebrow">IPv8 address · version 1</span><h2 style="margin-top:7px">Network identity</h2><p>128-bit random identifier, unique among this project's stored accounts.</p></div><button class="button" id="generate-address" ${address ? 'disabled' : ''}>${address ? 'Address generated' : 'Generate address'}</button></div><div class="address-box">${address ? `<span class="address" id="address-value">${esc(address)}</span><button class="button secondary small" id="copy-address">Copy address</button>` : '<span class="empty">Generate an address to view your identifier and client configuration.</span>'}</div>${address ? `<div class="row" style="margin-top:16px"><span class="status"><i class="dot"></i>Local prototype identifier</span></div><h2 style="margin:22px 0 9px">Client configuration</h2><pre class="mono" id="config">${esc(JSON.stringify({ experimental: true, address, network: 'local-prototype-only', note: 'No public routing or transport is implemented.' }, null, 2))}</pre><button class="button secondary small" id="copy-config">Copy config</button>` : ''}</article><article class="card"><span class="eyebrow">Local simulator</span><h2 style="margin-top:7px">Send a loopback packet</h2><p>Encodes, validates, decodes, and delivers a packet back to this address inside the panel process.</p><form id="simulate-form"><div class="input-wrap"><label for="destination">Destination address</label><input class="input" id="destination" value="${esc(address)}" required ${address ? '' : 'disabled'}></div><div class="input-wrap"><label for="payload">Payload · up to 1200 bytes</label><textarea class="input" id="payload" rows="3" maxlength="1200" required ${address ? '' : 'disabled'}></textarea></div><button class="button secondary" ${address ? '' : 'disabled'}>Encode and deliver locally</button></form><div id="sim-result" class="caption" style="margin-top:12px"></div></article><article class="card"><span class="eyebrow">Agent enrollment</span><h2 style="margin-top:7px">Install an agent</h2><p>Creates a single-use token that expires after 15 minutes. The agent reports an authenticated heartbeat every 30 seconds.</p><button class="button secondary" id="make-token">Create enrollment command</button><div id="install-command" style="margin-top:15px"></div><div class="caption" style="margin-top:10px">The token is shown once. Treat it as a temporary credential. Install command supports Debian, Ubuntu, Fedora, and Arch Linux with systemd on amd64 or arm64.</div></article><article class="card wide"><div class="row" style="justify-content:space-between"><div><span class="eyebrow">Agent status</span><h2 style="margin-top:7px">Your installations</h2><p>Online status means an authenticated heartbeat arrived in the past 90 seconds.</p></div><button class="button secondary small" id="refresh-installations">Refresh</button></div><div id="install-list">${renderInstallations()}</div></article></div>`);
  document.querySelector('#generate-address')?.addEventListener('click', async e => { e.currentTarget.disabled = true; try { await api('/api/address', { method: 'POST', body: '{}' }); toast('IPv8 address generated'); await render(); } catch (error) { toast(error.message, 'error'); e.currentTarget.disabled = false; } });
  document.querySelector('#copy-address')?.addEventListener('click', () => copy(address));
  document.querySelector('#copy-config')?.addEventListener('click', () => copy(document.querySelector('#config').textContent));
  document.querySelector('#simulate-form')?.addEventListener('submit', async e => { e.preventDefault(); const out = document.querySelector('#sim-result'); out.textContent = 'Validating packet…'; try { const r = await api('/api/simulate', { method: 'POST', body: json({ destination: document.querySelector('#destination').value, payload: document.querySelector('#payload').value }) }); out.textContent = `Delivered locally · ${r.packetBytes} packet bytes · payload: ${r.payload}`; } catch (error) { out.textContent = error.message; out.className = 'error'; } });
  document.querySelector('#make-token')?.addEventListener('click', async e => { e.currentTarget.disabled = true; try { const r = await api('/api/installations/token', { method: 'POST', body: '{}' }); state.token = r.token; const command = `curl -fsSL https://raw.githubusercontent.com/mryuvraj1/ipv8/main/install.sh | sudo sh -s -- --server ${shellQuote(location.origin)} --token ${shellQuote(r.token)}`; document.querySelector('#install-command').innerHTML = `<pre class="mono" id="install-command-value">${esc(command)}</pre><button class="button secondary small" id="copy-install-command">Copy command</button><div class="caption" style="margin-top:8px">Expires ${esc(new Date(r.expiresAt).toLocaleTimeString())} · single use</div>`; document.querySelector('#copy-install-command').addEventListener('click', () => copy(command)); } catch (error) { toast(error.message, 'error'); } finally { e.currentTarget.disabled = false; } });
  document.querySelector('#refresh-installations')?.addEventListener('click', async () => { const r = await api('/api/installations'); state.installs = r.installations || []; document.querySelector('#install-list').innerHTML = renderInstallations(); wireRevoke(); });
  wireRevoke();
}

function renderInstallations() {
  if (!state.installs.length) return '<div class="empty">No installations enrolled yet.</div>';
  return `<div class="table-scroll"><table class="table"><thead><tr><th>Name</th><th>Platform</th><th>Status</th><th>Last heartbeat</th><th></th></tr></thead><tbody>${state.installs.map(i => `<tr><td>${esc(i.name)}</td><td>${esc(i.platform)}</td><td><span class="status"><i class="dot ${i.status === 'online' ? 'online' : ''}"></i>${esc(i.status)}</span></td><td>${i.lastSeen && !i.lastSeen.startsWith('0001-') ? esc(new Date(i.lastSeen).toLocaleString()) : 'Never'}</td><td><button class="button danger small revoke" data-id="${esc(i.id)}">Revoke</button></td></tr>`).join('')}</tbody></table></div>`;
}
function wireRevoke() { document.querySelectorAll('.revoke').forEach(button => button.addEventListener('click', async () => { if (!confirm('Revoke this installation? Its agent credentials will stop working.')) return; try { await api(`/api/installations/${encodeURIComponent(button.dataset.id)}`, { method: 'DELETE' }); state.installs = state.installs.filter(i => i.id !== button.dataset.id); document.querySelector('#install-list').innerHTML = renderInstallations(); wireRevoke(); toast('Installation revoked'); } catch (error) { toast(error.message, 'error'); } })); }

function shellQuote(value) { return `'${String(value).replaceAll("'", "'\\''")}'`; }
async function copy(value) { try { await navigator.clipboard.writeText(value); toast('Copied to clipboard'); } catch (_) { toast('Clipboard access unavailable in this browser', 'error'); } }

function renderSettings() {
  const admin = state.user.role === 'admin';
  if (admin) {
    shell('Brand settings', `<div class="eyebrow">Platform identity</div><h2 class="page-title">Make the panel yours.</h2><p class="sub">Changes are stored in the project database and applied on the public auth pages, navigation, dashboards, and browser title.</p><div class="grid"><article class="card wide"><h2>Platform branding</h2><p>Use a 2–40 character name and optional PNG or JPEG logo up to 128 KiB and 4096 by 4096 pixels.</p><form id="brand-form"><div class="input-wrap"><label for="platformName">Platform name</label><input class="input" name="name" id="platformName" maxlength="40" required value="${esc(state.branding.name)}"></div><div class="input-wrap"><label for="brand-logo">Logo image</label><input class="input" type="file" id="brand-logo" accept="image/png,image/jpeg"><div class="row"><img class="logo-preview" id="brand-preview" src="${esc(state.branding.logo || '')}" alt="" ${state.branding.logo ? '' : 'hidden'}><span class="caption">Leave empty to keep the current logo. Select an image to replace it.</span>${state.branding.logo ? '<button class="button secondary small" type="button" id="remove-logo">Remove logo</button>' : ''}</div></div><button class="button">Save branding</button><div id="brand-error" class="error"></div></form></article></div>`);
    let logoData = state.branding.logo || '';
    document.querySelector('#brand-logo').addEventListener('change', async e => { try { logoData = await imageData(e.target.files[0]); const img = document.querySelector('#brand-preview'); img.src = logoData; img.hidden = false; } catch (error) { document.querySelector('#brand-error').textContent = error.message; e.target.value = ''; } });
    document.querySelector('#remove-logo')?.addEventListener('click', () => { logoData = ''; document.querySelector('#brand-preview').hidden = true; document.querySelector('#brand-logo').value = ''; });
    document.querySelector('#brand-form').addEventListener('submit', async e => { e.preventDefault(); try { state.branding = await api('/api/admin/branding', { method: 'PATCH', body: json({ name: new FormData(e.currentTarget).get('name'), logo: logoData }) }); await render(); toast('Branding saved'); } catch (error) { document.querySelector('#brand-error').textContent = error.message; } });
    return;
  }
    shell('Account settings', `<div class="eyebrow">Personal profile</div><h2 class="page-title">Account settings</h2><p class="sub">Change how your name and avatar appear in the panel. Your private address and installations remain tied to this account.</p><div class="grid"><article class="card wide"><h2>Profile</h2><p>Avatar data is stored locally in the panel database, limited to 128 KiB and 4096 by 4096 pixels.</p><form id="profile-form"><div class="input-wrap"><label for="display-name">Display name</label><input class="input" name="displayName" id="display-name" required maxlength="60" value="${esc(state.user.displayName)}"></div><div class="input-wrap"><label for="avatar">Avatar</label><input class="input" type="file" id="avatar" accept="image/png,image/jpeg"><div class="row"><img class="logo-preview" id="avatar-preview" src="${esc(state.user.avatar || '')}" alt="" ${state.user.avatar ? '' : 'hidden'}><span class="caption">Choose a PNG or JPEG. Leave unchanged to keep your current avatar.</span><button class="button secondary small" type="button" id="remove-avatar">Remove avatar</button></div></div><button class="button">Save profile</button><div id="profile-error" class="error"></div></form></article></div>`);
  let avatarData = state.user.avatar || '';
  document.querySelector('#avatar').addEventListener('change', async e => { try { avatarData = await imageData(e.target.files[0]); const img = document.querySelector('#avatar-preview'); img.src = avatarData; img.hidden = false; } catch (error) { document.querySelector('#profile-error').textContent = error.message; e.target.value = ''; } });
  document.querySelector('#remove-avatar').addEventListener('click', () => { avatarData = ''; document.querySelector('#avatar-preview').hidden = true; document.querySelector('#avatar').value = ''; });
  document.querySelector('#profile-form').addEventListener('submit', async e => { e.preventDefault(); try { const result = await api('/api/me', { method: 'PATCH', body: json({ displayName: new FormData(e.currentTarget).get('displayName'), avatar: avatarData }) }); state.user = result.user; await render(); toast('Profile saved'); } catch (error) { document.querySelector('#profile-error').textContent = error.message; } });
}

async function imageData(file) {
  if (!file || !['image/png', 'image/jpeg'].includes(file.type) || file.size > 128 * 1024) throw new Error('Choose a PNG or JPEG image no larger than 128 KiB.');
  return new Promise((resolve, reject) => { const r = new FileReader(); r.onload = () => resolve(r.result); r.onerror = () => reject(new Error('Could not read image')); r.readAsDataURL(file); });
}

async function render() {
  if (!state.user) {
    try { const result = await api('/api/me'); state.user = result.user; }
    catch (_) { state.user = null; }
  }
  const route = currentRoute();
  if (!state.user) { renderAuth(['register', 'admin/login'].includes(route) ? route : 'login'); return; }
  if (state.user.role === 'user' && route.startsWith('admin')) { go('dashboard'); return; }
  if (route === 'settings') { renderSettings(); return; }
  try { await renderDashboard(); }
  catch (error) { shell('Dashboard', `<div class="error">${esc(error.message)} <button class="button secondary small" id="try-again">Retry</button></div>`); document.querySelector('#try-again').addEventListener('click', render); }
}

window.addEventListener('hashchange', render);
(async function start() {
  try { state.branding = await api('/api/branding'); } catch (_) { /* display the default brand while the API is unavailable */ }
  await render();
})();
