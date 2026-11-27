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
  const text=abs<60?`${abs} sn`:abs<3600?`${Math.round(abs/60)} dk`:abs<86400?`${Math.round(abs/3600)} sa`:`${Math.round(abs/86400)} gün`;
  return diff>=0?`${text} sonra`:`${text} önce`;
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
    <div class="strip-item"><span class="status-dot ${running?'ok':inPool?'bad':'warn'}"></span><div><span>Proxy çekirdeği</span><strong class="${running?'ok':'bad'}">${running?'Çalışıyor':'Çalışmıyor'}</strong></div></div>
    <div class="strip-item"><div><span>Çalışma modu</span><strong>${esc(S.settings?.mode||runtime.mode||'-')}</strong></div></div>
    <div class="strip-item"><div><span>Proxy giriş adresi</span><strong class="link">${esc(dashboardEntry())}</strong></div></div>
    <div class="strip-item"><div><span>Yönetim adresi</span><strong>${esc(location.host)}</strong></div></div>
    <div class="strip-actions">
      <div class="clock"><strong id="dashClock"></strong><span id="dashWeekday"></span></div>
    <button class="btn primary" type="button" onclick="go('import')">${icon('plus')}Düğüm içe aktar</button>
    </div>
  </div>
  <div class="kpi-grid">
    ${kpi('pool','green','server','Havuzdaki portlar',inPool,`Tüm düğümlerin ${percent(inPool,total)} kadarı`)}
    ${kpi('nodes','blue','list','Aday düğümler',passed,passed?'Proxy havuzuna eklenebilir':'Bekleyen aday yok')}
    ${kpi('failed','red','alert','Başarısız düğümler',failed,`Tüm düğümlerin ${percent(failed,total)} kadarı`)}
    ${kpi('','amber','timer','Test ediliyor',testing,testing?'Hız testi sürüyor veya bekliyor':'Etkin test görevi yok')}
    ${kpi('','violet','layers','Toplam düğüm',total,excluded?`${excluded} düğüm hariç tutuldu`:`${(S.importSources||[]).length} içe aktarma kaynağı`)}
  </div>
  <div class="dash-grid">
    <div class="card"><div class="card-head"><h2 class="panel-title">Düğüm dağılımı</h2></div><div class="card-body">${compositionHTML([{label:'Havuzda',value:inPool,color:'#22c55e'},{label:'Aday',value:passed,color:'#3b82f6'},{label:'Başarısız',value:failed,color:'#ef4444'},{label:'Test ediliyor',value:testing,color:'#f59e0b'},{label:'Hariç',value:excluded,color:'#94a3b8'}],total)}</div></div>
    <div class="card"><div class="card-head"><h2 class="panel-title">Abonelik yenileme</h2>${(S.importSources||[]).some(source=>source.refreshable)?`<button class="btn small secondary" type="button" onclick="refreshAllImportSources()" ${S.activeRefreshJob?'disabled':''}>${icon('refresh','sm')}Tümünü yenile</button>`:''}</div><div class="card-body">${subscriptionPanelHTML()}</div></div>
    <div class="card"><div class="card-head"><h2 class="panel-title">Çalışma tanılama</h2></div><div class="card-body"><div class="kv">
      ${kvRow('Giriş / çıkış',runtime.running?`${esc(runtime.inbounds??0)} / ${esc(runtime.outbounds??0)}`:'-')}
      ${kvRow('Goroutine sayısı',esc(goStats.goroutines??'-'))}
      ${kvRow('Heap belleği',goStats.heap_alloc_bytes!=null?esc(formatHeap(goStats.heap_alloc_bytes)):'-')}
      ${kvRow('GC sayısı',esc(goStats.gc_count??'-'))}
      ${kvRow('Başlatma süresi',runtime.startup_ms?.total!=null?`${esc(runtime.startup_ms.total)} ms`:'-')}
      ${kvRow('İlerleme akışı',S.jobStreamConnected?'Bağlı':'Bağlı değil',S.jobStreamConnected?'ok':'warn')}
    </div></div></div>
  </div>
  <div class="dash-grid two">
    <div class="card"><div class="card-head"><h2 class="panel-title">İçe aktarma kaynakları</h2><button class="btn small ghost" type="button" onclick="go('settings')">Kaynakları yönet</button></div><div class="card-body">${sourcesTableHTML()}</div></div>
    <div class="card"><div class="card-head"><h2 class="panel-title">Görev hareketleri</h2></div><div class="card-body" id="dashJobs"></div></div>
  </div>`;
  renderDashboardJobs();
  startDashboardClock();
}
function kvRow(label,value,cls=''){return `<div class="kv-row"><span>${label}</span><strong class="${cls}">${value}</strong></div>`}
function compositionHTML(items,total){
  if(!total)return '<div class="empty"><strong>Henüz düğüm yok</strong><span>Bir abonelik veya düğüm içe aktardığınızda havuz, aday ve başarısız düğümlerin dağılımı burada görünür.</span><button class="btn small primary" type="button" onclick="go(\'import\')">İçe aktarmaya git</button></div>';
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
    ${kvRow('Otomatik yenileme',enabled?`Etkin · Her ${esc(formatInterval(sub.interval))}`:'Devre dışı',enabled?'ok':'')}
    ${kvRow('Abonelik / yenilenebilir kaynak',`${esc(urls)} / ${esc(sources)}`)}
    ${kvRow('Son yenileme',last?`${esc(last.toLocaleString('tr-TR'))}`:'Henüz yenilenmedi')}
    ${kvRow('Sonraki yenileme',enabled&&next?esc(relativeTime(next)):'Planlanmadı')}
    ${kvRow('Etkin görev',job?esc(refreshPhaseText(job)):status.is_refreshing?'Yenileniyor':'Boşta',job?.status==='running'||status.is_refreshing?'warn':'')}
    ${status.last_error?kvRow('Son hata',`<span title="${esc(status.last_error)}">${esc(status.last_error)}</span>`,'bad'):''}
  </div>`;
}
function formatInterval(value){
  const {days,hours,minutes}=durationParts(value||'24h');
  return [days?`${days} gün`:'',hours?`${hours} saat`:'',minutes?`${minutes} dakika`:''].filter(Boolean).join(' ')||'24 saat';
}
function sourcesTableHTML(){
  const sources=(S.importSources||[]).slice().sort((a,b)=>(Number(b.total)||0)-(Number(a.total)||0));
  if(!sources.length)return '<div class="empty"><strong>Henüz içe aktarma kaynağı yok</strong><span>Abonelik bağlantısı veya düğüm içeriği eklediğinizde burada listelenir.</span></div>';
  const rows=sources.slice(0,8).map(src=>`<tr><td class="tag-cell" title="${esc(src.tag_prefix||'local')}">${esc(src.tag_prefix||'local')}</td><td>${esc(importSourceType(src))}</td><td class="num">${esc(src.pool||0)}</td><td class="num">${esc(src.candidate||0)}</td><td class="num">${esc(src.failed||0)}</td><td class="num">${esc(src.total||0)}</td><td>${esc(relativeTime(src.updated_at))}</td></tr>`).join('');
  return `<div class="mini-table-wrap"><table class="mini-table"><thead><tr><th>Etiket</th><th>Tür</th><th class="num">Havuz</th><th class="num">Aday</th><th class="num">Başarısız</th><th class="num">Toplam</th><th>Güncelleme</th></tr></thead><tbody>${rows}</tbody></table></div>${sources.length>8?`<p class="muted" style="margin-top:10px">${sources.length-8} kaynak daha var; tümünü Ayarlar bölümünde görebilirsiniz.</p>`:''}`;
}
function jobEventSummary(kind,job){
  if(kind==='refresh')return {title:'Abonelik yenileme',detail:`${refreshPhaseText(job)} · Kaynak ${job.done_urls||0}/${job.total_urls||0} · Düğüm ${job.done_nodes||0}/${job.total_nodes||0}`,icon:'refresh'};
  if(kind==='connectivity')return {title:'Site bağlantı testi',detail:`${connectivityPhaseText(job)} · ${job.done_checks||0}/${job.total_checks||0}`,icon:'globe'};
  return {title:'Düğüm hız testi',detail:`İlerleme ${job.done||0}/${job.total||0} · Başarılı ${job.passed||0} · Başarısız ${job.failed||0}`,icon:'activity'};
}
function renderDashboardJobs(){
  const host=qs('#dashJobs');if(!host)return;
  const jobs=[...S.jobEvents.entries()].reverse().slice(0,6);
  if(!jobs.length){host.innerHTML='<div class="empty"><strong>Henüz görev yok</strong><span>Hız testi, abonelik yenileme ve site bağlantı testlerinin ilerlemesi burada canlı gösterilir.</span></div>';return}
  host.innerHTML=`<div class="job-feed">${jobs.map(([key,job])=>{
    const kind=key.split(':')[0],info=jobEventSummary(kind,job),status=job.status||'running';
    const tone=status==='running'?'amber':status==='finished'||status==='completed'?'green':status==='canceled'?'':'red';
  const label={running:'Devam ediyor',finished:'Tamamlandı',completed:'Tamamlandı',failed:'Başarısız',canceled:'Durduruldu'}[status]||status;
    return `<div class="job-item"><span class="kpi-icon">${icon(info.icon,'sm')}</span><div><strong>${info.title}</strong><span title="${esc(info.detail)}">${esc(info.detail)}</span></div><span class="pill ${tone}">${esc(label)}</span></div>`;
  }).join('')}</div>`;
}
function tickDashboardClock(){
  const clock=qs('#dashClock'),weekday=qs('#dashWeekday');if(!clock){stopDashboardClock();return}
  const now=new Date();
  clock.textContent=now.toLocaleString('tr-TR',{hour12:false});
  weekday.textContent=now.toLocaleDateString('tr-TR',{weekday:'long'});
}
function startDashboardClock(){stopDashboardClock();tickDashboardClock();S.dashClock=setInterval(tickDashboardClock,1000)}
function stopDashboardClock(){if(S.dashClock){clearInterval(S.dashClock);S.dashClock=null}}
