// Kota panel: renders usage, settings and the update banner; talks to Go
// through window.go.panel.API and the Wails runtime events. Pure helpers
// (translation, formatting, pace) are in core.js, charts in chart.js.

// Official app icons. A provider without an image falls back to a letter.
const LOGOS = {
  claude:      { img: 'logos/claude.png',      bg: '#d97757', ch: 'C' },
  antigravity: { img: 'logos/antigravity.png', bg: '#4f7cff', ch: 'A' },
  codex:       { img: 'logos/codex.png',       bg: '#10a37f', ch: 'X' },
  gemini:      { img: 'logos/gemini.png',      bg: '#4285f4', ch: 'G' },
  openrouter:  { img: 'logos/openrouter.png',  bg: '#6467f2', ch: 'O' },
  copilot:     { img: 'logos/copilot.png',     bg: '#2b3137', ch: 'C' },
  cursor:      { img: 'logos/cursor.png',      bg: '#1b1b1b', ch: 'C' },
};
function logoOf(u) { return LOGOS[u.id] || { bg: '#555', ch: (u.name || '?')[0] }; }
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

// Settings come from Go (config.json).
let settings = { taskbar: null, display: 'used', width: 200, minWidth: 180, maxWidth: 320 };

/* ---------- language ---------- */
// The page's fixed texts carry their English text as the key.
function applyStatic() {
  document.documentElement.lang = LANG;
  document.querySelectorAll('[data-i18n]').forEach(el => {
    if (el.dataset.en === undefined) el.dataset.en = el.textContent;
    el.textContent = t(el.dataset.en);
  });
  document.querySelectorAll('[data-i18n-title]').forEach(el => {
    if (el.dataset.enTitle === undefined) el.dataset.enTitle = el.title;
    el.title = t(el.dataset.enTitle);
  });
}
async function loadLanguage() {
  let dict = {};
  try { dict = (await api().GetTranslations()) || {}; } catch {}
  setLanguage(settings.lang, dict);
  applyStatic();
}

