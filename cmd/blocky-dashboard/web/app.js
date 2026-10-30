'use strict';

const $ = id => document.getElementById(id);
const fmt = n => Number(n || 0).toLocaleString();
const pct = (a, b) => (b ? (a / b * 100).toFixed(1) + '%' : '—');

// h builds a DOM element; avoids innerHTML so data is never parsed as markup
// and the page works under a strict Content-Security-Policy.
function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'text') el.textContent = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const c of children.flat()) {
    if (c != null && c !== false) el.append(c instanceof Node ? c : String(c));
  }
  return el;
}

const state = {
  cfg: null,
  instances: [],      // [{id, name, queryLogs}] from api/config
  overview: {},       // id -> {blocking, blockingError, stats, statsError}
  autoEnableAt: {},   // id -> timestamp when Blocky re-enables blocking
  cards: {},          // id -> card element references
  selected: null,
  refreshTimer: null,
};

const multi = () => state.instances.length > 1;
const selected = () => state.instances.find(i => i.id === state.selected) || state.instances[0];
const withInstance = (path, id) => path + (path.includes('?') ? '&' : '?') + 'instance=' + encodeURIComponent(id);

async function api(path, opts) {
  const r = await fetch(path, opts);
  const t = await r.text();
  let d;
  try { d = JSON.parse(t); } catch { d = { error: t.trim() }; }
  if (!r.ok) throw new Error(d.error || `HTTP ${r.status}`);
  return d;
}

let toastTimer;
function toast(msg, isError) {
  const el = $('toast');
  el.textContent = msg;
  el.classList.toggle('error', !!isError);
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.hidden = true; }, isError ? 6000 : 2500);
}

/* ---------- loading ---------- */

async function load() {
  let data;
  try {
    data = await api('api/overview');
  } catch (e) {
    $('connDot').className = 'dot bad';
    $('connDot').title = 'Dashboard unavailable: ' + e.message;
    return;
  }
  for (const o of data.instances) {
    state.overview[o.id] = o;
    const b = o.blocking;
    state.autoEnableAt[o.id] = b && !b.enabled && b.autoEnableInSec ? Date.now() + b.autoEnableInSec * 1000 : 0;
  }

  const up = data.instances.filter(o => o.blocking).length;
  $('connDot').className = 'dot ' + (up === data.instances.length ? 'ok' : up ? 'warn' : 'bad');
  $('connDot').title = `${up} of ${data.instances.length} instance${data.instances.length > 1 ? 's' : ''} reachable`;

  state.instances.forEach(renderCard);
  renderTotals();
  updatePills();
  renderDetails();
  if (selected().queryLogs) loadLogs();
  $('updated').textContent = 'Updated ' + new Date().toLocaleTimeString();
}

function scheduleRefresh() {
  clearInterval(state.refreshTimer);
  const s = state.cfg.autoRefreshSeconds;
  if (s > 0) state.refreshTimer = setInterval(() => { if (!document.hidden) load(); }, s * 1000);
}

/* ---------- instance cards ---------- */

function metric(label) {
  const value = h('strong', { text: '—' }), sub = h('small', { text: '' });
  return { el: h('div', { class: 'metric' }, h('span', { text: label }), value, sub), value, sub };
}

function buildCard(inst) {
  const m = { queries: metric('Queries'), blocked: metric('Blocked'), cache: metric('Cache hit'), latency: metric('Avg response') };
  const pill = h('summary', { class: 'pill sm' });
  const menu = h('details', { class: 'menu', hidden: !multi() }, pill, blockingMenuBody(inst.id, inst.name));
  const card = h('article', {
    class: 'inst-card', tabindex: multi() ? 0 : null, role: multi() ? 'button' : null,
    'aria-label': multi() ? `Show details for ${inst.name}` : null,
    onclick: e => { if (!e.target.closest('details')) select(inst.id); },
    onkeydown: e => { if ((e.key === 'Enter' || e.key === ' ') && e.target === card) { e.preventDefault(); select(inst.id); } },
  },
  h('div', { class: 'inst-head' }, h('span', { class: 'dot' }), h('b', { class: 'inst-name', text: inst.name }), menu),
  h('div', { class: 'inst-metrics' }, Object.values(m).map(x => x.el)),
  h('p', { class: 'inst-foot muted small' }),
  h('p', { class: 'inst-error small', hidden: true }));
  state.cards[inst.id] = { card, pill, menu, m, dot: card.querySelector('.dot'), foot: card.querySelector('.inst-foot'), error: card.querySelector('.inst-error') };
  return card;
}

