// Kota panel: renders usage, settings and the update banner; talks to Go
// through window.go.panel.API and the Wails runtime events.

// This page is the panel. Data is fetched in Go (internal/application) and pushed with the
// "usage" event; the taskbar pill is a native window drawn by Go.
// Official app icons (taken from the installed apps). A provider without an
// image falls back to a coloured letter.
const LOGOS = {
  claude:      { img: 'logos/claude.png',      bg: '#d97757', ch: 'C' },
  antigravity: { img: 'logos/antigravity.png', bg: '#4f7cff', ch: 'A' },
  codex:       { img: 'logos/codex.png',       bg: '#10a37f', ch: 'X' },
  gemini:      { img: 'logos/gemini.png',      bg: '#4285f4', ch: 'G' },
  openrouter:  { img: 'logos/openrouter.png',  bg: '#6467f2', ch: 'O' },
  copilot:     { img: 'logos/copilot.png',     bg: '#2b3137', ch: 'C' },
  cursor:      { img: 'logos/cursor.png',      bg: '#1b1b1b', ch: 'C' },
};
function logoHTML(u) {
  const lg = logoOf(u);
  const letter = `<span class="logo" style="background:${lg.bg}">${esc(lg.ch)}</span>`;
  if (!lg.img) return letter;
  // onerror swaps in the letter if the image is missing.
  return `<img class="logo img" src="${lg.img}" alt="" onerror="this.outerHTML=this.dataset.fb" data-fb="${esc(letter)}">`;
}
const $ = (id) => document.getElementById(id);
const api = () => window.go && window.go.panel && window.go.panel.API;
let lastUpdate = null, loading = false, lastData = [], shownAt = 0;

const store = {
  get(k, d) { try { const v = localStorage.getItem(k); return v === null ? d : JSON.parse(v); } catch { return d; } },
  set(k, v) { try { localStorage.setItem(k, JSON.stringify(v)); } catch {} },
};
let openDetails = store.get('openDetails', {});

// Settings come from Go (config.json). taskbar: provider IDs (max 2),
// display: 'used' | 'remaining'.
let settings = { taskbar: null, display: 'used', width: 200, minWidth: 180, maxWidth: 320 };

// Percent shown for a bar, following the display setting. Colour always
// reflects how much is used, so red means "close to the limit" either way.
function shown(usedPct) {
  const used = clampPct(usedPct);
  const rem = settings.display === 'remaining';
  return { used, value: rem ? 100 - used : used, word: rem ? 'left' : 'used' };
}
function pctHTML(p) { return `${Math.floor(p.value)}%<small>${p.word}</small>`; }

// Colour by how much is USED.
function color(used) {
  if (used >= 85) return 'var(--bad)';
  if (used >= 60) return 'var(--warn)';
  return 'var(--ok)';
}
const clampPct = (v) => Math.max(0, Math.min(100, v || 0));

