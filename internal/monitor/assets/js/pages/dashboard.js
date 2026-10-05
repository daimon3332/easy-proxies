async function loadDashboard(request){
  const [diagnostics,subStatus]=await Promise.all([
    api('/api/diagnostics',{signal:request?.signal}).catch(err=>{if(isAbortError(err))throw err;return null}),
    api('/api/subscription/status',{signal:request?.signal}).catch(err=>{if(isAbortError(err))throw err;return null}),
    loadImportSummary(request),
    loadImportSources(request),
    loadSubscriptionConfig(request),
    loadSettings(request).catch(err=>{if(isAbortError(err))throw err})
  ]);
  if(!isCurrentPageRequest(request))return;
  S.diagnostics=diagnostics;S.subStatus=subStatus;S.shellStatusAt=Date.now();
}
function validTime(value){const d=new Date(value||0);return d.getFullYear()>2000?d:null}
function relativeTime(value){
  const d=validTime(value);if(!d)return '-';
  const diff=Math.round((d.getTime()-Date.now())/1000),abs=Math.abs(diff);
  const text=abs<60?`${abs} 秒`:abs<3600?`${Math.round(abs/60)} 分钟`:abs<86400?`${Math.round(abs/3600)} 小时`:`${Math.round(abs/86400)} 天`;
  return diff>=0?`${text}后`:`${text}前`;
}
function percent(part,total){const value=total?part*100/total:0;return `${value&&value<10?value.toFixed(1):Math.round(value)}%`}
function dashboardEntry(){
  const settings=S.settings||{},runtime=S.diagnostics?.runtime||{};
  const mode=settings.mode||runtime.mode||'';
  if(mode==='multi-port'||mode==='hybrid'){
    const multi=settings.multi_port||{},count=Number(runtime.listeners)||Number(S.summary?.in_pool)||0;
    const base=Number(multi.base_port)||0;
    return base?`${multi.address||'0.0.0.0'}:${base}${count>1?`–${base+count-1}`:''}`:'-';
  }
  const listener=settings.listener||{};
  return listener.port?`${listener.address||'0.0.0.0'}:${listener.port}`:'-';
}
function renderDashboard(){
  const summary=S.summary||{},runtime=S.diagnostics?.runtime||{},goStats=S.diagnostics?.go||{};
  const total=Number(summary.total)||0,inPool=Number(summary.in_pool)||0,passed=Number(summary.passed)||0,failed=Number(summary.failed)||0;
  const testing=(Number(summary.testing)||0)+(Number(summary.parsed)||0),excluded=Number(summary.excluded)||0;
  const running=!!runtime.running;
  const kpi=(page,tone,iconName,label,value,foot)=>`<${page?`button type="button" onclick="go('${page}')"`:'div'} class="kpi ${tone}"><div class="kpi-head"><span class="kpi-icon">${icon(iconName)}</span>${label}</div><div class="kpi-value">${esc(value)}</div><div class="kpi-foot">${esc(foot)}</div></${page?'button':'div'}>`;
  qs('#view').innerHTML=`
  <div class="card dash-strip">
    <div class="strip-item"><span class="status-dot ${running?'ok':inPool?'bad':'warn'}"></span><div><span>代理核心</span><strong class="${running?'ok':'bad'}">${running?'运行中':'未运行'}</strong></div></div>
    <div class="strip-item"><div><span>运行模式</span><strong>${esc(S.settings?.mode||runtime.mode||'-')}</strong></div></div>
    <div class="strip-item"><div><span>代理入口</span><strong class="link">${esc(dashboardEntry())}</strong></div></div>
    <div class="strip-item"><div><span>管理地址</span><strong>${esc(location.host)}</strong></div></div>
    <div class="strip-actions">
      <div class="clock"><strong id="dashClock"></strong><span id="dashWeekday"></span></div>
      <button class="btn primary" type="button" onclick="go('import')">${icon('plus')}导入节点</button>
    </div>
  </div>
  <div class="kpi-grid">
    ${kpi('pool','green','server','池内端口',inPool,`占全部节点 ${percent(inPool,total)}`)}
    ${kpi('nodes','blue','list','候选节点',passed,passed?'可加入节点池':'暂无待入池节点')}
    ${kpi('failed','red','alert','失败节点',failed,`占全部节点 ${percent(failed,total)}`)}
    ${kpi('','amber','timer','检测中',testing,testing?'正在测速或等待检测':'当前没有检测任务')}
    ${kpi('','violet','layers','节点总数',total,excluded?`已排除 ${excluded}`:`导入来源 ${(S.importSources||[]).length} 个`)}
  </div>
  <div class="dash-grid">
    <div class="card"><div class="card-head"><h2 class="panel-title">节点构成</h2></div><div class="card-body">${compositionHTML([{label:'池内端口',value:inPool,color:'#22c55e'},{label:'候选节点',value:passed,color:'#3b82f6'},{label:'失败节点',value:failed,color:'#ef4444'},{label:'检测中',value:testing,color:'#f59e0b'},{label:'已排除',value:excluded,color:'#94a3b8'}],total)}</div></div>
    <div class="card"><div class="card-head"><h2 class="panel-title">订阅刷新</h2>${(S.importSources||[]).some(source=>source.refreshable)?`<button class="btn small secondary" type="button" onclick="refreshAllImportSources()" ${S.activeRefreshJob?'disabled':''}>${icon('refresh','sm')}重新检测全部</button>`:''}</div><div class="card-body">${subscriptionPanelHTML()}</div></div>
    <div class="card"><div class="card-head"><h2 class="panel-title">运行诊断</h2></div><div class="card-body"><div class="kv">
      ${kvRow('入站 / 出站',runtime.running?`${esc(runtime.inbounds??0)} / ${esc(runtime.outbounds??0)}`:'-')}
      ${kvRow('协程数',esc(goStats.goroutines??'-'))}
      ${kvRow('堆内存',goStats.heap_alloc_bytes!=null?esc(formatHeap(goStats.heap_alloc_bytes)):'-')}
      ${kvRow('GC 次数',esc(goStats.gc_count??'-'))}
      ${kvRow('启动耗时',runtime.startup_ms?.total!=null?`${esc(runtime.startup_ms.total)} ms`:'-')}
      ${kvRow('进度推送',S.jobStreamConnected?'已连接':'未连接',S.jobStreamConnected?'ok':'warn')}
    </div></div></div>
  </div>
  <div class="dash-grid two">
    <div class="card"><div class="card-head"><h2 class="panel-title">导入来源</h2><button class="btn small ghost" type="button" onclick="go('settings')">管理来源</button></div><div class="card-body">${sourcesTableHTML()}</div></div>
    <div class="card"><div class="card-head"><h2 class="panel-title">任务动态</h2></div><div class="card-body" id="dashJobs"></div></div>
  </div>`;
  renderDashboardJobs();
  startDashboardClock();
}
function kvRow(label,value,cls=''){return `<div class="kv-row"><span>${label}</span><strong class="${cls}">${value}</strong></div>`}
function compositionHTML(items,total){
  if(!total)return '<div class="empty"><strong>还没有节点</strong><span>导入订阅或节点后，这里会显示池内、候选和失败的比例。</span><button class="btn small primary" type="button" onclick="go(\'import\')">去导入</button></div>';
  const visible=items.filter(item=>item.value>0);
  return `<div class="composition-bar" role="img" aria-label="${esc(visible.map(item=>`${item.label} ${item.value}`).join('，'))}">${visible.map(item=>`<span style="flex-grow:${item.value};background:${item.color}" title="${esc(item.label)} ${esc(item.value)}"></span>`).join('')}</div>
  <div class="legend">${items.map(item=>`<div class="legend-row"><i style="background:${item.color}"></i><span>${item.label}</span><strong>${esc(item.value)}</strong><em>${percent(item.value,total)}</em></div>`).join('')}</div>`;
}
function subscriptionPanelHTML(){
  const sub=S.subConfig||{},status=S.subStatus||{};
  const urls=(sub.subscriptions||[]).length,sources=(S.importSources||[]).filter(source=>source.refreshable).length;
  const enabled=sub.enabled!==false&&urls>0;
  const next=validTime(status.next_refresh),last=validTime(status.last_refresh);
  const job=S.refreshJob;
  return `<div class="kv">
    ${kvRow('自动刷新',enabled?`已启用 · 每 ${esc(formatInterval(sub.interval))}`:'未启用',enabled?'ok':'')}
    ${kvRow('订阅链接 / 可刷新来源',`${esc(urls)} / ${esc(sources)}`)}
    ${kvRow('上次刷新',last?`${esc(last.toLocaleString())}`:'尚未刷新')}
    ${kvRow('下次刷新',enabled&&next?esc(relativeTime(next)):'未计划')}
    ${kvRow('当前任务',job?esc(refreshPhaseText(job)):status.is_refreshing?'刷新中':'空闲',job?.status==='running'||status.is_refreshing?'warn':'')}
    ${status.last_error?kvRow('最近错误',`<span title="${esc(status.last_error)}">${esc(status.last_error)}</span>`,'bad'):''}
  </div>`;
}
function formatInterval(value){
  const {days,hours,minutes}=durationParts(value||'24h');
  return [days?`${days} 天`:'',hours?`${hours} 小时`:'',minutes?`${minutes} 分钟`:''].filter(Boolean).join(' ')||'24 小时';
}
function sourcesTableHTML(){
  const sources=(S.importSources||[]).slice().sort((a,b)=>(Number(b.total)||0)-(Number(a.total)||0));
  if(!sources.length)return '<div class="empty"><strong>还没有导入来源</strong><span>通过订阅链接或节点内容导入后会出现在这里。</span></div>';
  const rows=sources.slice(0,8).map(src=>`<tr><td class="tag-cell" title="${esc(src.tag_prefix||'local')}">${esc(src.tag_prefix||'local')}</td><td>${esc(importSourceType(src))}</td><td class="num">${esc(src.pool||0)}</td><td class="num">${esc(src.candidate||0)}</td><td class="num">${esc(src.failed||0)}</td><td class="num">${esc(src.total||0)}</td><td>${esc(relativeTime(src.updated_at))}</td></tr>`).join('');
  return `<div class="mini-table-wrap"><table class="mini-table"><thead><tr><th>Tag</th><th>类型</th><th class="num">池内</th><th class="num">候选</th><th class="num">失败</th><th class="num">总数</th><th>更新</th></tr></thead><tbody>${rows}</tbody></table></div>${sources.length>8?`<p class="muted" style="margin-top:10px">另有 ${sources.length-8} 个来源，可在设置中查看。</p>`:''}`;
}
function jobEventSummary(kind,job){
  if(kind==='refresh')return {title:'订阅重新检测',detail:`${refreshPhaseText(job)} · 来源 ${job.done_urls||0}/${job.total_urls||0} · 节点 ${job.done_nodes||0}/${job.total_nodes||0}`,icon:'refresh'};
  if(kind==='connectivity')return {title:'站点检测',detail:`${connectivityPhaseText(job)} · ${job.done_checks||0}/${job.total_checks||0}`,icon:'globe'};
  return {title:'节点测速',detail:`进度 ${job.done||0}/${job.total||0} · 成功 ${job.passed||0} · 失败 ${job.failed||0}`,icon:'activity'};
}
function renderDashboardJobs(){
  const host=qs('#dashJobs');if(!host)return;
  const jobs=[...S.jobEvents.entries()].reverse().slice(0,6);
  if(!jobs.length){host.innerHTML='<div class="empty"><strong>暂无任务</strong><span>测速、订阅重新检测和站点检测的进度会实时显示在这里。</span></div>';return}
  host.innerHTML=`<div class="job-feed">${jobs.map(([key,job])=>{
    const kind=key.split(':')[0],info=jobEventSummary(kind,job),status=job.status||'running';
    const tone=status==='running'?'amber':status==='finished'||status==='completed'?'green':status==='canceled'?'':'red';
    const label={running:'进行中',finished:'已完成',completed:'已完成',failed:'失败',canceled:'已终止'}[status]||status;
    return `<div class="job-item"><span class="kpi-icon">${icon(info.icon,'sm')}</span><div><strong>${info.title}</strong><span title="${esc(info.detail)}">${esc(info.detail)}</span></div><span class="pill ${tone}">${esc(label)}</span></div>`;
  }).join('')}</div>`;
}
function tickDashboardClock(){
  const clock=qs('#dashClock'),weekday=qs('#dashWeekday');if(!clock){stopDashboardClock();return}
  const now=new Date();
  clock.textContent=now.toLocaleString('zh-CN',{hour12:false});
  weekday.textContent=now.toLocaleDateString('zh-CN',{weekday:'long'});
}
function startDashboardClock(){stopDashboardClock();tickDashboardClock();S.dashClock=setInterval(tickDashboardClock,1000)}
function stopDashboardClock(){if(S.dashClock){clearInterval(S.dashClock);S.dashClock=null}}
