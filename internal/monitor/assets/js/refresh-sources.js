function refreshPhaseText(job){
  const phase=String(job?.phase||'waiting');
  if(job?.status==='failed'||phase==='failed')return '重新检测失败';
  if(job?.status==='canceled'||phase==='canceled')return '重新检测已取消';
  if(job?.protected||phase==='protected')return '检测异常，已恢复原节点池';
  if(phase==='partial')return '重新检测部分完成';
  if(job?.status==='finished'||phase==='finished')return '重新检测完成';
  return {pulling:'正在拉取订阅',retrying:'正在重试失败来源',testing:'正在分轮测试节点',promoting:'正在加入节点池',applying:'正在应用节点和端口',verifying:'正在验证端口监听',canceling:'正在取消',waiting:'等待开始'}[phase]||'正在刷新';
}
function refreshRowStatusText(status){
  return {pulling:'拉取节点',retrying:'最终重试',retry_waiting:'等待重试',testing:'测试节点',promoting:'加入节点池',applying:'应用端口',verifying:'验证端口',completed:'完成',failed:'失败',canceled:'已取消',waiting:'等待'}[String(status||'waiting')]||'等待';
}
function refreshElapsed(job){
  const start=new Date(job?.started_at||0).getTime();
  if(!start)return '0 秒';
  const terminal=job?.status==='running'?Date.now():new Date(job?.updated_at||Date.now()).getTime();
  const seconds=Math.max(0,Math.floor((terminal-start)/1000));
  if(seconds<60)return `${seconds} 秒`;
  const minutes=Math.floor(seconds/60),remain=seconds%60;
  return `${minutes} 分 ${remain} 秒`;
}
function maskedSubscriptionURL(raw){
  try{return new URL(String(raw||'')).host||'订阅地址'}catch(e){return '订阅地址'}
}
function activeRefreshProbe(job){
  for(const group of Array.isArray(job?.groups)?job.groups:[]){
    for(const row of Array.isArray(group?.urls)?group.urls:[]){
      if(row?.status==='testing'&&Number(row?.probe_round)>0)return row;
    }
  }
  return null;
}
function activeRefreshSiteProgress(job){
  for(const group of Array.isArray(job?.groups)?job.groups:[]){
    for(const row of Array.isArray(group?.urls)?group.urls:[]){
      if(row?.status==='testing'&&Array.isArray(row.site_progress)&&row.site_progress.length)return row.site_progress;
    }
  }
  return [];
}
function refreshProgressHTML(job){
  const total=Number(job?.total_nodes)||0,done=Number(job?.done_nodes)||0;
  const pct=total?Math.min(100,Math.round(done*100/total)):0;
  const indeterminate=job?.status==='running'&&(!total||(!done&&activeRefreshProbe(job)));
  return `<div class="refresh-progress"><span class="${indeterminate?'indeterminate':''}" style="${indeterminate?'':`width:${pct}%`}"></span></div>`;
}
function refreshNodeProgressText(job){
  const total=Number(job?.total_nodes)||0;
  return total?`${Number(job?.done_nodes)||0}/${total}`:'准备中';
}
function probeTargetLabel(target){
  const value=String(target||'');
  if(value.includes('gstatic.com'))return 'Google 204';
  if(value.includes('cloudflare.com'))return 'Cloudflare 204';
  return value?'备用探测目标':'';
}
function refreshStatsHTML(job){
  const terminal=job?.status!=='running';
  const parts=[`<span>来源 ${esc(job?.done_urls||0)}/${esc(job?.total_urls||0)}</span>`,`<span>节点 ${esc(refreshNodeProgressText(job))}</span>`];
  const probe=activeRefreshProbe(job);
  if(probe)parts.push(`<span>第 ${esc(probe.probe_round||0)}/${esc(probe.probe_rounds||3)} 轮 · 本轮 ${esc(probe.probe_round_done||0)}/${esc(probe.probe_round_total||0)} · 剩余 ${esc(probe.probe_pending||0)}</span>`);
  const sites=siteProgressSummary(activeRefreshSiteProgress(job));
  if(sites)parts.push(`<span>${esc(sites)}</span>`);
  if(terminal){
    parts.push(`<span>成功/可用端口 ${esc(job?.pool_count||0)}</span>`);
    parts.push(`<span>测速通过 ${esc(job?.probe_passed||0)}</span>`);
  }else{
    parts.push(`<span>已确定成功 ${esc(job?.probe_passed||job?.passed||0)}</span>`);
  }
  parts.push(`<span>最终失败 ${esc(job?.failed_nodes||0)}</span>`);
  parts.push(`<span>新增/重建 ${esc(job?.promoted||0)}</span>`);
  return parts.join('');
}
function refreshStatusHTML(job){
  if(!job)return '';
  const running=job.status==='running';
  const partial=job.phase==='partial',protectedResult=job.phase==='protected'||(!!job.protected&&job.status!=='failed');
  const cls=job.status==='failed'||partial||protectedResult?'failed':running?'':'done';
  const pollError=S.refreshPollError?`<span style="color:#b94a48">${esc(S.refreshPollError)}</span>`:'';
  return `<div class="refresh-status ${cls}">
    <div class="refresh-status-head"><div><strong>${esc(refreshPhaseText(job))}</strong><span class="muted">已耗时 ${esc(refreshElapsed(job))}</span></div><span class="pill ${job.status==='failed'||job.status==='canceled'||partial||protectedResult?'red':running?'amber':'green'}">${running?'后台运行':job.status==='canceled'?'已取消':job.status==='failed'?'失败':protectedResult?'已回滚':partial?'部分完成':'完成'}</span></div>
    ${refreshProgressHTML(job)}
    <div class="refresh-stats">${refreshStatsHTML(job)}${pollError}</div>
    ${job.protected?`<div class="action-status err">${esc(job.protection_reason||'检测结果异常，本轮结果未应用，已恢复原节点池。')}</div>`:''}
    <div class="refresh-actions">${running?'<button class="btn small danger" onclick="cancelRefreshSourceJob()">取消检测</button>':''}<button class="btn small secondary" onclick="openRefreshJobDetails()">查看详情</button></div>
  </div>`;
}
function renderGlobalRefreshStatus(){
  const host=qs('#globalRefreshStatus');
  if(!host)return;
  const job=S.refreshJob;
  if(!job){host.innerHTML='';return}
  const total=Number(job.total_nodes)||0,done=Number(job.done_nodes)||0;
  const pct=total?Math.min(100,Math.round(done*100/total)):0;
  const probe=activeRefreshProbe(job);
  const detail=probe?`第 ${probe.probe_round||0}/${probe.probe_rounds||3} 轮 · ${probe.probe_round_done||0}/${probe.probe_round_total||0}`:(total?`${done}/${total}`:'准备中');
  host.innerHTML=`<button class="refresh-top" onclick="openRefreshJobDetails()"><span><strong>${esc(refreshPhaseText(job))}</strong><span class="muted"> ${esc(detail)}</span></span><span class="progress"><div style="width:${pct}%"></div></span></button>`;
}
function renderRefreshStatusHost(){
  const host=qs('#refreshStatusHost');
  if(host)host.innerHTML=refreshStatusHTML(S.refreshJob);
  renderGlobalRefreshStatus();
  if(S.refreshJob?.status==='running')qsa('[data-refresh-action]').forEach(button=>button.disabled=true);
}
function renderRefreshJobDialog(job){
  if(!S.refreshDialogOpen)return;
  const modal=qs('#resultModal'),dialog=qs('#resultDialog');
  if(!modal||!dialog)return;
  modal.classList.add('refresh-modal');
  dialog.className='dialog wide standard-dialog';
  dialog.setAttribute('role','dialog');dialog.setAttribute('aria-modal','true');dialog.setAttribute('aria-labelledby','refreshDialogTitle');
  if(dialog.dataset.refreshShell!=='1'){
    dialog.innerHTML='<div class="dialog-header"><h2 id="refreshDialogTitle"></h2><button class="btn ghost dialog-close" type="button" onclick="closeDialog()" aria-label="关闭">×</button></div><div class="dialog-body"><div class="stack"><div id="refreshDialogProgress"></div><div class="refresh-stats" id="refreshDialogStats"></div><div id="refreshDialogNotice"></div><div class="refresh-detail-list" id="refreshDialogList"></div></div></div><div class="dialog-footer refresh-actions" id="refreshDialogActions"></div>';
    dialog.dataset.refreshShell='1';
  }
  const titleHost=qs('#refreshDialogTitle'),progressHost=qs('#refreshDialogProgress'),statsHost=qs('#refreshDialogStats'),noticeHost=qs('#refreshDialogNotice'),listHost=qs('#refreshDialogList'),actionsHost=qs('#refreshDialogActions');
  if(!job){
    titleHost.textContent='重新检测导入来源';
    progressHost.innerHTML='';
    statsHost.innerHTML='<span>正在读取任务状态...</span>';
    noticeHost.innerHTML='';
    listHost.innerHTML='';
    actionsHost.innerHTML='<button class="btn" onclick="closeDialog()">收起</button>';
    modal.classList.add('show');
    document.addEventListener('keydown',handleDialogKeydown);
    return;
  }
  const groups=Array.isArray(job.groups)?job.groups:[];
  const list=groups.map((g,groupIdx)=>{
    const rows=(Array.isArray(g.urls)?g.urls:[]).map((r,urlIdx)=>{
      const raw=String(r.status||'waiting').toLowerCase();
      const cls=raw==='completed'?'green':raw==='failed'||raw==='canceled'?'red':'amber';
      const counts=`进度 ${r.done||0}/${r.total||0} · 成功 ${r.passed||0} · 失败 ${r.failed||0} · 入池 ${r.promoted||0}`;
      const progress=r.detail||((raw==='pulling'||raw==='retrying')?'正在获取节点':counts);
      const local=r.kind==='content';
      const title=local?(r.label||'本地内容'):`订阅 ${groupIdx+1}-${urlIdx+1}`;
      const source=local?'使用已保存的节点重新检测':maskedSubscriptionURL(r.url);
      const attempt=r.attempt?` · 拉取尝试 ${r.attempt}/${r.attempts||2}`:'';
      const probe=r.probe_round?`节点检测第 ${r.probe_round}/${r.probe_rounds||3} 轮 · 本轮 ${r.probe_round_done||0}/${r.probe_round_total||0} · 剩余 ${r.probe_pending||0} · ${probeTargetLabel(r.probe_target)} · 并发 ${r.probe_concurrency||'-'}`:'';
      const sites=siteProgressSummary(r.site_progress);
      const front=r.chain_probe?(r.chain_probe.error?`前置 ${r.chain_probe.profile_name||r.chain_probe.profile_id} 失败：${r.chain_probe.error}`:`前置 ${r.chain_probe.profile_name||r.chain_probe.profile_id} · ${r.chain_probe.latency_ms||0} ms`):'';
      return `<div class="source-row"><div style="flex:1;min-width:0"><div class="row"><strong>${esc(title)}</strong><span class="pill ${cls}">${esc(refreshRowStatusText(raw))}</span>${r.cached?'<span class="pill amber">缓存节点</span>':''}${r.protected?'<span class="pill red">结果未应用</span>':''}</div><div class="muted">${esc(source+attempt)}</div><div class="muted">${esc(progress)}</div>${front?`<div class="muted">${esc(front)}</div>`:''}${probe?`<div class="muted">${esc(probe)}</div>`:''}${sites?`<div class="muted">${esc(sites)}</div>`:''}${r.total?`<div class="muted">${esc(counts)}</div>`:''}${r.warning?`<div class="muted" style="color:#9a6700">${esc(r.warning)}</div>`:''}${r.error?`<div class="muted" style="color:#b94a48">${esc(r.error)}</div>`:''}</div></div>`;
    }).join('');
    return `<div class="stack" style="gap:10px"><div class="row"><strong>${esc(g.tag_prefix||'local')}</strong><span class="muted">来源 ${esc(g.done||0)}/${esc(g.total||0)} · 成功 ${esc(g.successful||0)} · 失败 ${esc(g.failed||0)}</span></div>${rows}</div>`;
  }).join('');
  const scrollTop=listHost.scrollTop;
  titleHost.textContent=refreshPhaseText(job);
  progressHost.innerHTML=refreshProgressHTML(job);
  statsHost.innerHTML=`${refreshStatsHTML(job)}<span>耗时 ${esc(refreshElapsed(job))}</span>`;
  noticeHost.innerHTML=`${job.protected&&job.status!=='failed'?`<div class="action-status err">${esc(job.protection_reason||'执行异常，本轮结果未应用，已恢复原节点池。')}</div>`:''}${job.error&&(job.status==='failed'||!job.protected)?`<div style="color:#b94a48">${esc(job.error)}</div>`:''}`;
  listHost.innerHTML=list;
  listHost.scrollTop=scrollTop;
  actionsHost.innerHTML=`${job.status==='running'?'<button class="btn danger" onclick="cancelRefreshSourceJob()">取消检测</button>':''}<button class="btn ${job.status==='running'?'secondary':'primary'}" onclick="closeDialog()">${job.status==='running'?'收起':'关闭'}</button>`;
  modal.classList.add('show');
  document.addEventListener('keydown',handleDialogKeydown);
}
function openRefreshJobDetails(){
  const jobID=S.refreshJob?.id||S.activeRefreshJob;
  if(!jobID){toast('没有刷新任务','err');return}
  dialogReturnFocus=document.activeElement instanceof HTMLElement?document.activeElement:null;
  S.refreshDialogJob=jobID;
  S.refreshDialogOpen=true;
  renderRefreshJobDialog(S.refreshJob?.id===jobID?S.refreshJob:null);
}
function rememberRefreshJob(jobID){
  S.activeRefreshJob=jobID;
  localStorage.setItem(ACTIVE_REFRESH_JOB_KEY,jobID);
  S.refreshJob={id:jobID,status:'running',phase:'waiting',total_urls:0,done_urls:0,total_nodes:0,done_nodes:0,started_at:new Date().toISOString()};
  S.refreshPollError='';
  renderRefreshStatusHost();
}
async function pollRefreshSourceJob(jobID){
  if(!jobID||S.refreshPollingJob===jobID)return;
  S.refreshPollingJob=jobID;
  let failures=0,last=null;
  try{
    try{last=await api('/api/import/refresh/jobs/'+encodeURIComponent(jobID))}catch(err){if(err.status===401||err.status===404)throw err}
    while(true){
      try{
        const pushed=last?await waitForJobEvent('refresh',jobID,last.updated_at):null;
        last=pushed||await api('/api/import/refresh/jobs/'+encodeURIComponent(jobID));
        failures=0;
        S.refreshPollError='';
      }catch(err){
        if(err.status===401||err.status===404)throw err;
        failures++;
        S.refreshPollError=`连接中断，正在重试 ${failures}`;
        renderRefreshStatusHost();
        if(S.refreshDialogOpen&&S.refreshDialogJob===jobID)renderRefreshJobDialog(S.refreshJob);
        await new Promise(resolve=>setTimeout(resolve,Math.min(5000,1000*Math.pow(2,Math.min(failures-1,3)))));
        continue;
      }
      S.refreshJob=last;
      renderRefreshStatusHost();
      if(S.refreshDialogOpen&&S.refreshDialogJob===jobID)renderRefreshJobDialog(last);
      if(last.status==='finished'||last.status==='failed'||last.status==='canceled')break;
    }
    S.activeRefreshJob='';
    localStorage.removeItem(ACTIVE_REFRESH_JOB_KEY);
    const canceled=last.status==='canceled';
    const protectedResult=last.phase==='protected'||(!!last.protected&&last.status!=='failed'),partial=last.phase==='partial';
    toast(canceled?'重新检测已取消，原节点池已恢复':last.status==='failed'?'重新检测失败':protectedResult?'检测异常，已恢复原节点池':partial?'重新检测部分完成':'重新检测完成',last.status==='failed'||protectedResult||partial?'err':'');
    await refresh();
  }finally{
    S.refreshPollingJob='';
    if(last&&last.status!=='running')forgetJobEvent('refresh',jobID);
  }
}
async function cancelRefreshSourceJob(){
  const jobID=S.refreshJob?.id||S.activeRefreshJob;
  if(!jobID)return;
  if(!confirm('确认取消当前重新检测任务？\n\n取消后会停止子任务并恢复检测前节点池。'))return;
  try{
    S.refreshJob=await api('/api/import/refresh/jobs/'+encodeURIComponent(jobID),{method:'DELETE'});
    renderRefreshStatusHost();
    if(S.refreshDialogOpen)renderRefreshJobDialog(S.refreshJob);
    toast('正在取消重新检测');
  }catch(err){toast(err.message,'err')}
}
function handleRefreshPollError(err){
  S.activeRefreshJob='';
  S.refreshPollError='';
  localStorage.removeItem(ACTIVE_REFRESH_JOB_KEY);
  if(err.status===404)S.refreshJob=null;
  renderRefreshStatusHost();
  toast(err.status===404?'刷新任务已失效':err.message,'err');
}
async function startRefreshSource(key){
  try{
    const policy=verificationPolicy('refresh');
    if(!verificationPolicyValid(policy))return;
    const started=await api('/api/import/refresh',{method:'POST',body:JSON.stringify({key:key||'',...policy})});
    if(!started.job_id)throw new Error('启动刷新失败');
    rememberRefreshJob(started.job_id);
    openRefreshJobDetails();
    pollRefreshSourceJob(started.job_id).catch(handleRefreshPollError);
  }catch(err){openDialog('刷新失败',`<p>${esc(err.message)}</p>`)}
}
async function refreshAllImportSources(){
  const sources=(S.importSources||[]).filter(s=>s.refreshable);
  if(!sources.length){toast('没有带 Tag 的导入来源','err');return}
  await startRefreshSource('');
}
async function deleteSubscription(idx){
  const urls=S.subConfig?.subscriptions||[];
  const url=urls[idx];
  if(!url){toast('订阅不存在','err');return}
  if(!confirm(`确认删除订阅？\n${url}\n\n所有由该订阅导入的节点也会被删除。`))return;
  try{
    const res=await api('/api/subscription/delete',{method:'POST',body:JSON.stringify({url})});
    toast(`已删除订阅，移除节点 ${res.deleted_nodes||0} 个`);
    S.subConfig=await api('/api/subscription/config');
    renderSettings();
  }catch(err){toast(err.message,'err')}
}
async function deleteImportSource(idx){
  const src=(S.importSources||[])[idx];
  if(!src){toast('导入来源不存在','err');return}
  const label=importSourceText(src);
  if(!confirm(`确认删除导入来源？\n${label}\n\n该来源导入的节点都会被删除。`))return;
  try{
    const res=await api('/api/import/sources',{method:'POST',body:JSON.stringify({key:src.key})});
    if(src.mode==='url'&&src.source)await removeSubscriptionURLs(src.source.split(/\r?\n/).map(x=>x.trim()).filter(Boolean));
    toast(`已删除导入来源，移除节点 ${res.deleted_nodes||0} 个`);
    await Promise.all([loadSubscriptionConfig(),loadImportSources()]);
    renderSettings();
  }catch(err){toast(err.message,'err')}
}
async function refreshImportSource(idx){
  const src=(S.importSources||[])[idx];
  if(!src||!src.refreshable){toast('该来源没有可用的 Tag','err');return}
  await startRefreshSource(src.key);
}
async function removeSubscriptionURLs(urls){
  if(!urls.length)return;
  await loadSubscriptionConfig();
  const remove=new Set(urls);
  const next=(S.subConfig?.subscriptions||[]).filter(u=>!remove.has(u));
  S.subConfig=await api('/api/subscription/config',{method:'PUT',body:JSON.stringify(subscriptionConfigPayload({subscriptions:next,refresh:false}))});
}
async function deleteAllImportSources(){
  const sources=S.importSources||[];
  if(!sources.length){toast('没有可删除的导入来源','err');return}
  const total=sources.reduce((sum,src)=>sum+(Number(src.total)||0),0);
  if(!confirm(`确认删除全部导入？\n\n将删除 ${sources.length} 个导入来源、${total} 个节点，并清空所有订阅链接。此操作不会删除手动配置节点。`))return;
  try{
    const res=await api('/api/import/sources',{method:'POST',body:JSON.stringify({all:true})});
    toast(`已删除全部导入，移除节点 ${res.deleted_nodes||0} 个`);
    await Promise.all([loadSubscriptionConfig(),loadImportSources(),loadImportSummary()]);
    renderNav();
    renderSettings();
  }catch(err){toast(err.message,'err')}
}
