// The 7-day usage chart in each provider's details (SVG + hover tooltip).
// Uses helpers from app.js (esc, clampPct, settings, api, render, lastData).

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
function histLabel(s) { return s.label.replace(/ limit$/, ''); }
function histChart(id, series) {
  const now = Date.now() / 1000, t0 = now - HIST_SPAN;
  const W = 1000, H = 100; // viewBox, stretched to the box; strokes don't scale
  const x = t => ((t - t0) / HIST_SPAN) * W, y = v => H - (clampPct(v) / 100) * H;
  const paths = series.map((s, i) => {
    let d = '', prev = null;
    for (const [t, u] of s.points) {
      if (t < t0) continue;
      d += (prev === null || t - prev > HIST_GAP ? 'M' : 'L') + x(t).toFixed(1) + ' ' + y(histVal(u)).toFixed(1);
      prev = t;
    }
    return d ? `<path class="line" d="${d}" stroke="var(--s${i + 1})" vector-effect="non-scaling-stroke"/>` : '';
  }).join('');
  let ticks = ''; // day labels at local midnight
  const first = new Date(t0 * 1000); first.setHours(24, 0, 0, 0);
  for (let d = first; d.getTime() / 1000 < now; d = new Date(d.getTime() + 86400000)) {
    const left = (x(d.getTime() / 1000) / W) * 100;
    if (left > 3 && left < 97) ticks += `<span style="left:${left}%">${d.toLocaleDateString('en-US', { weekday: 'short' })}</span>`;
  }
  const grid = [0, 50, 100].map(v => `<line class="grid" x1="0" x2="${W}" y1="${v}" y2="${v}" vector-effect="non-scaling-stroke"/>`).join('');
  return `<div class="hist">
    <div class="hist-head">
      <div class="tok-title">Last 7 days · % ${settings.display === 'remaining' ? 'left' : 'used'}</div>
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
function nearest(points, t) {
  let best = null;
  for (const p of points) if (!best || Math.abs(p[0] - t) < Math.abs(best[0] - t)) best = p;
  return best && Math.abs(best[0] - t) <= HIST_GAP ? best : null;
}
function histHover(plot, clientX) {
  const series = historyById[plot.dataset.hist];
  if (!series) return;
  const r = plot.getBoundingClientRect();
  const f = Math.min(1, Math.max(0, (clientX - r.left) / r.width));
  const t0 = Date.now() / 1000 - HIST_SPAN, t = t0 + f * HIST_SPAN;
  const cross = plot.querySelector('.hist-cross'), tip = plot.querySelector('.hist-tip');
  const dots = plot.querySelectorAll('.hist-dot');
  let rows = '';
  series.forEach((s, i) => {
    const p = nearest(s.points, t), dot = dots[i];
    if (!p) { dot.style.display = 'none'; return; }
    const v = histVal(p[1]);
    dot.style.display = 'block';
    dot.style.left = ((p[0] - t0) / HIST_SPAN) * 100 + '%';
    dot.style.top = (100 - clampPct(v)) + '%';
    rows += `<div><i style="background:var(--s${i + 1})"></i>${esc(histLabel(s))}<b>${Math.round(v)}%</b></div>`;
  });
  const when = new Date(t * 1000).toLocaleString('en-US', { weekday: 'short', hour: 'numeric', minute: '2-digit' });
  tip.innerHTML = `<div class="t">${esc(when)}</div>` + (rows || '<div class="t">No data</div>');
  cross.style.display = tip.style.display = 'block';
  cross.style.left = f * 100 + '%';
  const tw = tip.offsetWidth, px = f * r.width; // keep the tooltip inside the plot
  tip.style.left = Math.max(0, px + 10 + tw > r.width ? px - tw - 10 : px + 10) + 'px';
}
function histLeave(plot) {
  plot.querySelectorAll('.hist-cross,.hist-tip,.hist-dot').forEach(el => { el.style.display = 'none'; });
}
