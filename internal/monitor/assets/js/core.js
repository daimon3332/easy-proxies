const ACTIVE_IMPORT_JOB_KEY='easy_proxies_active_import_job';
const ACTIVE_BATCH_JOB_KEY='easy_proxies_active_batch_job';
const ACTIVE_REFRESH_JOB_KEY='easy_proxies_active_refresh_job';
const ACTIVE_TAG_BINDING_JOB_KEY='easy_proxies_active_tag_binding_job';
const IMPORT_CHAIN_PROFILE_KEY='easy_proxies_import_chain_profile';
const CONNECTIVITY_JOB_KEY='easy_proxies_connectivity_job';
const CONNECTIVITY_TIMEOUT_KEY='easy_proxies_connectivity_timeout_seconds';
const CONNECTIVITY_DEFAULT_TIMEOUT_SECONDS=10;
const CONNECTIVITY_MIN_TIMEOUT_SECONDS=1;
const CONNECTIVITY_MAX_TIMEOUT_SECONDS=60;
const VERIFICATION_TARGETS=[{id:'google',name:'Google'},{id:'github',name:'GitHub'},{id:'outlook',name:'Outlook'},{id:'proxyspace',name:'ProxySpace'}];
const IMPORT_SITE_TARGETS_KEY='easy_proxies_import_site_targets';
const IMPORT_TEST_204_KEY='easy_proxies_import_test_204';
const MAX_JOB_EVENT_CACHE=32;
function storedTargetSet(key){try{const value=JSON.parse(localStorage.getItem(key)||'[]');return new Set(Array.isArray(value)?value.filter(id=>VERIFICATION_TARGETS.some(target=>target.id===id)):[])}catch(e){return new Set()}}
function parseConnectivityTimeout(value){
  const raw=String(value??'').trim(),seconds=Number(raw);
  if(!raw||!Number.isInteger(seconds))return {error:'请输入 1-60 秒的整数'};
  if(seconds<CONNECTIVITY_MIN_TIMEOUT_SECONDS||seconds>CONNECTIVITY_MAX_TIMEOUT_SECONDS)return {error:'单次超时必须在 1-60 秒之间'};
  return {seconds,error:''};
}
const initialConnectivityTimeout=parseConnectivityTimeout(localStorage.getItem(CONNECTIVITY_TIMEOUT_KEY));
const initialConnectivityTimeoutSeconds=initialConnectivityTimeout.error?CONNECTIVITY_DEFAULT_TIMEOUT_SECONDS:initialConnectivityTimeout.seconds;
const S={
  token:localStorage.getItem('easy_proxies_token')||'',
  page:'dashboard',
  diagnostics:null,
  subStatus:null,
  shellStatusAt:0,
  dashClock:null,
  importKind:'sub_url',
  importText:'',
  tagPrefix:'',
  chainProfileID:localStorage.getItem(IMPORT_CHAIN_PROFILE_KEY)||'',
  proxyProtocol:'http',
  chainProfiles:[],
  chainUsage:{},
  chainEditorOrigin:'settings',
  importAutoPromote:localStorage.getItem('easy_proxies_import_auto_promote')!=='0',
  importTest204:localStorage.getItem(IMPORT_TEST_204_KEY)!=='0',
  importSiteTargets:storedTargetSet(IMPORT_SITE_TARGETS_KEY),
  importSummary:null,
  nodes:[],
  viewNodes:[],
  runtime:[],
  configNodes:[],
  ports:null,
  settings:null,
  subConfig:null,
  importSources:[],
  webdav:{address:'',username:'',password:'',folder:'/easy_proxies'},
  webdavFiles:[],
  webdavLoading:false,
  selectedExportTags:new Set(),
  summary:null,
  portPreview:[],
  nodePage:null,
  nodesLoadedAt:0,
  summaryLoadedAt:0,
  pageSize:100,
  managedPage:1,
  pageRequest:0,
  pageController:null,
  filterTimer:null,
  logs:'',
  lastJob:null,
  activeImportJob:localStorage.getItem(ACTIVE_IMPORT_JOB_KEY)||'',
  activeBatchJob:localStorage.getItem(ACTIVE_BATCH_JOB_KEY)||'',
  activeRefreshJob:localStorage.getItem(ACTIVE_REFRESH_JOB_KEY)||'',
  activeTagBindingJob:localStorage.getItem(ACTIVE_TAG_BINDING_JOB_KEY)||'',
  tagBindingJob:null,
  tagBindingPollingJob:'',
  pendingTagBindingTags:[],
  selectedBindingTags:new Set(),
  connectivityJobID:localStorage.getItem(CONNECTIVITY_JOB_KEY)||'',
  connectivityScopes:null,
  connectivityJob:null,
  connectivityPollingJob:'',
  connectivitySelectedTags:new Set(),
  connectivityTagsInitialized:false,
  connectivityTestTargets:new Set(['outlook']),
  connectivitySelectedTargets:new Set(['outlook']),
  connectivityTimeoutInput:String(initialConnectivityTimeoutSeconds),
  connectivityTimeoutSeconds:initialConnectivityTimeoutSeconds,
  connectivityTimeoutValid:true,
  connectivityHistory:null,
  connectivityResultPage:null,
  connectivityResultFilters:{tag:'',target:'',status:'',page:1},
  connectivityDetailsOpen:false,
  connectivityPreviewRequest:null,
  connectivityPreview:null,
  importPollingJob:'',
  batchPollingJob:'',
  refreshPollingJob:'',
  refreshJob:null,
  refreshDialogOpen:false,
  refreshDialogJob:'',
  refreshPollError:'',
  jobEvents:new Map(),
  jobWaiters:new Map(),
  jobStreamAbort:null,
  jobStreamTask:null,
  jobStreamConnected:false,
  selected:new Set(),
  poolOrderMode:'country',
  poolSortMode:'port',
  poolSortDir:'asc',
  candSortDir:'asc',
  actionStatus:'',
  actionStatusType:'ok',
  failedAutoPromote:localStorage.getItem('easy_proxies_failed_auto_promote')==='1',
  autoTimers:{},
  autoProbe:JSON.parse(localStorage.getItem('easy_proxies_auto_probe')||'{}'),
  poolGroupOrder:{country:[],tag:[],latency:[]},
  draggingGroup:'',
  filters:{scope:'all',country:'',tag:'',source:'',latency:'',q:'',sort:'latency'}
};
const pages=[
  ['dashboard','仪表盘','dashboard','概览'],
  ['import','导入节点','import','节点'],
  ['pool','可用端口','server','节点'],
  ['nodes','候选节点','list','节点'],
  ['failed','失败节点','alert','节点'],
  ['bulk','批量工具','layers','工具'],
  ['connectivity','站点检测','globe','工具'],
  ['ports','端口状态','network','工具'],
  ['logs','日志','logs','系统'],
  ['settings','设置','settings','系统']
];
const icon=(name,cls='')=>`<svg class="icon${cls?' '+cls:''}" aria-hidden="true"><use href="#i-${name}"/></svg>`;
const qs=s=>document.querySelector(s);
const qsa=s=>Array.from(document.querySelectorAll(s));
const esc=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
function decodeText(s){s=String(s??'');for(let i=0;i<2;i++){try{const d=decodeURIComponent(s);if(!d||d===s)break;s=d}catch(e){break}}return s}
function countryLabel(code){return {JP:'日本',SG:'新加坡',HK:'香港',TW:'台湾',US:'美国',KR:'韩国',CH:'瑞士',NL:'荷兰',RU:'俄罗斯',GB:'英国',DE:'德国',FR:'法国',CA:'加拿大',AU:'澳大利亚',IN:'印度'}[String(code||'').toUpperCase()]||code||'未知'}
function nodeName(n){return decodeText(n.name||n.original_name||n.id)}
function toast(msg,type='ok'){const el=document.createElement('div');el.className='toast '+(type==='err'?'err':'');el.textContent=msg;qs('#toasts').appendChild(el);setTimeout(()=>el.remove(),3600)}
function setStatus(id,msg){const el=qs(id);if(el)el.textContent=msg||''}
function setActionStatus(msg,type='ok'){S.actionStatus=msg||'';S.actionStatusType=type}
function statePill(s){
  const cls=s==='in_pool'||s==='passed'?'green':s==='failed'||s==='blocked_by_chain'||s==='excluded'?'red':s==='testing'?'amber':'';
  const text={in_pool:'池内',passed:'候选',failed:'失败',blocked_by_chain:'前置不可用',excluded:'已排除',testing:'检测中',parsed:'待检测',finished:'已完成',running:'进行中',canceled:'已终止'}[s]||'未知';
  return `<span class="pill ${cls}">${esc(text)}</span>`;
}
function valuePill(text,cls='muted'){return `<span class="value-pill ${cls}">${esc(text||'-')}</span>`}
function latencyPill(n){
  const ms=Number(n.latency_ms)||0;
  if(!ms || n.state==='failed')return valuePill('-','muted');
  if(ms<=500)return valuePill(`${ms} ms`,'green');
  if(ms<=1500)return valuePill(`${ms} ms`,'amber');
  return valuePill(`${ms} ms`,'red');
}
function countryPill(n){return n.country_code?valuePill(countryLabel(n.country_code),'green'):valuePill('-','muted')}
function portPill(n){return n.port?valuePill(n.port,'green'):valuePill('-','muted')}
async function api(path,opt={}){
  const headers={...(opt.headers||{})};
  if(opt.body!==undefined && !(opt.body instanceof FormData)) headers['Content-Type']='application/json';
  if(S.token) headers.Authorization='Bearer '+S.token;
  const res=await fetch(path,{...opt,headers});
  const ct=res.headers.get('content-type')||'';
  let data=ct.includes('application/json')?await res.json().catch(()=>({})):await res.text();
  if(typeof data==='string'&&data.trim().startsWith('{')){try{data=JSON.parse(data)}catch(e){}}
  const message=typeof data==='string'?data:(data&&data.error);
  if(res.status===401){
    qs('#authModal').classList.add('show');
    const err=new Error(message||'未授权');err.status=res.status;throw err;
  }
  if(!res.ok){const err=new Error(message||res.statusText);err.status=res.status;throw err}
  return data;
}
function jobEventValue(event){return event?.kind==='test'?event.test:event?.kind==='refresh'?event.refresh:event?.kind==='connectivity'?event.connectivity:null}
function trimJobEventCache(){
  let completed=0;
  for(const [key,job] of S.jobEvents)if(job?.status&&job.status!=='running'&&!S.jobWaiters.has(key))completed++;
  if(completed<=MAX_JOB_EVENT_CACHE)return;
  for(const [key,job] of S.jobEvents){
    if(!job?.status||job.status==='running'||S.jobWaiters.has(key))continue;
    S.jobEvents.delete(key);
    if(--completed<=MAX_JOB_EVENT_CACHE)break;
  }
}
function storeJobEvent(event){
  const job=jobEventValue(event);if(!job||!event.id)return;
  const key=`${event.kind}:${event.id}`;
  S.jobEvents.delete(key);
  S.jobEvents.set(key,job);
  const waiters=S.jobWaiters.get(key);
  if(waiters)for(const notify of [...waiters])notify(job);
  trimJobEventCache();
  if(S.page==='dashboard')renderDashboardJobs();
}
function forgetJobEvent(kind,id){const key=`${kind}:${id}`;if(!S.jobWaiters.has(key))S.jobEvents.delete(key)}
function wakeJobEventWaiters(){
  for(const waiters of S.jobWaiters.values())for(const notify of [...waiters])notify({__stream_lost:true});
}
function waitForJobEvent(kind,id,stamp,timeout=120000){
  const key=`${kind}:${id}`,current=S.jobEvents.get(key);
  if(current&&String(current.updated_at||'')!==String(stamp||''))return Promise.resolve(current);
  return new Promise(resolve=>{
    let settled=false;
    const cleanup=()=>{const waiters=S.jobWaiters.get(key);if(waiters){waiters.delete(notify);if(!waiters.size)S.jobWaiters.delete(key)}};
    const finish=value=>{if(settled)return;settled=true;clearTimeout(timer);cleanup();resolve(value)};
    const notify=job=>{if(job?.__stream_lost)finish(null);else if(String(job?.updated_at||'')!==String(stamp||''))finish(job)};
    if(!S.jobWaiters.has(key))S.jobWaiters.set(key,new Set());
    S.jobWaiters.get(key).add(notify);
    const timer=setTimeout(()=>finish(null),timeout);
  });
}
function consumeJobEventData(data){
  if(Array.isArray(data)){for(const event of data)storeJobEvent(event);return}
  storeJobEvent(data);
}
async function readJobEventStream(response,signal){
  if(!response.body)throw new Error('浏览器不支持流式进度');
  const reader=response.body.getReader(),decoder=new TextDecoder();let buffer='';
  while(!signal.aborted){
    const {value,done}=await reader.read();if(done)break;
    buffer+=decoder.decode(value,{stream:true}).replace(/\r/g,'');
    let boundary;
    while((boundary=buffer.indexOf('\n\n'))>=0){
      const block=buffer.slice(0,boundary);buffer=buffer.slice(boundary+2);
      const payload=block.split('\n').filter(line=>line.startsWith('data:')).map(line=>line.slice(5).trimStart()).join('\n');
      if(payload){try{consumeJobEventData(JSON.parse(payload))}catch(e){}}
    }
  }
}
function startJobEventStream(force=false){
  if(force&&S.jobStreamAbort){S.jobStreamAbort.abort();S.jobStreamTask=null}
  if(S.jobStreamTask)return;
  const controller=new AbortController();S.jobStreamAbort=controller;
  S.jobStreamTask=(async()=>{
    let failures=0;
    while(!controller.signal.aborted){
      try{
        const headers=S.token?{Authorization:'Bearer '+S.token}:{};
        const response=await fetch('/api/job-events',{headers,signal:controller.signal});
        if(!response.ok)throw new Error(`进度流连接失败: ${response.status}`);
        failures=0;S.jobStreamConnected=true;await readJobEventStream(response,controller.signal);
      }catch(err){if(controller.signal.aborted)break;failures++}
      S.jobStreamConnected=false;wakeJobEventWaiters();
      if(!controller.signal.aborted)await new Promise(resolve=>setTimeout(resolve,Math.min(5000,500*Math.pow(2,Math.min(failures,3)))));
    }
  })().finally(()=>{if(S.jobStreamAbort===controller){S.jobStreamAbort=null;S.jobStreamTask=null}});
}
async function login(e){
  e.preventDefault();
  try{
    const data=await api('/api/auth',{method:'POST',body:JSON.stringify({password:qs('#password').value})});
    S.token=data.token||'';
    if(S.token)localStorage.setItem('easy_proxies_token',S.token);
    qs('#authModal').classList.remove('show');
    syncLogoutButton();
    startJobEventStream(true);
    await refresh();
  }catch(err){setStatus('#authStatus',err.message)}
}
function renderNav(){
  let group='';
  qs('#nav').innerHTML=pages.map(([id,label,iconName,section])=>{
    const count=id==='nodes'?(S.summary?.passed??''):id==='pool'?(S.summary?.in_pool??''):id==='failed'?(S.summary?.failed??''):'';
    const heading=section!==group?`<div class="nav-group">${section}</div>`:'';group=section;
    return `${heading}<button class="nav-item${S.page===id?' active':''}" onclick="go('${id}')" title="${label}"${S.page===id?' aria-current="page"':''}>${icon(iconName)}<span class="label">${label}</span>${count!==''?`<span class="badge${id==='failed'&&Number(count)>0?' red':''}">${count}</span>`:''}</button>`;
  }).join('');
}
function toggleSidebar(){
  const collapsed=qs('#app').classList.toggle('collapsed');
  localStorage.setItem('easy_proxies_sidebar_collapsed',collapsed?'1':'0');
}
function syncLogoutButton(){const button=qs('#logoutButton');if(button)button.hidden=!S.token}
function logout(){
  S.token='';localStorage.removeItem('easy_proxies_token');syncLogoutButton();
  S.jobStreamAbort?.abort();
  qs('#password').value='';setStatus('#authStatus','');
  qs('#authModal').classList.add('show');
  requestAnimationFrame(()=>qs('#password')?.focus());
}
async function refreshCurrentPage(){
  const button=qs('#topRefresh');button?.classList.add('spinning');
  try{S.shellStatusAt=0;await refresh()}finally{button?.classList.remove('spinning')}
}
async function updateShellStatus(force=false){
  if(!force&&Date.now()-S.shellStatusAt<15000){renderSideStatus();return}
  S.shellStatusAt=Date.now();
  const [summary,diagnostics]=await Promise.all([api('/api/ui/summary').catch(()=>null),api('/api/diagnostics').catch(()=>null)]);
  if(summary){S.summary=summary;renderNav()}
  if(diagnostics)S.diagnostics=diagnostics;
  renderSideStatus();
}
function formatHeap(bytes){const n=Number(bytes)||0;return n>=1<<30?`${(n/(1<<30)).toFixed(2)} GB`:`${(n/(1<<20)).toFixed(1)} MB`}
function renderSideStatus(){
  const host=qs('#sideStatus');if(!host)return;
  const runtime=S.diagnostics?.runtime,goStats=S.diagnostics?.go;
  if(!runtime&&!S.summary){host.innerHTML='';return}
  const running=!!runtime?.running,pool=Number(S.summary?.in_pool)||0;
  host.innerHTML=`<div class="side-stat"><span class="status-dot ${running?'ok':pool?'bad':'warn'}"></span><div><span>代理核心</span><strong>${running?'运行中':'未运行'}</strong></div></div>
    <div class="side-stat">${icon('server','sm')}<div><span>池内端口</span><strong>${esc(pool)}</strong></div></div>
    ${goStats?`<div class="side-stat">${icon('cpu','sm')}<div><span>堆内存 · 协程</span><strong>${esc(formatHeap(goStats.heap_alloc_bytes))} · ${esc(goStats.goroutines)}</strong></div></div>`:''}`;
}
function pageFromHash(){
  const page=decodeURIComponent(location.hash.replace(/^#/,''));
  return pages.some(item=>item[0]===page)?page:'dashboard';
}
async function go(page,fromHash=false){
  if(!pages.some(item=>item[0]===page))page='dashboard';
  if(!fromHash&&location.hash!==`#${page}`){location.hash=page;return}
	if(S.page!==page)resetFilters();
  S.page=page;
  const p=pages.find(x=>x[0]===page);
  qs('#pageTitle').textContent=p[1];
  qs('#crumbTitle').textContent=p[1];
  qs('#crumbGroup').textContent=p[3];
  document.title=`${p[1]} · Easy Proxies`;
  if(page!=='dashboard')stopDashboardClock();
	renderNav();
	await refresh();
}
window.addEventListener('hashchange',()=>go(pageFromHash(),true));
function resetFilters(){S.filters={scope:'all',country:'',tag:'',source:'',latency:'',q:'',sort:'latency'};S.candSortDir='asc';S.managedPage=1}
function beginPageRequest(){
  S.pageController?.abort();
  const controller=new AbortController();
  const request={id:++S.pageRequest,signal:controller.signal};
  S.pageController=controller;
  return request;
}
function isCurrentPageRequest(request){return !request||request.id===S.pageRequest}
function isAbortError(err){return err?.name==='AbortError'}
async function refresh(){
  const request=beginPageRequest();
  try{
    if(['nodes','pool','failed'].includes(S.page)){
      await Promise.all([loadManaged(S.page,request),loadImportSummary(request)]);
      if(!isCurrentPageRequest(request))return;
      renderManaged(S.page);
    }else if(S.page==='config'){
      await loadConfigNodes(request);
      if(!isCurrentPageRequest(request))return;
      renderConfig();
    }else if(S.page==='ports'){
      await loadPorts(request);
      if(!isCurrentPageRequest(request))return;
      renderPorts();
    }else if(S.page==='logs'){
      await loadLogs(request);
      if(!isCurrentPageRequest(request))return;
      renderLogs();
    }else if(S.page==='settings'){
      await Promise.all([loadSettings(request),loadSubscriptionConfig(request),loadImportSources(request),loadBackupData(request),loadChainProfiles(request)]);
      if(!isCurrentPageRequest(request))return;
      renderSettings();
	  if(S.webdav.address)loadWebDAVFiles(request);
	}else if(S.page==='bulk'){
	  await loadImportSummary(request);
	  if(!isCurrentPageRequest(request))return;
	  renderBulk();
	}else if(S.page==='connectivity'){
	  await loadConnectivityScopes(request);
	  if(!isCurrentPageRequest(request))return;
	  renderConnectivity();
	}else if(S.page==='dashboard'){
	  await loadDashboard(request);
	  if(!isCurrentPageRequest(request))return;
	  renderDashboard();
	}else{
      await Promise.all([loadSubscriptionConfig(request),loadImportSources(request),loadImportSummary(request),loadPortPreview(request),loadChainProfiles(request)]);
      if(!isCurrentPageRequest(request))return;
      renderImport();
    }
    renderNav();
    restoreActiveJobs();
    updateShellStatus().catch(()=>{});
  }catch(err){if(!isAbortError(err))toast(err.message,'err')}
}
function managedScope(kind){return kind==='pool'?'pool':kind==='failed'?'failed':'candidate'}
async function loadManaged(kind=S.page,request){
  const params=new URLSearchParams({
    scope:managedScope(kind),
    page:String(S.managedPage),
    page_size:String(S.pageSize),
    sort:kind==='pool'?(S.poolSortMode||'port'):(S.filters.sort||'latency'),
    order:kind==='pool'?(S.poolSortDir||'asc'):(S.candSortDir||'asc'),
  });
  if(S.filters.country)params.set('country',S.filters.country);
  if(S.filters.tag)params.set('tag',S.filters.tag);
  if(S.filters.latency)params.set('latency',S.filters.latency);
  if(S.filters.q)params.set('q',S.filters.q);
  const data=await api('/api/ui/nodes?'+params.toString(),{signal:request?.signal});
  if(!isCurrentPageRequest(request))return null;
  S.nodePage=data;
  S.nodes=Array.isArray(data.items)?data.items:[];
  S.nodesLoadedAt=Date.now();
  S.viewNodes=S.nodes.slice();
  return data;
}
async function loadImportSummary(request){
  const summary=await api('/api/ui/summary',{signal:request?.signal}).catch(err=>{if(isAbortError(err))throw err;return S.summary||null});
  if(!isCurrentPageRequest(request))return null;
  S.summary=summary;
  S.summaryLoadedAt=Date.now();
  return S.summary;
}
async function loadPortPreview(request){
  const ports=await api('/api/ui/ports?limit=12',{signal:request?.signal});
  if(!isCurrentPageRequest(request))return null;
  S.portPreview=Array.isArray(ports)?ports:[];
  return S.portPreview;
}
async function loadConfigNodes(request){
  const data=await api('/api/nodes/config',{signal:request?.signal});
  if(!isCurrentPageRequest(request))return null;
  S.configNodes=data.nodes||[];
}
async function loadPorts(request){
  const ports=await api('/api/ports/status',{signal:request?.signal});
  if(!isCurrentPageRequest(request))return null;
  S.ports=ports;
}
let dialogReturnFocus=null;
function openDialog(title,html,options={}){
  let modal=qs('#resultModal');
  if(!modal){
    modal=document.createElement('div');
    modal.className='modal';
    modal.id='resultModal';
    modal.innerHTML='<div class="dialog" id="resultDialog"></div>';
    document.body.appendChild(modal);
  }
  const dialog=qs('#resultDialog')||modal.firstElementChild;
  dialogReturnFocus=document.activeElement instanceof HTMLElement?document.activeElement:null;
  S.refreshDialogOpen=false;
  modal.classList.remove('refresh-modal');
  dialog.className=`dialog standard-dialog${options.wide?' wide':''}`;
  delete dialog.dataset.refreshShell;
  delete dialog.dataset.tagBindingShell;
  const actions=options.actions===undefined?'<button class="btn primary" type="button" onclick="closeDialog()">确定</button>':options.actions;
  dialog.innerHTML=`<div class="dialog-header"><h2 id="resultDialogTitle">${esc(title)}</h2><button class="btn ghost dialog-close" type="button" onclick="closeDialog()" aria-label="关闭">×</button></div><div class="dialog-body"><div class="stack">${html}</div></div>${actions!==''?`<div class="dialog-footer">${actions}</div>`:''}`;
  dialog.setAttribute('role','dialog');
  dialog.setAttribute('aria-modal','true');
  dialog.setAttribute('aria-labelledby','resultDialogTitle');
  modal.onclick=event=>{if(event.target===modal)closeDialog()};
  modal.classList.add('show');
  document.addEventListener('keydown',handleDialogKeydown);
  requestAnimationFrame(()=>dialog.querySelector('button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled])')?.focus());
}
function handleDialogKeydown(event){
  const modal=qs('#resultModal'),dialog=qs('#resultDialog');
  if(!modal?.classList.contains('show')||!dialog)return;
  if(event.key==='Escape'){event.preventDefault();closeDialog();return}
  if(event.key!=='Tab')return;
  const focusable=qsa('#resultDialog button:not([disabled]),#resultDialog input:not([disabled]),#resultDialog select:not([disabled]),#resultDialog textarea:not([disabled]),#resultDialog a[href]').filter(el=>el.offsetParent!==null);
  if(!focusable.length){event.preventDefault();dialog.focus();return}
  const first=focusable[0],last=focusable[focusable.length-1];
  if(event.shiftKey&&document.activeElement===first){event.preventDefault();last.focus()}
  else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first.focus()}
}
function closeDialog(){
  const modal=qs('#resultModal');
  S.refreshDialogOpen=false;
  modal?.classList.remove('show','refresh-modal');
  document.removeEventListener('keydown',handleDialogKeydown);
  const target=dialogReturnFocus;dialogReturnFocus=null;
  if(target?.isConnected)requestAnimationFrame(()=>target.focus());
}
async function loadLogs(request){
  const data=await api('/api/logs',{signal:request?.signal});
  if(!isCurrentPageRequest(request))return null;
  S.logs=data.logs||'';
}
async function loadSettings(request){
  const settings=await api('/api/settings',{signal:request?.signal});
  if(!isCurrentPageRequest(request))return null;
  S.settings=settings;
}
async function loadSubscriptionConfig(request){
  const sub=await api('/api/subscription/config',{signal:request?.signal}).catch(err=>{if(isAbortError(err))throw err;return {subscriptions:[],enabled:true,interval:'24h'}});
  if(!isCurrentPageRequest(request))return null;
  sub.subscriptions=Array.isArray(sub.subscriptions)?sub.subscriptions:[];
  sub.test_204=sub.test_204!==false;
  sub.site_targets=Array.isArray(sub.site_targets)?sub.site_targets.filter(id=>VERIFICATION_TARGETS.some(target=>target.id===id)):[];
  if(!sub.subscriptions.length && (!sub.interval || sub.interval==='1h0m0s' || sub.interval==='1h'))sub.interval='24h';
  if(sub.enabled===undefined || !sub.subscriptions.length)sub.enabled=true;
  S.subConfig=sub;
}
async function loadImportSources(request){
  const sources=await api('/api/import/sources',{signal:request?.signal}).catch(err=>{if(isAbortError(err))throw err;return []});
  if(!isCurrentPageRequest(request))return null;
  S.importSources=Array.isArray(sources)?sources:[];
}
async function loadChainProfiles(request){
  const data=await api('/api/chain-profiles',{signal:request?.signal});
  if(!isCurrentPageRequest(request))return null;
  S.chainProfiles=Array.isArray(data.profiles)?data.profiles:[];
  S.chainUsage=data.usage&&typeof data.usage==='object'?data.usage:{};
  if(S.chainProfileID&&!S.chainProfiles.some(profile=>profile.id===S.chainProfileID&&profile.enabled))selectImportChain('');
  return S.chainProfiles;
}
async function loadBackupData(request){
  const webdav=await api('/api/backup/webdav/settings',{signal:request?.signal});
  if(!isCurrentPageRequest(request))return null;
  S.webdav=webdav||{address:'',username:'',password:'',folder:'/easy_proxies'};
  if(!S.webdav.folder)S.webdav.folder='/easy_proxies';
  S.webdavFiles=[];
  S.webdavLoading=!!S.webdav.address;
  return S.webdav;
}
async function loadWebDAVFiles(request,throwOnError=false){
  S.webdavLoading=true;
  try{
    const data=await api('/api/backup/webdav/files',{signal:request?.signal});
    if(!isCurrentPageRequest(request))return;
    S.webdavFiles=Array.isArray(data.files)?data.files:[];
  }catch(err){
    if(isAbortError(err))return;
    if(!isCurrentPageRequest(request))return;
    S.webdavFiles=[];
    if(throwOnError)throw err;
  }finally{
    if(isCurrentPageRequest(request)){
      S.webdavLoading=false;
      const host=qs('#webdavFiles');
      if(host)host.innerHTML=webDAVFilesHTML(S.webdavFiles,false);
    }
  }
}