function resetShort(iso) {
  if (!iso) return '';
  const d = new Date(iso), ms = d - Date.now();
  if (isNaN(ms)) return '';
  if (ms <= 0) return 'now';
  const m = Math.round(ms / 60000);
  if (m < 60) return m + 'm';
  if (m < 24 * 60) return Math.floor(m / 60) + 'h ' + (m % 60) + 'm';
  return d.toLocaleDateString('en-GB', { weekday: 'short' }) + ' ' +
         d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });
}
function resetLong(iso) {
  const d = new Date(iso);
  return isNaN(d) ? '' : 'Resets ' + d.toLocaleString('en-GB', { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
}
function esc(s) {
  return String(s ?? '').replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
}
function logoOf(u) { return LOGOS[u.id] || { bg: '#555', ch: (u.name || '?')[0] }; }

// Providers on the taskbar: the user's choice, else the first two
// (same rule as the native pill, pill_model.go).
function taskbarItems(items) {
  const ids = (settings.taskbar || []).filter(id => items.some(u => u.id === id));
  if (!ids.length) return items.slice(0, 2);
  return ids.slice(0, 2).map(id => items.find(u => u.id === id));
}

/* ---------- panel ---------- */
// Pace: how much of the window's time has elapsed and whether the current
// burn rate reaches 100% before the reset (same rules as pill_model.go).
const MIN_PACE_USED = 20;
function pace(b) {
  const reset = Date.parse(b.resetsAt || ''), win = (b.windowSec || 0) * 1000, now = Date.now();
  if (isNaN(reset) || win <= 0 || reset <= now) return null;
  const left = Math.min(reset - now, win), elapsed = win - left;
  const out = { elapsedPct: 100 * elapsed / win, runsOutIn: 0 };
  const used = clampPct(b.usedPct);
  if (used >= MIN_PACE_USED && used < 100 && elapsed > 0) {
    const toLimit = (100 - used) / (used / elapsed);
    if (toLimit < left) out.runsOutIn = toLimit;
  }
  return out;
}
function durShort(ms) {
  const m = Math.round(ms / 60000);
  if (m < 1) return '<1m';
  if (m < 60) return m + 'm';
  if (m < 1440) return Math.floor(m / 60) + 'h ' + (m % 60) + 'm';
  return Math.floor(m / 1440) + 'd ' + Math.floor((m % 1440) / 60) + 'h';
}
function barRow(b) {
  const p = shown(b.usedPct);
  const pc = pace(b);
  const marker = pc ? (settings.display === 'remaining' ? 100 - pc.elapsedPct : pc.elapsedPct) : -1;
  const tip = [b.tooltip, resetLong(b.resetsAt),
    pc ? `${Math.round(pc.elapsedPct)}% of the window has passed` : ''].filter(Boolean).join('\n');
  return `<div class="row" title="${esc(tip)}">
    <span class="label">${esc(b.label)}</span>
    <div class="track">
      <div class="fill" style="width:${p.value}%;background:${color(p.used)}"></div>
      ${marker >= 0 ? `<div class="marker" style="left:${marker}%"></div>` : ''}
    </div>
    <span class="reset">${esc(resetShort(b.resetsAt))}</span>
    <span class="pct">${pctHTML(p)}</span>
  </div>
  ${pc && pc.runsOutIn ? `<div class="pace-warn">At this pace: limit reached in ${durShort(pc.runsOutIn)}</div>` : ''}`;
}
// Token dropdown: one block per time window, side by side.
function tokenBlocks(tw) {
  return `<div class="tok-grid">${tw.map(w => `
    <div class="tok">
      <div class="tok-title">${esc(w.label)}</div>
      <div class="stat"><span class="k">Input</span><span class="v">${esc(w.input)}</span></div>
      <div class="stat"><span class="k">Output</span><span class="v">${esc(w.output)}</span></div>
      <div class="stat total"><span class="k">Total Tokens</span><span class="v">${esc(w.total)}</span></div>
    </div>`).join('')}</div>`;
}
function card(u) {
  const tokens = u.tokens || [];
  const hist = historyById[u.id];
  const hasDetails = tokens.length > 0 || (u.details || []).length > 0 || !!hist;
  const open = !!openDetails[u.id];
  const groups = (u.groups || []).map(g => `
    ${g.name ? `<div class="group-name" title="${esc(g.tooltip)}">${esc(g.name)}</div>` : ''}
    ${(g.bars || []).map(barRow).join('')}
  `).join('');
  const details = hasDetails ? `<div class="details ${open ? 'open' : ''}">
      ${tokens.length ? tokenBlocks(tokens) : ''}
      ${(u.details || []).map(s => `<div class="stat"><span class="k">${esc(s.label)}</span><span class="v">${esc(s.value)}</span></div>`).join('')}
      ${hist ? histChart(u.id, hist) : ''}
    </div>` : '';
  return `<section class="card">
    <div class="card-head">
      ${logoHTML(u)}
      <span class="name">${esc(u.name)}</span>
      ${u.plan ? `<span class="pill">${esc(u.plan)}</span>` : ''}
      ${hasDetails ? `<button class="btn toggle ${open ? 'open' : ''}" data-id="${esc(u.id)}" title="Details">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M6 9l6 6 6-6"/></svg></button>` : ''}
    </div>
    ${u.error ? `<div class="err">${esc(u.error)}</div>` : ''}
    ${groups}
    ${u.note ? `<div class="note">${esc(u.note)}</div>` : ''}
    ${details}
  </section>`;
}

// A window whose reset time has passed has been refilled, even if the data
// is from before that moment (cache, rate limit, closed app): show it as 0%.
function liveView(items) {
  const now = Date.now();
  return items.map(u => ({
    ...u,
    groups: (u.groups || []).map(g => ({
      ...g,
      bars: (g.bars || []).map(b => {
        const t = b.resetsAt ? Date.parse(b.resetsAt) : NaN;
        return !isNaN(t) && t <= now ? { ...b, usedPct: 0, resetsAt: '' } : b;
      }),
    })),
  }));
}

function render(raw) {
  lastData = raw;
  const items = liveView(raw);
  $('list').innerHTML = items.map(card).join('') || '<div class="empty">No providers</div>';
  if (!$('settings').hidden) renderSettings();
}

/* ---------- settings ---------- */
function currentTaskbarIds() { return taskbarItems(lastData).map(u => u.id); }
function renderSettings() {
  const chosen = currentTaskbarIds();
  $('taskbarChoices').innerHTML = lastData.map(u => {
    const on = chosen.includes(u.id);
    const disabled = !on && chosen.length >= 2;
    return `<label class="choice ${disabled ? 'disabled' : ''}">
      <input type="checkbox" data-id="${esc(u.id)}" ${on ? 'checked' : ''} ${disabled ? 'disabled' : ''}>
      ${logoHTML(u)}<span>${esc(u.name)}</span>
    </label>`;
  }).join('') || '<div class="s-hint">No providers found.</div>';
  for (const b of $('displaySeg').querySelectorAll('button')) b.classList.toggle('on', b.dataset.v === settings.display);
  const s = $('widthSlider');
  if (settings.minWidth) { s.min = settings.minWidth; s.max = settings.maxWidth; }
  if (document.activeElement !== s) s.value = settings.width;
  $('widthVal').textContent = settings.width + ' px';
  $('autostart').checked = !!settings.autostart;
  $('notify').checked = settings.notify !== false;
  $('follow').checked = !!settings.follow;
  $('multi').checked = !!settings.multi;
  $('updateCheck').checked = settings.update !== false;
  fillLevels();
}
// Notification thresholds: warn 50–95, critical above warn up to 100.
function fillLevels() {
  const w = settings.warn || 80, c = settings.critical || 95;
  const opts = (from, to, v) => {
    let h = '';
    for (let p = from; p <= to; p += 5) h += `<option value="${p}"${p === v ? ' selected' : ''}>${p}%</option>`;
    return h;
  };
  $('warnAt').innerHTML = opts(50, 95, w);
  $('critAt').innerHTML = opts(Math.ceil((w + 1) / 5) * 5, 100, c);
  $('warnAt').disabled = $('critAt').disabled = settings.notify === false;
}
async function saveLevels() {
  const r = await api().SetNotifyLevels(+$('warnAt').value, +$('critAt').value);
  settings.warn = r[0]; settings.critical = r[1];
  fillLevels();
}
$('warnAt').addEventListener('change', saveLevels);
$('critAt').addEventListener('change', saveLevels);
$('updateCheck').addEventListener('change', (e) => {
  settings.update = e.target.checked;
  api().SetUpdateCheck(settings.update);
});
// A newer release: one line under the header, opens its GitHub page.
function showUpdate(u) {
  const b = $('update');
  b.hidden = !u;
  if (u) b.textContent = `Kota ${u.version} is available — view release`;
}
$('update').onclick = () => api().OpenUpdate();
$('multi').addEventListener('change', (e) => {
  settings.multi = e.target.checked;
  api().SetAllMonitors(settings.multi);
});
$('follow').addEventListener('change', (e) => {
  settings.follow = e.target.checked;
  api().SetFollowActive(settings.follow);
});
$('notify').addEventListener('change', (e) => {
  settings.notify = e.target.checked;
  api().SetNotifications(settings.notify);
  fillLevels();
});
$('testNotify').onclick = () => api().TestNotification();
$('resetPos').onclick = () => api().ResetPillPosition();
$('autostart').addEventListener('change', async (e) => {
  settings.autostart = await api().SetAutostart(e.target.checked);
  e.target.checked = settings.autostart; // reflect what actually happened
});
// Width slider: the taskbar pill resizes live while dragging, saved on release.
$('widthSlider').addEventListener('input', (e) => {
  settings.width = +e.target.value;
  $('widthVal').textContent = settings.width + ' px';
  api().SetCompactWidth(settings.width, false);
});
$('widthSlider').addEventListener('change', (e) => {
  settings.width = +e.target.value;
  api().SetCompactWidth(settings.width, true);
});
function saveSettings() {
  api().SaveSettings(settings);
  render(lastData);
}
$('taskbarChoices').addEventListener('change', (e) => {
  const id = e.target.dataset.id;
  if (!id) return;
  let ids = currentTaskbarIds().filter(x => x !== id);
  if (e.target.checked) ids.push(id);
  // keep provider display order on the pill
  settings.taskbar = lastData.map(u => u.id).filter(x => ids.includes(x)).slice(0, 2);
  saveSettings();
});
$('displaySeg').addEventListener('click', (e) => {
  const v = e.target.dataset && e.target.dataset.v;
  if (!v || v === settings.display) return;
  settings.display = v;
  saveSettings();
});
function showSettings(on) {
  $('settings').hidden = !on;
  $('list').hidden = on;
  $('settingsBtn').classList.toggle('active', on);
  if (on) { renderSettings(); loadCredentials(); }
}

/* ---------- API keys ---------- */
// Rendered on their own (not on every data refresh) so typing isn't lost.
let creds = [], editingCred = null;
async function loadCredentials() {
  try { creds = (await api().GetCredentials()) || []; } catch { creds = []; }
  renderCredentials();
}
function renderCredentials() {
  $('credList').innerHTML = creds.map(c => {
    const editing = !c.set || editingCred === c.id;
    return `<div class="cred" data-id="${esc(c.id)}">
      <div class="cred-head">
        ${logoHTML({ id: c.id, name: c.name })}<span class="cred-name">${esc(c.name)}</span>
        <span class="cred-state ${c.set ? 'on' : ''}">${c.set ? 'Key saved' : 'No key'}</span>
      </div>
      ${editing ? `
        <div class="cred-row">
          <input type="password" class="cred-input" placeholder="Paste API key" autocomplete="off" spellcheck="false">
          <button class="link-btn cred-save">Save</button>
          ${c.set ? '<button class="link-btn cred-cancel">Cancel</button>' : ''}
        </div>
        <div class="s-hint cred-hint">${esc(c.hint)} <a href="#" class="cred-get">Get a key</a></div>`
      : `<div class="cred-row">
          <button class="link-btn cred-replace">Replace</button>
          <button class="link-btn cred-remove">Remove</button>
        </div>`}
      <div class="err cred-err"></div>
    </div>`;
  }).join('') || '<div class="s-hint">No providers need a key.</div>';
}
$('credList').addEventListener('click', async (e) => {
  const row = e.target.closest('.cred');
  if (!row) return;
  const id = row.dataset.id;
  if (e.target.closest('.cred-get')) { e.preventDefault(); api().OpenKeyPage(id); return; }
  if (e.target.closest('.cred-replace')) { editingCred = id; renderCredentials(); focusCred(id); return; }
  if (e.target.closest('.cred-cancel')) { editingCred = null; renderCredentials(); return; }
  if (e.target.closest('.cred-remove')) { await api().ClearCredential(id); editingCred = null; loadCredentials(); return; }
  if (e.target.closest('.cred-save')) {
    const input = row.querySelector('.cred-input');
    const err = await api().SetCredential(id, input.value);
    input.value = '';
    if (err) { row.querySelector('.cred-err').textContent = err; return; }
    editingCred = null;
    loadCredentials();
  }
});
function focusCred(id) { const i = $('credList').querySelector(`.cred[data-id="${id}"] .cred-input`); if (i) i.focus(); }
$('credList').addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && e.target.classList.contains('cred-input')) e.target.closest('.cred').querySelector('.cred-save').click();
});
$('settingsBtn').onclick = () => showSettings($('settings').hidden);

$('list').addEventListener('click', (e) => {
  const t = e.target.closest('.toggle');
  if (!t) return;
  openDetails[t.dataset.id] = !openDetails[t.dataset.id];
  store.set('openDetails', openDetails);
  render(lastData);
});


/* ---------- open / close ---------- */
// Go shows the panel (pill click) and hides it; the page only asks to hide.
function hidePanel() { showSettings(false); api().HidePanel(); }
window.addEventListener('keydown', (e) => { if (e.key === 'Escape') hidePanel(); });
// Losing focus closes the panel (ignore the moment right after it appears).
window.addEventListener('blur', () => { if (Date.now() - shownAt > 300) hidePanel(); });

/* ---------- data ---------- */
function gotData(data) {
  render(data || []);
  lastUpdate = Date.now();
  tickAgo();
  loadHistory();
}
function tickAgo() {
  if (!lastUpdate) return;
  const s = Math.round((Date.now() - lastUpdate) / 1000);
  $('ago').textContent = s < 60 ? 'just now' : Math.floor(s / 60) + 'm ago';
  if (lastData.length) render(lastData); // keep reset countdowns fresh
}
async function refresh() {
  if (loading || !api()) return;
  loading = true;
  $('refresh').classList.add('spin');
  try {
    gotData(await api().Refresh());
  } catch (e) {
    $('list').innerHTML = `<div class="empty err">${esc(e)}</div>`;
  } finally {
    loading = false;
    $('refresh').classList.remove('spin');
  }
}

$('refresh').onclick = refresh;
$('collapse').onclick = hidePanel;
$('close').onclick = () => api().Quit();

async function boot() {
  if (!api() || !(window.runtime && window.runtime.EventsOn)) return setTimeout(boot, 50);
  window.runtime.EventsOn('usage', gotData);
  window.runtime.EventsOn('update', showUpdate);
  api().GetUpdate().then(showUpdate).catch(() => {});
  window.runtime.EventsOn('panel:shown', () => { shownAt = Date.now(); tickAgo(); });
  window.runtime.EventsOn('panel:settings', () => { shownAt = Date.now(); showSettings(true); });
  try { settings = await api().GetSettings(); } catch {}
  try { gotData(await api().GetUsage()); } catch {}
}
boot();
setInterval(tickAgo, 30000);

// Chart hover (chart.js).
$('list').addEventListener('mousemove', (e) => {
  const plot = e.target.closest('.hist-plot');
  document.querySelectorAll('.hist-plot').forEach(p => { if (p !== plot) histLeave(p); });
  if (plot) histHover(plot, e.clientX);
});
$('list').addEventListener('mouseleave', () => document.querySelectorAll('.hist-plot').forEach(histLeave));