function renderCard(inst) {
  const c = state.cards[inst.id], o = state.overview[inst.id] || {};
  const st = o.stats || {}, x = st.summary || {};
  c.card.classList.toggle('selected', multi() && inst.id === state.selected);
  c.dot.className = 'dot ' + (o.blocking ? 'ok' : 'bad');

  if (o.stats) {
    c.m.queries.value.textContent = compact(x.queries || 0);
    c.m.queries.sub.textContent = 'last 24h';
    c.m.blocked.value.textContent = pct(x.blocked, x.queries);
    c.m.blocked.sub.textContent = fmt(x.blocked) + ' queries';
    c.m.cache.value.textContent = ((x.cacheHitRate || 0) * 100).toFixed(1) + '%';
    c.m.cache.sub.textContent = fmt(st.cache && st.cache.entries) + ' entries';
    c.m.latency.value.textContent = x.avgResponseMs != null ? Number(x.avgResponseMs).toFixed(1) : '—';
    c.m.latency.sub.textContent = 'ms';
    const sum = obj => Object.values(obj || {}).reduce((a, b) => a + Number(b || 0), 0);
    const lists = st.lists || {};
    c.foot.textContent = `${fmt(sum(lists.denylist))} denylisted · ${fmt(sum(lists.allowlist))} allowlisted`;
  } else {
    Object.values(c.m).forEach(m => { m.value.textContent = '—'; m.sub.textContent = ''; });
    c.foot.textContent = '';
  }
  const err = o.blockingError || o.statsError;
  c.error.hidden = !err;
  c.error.textContent = err ? (o.blocking ? 'Stats unavailable: ' : 'Unreachable: ') + err : '';
}

function renderTotals() {
  const el = $('totals');
  const withStats = state.instances.map(i => state.overview[i.id]).filter(o => o && o.stats);
  if (!multi() || !withStats.length) { el.textContent = ''; return; }
  const q = withStats.reduce((a, o) => a + Number(o.stats.summary?.queries || 0), 0);
  const b = withStats.reduce((a, o) => a + Number(o.stats.summary?.blocked || 0), 0);
  el.textContent = `${fmt(q)} queries · ${pct(b, q)} blocked across ${withStats.length} of ${state.instances.length}`;
}

/* ---------- blocking ---------- */

function blockingMenuBody(target, name) {
  const label = target === 'all' ? (multi() ? 'Pause all instances for' : 'Pause blocking for') : `Pause ${name} for`;
  const run = (path, msg) => blockingAction(target, path, msg);
  return h('div', { class: 'menu-body' },
    h('p', { class: 'menu-label', text: label }),
    [['5m', '5 minutes'], ['15m', '15 minutes'], ['1h', '1 hour']].map(([d, text]) =>
      h('button', { type: 'button', text, onclick: () => run('api/blocking/disable?duration=' + d, `Paused for ${text}`) })),
    h('button', { type: 'button', text: 'Until re-enabled', onclick: () => run('api/blocking/disable', 'Paused') }),
    h('hr'),
    h('button', { type: 'button', class: 'primary resume', text: 'Resume blocking', onclick: () => run('api/blocking/enable', 'Blocking resumed') }));
}

async function blockingAction(target, path, msg) {
  document.querySelectorAll('details.menu[open]').forEach(d => { d.open = false; });
  await runAction(withInstance(path, target), msg);
}

// runAction posts an action and reports per-instance results.
async function runAction(path, msg) {
  try {
    const d = await api(path, { method: 'POST' });
    const failed = (d.results || []).filter(r => !r.ok);
    if (failed.length) toast(`${msg} on ${d.results.length - failed.length} of ${d.results.length}. Failed: ` + failed.map(r => `${r.name} (${r.error})`).join(', '), true);
    else toast(msg + (d.results && d.results.length > 1 ? ` on all ${d.results.length} instances` : ''));
  } catch (e) { toast(e.message, true); }
  await load();
}

