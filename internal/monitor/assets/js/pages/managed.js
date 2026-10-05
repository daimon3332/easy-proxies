function renderManaged(kind){
  const preset=kind==='pool'?'pool':kind==='failed'?'failed':'candidate';
  const items=S.nodes||[];
  const countries=S.nodePage?.countries||[];
  const tags=S.nodePage?.tags||[];
  const filters=`<div class="filters compact">
        <select id="countryFilter" onchange="updateFilters()" aria-label="按国家筛选"><option value="">全部国家</option>${countries.map(v=>`<option value="${esc(v)}">${esc(countryLabel(v))} (${esc(v)})</option>`).join('')}</select>
        <select id="tagFilter" onchange="updateFilters()" aria-label="按标签筛选"><option value="">全部标签</option>${tags.map(v=>`<option value="${esc(v)}">${esc(v)}</option>`).join('')}</select>
        <label class="search-field">${icon('search','sm')}<input id="nameFilter" type="search" placeholder="根据名称搜索" aria-label="根据名称搜索" oninput="onNameFilterInput(event)" value="${esc(S.filters.q||'')}"></label>
      </div>`;
  const heading=kind==='pool'?'节点池':kind==='failed'?'失败节点':'候选节点';
  const total=S.nodePage?.total||0;
  qs('#view').innerHTML=`<div class="card table-card">
    <div class="card-head"><h2>${heading}<span class="section-count">${esc(total)} 个节点</span></h2>
      <div class="head-actions">${kind==='pool'?poolActionBar():''}${kind==='failed'?failedOptionBar(tags):''}${autoProbeBar(kind)}</div>
    </div>
    <div class="table-toolbar">${filters}<div class="selection-host" id="selectionHost">${batchActionBar(items,kind)}</div></div>
    ${S.actionStatus?`<p class="action-status ${S.actionStatusType==='err'?'err':''}">${esc(S.actionStatus)}</p>`:''}
    <div id="nodeList">${renderNodeTable(items,kind)}</div>
  </div>`;
  if(kind==='nodes'||kind==='failed'||kind==='pool')setFilterValues(preset);
}
function autoProbeBar(kind){
  const cfg=autoProbeConfig(kind);
  return `<details class="row-menu">
    <summary class="btn ${cfg.enabled?'secondary':''}">${icon('timer','sm')}自动测速${cfg.enabled?` · ${cfg.seconds} 秒`:''}</summary>
    <div class="row-menu-panel auto-probe-panel">
      <label class="inline-check"><input type="checkbox" ${cfg.enabled?'checked':''} onchange="setAutoProbe('${kind}','enabled',this.checked)">启用自动测速</label>
      <div class="field"><label>测速间隔</label><div class="number-suffix"><input type="number" min="10" value="${cfg.seconds}" aria-label="测速间隔（秒）" onchange="setAutoProbe('${kind}','seconds',this.value)"><span>秒</span></div></div>
      <button class="btn small secondary" onclick="runAutoProbe('${kind}',true)">立即执行</button>
    </div>
  </details>`;
}
function autoProbeConfig(kind){
  const v=S.autoProbe[kind]||{};
  return {enabled:!!v.enabled,seconds:Number(v.seconds)||300};
}
function setAutoProbe(kind,key,value){
  const cfg=autoProbeConfig(kind);
  if(key==='enabled')cfg.enabled=!!value;
  if(key==='seconds')cfg.seconds=Math.max(10,Number(value)||300);
  S.autoProbe[kind]=cfg;
  localStorage.setItem('easy_proxies_auto_probe',JSON.stringify(S.autoProbe));
  setupAutoProbe(kind);
  renderManaged(S.page);
}
function setupAutoProbe(kind){
  if(S.autoTimers[kind]){clearInterval(S.autoTimers[kind]);S.autoTimers[kind]=null}
  const cfg=autoProbeConfig(kind);
  if(!cfg.enabled)return;
  S.autoTimers[kind]=setInterval(()=>runAutoProbe(kind,false),cfg.seconds*1000);
}
function setupAllAutoProbe(){['nodes','pool','failed'].forEach(setupAutoProbe)}
function failedOptionBar(tags=[]){
  return `<label class="inline-check" title="失败节点重新测速成功后的去向"><input type="checkbox" ${S.failedAutoPromote?'checked':''} onchange="setFailedAutoPromote(this.checked)">测速成功后自动加入到节点池</label>`;
}
function setFailedAutoPromote(v){S.failedAutoPromote=!!v;localStorage.setItem('easy_proxies_failed_auto_promote',S.failedAutoPromote?'1':'0');renderManaged(S.page)}
function renderNodeTable(items,kind='nodes'){
  const visible=items;
  const allChecked=visible.length&&visible.every(n=>S.selected.has(n.id));
  const sortable=kind!=='pool';
  const filtered=S.filters.q||S.filters.country||S.filters.tag;
  const emptyHint=filtered?'调整筛选条件后再试':kind==='pool'?'导入并测速成功的节点会出现在这里':kind==='failed'?'测速失败的节点会出现在这里':'测速通过但未入池的节点会出现在这里';
  return `<div class="node-table">
    <div class="table-head">
      <input class="check" type="checkbox" aria-label="选择当前页全部节点" ${allChecked?'checked':''} onchange="toggleAllVisible(this.checked)">
      <div>序号</div>
      ${sortable?sortHeader('名称','name','candidate'):'<div>名称</div>'}
      <div>状态</div>
      ${sortable?sortHeader('延迟','latency','candidate'):'<div>延迟</div>'}
      ${sortable?sortHeader('国家','country','candidate'):'<div>国家</div>'}
      <div>端口</div>
      <div>标签</div>
      <div>操作</div>
    </div>
    ${visible.length?visible.map((n,i)=>renderNodeRow(n,kind,(S.nodePage?.page-1||0)*(S.nodePage?.page_size||S.pageSize)+i)).join(''):`<div class="empty"><strong>没有节点</strong><span>${emptyHint}</span></div>`}
    ${renderNodePager()}
  </div>`;
}
function renderNodePager(){
  const total=S.nodePage?.total||0,page=S.nodePage?.page||1,size=S.nodePage?.page_size||S.pageSize;
  const pages=Math.max(1,Math.ceil(total/size));
  if(!total)return '';
  const from=(page-1)*size+1,to=Math.min(total,page*size);
  return `<div class="table-footer"><span>第 ${from}–${to} 条，共 ${total} 条</span>${pages>1?`<div class="pager"><button class="btn small" ${page<=1?'disabled':''} onclick="changeManagedPage(-1)">上一页</button><span>${page} / ${pages}</span><button class="btn small" ${page>=pages?'disabled':''} onclick="changeManagedPage(1)">下一页</button></div>`:''}</div>`;
}
async function changeManagedPage(delta){
  const total=S.nodePage?.total||0,size=S.nodePage?.page_size||S.pageSize;
  const pages=Math.max(1,Math.ceil(total/size));
  S.managedPage=Math.min(pages,Math.max(1,(S.nodePage?.page||1)+delta));
  await refresh();
}
function renderNodeRow(n,kind,index=0){
  return `<div class="table-row" data-id="${esc(n.id)}" data-name="${esc(searchText(n))}">
    <input class="check" type="checkbox" aria-label="选择 ${esc(nodeName(n))}" ${S.selected.has(n.id)?'checked':''} onchange="toggleNode('${esc(n.id)}',this.checked)">
    <div class="table-index">${index+1}</div>
    <div class="table-name"><strong title="${esc(nodeName(n))}">${esc(nodeName(n))}</strong>${n.last_error?`<div class="table-uri" title="${esc(n.last_error)}">错误：${esc(shortError(n.last_error))}</div>`:''}</div>
    <div>${statePill(n.state)}</div>
    <div>${latencyPill(n)}</div>
    <div>${countryPill(n)}</div>
    <div>${portPill(n)}</div>
    <div>${n.tag_prefix?valuePill(n.tag_prefix,'muted'):valuePill(shortSource(n.import_source)||'-','muted')}</div>
    <div>${renderNodeActions(n,kind)}</div>
  </div>`;
}
function sortHeader(label,mode,target){
  const active=(target==='pool'?S.poolSortMode:S.filters.sort)===mode;
  return `<button class="th-btn${active?' active':''}" onclick="setListSort('${target}','${mode}')">${label}</button>`;
}
async function setListSort(target,mode){
  if(target==='pool'){
    if(S.poolSortMode===mode)S.poolSortDir=S.poolSortDir==='asc'?'desc':'asc';
    else{S.poolSortMode=mode;S.poolSortDir='asc'}
  }else{
    if(S.filters.sort===mode)S.candSortDir=S.candSortDir==='asc'?'desc':'asc';
    else{S.filters.sort=mode;S.candSortDir='asc'}
  }
  S.managedPage=1;
  await refresh();
}
function renderNodeActions(n,kind){
  const del=`<button class="btn small danger" onclick="deleteManagedNode('${esc(n.id)}')">删除</button>`;
  const exportBtn=`<button class="btn small" onclick="openNodeExportDialog(['${esc(n.id)}'])">导出</button>`;
  const more=items=>`<details class="row-menu"><summary class="btn small ghost" aria-label="更多操作" title="更多操作">${icon('more','sm')}</summary><div class="row-menu-panel">${items.join('')}</div></details>`;
  if(kind==='pool')return `<div class="row-actions"><button class="btn small secondary" onclick="managedAction('${esc(n.id)}','retest')">测速</button>${more([`<button class="btn small" onclick="managedAction('${esc(n.id)}','country')">测试国家</button>`,exportBtn,del])}</div>`;
  if(n.state==='failed')return `<div class="row-actions"><button class="btn small secondary" onclick="retestFailedNodes(['${esc(n.id)}'])">测速</button>${more([exportBtn,del])}</div>`;
  if(candidate(n))return `<div class="row-actions"><button class="btn small green" onclick="managedAction('${esc(n.id)}','promote')">加入池</button>${more([`<button class="btn small" onclick="managedAction('${esc(n.id)}','retest')">测速</button>`,`<button class="btn small" onclick="managedAction('${esc(n.id)}','country')">测试国家</button>`,exportBtn,del])}</div>`;
  if(n.state==='parsed')return `<div class="row-actions"><button class="btn small secondary" onclick="managedAction('${esc(n.id)}','retest')">测速</button>${more([exportBtn,del])}</div>`;
  if(n.state==='excluded')return '';
  return `<div class="row-actions"><button class="btn small" onclick="managedAction('${esc(n.id)}','retest')">重新测速</button>${more([exportBtn,del])}</div>`;
}
function inPool(n){return n.state==='in_pool'||n.in_pool}
function candidate(n){return !inPool(n)&&n.state==='passed'}
function unique(a){return [...new Set(a)].sort((x,y)=>String(x).localeCompare(String(y)))}
function shortSource(s){s=String(s||'');return s.length>42?s.slice(0,39)+'...':s}
function shortError(s){s=String(s||'');return s.length>54?s.slice(0,51)+'...':s}
function searchText(n){return [nodeName(n),n.original_name,n.uri,n.tag_prefix,n.country_code,countryLabel(n.country_code),n.import_source,n.import_format,n.state].filter(Boolean).join(' ').toLowerCase()}
function filteredManaged(scope=S.filters.scope){
  let items=S.nodes.slice();
  if(scope==='pool')items=items.filter(inPool);
  if(scope==='not_pool')items=items.filter(n=>!inPool(n));
  if(scope==='failed')items=items.filter(n=>n.state==='failed');
  if(scope==='candidate')items=items.filter(candidate);
  return filteredManagedFrom(items,scope);
}
function filteredManagedFrom(items,scope=S.filters.scope){
  items=items.slice();
  if(S.filters.country)items=items.filter(n=>n.country_code===S.filters.country);
  if(S.filters.tag)items=items.filter(n=>(n.tag_prefix||'local')===S.filters.tag);
  if(S.filters.source)items=items.filter(n=>(n.import_source||n.import_mode||'unknown')===S.filters.source);
  if(S.filters.latency){
    items=items.filter(n=>{
      const l=Number(n.latency_ms)||0;
      if(S.filters.latency==='none')return !l;
      if(S.filters.latency==='0-500')return l>0&&l<=500;
      if(S.filters.latency==='500-1500')return l>500&&l<=1500;
      if(S.filters.latency==='1500+')return l>1500;
      return true;
    });
  }
  if(S.filters.q){const q=S.filters.q.toLowerCase();items=items.filter(n=>searchText(n).includes(q))}
  const dir=S.candSortDir==='desc'?-1:1;
  items.sort((a,b)=>{
    return dir*compareNodes(a,b,S.filters.sort);
  });
  return items;
}
function compareNodes(a,b,mode){
  if(mode==='latency')return (a.latency_ms||9e12)-(b.latency_ms||9e12)||nodeName(a).localeCompare(nodeName(b));
  if(mode==='tag')return String(a.tag_prefix||'').localeCompare(String(b.tag_prefix||''))||nodeName(a).localeCompare(nodeName(b));
  if(mode==='country')return String(a.country_code||'ZZ').localeCompare(String(b.country_code||'ZZ'))||nodeName(a).localeCompare(nodeName(b));
  if(mode==='port')return (a.port||9e12)-(b.port||9e12)||(a.order||0)-(b.order||0)||nodeName(a).localeCompare(nodeName(b));
  return nodeName(a).localeCompare(nodeName(b));
}
function sortPoolItems(items){
  const mode=S.poolSortMode||'port';
  const dir=S.poolSortDir==='desc'?-1:1;
  return items.slice().sort((a,b)=>dir*compareNodes(a,b,mode));
}
function setFilterValues(scope){
  const set=(id,val)=>{const el=qs('#'+id);if(el)el.value=val};
  set('countryFilter',S.filters.country);set('tagFilter',S.filters.tag);set('nameFilter',S.filters.q||'');
  const nameEl=qs('#nameFilter');
  if(nameEl&&!nameEl.dataset.composeBound){
    nameEl.dataset.composeBound='1';
    nameEl.addEventListener('compositionend',()=>{S.filters.q=nameEl.value||'';updateFilters()});
  }
}
function updateFilters(){
  ['country','tag'].forEach(k=>{const el=qs('#'+k+'Filter');if(el&&!el.disabled)S.filters[k]=el.value});
  const nameEl=qs('#nameFilter');if(nameEl)S.filters.q=nameEl.value||'';
  S.managedPage=1;
  clearTimeout(S.filterTimer);
  S.filterTimer=setTimeout(()=>refresh(),160);
}
function onNameFilterInput(e){
  if(e&&e.isComposing){S.filters.q=e.target.value||'';return}
  updateFilters();
}
function toggleNode(id,checked){if(checked)S.selected.add(id);else S.selected.delete(id);updateSelectionBar()}
function updateSelectionBar(){
  const host=qs('#selectionHost');if(host)host.innerHTML=batchActionBar(S.nodes||[],S.page);
  const all=qs('#nodeList .table-head input');const ids=currentVisibleIds();if(all)all.checked=!!ids.length&&ids.every(id=>S.selected.has(id));
}
function currentVisibleIds(){return qsa('#nodeList .table-row').map(el=>el.dataset.id).filter(Boolean)}
function toggleAllVisible(checked){currentVisibleIds().forEach(id=>toggleNode(id,checked));renderManaged(S.page)}
function clearSelection(){S.selected.clear();renderManaged(S.page)}
async function retestSelectedVisible(){
  const ids=[...S.selected].filter(id=>currentVisibleIds().includes(id));
  if(!ids.length){toast('请先选择节点','err');return}
  const failedIds=ids.filter(id=>S.nodes.find(n=>n.id===id)?.state==='failed');
  if(failedIds.length===ids.length){
    await retestFailedNodes(ids);
    return;
  }
  await retestNodes(ids);
}
async function countrySelectedVisible(){
  const ids=[...S.selected].filter(id=>currentVisibleIds().includes(id));
  if(!ids.length){toast('请先选择节点','err');return}
  await countryNodes(ids);
}
async function promoteSelectedVisible(){
  const visible=new Set(currentVisibleIds());
  const selectedCandidates=S.nodes.filter(n=>visible.has(n.id)&&S.selected.has(n.id)&&candidate(n)).map(n=>n.id);
  if(!selectedCandidates.length){toast('请先选择候选节点','err');return}
  await promoteNodes(selectedCandidates);
}
async function runAsyncBatchTest(opts){
  const body={
    node_ids:opts.ids||[],
    scopes:opts.scopes||[],
    retest:!!opts.retest,
    country:!!opts.country,
    promote_passed:!!opts.promote_passed,
    auto_reload:opts.auto_reload!==false,
  };
  const start=await api('/api/managed-nodes/batch-test/start',{method:'POST',body:JSON.stringify(body)});
  const jobId=start.job_id;
  if(!jobId)throw new Error('启动测试失败');
  S.activeBatchJob=jobId;
  localStorage.setItem(ACTIVE_BATCH_JOB_KEY,jobId);
  return await pollBatchJob(jobId,opts.label||'正在测试');
}
async function pollBatchJob(jobId,label){
  if(S.batchPollingJob===jobId)return null;
  S.batchPollingJob=jobId;
  S.activeBatchJob=jobId;
  localStorage.setItem(ACTIVE_BATCH_JOB_KEY,jobId);
  openProgress(label||'正在测试');
  let last=null;
  try{
    last=await api('/api/managed-nodes/batch-test/status?id='+encodeURIComponent(jobId));
    while(true){
      updateProgress(last,label||'正在测试');
      if(last.status!=='running')break;
      const pushed=await waitForJobEvent('test',jobId,last.updated_at);
      last=pushed||await api('/api/managed-nodes/batch-test/status?id='+encodeURIComponent(jobId));
    }
    if(last?.status==='canceled')throw new Error('测速已终止，检测前节点池已恢复');
    if(last?.status==='failed')throw new Error(last.error||'测速任务失败');
    if(last?.protected||last?.phase==='protected')throw new Error(last.error||last.protection_reason||'检测异常，检测前节点池已恢复');
    setTimeout(closeProgress,400);
    return last;
  }catch(e){
    closeProgress();
    throw e;
  }finally{
    S.batchPollingJob='';
    if(last && last.status!=='running'){
      forgetJobEvent('test',jobId);
      S.activeBatchJob='';
      localStorage.removeItem(ACTIVE_BATCH_JOB_KEY);
    }
  }
}
function openProgress(title){
  let host=qs('#progressHost');
  if(!host){host=document.createElement('div');host.id='progressHost';document.body.appendChild(host)}
  host.innerHTML=`<div class="dialog-back"><div class="dialog">
    <h3 id="progressTitle">${esc(title)}</h3>
    <div id="progressBody" class="stack"><div class="muted">等待启动...</div></div>
  </div></div>`;
}
async function cancelActiveBatchTest(){
  const id=S.activeBatchJob||localStorage.getItem(ACTIVE_BATCH_JOB_KEY);
  if(!id){toast('没有正在运行的测速任务','err');return}
  try{
    const job=await api('/api/managed-nodes/batch-test/cancel?id='+encodeURIComponent(id),{method:'POST'});
    updateProgress(job,'正在终止');
    toast('已发送终止请求');
  }catch(err){toast(err.message,'err')}
}
function siteProgressSummary(progress){
  if(!Array.isArray(progress)||!progress.length)return '';
  return progress.map(item=>`${item.name||connectivityTargetName(item.target_id)} ${Number(item.done)||0}/${Number(item.total)||0}，成功 ${Number(item.passed)||0}`).join(' · ');
}
function updateProgress(job,title){
  const t=qs('#progressTitle');if(t)t.textContent=title;
  const body=qs('#progressBody');if(!body)return;
  const total=job.total||0,done=job.done||0;
  const probing=job.phase==='probe'&&Number(job.probe_round)>0;
  const siteTesting=job.phase==='sites'&&Array.isArray(job.site_progress)&&job.site_progress.length;
  const siteDone=siteTesting?job.site_progress.reduce((sum,item)=>sum+(Number(item.done)||0),0):0;
  const siteTotal=siteTesting?job.site_progress.reduce((sum,item)=>sum+(Number(item.total)||0),0):0;
  const progressDone=probing?Number(job.probe_round_done||0):siteTesting?siteDone:done;
  const progressTotal=probing?Number(job.probe_round_total||0):siteTesting?siteTotal:total;
  const pct=progressTotal>0?Math.min(100,Math.round(progressDone/progressTotal*100)):0;
  const phaseLabel={probe:'204 测速中',sites:'站点检测中',country:'测试国家',promote:'入池',done:'完成',protected:'已回滚',failed:'失败',queued:'排队中',empty:'空',canceling:'正在终止',canceled:'已终止'}[job.phase]||job.phase||'';
  const progressText=probing?`第 ${job.probe_round}/${job.probe_rounds||3} 轮 · 本轮 ${progressDone}/${progressTotal} (${pct}%) · 剩余 ${job.probe_pending||0}`:siteTesting?siteProgressSummary(job.site_progress):`${done}/${total} (${pct}%)`;
  const stats=[];
  if(Array.isArray(job.chain_probes))job.chain_probes.forEach(probe=>stats.push(probe.error?`前置 ${probe.profile_name||probe.profile_id} 失败`:`前置 ${probe.profile_name||probe.profile_id} ${probe.latency_ms||0} ms`));
  if(job.passed!=null)stats.push(`成功 ${job.passed}`);
  if(job.failed!=null)stats.push(`失败 ${job.failed}`);
  if(job.country_ok)stats.push(`国家OK ${job.country_ok}`);
  if(job.country_bad)stats.push(`国家失败 ${job.country_bad}`);
  if(job.promoted)stats.push(`已入池 ${job.promoted}`);
  body.innerHTML=`
    <div><strong>${esc(phaseLabel)}</strong> · ${esc(progressText)}</div>
    <div class="progress-bar"><div class="progress-fill" style="width:${pct}%"></div></div>
    <div class="muted">${stats.map(esc).join(' · ')||'-'}</div>
    ${job.error?`<div class="action-status err">${esc(job.error)}</div>`:''}
    <div class="row" style="justify-content:flex-end">
      ${job.status==='running'?(job.phase==='canceling'?'<button class="btn danger" disabled>正在终止</button>':'<button class="btn danger" onclick="cancelActiveBatchTest()">终止</button>'):'<button class="btn" onclick="closeProgress()">关闭</button>'}
    </div>
  `;
}
function closeProgress(){const host=qs('#progressHost');if(host)host.innerHTML=''}
async function retestNodes(ids){
  setActionStatus(`正在测速 ${ids.length} 个节点...`);
  renderManaged(S.page);
  let job;
  try{
    job=await runAsyncBatchTest({ids,retest:true,auto_reload:true,label:'测速候选/池节点'});
  }catch(e){
    setActionStatus(e.message,'err');toast(e.message,'err');return;
  }
  const ok=job?.passed||0,fail=job?.failed||0;
  setActionStatus(`测速完成：成功 ${ok}，失败 ${fail}`, fail?'err':'ok');
  toast(`测速完成：成功 ${ok}，失败 ${fail}`, fail?'err':'ok');
  S.selected.clear();
  await refresh();
}
async function retestFailedSelectedVisible(){
  const ids=[...S.selected].filter(id=>currentVisibleIds().includes(id)&&S.nodes.find(n=>n.id===id)?.state==='failed');
  if(!ids.length){toast('请先选择失败节点','err');return}
  await retestFailedNodes(ids);
}
async function retestFailedNodes(ids){
  setActionStatus(`正在处理 ${ids.length} 个失败节点...`);
  renderManaged(S.page);
  let job;
  try{
    job=await runAsyncBatchTest({ids,retest:true,country:true,promote_passed:S.failedAutoPromote,auto_reload:true,label:'失败节点测速 + 测国家'});
  }catch(e){
    setActionStatus(e.message,'err');toast(e.message,'err');return;
  }
  const passed=job?.passed||0,failed=job?.failed||0,promotedCount=job?.promoted||0,candidateCount=S.failedAutoPromote?0:passed;
  const msg=S.failedAutoPromote
    ? `失败节点处理完成：测速成功 ${passed}，失败 ${failed}，已入池 ${promotedCount}`
    : `失败节点处理完成：测速成功 ${passed}，失败 ${failed}，进入候选 ${candidateCount}`;
  setActionStatus(msg, failed?'err':'ok');
  toast(msg, failed?'err':'ok');
  S.selected.clear();
  await refresh();
}
async function promoteNodes(ids){
  if(!ids.length){toast('请先选择候选节点','err');return}
  setActionStatus(`正在加入节点池 ${ids.length} 个节点...`);
  try{
    const res=await api('/api/managed-nodes/batch-promote',{method:'POST',body:JSON.stringify({node_ids:ids,auto_reload:true})});
    const ok=res.promoted||0,fail=Math.max(0,ids.length-ok);
    const msg=`加入节点池完成：成功 ${ok}，失败 ${fail}`;
    setActionStatus(msg,fail?'err':'ok');
    toast(msg,fail?'err':'ok');
    S.selected.clear();
    await refresh();
  }catch(err){
    setActionStatus(err.message,'err');
    toast(err.message,'err');
  }
}
async function runAutoProbe(kind,manual){
  if(!['nodes','pool','failed'].includes(kind))return;
  if(!manual && S.page!==kind)return;
  const scope=kind==='pool'?'pool':kind==='failed'?'failed':'candidate';
  await loadImportSummary();
  const count=scope==='pool'?(S.summary?.in_pool||0):scope==='failed'?(S.summary?.failed||0):(S.summary?.passed||0);
  if(!count){
    if(manual)toast('当前范围没有可测速节点','err');
    return;
  }
  setActionStatus(`自动测速 ${count} 个节点...`);
  if(manual)renderManaged(kind);
  let job;
  try{
    job=await runAsyncBatchTest({scopes:[scope],retest:true,country:kind==='failed',promote_passed:kind==='failed'&&S.failedAutoPromote,auto_reload:true,label:'自动测速'});
  }catch(e){
    setActionStatus(`自动测速失败：${e.message}`,'err');
    if(manual)toast(e.message,'err');
    return;
  }
  const msg=`自动测速完成：成功 ${job?.passed||0}，失败 ${job?.failed||0}`;
  setActionStatus(msg,(job?.failed||0)?'err':'ok');
  if(manual)toast(msg,(job?.failed||0)?'err':'ok');
  await refresh();
}
async function countryNodes(ids){
  setActionStatus(`正在测试国家 ${ids.length} 个节点...`);
  renderManaged(S.page);
  let job;
  try{
    job=await runAsyncBatchTest({ids,country:true,auto_reload:true,label:'测试国家'});
  }catch(e){
    setActionStatus(e.message,'err');toast(e.message,'err');return;
  }
  const ok=job?.country_ok||0,fail=job?.country_bad||0;
  setActionStatus(`测试国家完成：成功 ${ok}，失败 ${fail}`, fail?'err':'ok');
  toast(`测试国家完成：成功 ${ok}，失败 ${fail}`, fail?'err':'ok');
  S.selected.clear();
  await refresh();
}
async function managedAction(id,action){
  try{
    const cur=S.nodes.find(n=>n.id===id);
    if(action==='retest'&&cur&&cur.state==='failed')return await retestFailedNodes([id]);
    if(action==='retest')return await retestNodes([id]);
    const n=await api('/api/managed-nodes/'+encodeURIComponent(id)+'/'+action,{method:'POST'});
    let msg='操作完成';
    if(action==='retest')msg=(candidate(n)||inPool(n))?'测速完成':(n.state==='failed'?'测速失败，已进入失败节点':'测速完成');
    if(action==='country')msg=n.country_code?`国家已更新为 ${countryLabel(n.country_code)}`:'国家测试完成';
    if(action==='promote'){
      if(inPool(n)){
        await api('/api/reload',{method:'POST'});
        msg='已加入节点池并重载核心';
      }else msg='未加入节点池';
    }
    toast(msg);
    await refresh();
  }catch(err){toast(err.message,'err')}
}
async function deleteManagedNode(id){
  if(!confirm('删除后将从所有列表移除，确定继续？'))return;
  try{
    setActionStatus('正在删除节点...');
    await api('/api/managed-nodes/batch-delete',{method:'POST',body:JSON.stringify({node_ids:[id]})});
    S.selected.delete(id);
    setActionStatus('节点已删除');
    toast('节点已删除');
    await refresh();
  }catch(err){
    setActionStatus(err.message,'err');
    toast(err.message,'err');
  }
}
async function deleteSelectedVisible(){
  const ids=[...S.selected].filter(id=>currentVisibleIds().includes(id));
  if(!ids.length){toast('请先选择要删除的节点','err');return}
  if(!confirm(`将删除当前选中的 ${ids.length} 个节点，删除后不会出现在任何列表中，确定继续？`))return;
  setActionStatus(`正在删除 ${ids.length} 个节点...`);
  try{
    const res=await api('/api/managed-nodes/batch-delete',{method:'POST',body:JSON.stringify({node_ids:ids})});
    const ok=res.deleted||0,fail=Math.max(0,ids.length-ok);
    ids.forEach(id=>S.selected.delete(id));
    const msg=`删除完成：成功 ${ok}，失败 ${fail}`;
    setActionStatus(msg,fail?'err':'ok');
    toast(msg,fail?'err':'ok');
    await refresh();
  }catch(err){
    setActionStatus(err.message,'err');
    toast(err.message,'err');
  }
}
async function saveOrder(){
  const ids=S.nodes.filter(n=>n.state==='in_pool'||n.in_pool).map(n=>n.id);
  try{await api('/api/nodes/order',{method:'PUT',body:JSON.stringify({order:ids})});toast('排序已保存')}catch(err){toast(err.message,'err')}
}
function poolActionBar(){
  return `<span class="head-label">显示排序</span><div class="segmented" role="group" aria-label="节点池显示排序">
      ${poolSortButton('port','端口')}${poolSortButton('country','国家')}${poolSortButton('tag','标签')}${poolSortButton('latency','延迟')}
    </div>
    <details class="row-menu"><summary class="btn">${icon('network','sm')}重排端口</summary><div class="row-menu-panel">
      <div class="menu-label">按顺序重新分配端口</div>
      <button class="btn small" onclick="saveGroupedOrderMode('country')">按国家重排</button>
      <button class="btn small" onclick="saveGroupedOrderMode('tag')">按标签重排</button>
      <button class="btn small" onclick="saveGroupedOrderMode('latency')">按延迟重排</button>
    </div></details>`;
}
function poolSortButton(mode,label){
  const active=S.poolSortMode===mode;
  const dir=active?(S.poolSortDir==='desc'?' ↓':' ↑'):'';
  return `<button type="button" class="${active?'active':''}" aria-pressed="${active}" onclick="setListSort('pool','${mode}')">${label}${dir}</button>`;
}
function batchActionBar(items,kind){
  const selected=[...S.selected].filter(id=>items.some(n=>n.id===id));
  if(!S.selected.size)return '<span class="selection-hint">勾选节点后可批量操作</span>';
  const promoteButton=kind==='nodes'?'<button class="btn small green" onclick="promoteSelectedVisible()">加入节点池</button>':'';
  const failedButton=kind==='failed'?'<button class="btn small primary" onclick="retestFailedSelectedVisible()">一键测速</button>':'';
  const deleteButton=(kind==='failed'||kind==='nodes'||kind==='pool')?'<button class="btn small danger" onclick="deleteSelectedVisible()">删除选中</button>':'';
  const speedButtons=kind==='failed'?'':'<button class="btn small" onclick="retestSelectedVisible()">测速</button><button class="btn small" onclick="countrySelectedVisible()">测试国家</button>';
  return `<div class="selection-bar" role="toolbar" aria-label="批量操作">
      <strong>已选择 ${selected.length} 个</strong>
      ${promoteButton}${failedButton}${speedButtons}
      <button class="btn small" onclick="openNodeExportDialog([...S.selected])">导出选中 (${S.selected.size})</button>
      ${deleteButton}
      <button class="btn small ghost" onclick="clearSelection()">取消选择</button>
    </div>`;
}
function poolGroups(mode){
  const map=new Map();
  for(const n of S.nodes.filter(inPool)){
    const key=poolGroupKey(mode,n);
    if(!map.has(key))map.set(key,{key,label:poolGroupLabel(mode,key),nodes:[]});
    map.get(key).nodes.push(n);
  }
  for(const g of map.values()){
    g.nodes.sort((a,b)=>{
      if(mode==='latency')return (a.latency_ms||9e12)-(b.latency_ms||9e12);
      return (a.port||9e12)-(b.port||9e12)||String(a.name||'').localeCompare(String(b.name||''));
    });
  }
  const known=[...(S.poolGroupOrder[mode]||[])].filter(k=>map.has(k));
  const rest=[...map.keys()].filter(k=>!known.includes(k)).sort((a,b)=>String(a).localeCompare(String(b)));
  S.poolGroupOrder[mode]=[...known,...rest];
  return S.poolGroupOrder[mode].map(k=>map.get(k)).filter(Boolean);
}
function poolGroupKey(mode,n){
  if(mode==='tag')return n.tag_prefix||'local';
  if(mode==='latency'){
    const l=Number(n.latency_ms)||0;
    if(!l)return 'none';
    if(l<=500)return '0-500';
    if(l<=1500)return '500-1500';
    return '1500+';
  }
  return n.country_code||'OTHER';
}
function poolGroupLabel(mode,key){
  if(mode==='latency')return {'none':'未测速/无延迟','0-500':'0-500ms','500-1500':'500-1500ms','1500+':'1500ms+'}[key]||key;
  if(mode==='country')return `${countryLabel(key)} (${key})`;
  return key;
}
function renderPoolGroup(g){
  const ports=g.nodes.map(n=>Number(n.port)||0).filter(Boolean).sort((a,b)=>a-b);
  const range=ports.length?(ports[0]===ports[ports.length-1]?String(ports[0]):`${ports[0]}-${ports[ports.length-1]}`):'未分配';
  return `<div class="group-item" draggable="true" data-key="${esc(g.key)}" ondragstart="groupDragStart(event)" ondragover="event.preventDefault()" ondrop="groupDrop(event)" ondragend="groupDragEnd(event)">
    <div class="group-main"><strong><span class="drag-handle">::</span> ${esc(g.label)}</strong><span class="pill green">${g.nodes.length} 个</span></div>
    <div class="meta"><span class="pill">端口 ${esc(range)}</span><span class="pill">${esc(g.nodes[0]?.tag_prefix||'mixed')}</span></div>
  </div>`;
}
function groupDragStart(e){S.draggingGroup=e.currentTarget.dataset.key;e.currentTarget.classList.add('dragging')}
function groupDragEnd(e){e.currentTarget.classList.remove('dragging');S.draggingGroup=''}
function groupDrop(e){
  e.preventDefault();
  const from=S.draggingGroup,to=e.currentTarget.dataset.key,mode=S.poolOrderMode;
  if(!from||!to||from===to)return;
  const order=[...(S.poolGroupOrder[mode]||[])];
  const a=order.indexOf(from),b=order.indexOf(to);
  if(a<0||b<0)return;
  order.splice(b,0,order.splice(a,1)[0]);
  S.poolGroupOrder[mode]=order;
  renderManaged(S.page);
}
async function saveGroupedOrder(){
  const ids=poolGroups(S.poolOrderMode).flatMap(g=>g.nodes.map(n=>n.id));
  if(!ids.length){toast('池内没有可排序节点','err');return}
  try{await api('/api/nodes/order',{method:'PUT',body:JSON.stringify({order:ids})});toast('分组排序已保存，核心已按新端口顺序重载');await refresh()}catch(err){toast(err.message,'err')}
}
async function saveGroupedOrderMode(mode){
  S.poolOrderMode=mode;
  try{
    await api('/api/ui/pool-order',{method:'POST',body:JSON.stringify({mode})});
    toast('节点池端口顺序已重排，核心已重载');
    await refresh();
  }catch(err){toast(err.message,'err')}
}
async function probeAll(){
  try{
    const res=await fetch('/api/nodes/probe-all',{method:'POST',headers:S.token?{Authorization:'Bearer '+S.token}:{}});
    if(!res.ok)throw new Error('探测启动失败: '+res.status);
    const reader=res.body.getReader(),dec=new TextDecoder();let buf='',last='';
    while(true){
      const {value,done}=await reader.read();if(done)break;
      buf+=dec.decode(value,{stream:true});
      for(const line of buf.split('\n'))if(line.startsWith('data:'))last=line.slice(5).trim();
      buf=buf.slice(buf.lastIndexOf('\n')+1);
    }
    toast(last?'探测完成: '+last:'探测完成');
  }catch(err){toast(err.message,'err')}
}
