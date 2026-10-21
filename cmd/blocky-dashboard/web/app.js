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
  stats: null,
  blocking: null,
  autoEnableAt: 0,
  refreshTimer: null,
};

async function api(path, opts) {
  const r = await fetch(path, opts);
  const t = await r.text();
  let d;
  try { d = JSON.parse(t); } catch { d = { error: t.trim() }; }
  if (!r.ok) throw new Error(d.error || `HTTP ${r.status}`);
  return d;
}
const post = path => api(path, { method: 'POST' });

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
  const [st, ss] = await Promise.allSettled([api('api/status'), api('api/stats')]);

  const ok = st.status === 'fulfilled';
  $('connDot').className = 'dot ' + (ok ? 'ok' : 'bad');
  $('connDot').title = ok ? 'Blocky connected' : 'Blocky unavailable: ' + st.reason.message;
  if (ok) renderBlocking(st.value.blocking || {});
  else renderBlocking(null);

  if (ss.status === 'fulfilled') {
    $('statsError').hidden = true;
    state.stats = ss.value;
    renderStats(ss.value);
  } else {
    $('statsError').hidden = false;
    $('statsError').textContent = 'Statistics unavailable: ' + ss.reason.message;
  }

  if (state.cfg.queryLogs) loadLogs();
  $('updated').textContent = 'Updated ' + new Date().toLocaleTimeString();
}

function scheduleRefresh() {
  clearInterval(state.refreshTimer);
  const s = state.cfg.autoRefreshSeconds;
  if (s > 0) state.refreshTimer = setInterval(() => { if (!document.hidden) load(); }, s * 1000);
}

/* ---------- blocking ---------- */

function renderBlocking(b) {
  state.blocking = b;
  state.autoEnableAt = b && b.autoEnableInSec ? Date.now() + b.autoEnableInSec * 1000 : 0;
  updateBlockingPill();
}

function updateBlockingPill() {
  const pill = $('blockingPill');
  const b = state.blocking;
  if (!b) {
    pill.textContent = 'Blocking unknown';
    pill.className = 'pill';
    return;
  }
  let text = b.enabled ? 'Blocking on' : 'Blocking paused';
  if (!b.enabled && state.autoEnableAt) {
    const left = Math.max(0, Math.round((state.autoEnableAt - Date.now()) / 1000));
    text += ' · ' + Math.floor(left / 60) + ':' + String(left % 60).padStart(2, '0');
    if (left === 0) { state.autoEnableAt = 0; setTimeout(load, 1500); }
  }
  if (!b.enabled && b.disabledGroups && b.disabledGroups.length) text += ' (' + b.disabledGroups.join(', ') + ')';
  pill.textContent = text;
  pill.className = 'pill ' + (b.enabled ? 'on' : 'off');
  $('enableBtn').hidden = b.enabled;
}

async function blockingAction(path, msg) {
  $('blockingMenu').open = false;
  try {
    await post(path);
    toast(msg);
    await load();
  } catch (e) { toast(e.message, true); }
}

/* ---------- stats ---------- */

function renderStats(s) {
  const x = s.summary || {};
  $('kQueries').textContent = fmt(x.queries);
  $('kBlocked').textContent = fmt(x.blocked);
  $('kBlockedPct').textContent = x.queries ? pct(x.blocked, x.queries) + ' of queries' : '—';
  $('kCache').textContent = ((x.cacheHitRate || 0) * 100).toFixed(1) + '%';
  $('kCacheEntries').textContent = fmt(s.cache && s.cache.entries) + ' cached entries';
  $('kLatency').textContent = x.avgResponseMs != null ? Number(x.avgResponseMs).toFixed(1) : '—';
  if (s.start) {
    $('kWindow').textContent = 'since ' + new Date(s.start).toLocaleString([], { weekday: 'short', hour: '2-digit', minute: '2-digit' });
  }

  const sum = o => Object.values(o || {}).reduce((a, b) => a + Number(b || 0), 0);
  const lists = s.lists || {};
  $('kDeny').textContent = fmt(sum(lists.denylist));
  $('kAllow').textContent = fmt(sum(lists.allowlist)) + ' allowlisted';

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
  el.replaceChildren(...rows.map(r => {
    const bg = h('span', { class: 'bg' });
    bg.style.width = (Number(r.count) / max * 100) + '%';
    return h('li', {},
      h('button', { type: 'button', class: 'top-item', title: `Show ${r.name} in the query log`, onclick: () => drillDown(kind, r.name) },
        bg, h('span', { class: 'name', text: r.name }), h('b', { text: fmt(r.count) })));
  }));
}

function drillDown(kind, name) {
  if (state.cfg.queryLogs) {
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
  const points = (state.stats && state.stats.perHour) || [];
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
    const d = await api('api/query', {
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

let logSeq = 0;
async function loadLogs() {
  const seq = ++logSeq;
  const params = new URLSearchParams({ q: $('logSearch').value.trim(), client: $('logClient').value.trim(), type: $('logType').value, limit: 200 });
  try {
    const d = await api('api/query-log?' + params);
    if (seq !== logSeq) return;
    $('logStatus').textContent = d.entries.length === 200 ? 'Newest 200 matches' : `${d.entries.length} matches`;
    renderLogs(d.entries);
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
  $('enableBtn').addEventListener('click', () => blockingAction('api/blocking/enable', 'Blocking resumed'));
  document.querySelectorAll('[data-disable]').forEach(btn => btn.addEventListener('click', () => {
    const d = btn.dataset.disable;
    blockingAction('api/blocking/disable' + (d !== '0' ? '?duration=' + encodeURIComponent(d) : ''),
      d !== '0' ? `Blocking paused for ${btn.textContent}` : 'Blocking paused');
  }));
  document.querySelectorAll('[data-action]').forEach(btn => btn.addEventListener('click', () => confirmClick(btn, async () => {
    btn.closest('details').open = false;
    try { await post('api/' + btn.dataset.action); toast(btn.dataset.label + ' done'); load(); }
    catch (e) { toast(e.message, true); }
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
  $('logForm').addEventListener('submit', e => { e.preventDefault(); loadLogs(); });
  $('logClear').addEventListener('click', () => { $('logForm').reset(); loadLogs(); });

  document.addEventListener('visibilitychange', () => { if (!document.hidden) load(); });
  let lastWidth = 0;
  new ResizeObserver(entries => {
    const w = Math.round(entries[0].contentRect.width);
    if (w !== lastWidth) { lastWidth = w; renderChart(); }
  }).observe($('chart'));
  setInterval(updateBlockingPill, 1000);
}

async function init() {
  wire();
  try {
    state.cfg = await api('api/config');
  } catch (e) {
    state.cfg = { autoRefreshSeconds: 30, queryLogs: false };
    toast('Could not load dashboard config: ' + e.message, true);
  }
  const title = state.cfg.title || 'Blocky Dashboard';
  document.title = state.cfg.instance ? `${title} · ${state.cfg.instance}` : title;
  $('appTitle').textContent = title;
  $('instance').textContent = state.cfg.instance || '';
  $('logs').hidden = !state.cfg.queryLogs;
  await load();
  scheduleRefresh();
}

init();