function countdown(id) {
  const at = state.autoEnableAt[id];
  if (!at) return '';
  const left = Math.max(0, Math.round((at - Date.now()) / 1000));
  if (left === 0) { state.autoEnableAt[id] = 0; setTimeout(load, 1500); }
  return Math.floor(left / 60) + ':' + String(left % 60).padStart(2, '0');
}

function pillText(o, id) {
  const b = o && o.blocking;
  if (!b) return { text: 'Unreachable', cls: '' };
  if (b.enabled) return { text: 'Blocking on', cls: 'on' };
  let text = 'Paused';
  const left = countdown(id);
  if (left) text += ' · ' + left;
  if (b.disabledGroups && b.disabledGroups.length) text += ' (' + b.disabledGroups.join(', ') + ')';
  return { text, cls: 'off' };
}

function updatePills() {
  for (const inst of state.instances) {
    const { text, cls } = pillText(state.overview[inst.id], inst.id);
    const c = state.cards[inst.id];
    c.pill.textContent = text;
    c.pill.className = 'pill sm ' + cls;
    c.menu.querySelector('.resume').hidden = cls !== 'off';
  }

  const pill = $('blockingPill');
  const known = state.instances.map(i => [i.id, state.overview[i.id]]).filter(([, o]) => o && o.blocking);
  const resume = $('globalBlocking').querySelector('.resume');
  if (!known.length) { pill.textContent = 'Blocking unknown'; pill.className = 'pill'; return; }
  if (!multi()) {
    const { text, cls } = pillText(known[0][1], known[0][0]);
    pill.textContent = text.replace(/^Paused/, 'Blocking paused');
    pill.className = 'pill ' + cls;
    resume.hidden = cls !== 'off';
    return;
  }
  const on = known.filter(([, o]) => o.blocking.enabled).length;
  pill.textContent = on === known.length ? `Blocking on · ${on}/${state.instances.length}`
    : on === 0 ? `Blocking paused · ${known.length}/${state.instances.length}`
      : `Blocking on ${on}/${state.instances.length}`;
  pill.className = 'pill ' + (on === known.length ? 'on' : on === 0 ? 'off' : 'mixed');
  resume.hidden = on === known.length;
}

/* ---------- instance selection ---------- */

function renderTabs() {
  const tabs = $('instanceTabs');
  tabs.hidden = !multi();
  tabs.replaceChildren(...state.instances.map(i => h('button', {
    type: 'button', role: 'tab', class: 'tab', 'data-id': i.id, text: i.name,
    'aria-selected': String(i.id === state.selected), onclick: () => select(i.id),
  })));
}

function select(id) {
  if (!state.instances.some(i => i.id === id) || id === state.selected) return;
  state.selected = id;
  try { localStorage.setItem('instance', id); } catch {}
  history.replaceState(null, '', '#' + encodeURIComponent(id));
  $('instanceTabs').querySelectorAll('.tab').forEach(t => t.setAttribute('aria-selected', String(t.dataset.id === id)));
  state.instances.forEach(renderCard);
  $('queryResult').hidden = true;
  $('logForm').reset();
  renderDetails();
  if (selected().queryLogs) loadLogs();
}

function initialSelection() {
  const fromHash = decodeURIComponent(location.hash.slice(1));
  let stored = '';
  try { stored = localStorage.getItem('instance') || ''; } catch {}
  return [fromHash, stored].find(id => state.instances.some(i => i.id === id)) || state.instances[0].id;
}

/* ---------- details ---------- */

function renderDetails() {
  const inst = selected();
  const o = state.overview[inst.id] || {};
  $('detailsTitle').textContent = multi() ? 'Details · ' + inst.name : 'Details';
  $('testerHint').textContent = multi() ? `Runs the query through ${inst.name} and shows its decision` : 'Runs the query through Blocky and shows its decision';
  $('logs').hidden = !inst.queryLogs;
  $('noLogs').hidden = inst.queryLogs || !multi() || !state.instances.some(i => i.queryLogs);

  $('statsError').hidden = !!o.stats;
  if (!o.stats) $('statsError').textContent = `Statistics unavailable for ${inst.name}: ` + (o.statsError || o.blockingError || 'loading…');
  const s = o.stats || {};
  const x = s.summary || {};
  renderChart();
  renderBars('outcomes', [
    ['Forwarded', x.forwarded], ['Cached', x.cached], ['Blocked', x.blocked, 'blocked'],
    ['Filtered', x.filtered], ['Local', x.local], ['Errors', x.errors, 'blocked'], ['Dropped', x.dropped],
  ]);
  renderBars('types', Object.entries(s.byQueryType || {}).sort((a, b) => b[1] - a[1]).slice(0, 6));
  renderTopList('topDomains', s.topDomains);
  renderTopList('blockedDomains', s.topBlockedDomains);
  renderTopList('topClients', s.topClients);
}

