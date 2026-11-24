// Tests of the panel's pure helpers (frontend/dist/core.js, chart.js).
// Run: node --test frontend/tests
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');

const core = require('../dist/core.js');
const { dailyValue } = require('../dist/chart.js');
const tr = JSON.parse(fs.readFileSync(path.join(__dirname, '../../internal/i18n/tr.json'), 'utf8'));

const MIN = 60000, HOUR = 60 * MIN, DAY = 24 * HOUR;

test.afterEach(() => core.setLanguage('en'));

test('format: verbs, precision, indexed args and %%', () => {
  assert.equal(core.format('%s at %.0f%%', ['Claude', 84.6]), 'Claude at 85%');
  assert.equal(core.format('%d of %d', [3.9, 10]), '3 of 10');
  assert.equal(core.format('%.2f', [1]), '1.00');
  assert.equal(core.format('%[2]d %[1]s %[3]s', ['Nov', 24, '15:00']), '24 Nov 15:00');
  assert.equal(core.format('%s', [undefined]), '');
});

test('t: English by default, Turkish with the dictionary, falls back to the key', () => {
  assert.equal(core.t('used'), 'used');
  assert.equal(core.t('%dm', 5), '5m');
  core.setLanguage('tr', tr);
  assert.equal(core.t('used'), tr['used']);
  assert.equal(core.t('%dm', 5), '5 dk');
  assert.equal(core.t('Max %s', '$1.00'), 'En çok $1.00');
  assert.equal(core.t('not a key %s', 'x'), 'not a key x');
});

test('setLanguage ignores unknown languages and a dictionary for English', () => {
  core.setLanguage('de', tr);
  assert.equal(core.t('used'), 'used');
  core.setLanguage('en', tr);
  assert.equal(core.t('used'), 'used');
});

test('every t() key in the panel scripts has a Turkish entry', () => {
  const missing = [];
  for (const f of ['core.js', 'chart.js', 'app.js']) {
    const src = fs.readFileSync(path.join(__dirname, '../dist', f), 'utf8');
    for (const m of src.matchAll(/\bt\(\s*'((?:[^'\\]|\\.)*)'/g)) {
      const k = m[1].replace(/\\'/g, "'");
      if (/[a-z]/.test(k) && !tr[k]) missing.push(`${f}: ${k}`);
    }
  }
  assert.deepEqual(missing, []);
});

test('durShort', () => {
  assert.equal(core.durShort(20 * 1000), '<1m');
  assert.equal(core.durShort(45 * MIN), '45m');
  assert.equal(core.durShort(2 * HOUR + 10 * MIN), '2h 10m');
  assert.equal(core.durShort(3 * DAY + 4 * HOUR), '3d 4h');
  core.setLanguage('tr', tr);
  assert.equal(core.durShort(2 * HOUR + 10 * MIN), '2 sa 10 dk');
});

test('resetShort', () => {
  const now = Date.parse('2026-11-24T12:00:00Z');
  assert.equal(core.resetShort('', now), '');
  assert.equal(core.resetShort('garbage', now), '');
  assert.equal(core.resetShort('2026-11-24T11:00:00Z', now), 'now');
  assert.equal(core.resetShort('2026-11-24T14:30:00Z', now), '2h 30m');
  assert.match(core.resetShort('2026-11-27T12:00:00Z', now), /^\w{3} \d\d:\d\d$/);
});

test('shown and color follow the used percentage', () => {
  assert.deepEqual(core.shown(30, 'used'), { used: 30, value: 30, word: 'used' });
  assert.deepEqual(core.shown(30, 'remaining'), { used: 30, value: 70, word: 'left' });
  assert.equal(core.shown(140, 'used').value, 100);
  assert.equal(core.color(90), 'var(--bad)');
  assert.equal(core.color(60), 'var(--warn)');
  assert.equal(core.color(10), 'var(--ok)');
});