/* ---------- panel ---------- */
function pctHTML(p) { return `${Math.floor(p.value)}%<small>${esc(p.word)}</small>`; }
function barRow(b) {
  const p = shown(b.usedPct, settings.display);
  const pc = pace(b);
  const marker = pc ? (settings.display === 'remaining' ? 100 - pc.elapsedPct : pc.elapsedPct) : -1;
  const tip = [b.tooltip, resetLong(b.resetsAt),
    pc ? t('%d%% of the window has passed', Math.round(pc.elapsedPct)) : ''].filter(Boolean).join('\n');
  return `<div class="row" title="${esc(tip)}">
    <span class="label">${esc(t(b.label))}</span>
    <div class="track">
      <div class="fill" style="width:${p.value}%;background:${color(p.used)}"></div>
      ${marker >= 0 ? `<div class="marker" style="left:${marker}%"></div>` : ''}
    </div>
    <span class="reset">${esc(resetShort(b.resetsAt))}</span>
    <span class="pct">${pctHTML(p)}</span>
  </div>
  ${pc && pc.runsOutIn ? `<div class="pace-warn">${esc(t('At this pace: limit reached in %s', durShort(pc.runsOutIn)))}</div>` : ''}`;
}
// Token dropdown: one block per time window, side by side.
function tokenBlocks(tw) {
  return `<div class="tok-grid">${tw.map(w => `
    <div class="tok">
      <div class="tok-title">${esc(t(w.label))}</div>
      <div class="stat"><span class="k">${esc(t('Input'))}</span><span class="v">${esc(w.input)}</span></div>
      <div class="stat"><span class="k">${esc(t('Output'))}</span><span class="v">${esc(w.output)}</span></div>
      <div class="stat total"><span class="k">${esc(t('Total Tokens'))}</span><span class="v">${esc(w.total)}</span></div>
    </div>`).join('')}</div>`;
}
function card(u) {
  const tokens = u.tokens || [];
  const hist = historyById[u.id];
  const daily = dailyChart(u);
  const hasDetails = tokens.length > 0 || (u.details || []).length > 0 || !!hist || !!daily;
  const open = !!openDetails[u.id];
  const groups = (u.groups || []).map(g => `
    ${g.name ? `<div class="group-name" title="${esc(g.tooltip)}">${esc(t(g.name))}</div>` : ''}
    ${(g.bars || []).map(barRow).join('')}
  `).join('');
  const details = hasDetails ? `<div class="details ${open ? 'open' : ''}">
      ${tokens.length ? tokenBlocks(tokens) : ''}
      ${(u.details || []).map(s => `<div class="stat"><span class="k">${esc(t(s.label))}</span><span class="v">${esc(s.value)}</span></div>`).join('')}
      ${daily}
      ${hist ? histChart(u.id, hist) : ''}
    </div>` : '';
  return `<section class="card">
    <div class="card-head">
      ${logoHTML(u)}
      <span class="name">${esc(u.name)}</span>
      ${u.plan ? `<span class="pill">${esc(t(u.plan))}</span>` : ''}
      ${hasDetails ? `<button class="btn toggle ${open ? 'open' : ''}" data-id="${esc(u.id)}" title="${esc(t('Details'))}">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M6 9l6 6 6-6"/></svg></button>` : ''}
    </div>
    ${u.error ? `<div class="err">${esc(t(u.error))}</div>` : ''}
    ${groups}
    ${u.note ? `<div class="note">${esc(t(u.note))}</div>` : ''}
    ${details}
  </section>`;
}

function render(raw) {
  lastData = raw;
  const items = liveView(raw);
  $('list').innerHTML = items.map(card).join('') || `<div class="empty">${esc(t('No providers'))}</div>`;
  if (!$('settings').hidden) renderSettings();
}

/* ---------- settings ---------- */
function currentTaskbarIds() { return taskbarItems(lastData, settings.taskbar).map(u => u.id); }
function renderSettings() {
  const chosen = currentTaskbarIds();
  $('taskbarChoices').innerHTML = lastData.map(u => {
    const on = chosen.includes(u.id);
    const disabled = !on && chosen.length >= 2;
    return `<label class="choice ${disabled ? 'disabled' : ''}">
      <input type="checkbox" data-id="${esc(u.id)}" ${on ? 'checked' : ''} ${disabled ? 'disabled' : ''}>
      ${logoHTML(u)}<span>${esc(u.name)}</span>
    </label>`;
  }).join('') || `<div class="s-hint">${esc(t('No providers found.'))}</div>`;
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
  fillHotkey();
  fillLanguage();
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

// Panel shortcut: a few safe combinations (Win+Alt+K is Windows' mic mute).
const HOTKEYS = ['Win+Shift+K', 'Ctrl+Alt+K', 'Ctrl+Shift+K', 'Win+Ctrl+K', 'off'];
function fillHotkey() {
  const cur = settings.hotkey || HOTKEYS[0];
  $('hotkey').innerHTML = HOTKEYS.map(h =>
    `<option value="${h}"${h === cur ? ' selected' : ''}>${esc(h === 'off' ? t('Off') : h)}</option>`).join('');
}
$('hotkey').addEventListener('change', async (e) => {
  settings.hotkey = e.target.value;
  const err = await api().SetHotkey(settings.hotkey);
  $('hotkeyErr').textContent = err ? t(err) : '';
});

// Language: "" follows Windows.
const LANGUAGES = [['', 'Automatic (Windows)'], ['en', 'English'], ['tr', 'Türkçe']];
function fillLanguage() {
  const cur = settings.language || '';
  $('language').innerHTML = LANGUAGES.map(([v, label]) =>
    `<option value="${v}"${v === cur ? ' selected' : ''}>${esc(v ? label : t(label))}</option>`).join('');
}
$('language').addEventListener('change', async (e) => {
  settings.language = e.target.value;
  const dict = await api().SetLanguage(settings.language);
  try { settings = await api().GetSettings(); } catch {}
  setLanguage(settings.lang, dict);
  applyStatic();
  render(lastData);
  renderSettings();
  renderCredentials();
  tickAgo();
});

$('updateCheck').addEventListener('change', (e) => {
  settings.update = e.target.checked;
  api().SetUpdateCheck(settings.update);
});
// A newer release: one line under the header, opens its GitHub page.
let pendingUpdate = null;
function showUpdate(u) {
  pendingUpdate = u;
  const b = $('update');
  b.hidden = !u;
  if (u) b.textContent = t('Kota %s is available — view release', u.version);
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
        <span class="cred-state ${c.set ? 'on' : ''}">${esc(t(c.set ? 'Key saved' : 'No key'))}</span>
      </div>
      ${editing ? `
        <div class="cred-row">
          <input type="password" class="cred-input" placeholder="${esc(t('Paste API key'))}" autocomplete="off" spellcheck="false">
          <button class="link-btn cred-save">${esc(t('Save'))}</button>
          ${c.set ? `<button class="link-btn cred-cancel">${esc(t('Cancel'))}</button>` : ''}
        </div>
        <div class="s-hint cred-hint">${esc(t(c.hint))} <a href="#" class="cred-get">${esc(t('Get a key'))}</a></div>`
      : `<div class="cred-row">
          <button class="link-btn cred-replace">${esc(t('Replace'))}</button>
          <button class="link-btn cred-remove">${esc(t('Remove'))}</button>
        </div>`}
      <div class="err cred-err"></div>
    </div>`;
  }).join('') || `<div class="s-hint">${esc(t('No providers need a key.'))}</div>`;
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
    if (err) { row.querySelector('.cred-err').textContent = t(err); return; }
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
  const tg = e.target.closest('.toggle');
  if (!tg) return;
  openDetails[tg.dataset.id] = !openDetails[tg.dataset.id];
  store.set('openDetails', openDetails);
  render(lastData);
});

/* ---------- open / close ---------- */
// Go shows the panel (pill click, shortcut) and hides it; the page only asks to hide.
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
  $('ago').textContent = s < 60 ? t('just now') : t('%dm ago', Math.floor(s / 60));
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
  try { settings = await api().GetSettings(); } catch {}
  await loadLanguage();
  window.runtime.EventsOn('usage', gotData);
  window.runtime.EventsOn('update', showUpdate);
  api().GetUpdate().then(showUpdate).catch(() => {});
  window.runtime.EventsOn('panel:shown', () => { shownAt = Date.now(); tickAgo(); });
  window.runtime.EventsOn('panel:settings', () => { shownAt = Date.now(); showSettings(true); });
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