function renderBars(id, items) {
  const rows = items.filter(x => Number(x[1]) > 0);
  const el = $(id);
  if (!rows.length) { el.replaceChildren(h('p', { class: 'muted small', text: 'No data' })); return; }
  const max = Math.max(...rows.map(x => Number(x[1])));
  el.replaceChildren(...rows.map(([label, v, cls]) => {
    const fill = h('div', { class: 'fill' + (cls ? ' ' + cls : '') });
    fill.style.width = Math.max(2, Number(v) / max * 100) + '%';
    return h('div', { class: 'barline' },
      h('span', { text: label }),
      h('div', { class: 'track' }, fill),
      h('b', { text: fmt(v) }));
  }));
}

function renderTopList(id, data) {
  const el = $(id);
  const rows = Array.isArray(data) ? [...data].sort((a, b) => b.count - a.count).slice(0, 10) : [];
  if (!rows.length) { el.replaceChildren(h('li', { class: 'muted small', text: 'No data' })); return; }
  const max = Math.max(...rows.map(r => Number(r.count) || 0), 1);
  const kind = el.dataset.kind;
  const logs = selected().queryLogs;
  el.replaceChildren(...rows.map(r => {
    const bg = h('span', { class: 'bg' });
    bg.style.width = (Number(r.count) / max * 100) + '%';
    const title = logs ? `Show ${r.name} in the query log` : kind === 'domain' ? `Test ${r.name}` : r.name;
    return h('li', {},
      h('button', { type: 'button', class: 'top-item', title, onclick: () => drillDown(kind, r.name) },
        bg, h('span', { class: 'name', text: r.name }), h('b', { text: fmt(r.count) })));
  }));
}

function drillDown(kind, name) {
  if (selected().queryLogs) {
    if (kind === 'client') { $('logClient').value = name; $('logSearch').value = ''; }
    else { $('logSearch').value = name; $('logClient').value = ''; }
    $('logType').value = '';
    loadLogs();
    $('logs').scrollIntoView({ behavior: 'smooth', block: 'start' });
  } else if (kind === 'domain') {
    $('domain').value = name;
    $('tester').scrollIntoView({ behavior: 'smooth', block: 'start' });
    $('queryForm').requestSubmit();
  }
}

/* ---------- chart ---------- */

const SVG = 'http://www.w3.org/2000/svg';
function svgEl(tag, attrs, ...children) {
  const el = document.createElementNS(SVG, tag);
  for (const [k, v] of Object.entries(attrs || {})) el.setAttribute(k, v);
  el.append(...children);
  return el;
}

function renderChart() {
  const el = $('chart');
  const stats = state.instances.length && (state.overview[selected().id] || {}).stats;
  const points = (stats && stats.perHour) || [];
  if (!points.length) { el.replaceChildren(h('p', { class: 'muted small empty', text: 'No hourly statistics available.' })); return; }

  const w = Math.max(280, el.clientWidth), ht = el.clientHeight || 220;
  const pad = { l: 40, r: 8, t: 10, b: 22 };
  const iw = w - pad.l - pad.r, ih = ht - pad.t - pad.b;
  const max = Math.max(1, ...points.map(p => Number(p.queries) || 0));
  const hasBlocked = points.some(p => p.blocked != null);
  const bw = iw / points.length;
  const y = v => pad.t + ih - (v / max) * ih;

  const svg = svgEl('svg', { viewBox: `0 0 ${w} ${ht}`, width: w, height: ht, role: 'img', 'aria-label': 'Queries per hour' });

  for (const f of [0, 0.5, 1]) {
    const gy = y(max * f);
    svg.append(svgEl('line', { x1: pad.l, x2: w - pad.r, y1: gy, y2: gy, class: 'grid' }));
    svg.append(svgEl('text', { x: pad.l - 6, y: gy + 4, class: 'axis', 'text-anchor': 'end' }, compact(max * f)));
  }

  const every = Math.ceil(points.length / Math.max(2, Math.floor(iw / 60)));
  points.forEach((p, i) => {
    const q = Number(p.queries) || 0, b = Number(p.blocked) || 0;
    const x = pad.l + i * bw, gap = Math.min(2, bw * 0.2);
    const label = new Date(p.hour).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    const g = svgEl('g', { class: 'bar' }, svgEl('title', {}, `${label}: ${fmt(q)} queries` + (hasBlocked ? `, ${fmt(b)} blocked` : '')));
    g.append(svgEl('rect', { x: x + gap / 2, y: y(q), width: Math.max(1, bw - gap), height: Math.max(0, pad.t + ih - y(q)), class: 'total', rx: 2 }));
    if (hasBlocked && b > 0) g.append(svgEl('rect', { x: x + gap / 2, y: y(b), width: Math.max(1, bw - gap), height: Math.max(0, pad.t + ih - y(b)), class: 'blocked', rx: 2 }));
    svg.append(g);
    if (i % every === 0) svg.append(svgEl('text', { x: x + bw / 2, y: ht - 6, class: 'axis', 'text-anchor': 'middle' }, label));
  });
  el.replaceChildren(svg);
}