test('pace: elapsed share and when the limit is reached', () => {
  const now = Date.parse('2026-11-24T12:00:00Z');
  const win = 5 * 3600;
  // Half the window gone, 80% used: the rest goes in 1/4 of the elapsed time.
  const p = core.pace({ usedPct: 80, windowSec: win, resetsAt: new Date(now + 2.5 * HOUR).toISOString() }, now);
  assert.equal(Math.round(p.elapsedPct), 50);
  assert.equal(Math.round(p.runsOutIn / MIN), 38);
  // Slow burn: no warning.
  const slow = core.pace({ usedPct: 30, windowSec: win, resetsAt: new Date(now + 2.5 * HOUR).toISOString() }, now);
  assert.equal(slow.runsOutIn, 0);
  // Too little used to judge.
  const little = core.pace({ usedPct: core.MIN_PACE_USED - 1, windowSec: win, resetsAt: new Date(now + 4.9 * HOUR).toISOString() }, now);
  assert.equal(little.runsOutIn, 0);
  // No window or already reset.
  assert.equal(core.pace({ usedPct: 50, windowSec: 0, resetsAt: new Date(now + HOUR).toISOString() }, now), null);
  assert.equal(core.pace({ usedPct: 50, windowSec: win, resetsAt: new Date(now - HOUR).toISOString() }, now), null);
});

test('esc', () => {
  assert.equal(core.esc('<a href="x">&</a>'), '&lt;a href=&quot;x&quot;&gt;&amp;&lt;/a&gt;');
  assert.equal(core.esc(null), '');
});

test('liveView zeroes windows whose reset has passed', () => {
  const now = Date.parse('2026-11-24T12:00:00Z');
  const items = [{ id: 'claude', groups: [{ bars: [
    { label: 'a', usedPct: 70, resetsAt: '2026-11-24T11:00:00Z' },
    { label: 'b', usedPct: 40, resetsAt: '2026-11-24T13:00:00Z' },
    { label: 'c', usedPct: 10 },
  ] }] }];
  const bars = core.liveView(items, now)[0].groups[0].bars;
  assert.deepEqual(bars.map(b => b.usedPct), [0, 40, 10]);
  assert.equal(bars[0].resetsAt, '');
  assert.equal(items[0].groups[0].bars[0].usedPct, 70, 'input is not changed');
});

test('taskbarItems: chosen ones in order, else the first two', () => {
  const items = [{ id: 'a' }, { id: 'b' }, { id: 'c' }];
  assert.deepEqual(core.taskbarItems(items, []).map(u => u.id), ['a', 'b']);
  assert.deepEqual(core.taskbarItems(items, ['c', 'a']).map(u => u.id), ['c', 'a']);
  assert.deepEqual(core.taskbarItems(items, ['gone', 'b']).map(u => u.id), ['b']);
  assert.deepEqual(core.taskbarItems(items, ['gone']).map(u => u.id), ['a', 'b']);
});

test('seriesPath: scales to the box and breaks at gaps', () => {
  const now = 1000, span = 100;
  const pts = [[850, 50], [910, 0], [920, 100], [990, 50]];
  // 850 is before the span and dropped; 920→990 is a 70s gap > 30.
  assert.equal(core.seriesPath(pts, now, span, 30, 100, 100), 'M10.0 100.0L20.0 0.0M90.0 50.0');
  assert.equal(core.seriesPath(pts, now, span, 30, 100, 100, v => 100 - v), 'M10.0 0.0L20.0 100.0M90.0 50.0');
  assert.equal(core.seriesPath([], now, span, 30, 100, 100), '');
});

test('dailyValue', () => {
  assert.equal(dailyValue(1.5, true), '$1.50');
  assert.equal(dailyValue(999, false), '999');
  assert.equal(dailyValue(1500, false), '1.5K');
  assert.equal(dailyValue(2300000, false), '2.3M');
  assert.equal(dailyValue(4.2e9, false), '4.2B');
});
