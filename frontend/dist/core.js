// Pure helpers of the panel: translation, formatting, pace, data views.
// No DOM here, so the same file runs in the page (as globals) and in the
// Node tests (frontend/tests, via module.exports).

/* ---------- i18n ---------- */
// Keys are the English texts (with %s / %d / %.0f for the variable parts),
// the same dictionary the Go side uses (internal/i18n/tr.json).
let I18N = {}, LANG = 'en';
function setLanguage(lang, dict) {
  LANG = lang === 'tr' ? 'tr' : 'en';
  I18N = (LANG === 'tr' && dict) ? dict : {};
}
// sprintf-lite: %s %d %.Nf %% and indexed %[n]s / %[n]d.
function format(s, args) {
  let i = 0;
  return s.replace(/%(?:\[(\d+)\])?(\.\d+)?([sdf%])/g, (m, idx, prec, verb) => {
    if (verb === '%') return '%';
    const v = idx ? args[+idx - 1] : args[i++];
    if (verb === 'd') return String(Math.trunc(Number(v)));
    if (verb === 'f') return Number(v).toFixed(prec ? +prec.slice(1) : 6);
    return String(v ?? '');
  });
}
function t(key, ...args) {
  const s = (I18N[key] !== undefined && I18N[key] !== '') ? I18N[key] : key;
  return args.length ? format(s, args) : s;
}
const locale = () => (LANG === 'tr' ? 'tr-TR' : 'en-GB');

/* ---------- numbers & time ---------- */
const clampPct = (v) => Math.max(0, Math.min(100, v || 0));

// Percent shown for a bar, following the display setting. Colour always
// reflects how much is used, so red means "close to the limit" either way.
function shown(usedPct, display) {
  const used = clampPct(usedPct);
  const rem = display === 'remaining';
  return { used, value: rem ? 100 - used : used, word: rem ? t('left') : t('used') };
}

// Colour by how much is USED (same thresholds as domain.LevelOf).
function color(used) {
  if (used >= 85) return 'var(--bad)';
  if (used >= 60) return 'var(--warn)';
  return 'var(--ok)';
}

// "45m", "2h 10m", "3d 4h" in the current language (same keys as Go).
function durShort(ms) {
  const m = Math.round(ms / 60000);
  if (m < 1) return t('<1m');
  if (m < 60) return t('%dm', m);
  if (m < 1440) return t('%dh %dm', Math.floor(m / 60), m % 60);
  return t('%dd %dh', Math.floor(m / 1440), Math.floor((m % 1440) / 60));
}

function weekdayShort(d) { return d.toLocaleDateString(locale(), { weekday: 'short' }).replace('.', ''); }
function hhmm(d) { return d.toLocaleTimeString(locale(), { hour: '2-digit', minute: '2-digit', hour12: false }); }

// "now", "45m", "2h 14m" or "Sun 11:00".
function resetShort(iso, now = Date.now()) {
  if (!iso) return '';
  const d = new Date(iso), ms = d - now;
  if (isNaN(ms)) return '';
  if (ms <= 0) return t('now');
  if (ms < 86400000) return durShort(ms);
  return weekdayShort(d) + ' ' + hhmm(d);
}
function resetLong(iso) {
  const d = new Date(iso);
  if (isNaN(d)) return '';
  return t('Resets %s', d.toLocaleString(locale(), { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit', hour12: false }));
}

// Pace: how much of the window's time has elapsed and whether the current
// burn rate reaches 100% before the reset (same rules as domain.PaceOf).
const MIN_PACE_USED = 20;
function pace(b, now = Date.now()) {
  const reset = Date.parse(b.resetsAt || ''), win = (b.windowSec || 0) * 1000;
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

function esc(s) {
  return String(s ?? '').replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
}

/* ---------- data views ---------- */
// A window whose reset time has passed has been refilled, even if the data
// is from before that moment (cache, rate limit, closed app): show it as 0%.
function liveView(items, now = Date.now()) {
  return items.map(u => ({
    ...u,
    groups: (u.groups || []).map(g => ({
      ...g,
      bars: (g.bars || []).map(b => {
        const ts = b.resetsAt ? Date.parse(b.resetsAt) : NaN;
        return !isNaN(ts) && ts <= now ? { ...b, usedPct: 0, resetsAt: '' } : b;
      }),
    })),
  }));
}

// Providers on the taskbar: the user's choice, else the first two (same rule
// as domain.TaskbarProviders).
function taskbarItems(items, chosen) {
  const ids = (chosen || []).filter(id => items.some(u => u.id === id));
  if (!ids.length) return items.slice(0, 2);
  return ids.slice(0, 2).map(id => items.find(u => u.id === id));
}

// SVG path of a history series ([unix s, value] points) on a W×H box over
// the last `span` seconds; gaps longer than `gap` break the line.
function seriesPath(points, now, span, gap, W, H, value = (v) => v) {
  const t0 = now - span;
  const x = s => ((s - t0) / span) * W, y = v => H - (clampPct(v) / 100) * H;
  let d = '', prev = null;
  for (const [s, v] of points) {
    if (s < t0) continue;
    d += (prev === null || s - prev > gap ? 'M' : 'L') + x(s).toFixed(1) + ' ' + y(value(v)).toFixed(1);
    prev = s;
  }
  return d;
}

if (typeof module !== 'undefined') {
  module.exports = { setLanguage, t, format, clampPct, shown, color, durShort, resetShort, resetLong, pace, esc, liveView, taskbarItems, seriesPath, MIN_PACE_USED };
}
