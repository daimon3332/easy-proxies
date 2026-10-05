function renderPorts(){
  const ports=(S.ports&&S.ports.ports)||[];
  const occupied=ports.filter(p=>p.reason==='used_by_pool');
  const conflicts=ports.filter(p=>!p.available && p.reason!=='used_by_pool');
  const free=ports.filter(p=>p.available);
  const basePort=(S.ports&&S.ports.base_port)||0;
  const endPort=ports.length?ports[ports.length-1].port:basePort;
  const skipped=(S.ports&&S.ports.skipped_ports)||[];
  const visibleOccupied=occupied.slice(0,80);
  qs('#view').innerHTML=`<div class="port-stats">
    <div class="metric"><span>扫描范围</span><strong>${esc(basePort)}–${esc(endPort)}</strong></div>
    <div class="metric"><span>节点池占用</span><strong class="good">${esc(occupied.length)}</strong></div>
    <div class="metric"><span>冲突</span><strong class="${conflicts.length?'bad':''}">${esc(conflicts.length)}</strong></div>
    <div class="metric"><span>空闲</span><strong>${esc(free.length)}</strong></div>
  </div>
  <div class="card"><div class="card-head"><h2>端口状态</h2><button class="btn small secondary" onclick="refreshPorts()">${icon('refresh','sm')}重新扫描</button></div><div class="card-body stack">
    ${skipped.length?`<p class="action-status err">分配端口时跳过了被外部进程占用的 ${esc(skipped.length)} 个端口：${esc(skipped.join(' / '))}</p>`:''}
    <div class="section"><h3>节点池监听端口</h3><div class="table port-list-limited">${visibleOccupied.length?visibleOccupied.map(p=>`<div class="port"><b>${p.port}</b><span class="muted">${esc(p.node_name||'')}</span></div>`).join(''):'<div class="empty">节点池为空</div>'}</div>${occupied.length>visibleOccupied.length?`<p class="muted">仅显示前 ${visibleOccupied.length} 个，完整节点请在“可用端口”页面查看。</p>`:''}</div>
    ${conflicts.length?`<div class="section"><h3>冲突端口</h3><div class="table">${conflicts.map(p=>`<div class="port"><b>${p.port}</b><span class="pill red">${esc(reasonText(p.reason))}</span><span class="muted">${esc(reasonDetail(p.reason))}</span></div>`).join('')}</div></div>`:''}
  </div></div>`;
  maybeNotifySkippedPorts(skipped, S.ports&&S.ports.skipped_at);
}
function maybeNotifySkippedPorts(list, at){
  if(!list||!list.length||!at)return;
  if(S.lastSkipSeen===at)return;
  S.lastSkipSeen=at;
  openDialog('端口被外部占用',`<p>分配端口时检测到以下端口被其他进程占用，已自动跳过并使用后续可用端口：</p><p><b>${esc(list.join(' / '))}</b></p><p class="muted">如果你希望保持端口连续，请释放这些端口或在「设置」里调整 multi_port.base_port。</p>`);
}
async function refreshPorts(){
  try{
    await loadPorts();
    renderPorts();
    const ports=(S.ports?.ports)||[];
    const occupied=ports.filter(p=>p.reason==='used_by_pool').length;
    const conflicts=ports.filter(p=>!p.available && p.reason!=='used_by_pool').length;
    const free=ports.filter(p=>p.available).length;
    openDialog('端口扫描完成',`<p>节点池占用 ${esc(occupied)} 个，其他进程占用 ${esc(conflicts)} 个，空闲 ${esc(free)} 个。</p>`);
  }catch(err){
    openDialog('端口扫描失败',`<p>${esc(err.message)}</p>`);
  }
}
function reasonText(r){return {used_by_pool:'节点池占用',used_by_config:'配置占用',listener_conflict:'入口端口冲突',occupied_by_os:'系统占用'}[r]||r}
function reasonDetail(r){return {used_by_pool:'已分配给池内节点',used_by_config:'已被节点池/配置占用',listener_conflict:'与入口监听端口冲突',occupied_by_os:'被系统或其他进程占用'}[r]||r}
function bestPortRun(ports){
  let best=null,cur=null;
  for(const p of ports){
    if(p.available){if(!cur)cur={start:p.port,end:p.port,len:1};else{cur.end=p.port;cur.len++}}
    else{if(cur&&(!best||cur.len>best.len))best=cur;cur=null}
  }
  if(cur&&(!best||cur.len>best.len))best=cur;
  return best;
}
