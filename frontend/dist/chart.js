// Charts in each provider's details: the 7-day % used line chart (SVG with a
// hover crosshair) and the per-day tokens / spend bar chart.
// Uses core.js (t, esc, clampPct, seriesPath, locale) and app.js (settings,
// api, render, lastData).

let historyById = {};
const HIST_SPAN = 7 * 86400, HIST_GAP = 3600; // break the line where the app was off
async function loadHistory() {
  try {
    const h = await api().GetHistory();
    const next = {};
    (h || []).forEach(p => { next[p.id] = p.series; });
    historyById = next;
    if (lastData.length) render(lastData);
  } catch {}
}
function histVal(u) { return settings.display === 'remaining' ? 100 - u : u; }
function histLabel(s) { return t(s.label).replace(/ limit$/, '').replace(/ limiti$/, ''); }
function histChart(id, series) {
  const now = Date.now() / 1000, t0 = now - HIST_SPAN;
  const W = 1000, H = 100; // viewBox, stretched to the box; strokes don't scale
  const paths = series.map((s, i) => {
    const d = seriesPath(s.points, now, HIST_SPAN, HIST_GAP, W, H, histVal);
    return d ? `<path class="line" d="${d}" stroke="var(--s${i + 1})" vector-effect="non-scaling-stroke"/>` : '';
  }).join('');
  let ticks = ''; // day labels at local midnight
  const first = new Date(t0 * 1000); first.setHours(24, 0, 0, 0);
  for (let d = first; d.getTime() / 1000 < now; d = new Date(d.getTime() + 86400000)) {
    const left = ((d.getTime() / 1000 - t0) / HIST_SPAN) * 100;
    if (left > 3 && left < 97) ticks += `<span style="left:${left}%">${esc(weekdayShort(d))}</span>`;
  }
  const grid = [0, 50, 100].map(v => `<line class="grid" x1="0" x2="${W}" y1="${v}" y2="${v}" vector-effect="non-scaling-stroke"/>`).join('');
  return `<div class="hist">
    <div class="hist-head">
      <div class="tok-title">${esc(t(settings.display === 'remaining' ? 'Last 7 days · % left' : 'Last 7 days · % used'))}</div>
      <div class="legend">${series.map((s, i) => `<span><i style="background:var(--s${i + 1})"></i>${esc(histLabel(s))}</span>`).join('')}</div>
    </div>
    <div class="hist-plot" data-hist="${esc(id)}">
      <span class="hist-y" style="top:0">100%</span><span class="hist-y" style="top:50%">50%</span><span class="hist-y" style="top:100%">0%</span>
      <svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="none">${grid}${paths}</svg>
      <div class="hist-cross"></div>
      ${series.map((s, i) => `<div class="hist-dot" style="background:var(--s${i + 1})"></div>`).join('')}
      <div class="hist-tip"></div>
    </div>
    <div class="hist-x">${ticks}</div>
  </div>`;
}
// Hover: crosshair plus each series' value at that time.
function nearest(points, ts) {
  let best = null;
  for (const p of points) if (!best || Math.abs(p[0] - ts) < Math.abs(best[0] - ts)) best = p;
  return best && Math.abs(best[0] - ts) <= HIST_GAP ? best : null;
}
function histHover(plot, clientX) {
  const series = historyById[plot.dataset.hist];
  if (!series) return;
  const r = plot.getBoundingClientRect();
  const f = Math.min(1, Math.max(0, (clientX - r.left) / r.width));
  const t0 = Date.now() / 1000 - HIST_SPAN, ts = t0 + f * HIST_SPAN;
  const cross = plot.querySelector('.hist-cross'), tip = plot.querySelector('.hist-tip');
  const dots = plot.querySelectorAll('.hist-dot');
  let rows = '';
  series.forEach((s, i) => {
    const p = nearest(s.points, ts), dot = dots[i];
    if (!p) { dot.style.display = 'none'; return; }
    const v = histVal(p[1]);
    dot.style.display = 'block';
    dot.style.left = ((p[0] - t0) / HIST_SPAN) * 100 + '%';
    dot.style.top = (100 - clampPct(v)) + '%';
    rows += `<div><i style="background:var(--s${i + 1})"></i>${esc(histLabel(s))}<b>${Math.round(v)}%</b></div>`;
  });
  const when = new Date(ts * 1000).toLocaleString(locale(), { weekday: 'short', hour: 'numeric', minute: '2-digit' });
  tip.innerHTML = `<div class="t">${esc(when)}</div>` + (rows || `<div class="t">${esc(t('No data'))}</div>`);
  cross.style.display = tip.style.display = 'block';
  cross.style.left = f * 100 + '%';
  const tw = tip.offsetWidth, px = f * r.width; // keep the tooltip inside the plot
  tip.style.left = Math.max(0, px + 10 + tw > r.width ? px - tw - 10 : px + 10) + 'px';
}
function histLeave(plot) {
  plot.querySelectorAll('.hist-cross,.hist-tip,.hist-dot').forEach(el => { el.style.display = 'none'; });
}

/* ---------- per day: tokens or spend ---------- */
// One bar per calendar day; the value is in each bar's tooltip and the
// biggest day is labelled. Days without data have no bar.
function dailyChart(u) {
  const days = u.daily || [];
  if (!days.length) return '';
  const usd = u.dailyUnit === 'usd';
  const val = d => usd ? (d.cost || 0) : (d.tokens || 0);
  const peak = Math.max(...days.map(val));
  if (peak <= 0) return '';
  const bars = days.map(d => {
    const v = val(d), h = Math.max(v > 0 ? 3 : 0, (v / peak) * 100);
    const date = new Date(d.day + 'T12:00:00');
    const label = weekdayShort(date);
    const tip = `${date.toLocaleDateString(locale(), { weekday: 'long', day: 'numeric', month: 'short' })}: ${dailyValue(v, usd)}`;
    return `<div class="day" title="${esc(tip)}"><div class="day-bar" style="height:${h}%"></div><span>${esc(label)}</span></div>`;
  }).join('');
  return `<div class="hist daily">
    <div class="hist-head">
      <div class="tok-title">${esc(t(usd ? 'Spend per day' : 'Tokens per day'))}</div>
      <div class="legend">${esc(t('Max %s', dailyValue(peak, usd)))}</div>
    </div>
    <div class="days">${bars}</div>
  </div>`;
}
function dailyValue(v, usd) {
  if (usd) return '$' + v.toFixed(2);
  if (v >= 1e9) return (v / 1e9).toFixed(1) + 'B';
  if (v >= 1e6) return (v / 1e6).toFixed(1) + 'M';
  if (v >= 1e3) return (v / 1e3).toFixed(1) + 'K';
  return String(v);
}
if (typeof module !== 'undefined') module.exports = { dailyValue };