function compact(n) {
  if (n >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, '') + 'M';
  if (n >= 1e3) return (n / 1e3).toFixed(1).replace(/\.0$/, '') + 'k';
  return String(Math.round(n));
}

/* ---------- query tester ---------- */

async function runQuery(e) {
  e.preventDefault();
  const out = $('queryResult');
  out.hidden = false;
  out.replaceChildren(h('p', { class: 'muted small', text: 'Querying…' }));
  try {
    const d = await api(withInstance('api/query', selected().id), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ query: $('domain').value.trim(), type: $('qtype').value }),
    });
    out.replaceChildren(
      h('div', { class: 'result-head' },
        outcomeBadge(d.responseType),
        h('span', { class: 'tag', text: d.returnCode || '' }),
        h('span', { class: 'muted small', text: d.reason || '' })),
      h('pre', { class: 'answer', text: d.response || 'No answer' }));
  } catch (err) {
    out.replaceChildren(h('p', { class: 'error-text', text: err.message }));
  }
}

function outcomeBadge(type) {
  const t = String(type || '').toUpperCase();
  const cls = t === 'BLOCKED' || t === 'FILTERED' ? 'bad' : t === 'CACHED' ? 'info' : t ? 'good' : '';
  return h('span', { class: 'badge ' + cls, text: t || '—' });
}

/* ---------- query log ---------- */

// Rows to show in the query log; the default keeps the table short.
const DEFAULT_LOG_LIMIT = 100;

function logLimit() {
  return Number($('logLimit').value) || DEFAULT_LOG_LIMIT;
}

let logSeq = 0;
async function loadLogs() {
  const seq = ++logSeq;
  const limit = logLimit();
  const params = new URLSearchParams({ instance: selected().id, q: $('logSearch').value.trim(), client: $('logClient').value.trim(), type: $('logType').value, limit });
  try {
    const d = await api('api/query-log?' + params);
    if (seq !== logSeq) return;
    const rows = d.entries || [];
    $('logStatus').textContent = rows.length >= limit ? `Newest ${limit}` : `${rows.length} matches`;
    renderLogs(rows);
  } catch (e) {
    if (seq !== logSeq) return;
    $('logStatus').textContent = '';
    $('logsTable').replaceChildren(h('p', { class: 'empty error-text', text: e.message }));
  }
}

function renderLogs(rows) {
  if (!rows.length) { $('logsTable').replaceChildren(h('p', { class: 'empty muted', text: 'No matching queries.' })); return; }
  const today = new Date().toDateString();
  const when = t => {
    const d = new Date(t);
    if (!t || d.getFullYear() < 2000) return '—';
    return d.toDateString() === today ? d.toLocaleTimeString() : d.toLocaleString();
  };
  const filterBy = (input, value) => () => { $(input).value = value; loadLogs(); };
  $('logsTable').replaceChildren(h('table', { class: 'logs-table' },
    h('thead', {}, h('tr', {}, ['Time', 'Client', 'Domain', 'Type', 'Outcome', 'Reason', 'Code', 'ms'].map(t => h('th', { text: t })))),
    h('tbody', {}, rows.map(x => h('tr', {},
      h('td', { class: 'muted', text: when(x.time) }),
      h('td', {}, h('button', { type: 'button', class: 'link', title: x.clientIP, onclick: filterBy('logClient', x.clientName || x.clientIP), text: x.clientName || x.clientIP })),
      h('td', { class: 'domain' }, h('button', { type: 'button', class: 'link', onclick: filterBy('logSearch', x.question), text: x.question })),
      h('td', { class: 'muted', text: x.questionType || '' }),
      h('td', {}, outcomeBadge(x.responseType)),
      h('td', { class: 'muted reason', title: x.answer || '', text: x.reason || '' }),
      h('td', { class: 'muted', text: x.returnCode || '' }),
      h('td', { class: 'num', text: fmt(x.durationMs) })))),
  ));
}

