const $=id=>document.getElementById(id);
let stats=null, refreshTimer=null;

async function api(path,opts){
 const r=await fetch(path,opts);
 const t=await r.text(); let d; try{d=JSON.parse(t)}catch{d={error:t}};
 if(!r.ok)throw new Error(d.error||t||`HTTP ${r.status}`); return d;
}
const fmt=n=>Number(n||0).toLocaleString();
const esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
function setView(name){
 document.querySelectorAll('.view').forEach(v=>v.classList.toggle('active',v.id===name));
 document.querySelectorAll('.nav').forEach(v=>v.classList.toggle('active',v.dataset.view===name));
 $('pageTitle').textContent={overview:'Overview',query:'Query tool',logs:'Query logs',controls:'Controls'}[name];
 if(name==='logs')loadLogs();
}
document.querySelectorAll('.nav').forEach(n=>n.onclick=()=>setView(n.dataset.view));

async function load(){
 try{
  const [cfg,st,ss]=await Promise.all([api('/api/config'),api('/api/status'),api('/api/stats')]);
  stats=ss; $('connection').textContent='Blocky connected'; $('connection').style.color='#a7f3d0';
  $('instance').textContent=cfg.instance||cfg.title||'Blocky';
  renderStats(ss); renderStatus(st);
  if(cfg.autoRefreshSeconds>0&&!refreshTimer)refreshTimer=setInterval(load,cfg.autoRefreshSeconds*1000);
  $('updated').textContent='Updated '+new Date().toLocaleTimeString();
 }catch(e){$('connection').textContent='Blocky unavailable';$('connection').style.color='#fca5a5';$('updated').textContent=e.message}
}
function renderStats(s){
 const x=s.summary||{}; $('queries').textContent=fmt(x.queries); $('blocked').textContent=fmt(x.blocked);
 $('blockedPct').textContent=x.queries?((x.blocked/x.queries)*100).toFixed(1)+'% of queries':'—';
 $('cacheRate').textContent=((x.cacheHitRate||0)*100).toFixed(1)+'%'; $('cacheEntries').textContent=fmt(s.cache?.entries)+' entries';
 $('latency').textContent=fmt(x.avgResponseMs)+' ms'; $('window').textContent=`${new Date(s.start).toLocaleString()} → ${new Date(s.end).toLocaleTimeString()}`;
 renderChart(s.perHour||[]); renderBars('outcomes',[
  ['Forwarded',x.forwarded],['Cached',x.cached],['Blocked',x.blocked],['Filtered',x.filtered],['Local',x.local],['Errors',x.errors],['Dropped',x.dropped]
 ],x.queries);
 renderList('topDomains',s.topDomains); renderList('blockedDomains',s.topBlockedDomains); renderList('topClients',s.topClients);
 const deny=Object.values(s.lists?.denylist||{}).reduce((a,b)=>a+Number(b),0), allow=Object.values(s.lists?.allowlist||{}).reduce((a,b)=>a+Number(b),0);
 $('lists').innerHTML=`<div class="mini"><span>Denylist</span><b>${fmt(deny)}</b></div><div class="mini"><span>Allowlist</span><b>${fmt(allow)}</b></div>`;
 renderBars('types',Object.entries(s.byQueryType||{}),x.queries);
}
function renderList(id,data){
 const a=Array.isArray(data)?data:[]; $(id).innerHTML=a.length?a.slice(0,8).map(x=>`<div class="list-item"><span title="${esc(x.name)}">${esc(x.name)}</span><b>${fmt(x.count)}</b></div>`).join(''):'<div class="muted">No data</div>';
}
function renderBars(id,items,total){
 const a=items.filter(x=>Number(x[1])>0); if(!a.length){$(id).innerHTML='<div class="muted">No data</div>';return}
 const max=Math.max(...a.map(x=>Number(x[1])));
 $(id).innerHTML=a.map(([k,v])=>`<div class="barline"><span>${esc(k)}</span><div class="track"><div class="fill" style="width:${max?Math.max(2,Number(v)/max*100):0}%"></div></div><b>${fmt(v)}</b></div>`).join('');
}
function renderChart(points){
 const el=$('chart'), w=900,h=235,p=20; if(!points.length){el.innerHTML='<div class="empty-state">No hourly statistics available.</div>';return}
 const max=Math.max(1,...points.map(p=>Number(p.queries)||0)), step=(w-2*p)/Math.max(1,points.length-1);
 const pts=points.map((q,i)=>`${p+i*step},${h-p-(Number(q.queries)||0)/max*(h-2*p)}`).join(' ');
 const area=`${p},${h-p} ${pts} ${p+(points.length-1)*step},${h-p}`;
 el.innerHTML=`<svg viewBox="0 0 ${w} ${h}" preserveAspectRatio="none"><line x1="${p}" y1="${h-p}" x2="${w-p}" y2="${h-p}" stroke="#27272a"/><polygon points="${area}" fill="rgba(161,161,170,.10)"/><polyline points="${pts}" fill="none" stroke="#a1a1aa" stroke-width="2"/>${points.filter((_,i)=>i%Math.ceil(points.length/6)===0).map((q,i)=>`<text x="${p+i*step*0}" y="${h-3}" fill="#52525b" font-size="10">${new Date(q.hour).toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'})}</text>`).join('')}</svg>`;
}
function renderStatus(s){
 const b=s.blocking||{}; $('blockingStatus').textContent=b.enabled?'Enabled':'Disabled'; $('blockingStatus').style.color=b.enabled?'#a7f3d0':'#fca5a5';
 $('timer').textContent=b.autoEnableInSec?`Automatically re-enables in ${Math.ceil(b.autoEnableInSec/60)} minutes.`:'';
}
async function doAction(path){try{await api(path,{method:path.includes('blocking')?'GET':'POST'});await load()}catch(e){alert(e.message)}}
function enableBlocking(){return doAction('/api/blocking/enable')}
function disableBlocking(duration){return doAction('/api/blocking/disable?duration='+encodeURIComponent(duration))}
$('queryForm').onsubmit=async e=>{
 e.preventDefault(); $('queryResult').className='query-result';$('queryResult').textContent='Querying…';
 try{const d=await api('/api/query',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({query:$('domain').value.trim(),type:$('type').value})});
  $('queryResult').innerHTML=`<div class="result-head"><span class="tag">${esc(d.responseType)}</span><span class="tag">${esc(d.returnCode)}</span><span class="tag">${esc(d.reason)}</span></div><div class="answer">${esc(d.response||'No answer')}</div>`;
 }catch(err){$('queryResult').textContent=err.message}
};
async function loadLogs(){
 try{
  const d=await api('/api/query-log?q='+encodeURIComponent($('logSearch').value||'')+'&client='+encodeURIComponent($('logClient').value||'')+'&type='+encodeURIComponent($('logType').value||''));
  $('logStatus').textContent=`${d.entries.length} results`; renderLogs(d.entries);
 }catch(e){$('logStatus').textContent='Not configured';$('logsTable').innerHTML=`<div class="empty-state">${esc(e.message)}</div>`}
}
function renderLogs(a){
 const types=[...new Set(a.map(x=>x.responseType).filter(Boolean))]; $('logType').innerHTML='<option value="">All outcomes</option>'+types.map(x=>`<option>${esc(x)}</option>`).join('');
 if(!a.length){$('logsTable').innerHTML='<div class="empty-state">No matching queries.</div>';return}
 $('logsTable').innerHTML=`<table class="logs-table"><thead><tr><th>Time</th><th>Question</th><th>Client</th><th>Outcome</th><th>Code</th><th>Duration</th></tr></thead><tbody>${a.map(x=>`<tr><td>${new Date(x.time).toLocaleString()}</td><td>${esc(x.question)}</td><td>${esc(x.clientName||x.clientIP)}</td><td>${esc(x.responseType)}</td><td>${esc(x.responseCode)}</td><td>${fmt(x.durationMs)} ms</td></tr>`).join('')}</tbody></table>`;
}
load();