/* ---------- wiring ---------- */

function debounce(fn, ms) {
  let t;
  return () => { clearTimeout(t); t = setTimeout(fn, ms); };
}

function confirmClick(btn, run) {
  if (btn.dataset.armed) {
    delete btn.dataset.armed;
    btn.textContent = btn.dataset.label;
    run();
    return;
  }
  btn.dataset.label = btn.textContent;
  btn.dataset.armed = '1';
  btn.textContent = btn.dataset.confirm + ' Click again';
  setTimeout(() => {
    if (btn.dataset.armed) { delete btn.dataset.armed; btn.textContent = btn.dataset.label; }
  }, 4000);
}

function wire() {
  $('refreshBtn').addEventListener('click', load);
  document.querySelectorAll('[data-action]').forEach(btn => btn.addEventListener('click', () => confirmClick(btn, () => {
    btn.closest('details').open = false;
    runAction(withInstance('api/' + btn.dataset.action, 'all'), btn.dataset.label + ' done');
  })));
  // Close open menus when clicking elsewhere.
  document.addEventListener('click', e => {
    document.querySelectorAll('details.menu[open]').forEach(d => { if (!d.contains(e.target)) d.open = false; });
  });

  $('queryForm').addEventListener('submit', runQuery);

  const debounced = debounce(loadLogs, 300);
  $('logSearch').addEventListener('input', debounced);
  $('logClient').addEventListener('input', debounced);
  $('logType').addEventListener('change', loadLogs);
  try { $('logLimit').value = localStorage.getItem('logLimit') || DEFAULT_LOG_LIMIT; } catch { $('logLimit').value = DEFAULT_LOG_LIMIT; }
  if (!$('logLimit').value) $('logLimit').value = DEFAULT_LOG_LIMIT;
  $('logLimit').addEventListener('change', () => {
    try { localStorage.setItem('logLimit', $('logLimit').value); } catch {}
    loadLogs();
  });
  $('logForm').addEventListener('submit', e => { e.preventDefault(); loadLogs(); });
  $('logClear').addEventListener('click', () => { $('logForm').reset(); loadLogs(); });

  window.addEventListener('hashchange', () => select(decodeURIComponent(location.hash.slice(1))));
  document.addEventListener('visibilitychange', () => { if (!document.hidden) load(); });
  let lastWidth = 0;
  new ResizeObserver(entries => {
    const w = Math.round(entries[0].contentRect.width);
    if (w !== lastWidth) { lastWidth = w; renderChart(); }
  }).observe($('chart'));
  setInterval(updatePills, 1000);
}

async function init() {
  try {
    state.cfg = await api('api/config');
  } catch (e) {
    toast('Could not load dashboard config: ' + e.message, true);
    return;
  }
  state.instances = state.cfg.instances || [];
  const title = state.cfg.title || 'Blocky Dashboard';
  document.title = title;
  $('appTitle').textContent = title;
  $('brandSub').textContent = multi() ? `${state.instances.length} instances` : (state.instances[0] && state.instances[0].name) || '';
  $('instancesHead').hidden = !multi();
  $('instances').classList.toggle('single', !multi());
  $('maintLabel').textContent = multi() ? 'Maintenance · all instances' : 'Maintenance';
  $('globalBlocking').replaceWith(Object.assign(blockingMenuBody('all'), { id: 'globalBlocking' }));
  $('instances').replaceChildren(...state.instances.map(buildCard));

  state.selected = initialSelection();
  renderTabs();
  wire();
  await load();
  scheduleRefresh();
}

init();
