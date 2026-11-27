function refreshPhaseText(job){
  const phase=String(job?.phase||'waiting');
  if(job?.status==='failed'||phase==='failed')return 'Yeniden test başarısız';
  if(job?.status==='canceled'||phase==='canceled')return 'Yeniden test iptal edildi';
  if(job?.protected||phase==='protected')return 'Test hatası; önceki proxy havuzu geri yüklendi';
  if(phase==='partial')return 'Yeniden test kısmen tamamlandı';
  if(job?.status==='finished'||phase==='finished')return 'Yeniden test tamamlandı';
  return {pulling:'Abonelikler alınıyor',retrying:'Başarısız kaynaklar yeniden deneniyor',testing:'Düğümler aşamalı olarak test ediliyor',promoting:'Proxy havuzuna ekleniyor',applying:'Düğümler ve portlar uygulanıyor',verifying:'Port dinleme durumu doğrulanıyor',canceling:'İptal ediliyor',waiting:'Başlaması bekleniyor'}[phase]||'Yenileniyor';
}
function refreshRowStatusText(status){
  return {pulling:'Düğümler alınıyor',retrying:'Son yeniden deneme',retry_waiting:'Yeniden deneme bekleniyor',testing:'Düğümler test ediliyor',promoting:'Proxy havuzuna ekleniyor',applying:'Portlar uygulanıyor',verifying:'Portlar doğrulanıyor',completed:'Tamamlandı',failed:'Başarısız',canceled:'İptal edildi',waiting:'Bekliyor'}[String(status||'waiting')]||'Bekliyor';
}
function refreshElapsed(job){
  const start=new Date(job?.started_at||0).getTime();
  if(!start)return '0 sn';
  const terminal=job?.status==='running'?Date.now():new Date(job?.updated_at||Date.now()).getTime();
  const seconds=Math.max(0,Math.floor((terminal-start)/1000));
  if(seconds<60)return `${seconds} sn`;
  const minutes=Math.floor(seconds/60),remain=seconds%60;
  return `${minutes} dk ${remain} sn`;
}
function maskedSubscriptionURL(raw){
  try{return new URL(String(raw||'')).host||'Abonelik adresi'}catch(e){return 'Abonelik adresi'}
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
  return total?`${Number(job?.done_nodes)||0}/${total}`:'Hazırlanıyor';
}
function probeTargetLabel(target){
  const value=String(target||'');
  if(value.includes('gstatic.com'))return 'Google 204';
  if(value.includes('cloudflare.com'))return 'Cloudflare 204';
  return value?'Yedek test hedefi':'';
}
function refreshStatsHTML(job){
  const terminal=job?.status!=='running';
  const parts=[`<span>Kaynak ${esc(job?.done_urls||0)}/${esc(job?.total_urls||0)}</span>`,`<span>Düğüm ${esc(refreshNodeProgressText(job))}</span>`];
  const probe=activeRefreshProbe(job);
  if(probe)parts.push(`<span>${esc(probe.probe_round||0)}/${esc(probe.probe_rounds||3)}. tur · Bu tur ${esc(probe.probe_round_done||0)}/${esc(probe.probe_round_total||0)} · Kalan ${esc(probe.probe_pending||0)}</span>`);
  const sites=siteProgressSummary(activeRefreshSiteProgress(job));
  if(sites)parts.push(`<span>${esc(sites)}</span>`);
  if(terminal){
    parts.push(`<span>Başarılı / kullanılabilir port ${esc(job?.pool_count||0)}</span>`);
    parts.push(`<span>Hız testini geçen ${esc(job?.probe_passed||0)}</span>`);
  }else{
    parts.push(`<span>Kesin başarılı ${esc(job?.probe_passed||job?.passed||0)}</span>`);
  }
  parts.push(`<span>Son başarısız ${esc(job?.failed_nodes||0)}</span>`);
  parts.push(`<span>Yeni / yeniden oluşturulan ${esc(job?.promoted||0)}</span>`);
  return parts.join('');
}
function refreshStatusHTML(job){
  if(!job)return '';
  const running=job.status==='running';
  const partial=job.phase==='partial',protectedResult=job.phase==='protected'||(!!job.protected&&job.status!=='failed');
  const cls=job.status==='failed'||partial||protectedResult?'failed':running?'':'done';
  const pollError=S.refreshPollError?`<span style="color:#b94a48">${esc(S.refreshPollError)}</span>`:'';
  return `<div class="refresh-status ${cls}">
    <div class="refresh-status-head"><div><strong>${esc(refreshPhaseText(job))}</strong><span class="muted">Geçen süre: ${esc(refreshElapsed(job))}</span></div><span class="pill ${job.status==='failed'||job.status==='canceled'||partial||protectedResult?'red':running?'amber':'green'}">${running?'Arka planda çalışıyor':job.status==='canceled'?'İptal edildi':job.status==='failed'?'Başarısız':protectedResult?'Geri alındı':partial?'Kısmen tamamlandı':'Tamamlandı'}</span></div>
    ${refreshProgressHTML(job)}
    <div class="refresh-stats">${refreshStatsHTML(job)}${pollError}</div>
    ${job.protected?`<div class="action-status err">${esc(job.protection_reason||'Test sonucu olağandışı; bu turun sonuçları uygulanmadı ve önceki proxy havuzu geri yüklendi.')}</div>`:''}
    <div class="refresh-actions">${running?'<button class="btn small danger" onclick="cancelRefreshSourceJob()">Testi iptal et</button>':''}<button class="btn small secondary" onclick="openRefreshJobDetails()">Ayrıntıları gör</button></div>
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
  const detail=probe?`${probe.probe_round||0}/${probe.probe_rounds||3}. tur · ${probe.probe_round_done||0}/${probe.probe_round_total||0}`:(total?`${done}/${total}`:'Hazırlanıyor');
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
    dialog.innerHTML='<div class="dialog-header"><h2 id="refreshDialogTitle"></h2><button class="btn ghost dialog-close" type="button" onclick="closeDialog()" aria-label="Kapat">×</button></div><div class="dialog-body"><div class="stack"><div id="refreshDialogProgress"></div><div class="refresh-stats" id="refreshDialogStats"></div><div id="refreshDialogNotice"></div><div class="refresh-detail-list" id="refreshDialogList"></div></div></div><div class="dialog-footer refresh-actions" id="refreshDialogActions"></div>';
    dialog.dataset.refreshShell='1';
  }
  const titleHost=qs('#refreshDialogTitle'),progressHost=qs('#refreshDialogProgress'),statsHost=qs('#refreshDialogStats'),noticeHost=qs('#refreshDialogNotice'),listHost=qs('#refreshDialogList'),actionsHost=qs('#refreshDialogActions');
  if(!job){
    titleHost.textContent='İçe aktarma kaynaklarını yeniden test et';
    progressHost.innerHTML='';
    statsHost.innerHTML='<span>Görev durumu okunuyor...</span>';
    noticeHost.innerHTML='';
    listHost.innerHTML='';
    actionsHost.innerHTML='<button class="btn" onclick="closeDialog()">Kapat</button>';
    modal.classList.add('show');
    document.addEventListener('keydown',handleDialogKeydown);
    return;
  }
  const groups=Array.isArray(job.groups)?job.groups:[];
  const list=groups.map((g,groupIdx)=>{
    const rows=(Array.isArray(g.urls)?g.urls:[]).map((r,urlIdx)=>{
      const raw=String(r.status||'waiting').toLowerCase();
      const cls=raw==='completed'?'green':raw==='failed'||raw==='canceled'?'red':'amber';
      const counts=`İlerleme ${r.done||0}/${r.total||0} · Başarılı ${r.passed||0} · Başarısız ${r.failed||0} · Havuza eklenen ${r.promoted||0}`;
      const progress=r.detail||((raw==='pulling'||raw==='retrying')?'Düğümler alınıyor':counts);
      const local=r.kind==='content';
      const title=local?(r.label||'Yerel içerik'):`Abonelik ${groupIdx+1}-${urlIdx+1}`;
      const source=local?'Kaydedilmiş düğümler yeniden test ediliyor':maskedSubscriptionURL(r.url);
      const attempt=r.attempt?` · Alma denemesi ${r.attempt}/${r.attempts||2}`:'';
      const probe=r.probe_round?`Düğüm testi ${r.probe_round}/${r.probe_rounds||3}. tur · Bu tur ${r.probe_round_done||0}/${r.probe_round_total||0} · Kalan ${r.probe_pending||0} · ${probeTargetLabel(r.probe_target)} · Eşzamanlı ${r.probe_concurrency||'-'}`:'';
      const sites=siteProgressSummary(r.site_progress);
      const front=r.chain_probe?(r.chain_probe.error?`Ön proxy ${r.chain_probe.profile_name||r.chain_probe.profile_id} başarısız: ${r.chain_probe.error}`:`Ön proxy ${r.chain_probe.profile_name||r.chain_probe.profile_id} · ${r.chain_probe.latency_ms||0} ms`):'';
      return `<div class="source-row"><div style="flex:1;min-width:0"><div class="row"><strong>${esc(title)}</strong><span class="pill ${cls}">${esc(refreshRowStatusText(raw))}</span>${r.cached?'<span class="pill amber">Önbellek düğümü</span>':''}${r.protected?'<span class="pill red">Sonuç uygulanmadı</span>':''}</div><div class="muted">${esc(source+attempt)}</div><div class="muted">${esc(progress)}</div>${front?`<div class="muted">${esc(front)}</div>`:''}${probe?`<div class="muted">${esc(probe)}</div>`:''}${sites?`<div class="muted">${esc(sites)}</div>`:''}${r.total?`<div class="muted">${esc(counts)}</div>`:''}${r.warning?`<div class="muted" style="color:#9a6700">${esc(r.warning)}</div>`:''}${r.error?`<div class="muted" style="color:#b94a48">${esc(r.error)}</div>`:''}</div></div>`;
    }).join('');
    return `<div class="stack" style="gap:10px"><div class="row"><strong>${esc(g.tag_prefix||'Yerel')}</strong><span class="muted">Kaynak ${esc(g.done||0)}/${esc(g.total||0)} · Başarılı ${esc(g.successful||0)} · Başarısız ${esc(g.failed||0)}</span></div>${rows}</div>`;
  }).join('');
  const scrollTop=listHost.scrollTop;
  titleHost.textContent=refreshPhaseText(job);
  progressHost.innerHTML=refreshProgressHTML(job);
  statsHost.innerHTML=`${refreshStatsHTML(job)}<span>Geçen süre: ${esc(refreshElapsed(job))}</span>`;
  noticeHost.innerHTML=`${job.protected&&job.status!=='failed'?`<div class="action-status err">${esc(job.protection_reason||'İşlem hatası; bu turun sonuçları uygulanmadı ve önceki proxy havuzu geri yüklendi.')}</div>`:''}${job.error&&(job.status==='failed'||!job.protected)?`<div style="color:#b94a48">${esc(job.error)}</div>`:''}`;
  listHost.innerHTML=list;
  listHost.scrollTop=scrollTop;
  actionsHost.innerHTML=`${job.status==='running'?'<button class="btn danger" onclick="cancelRefreshSourceJob()">Testi iptal et</button>':''}<button class="btn ${job.status==='running'?'secondary':'primary'}" onclick="closeDialog()">${job.status==='running'?'Küçült':'Kapat'}</button>`;
  modal.classList.add('show');
  document.addEventListener('keydown',handleDialogKeydown);
}
function openRefreshJobDetails(){
  const jobID=S.refreshJob?.id||S.activeRefreshJob;
  if(!jobID){toast('Yenileme görevi bulunamadı','err');return}
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
        S.refreshPollError=`Bağlantı kesildi; yeniden deneniyor (${failures})`;
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
    toast(canceled?'Yeniden test iptal edildi; önceki proxy havuzu geri yüklendi':last.status==='failed'?'Yeniden test başarısız':protectedResult?'Test hatası; önceki proxy havuzu geri yüklendi':partial?'Yeniden test kısmen tamamlandı':'Yeniden test tamamlandı',last.status==='failed'||protectedResult||partial?'err':'');
    await refresh();
  }finally{
    S.refreshPollingJob='';
    if(last&&last.status!=='running')forgetJobEvent('refresh',jobID);
  }
}
async function cancelRefreshSourceJob(){
  const jobID=S.refreshJob?.id||S.activeRefreshJob;
  if(!jobID)return;
  if(!confirm('Geçerli yenileme görevi iptal edilsin mi?\n\nİptal işlemi alt görevleri durdurur ve test öncesindeki proxy havuzunu geri yükler.'))return;
  try{
    S.refreshJob=await api('/api/import/refresh/jobs/'+encodeURIComponent(jobID),{method:'DELETE'});
    renderRefreshStatusHost();
    if(S.refreshDialogOpen)renderRefreshJobDialog(S.refreshJob);
    toast('Yeniden test iptal ediliyor');
  }catch(err){toast(err.message,'err')}
}
function handleRefreshPollError(err){
  S.activeRefreshJob='';
  S.refreshPollError='';
  localStorage.removeItem(ACTIVE_REFRESH_JOB_KEY);
  if(err.status===404)S.refreshJob=null;
  renderRefreshStatusHost();
  toast(err.status===404?'Yenileme görevi artık geçerli değil':err.message,'err');
}
async function startRefreshSource(key){
  try{
    const policy=verificationPolicy('refresh');
    if(!verificationPolicyValid(policy))return;
    const started=await api('/api/import/refresh',{method:'POST',body:JSON.stringify({key:key||'',...policy})});
    if(!started.job_id)throw new Error('Yenileme başlatılamadı');
    rememberRefreshJob(started.job_id);
    openRefreshJobDetails();
    pollRefreshSourceJob(started.job_id).catch(handleRefreshPollError);
  }catch(err){openDialog('Yenileme başarısız',`<p>${esc(err.message)}</p>`)}
}
async function refreshAllImportSources(){
  const sources=(S.importSources||[]).filter(s=>s.refreshable);
  if(!sources.length){toast('Etiketli içe aktarma kaynağı yok','err');return}
  await startRefreshSource('');
}
async function deleteSubscription(idx){
  const urls=S.subConfig?.subscriptions||[];
  const url=urls[idx];
  if(!url){toast('Abonelik bulunamadı','err');return}
  if(!confirm(`Abonelik silinsin mi?\n${url}\n\nBu abonelikten içe aktarılan tüm düğümler de silinecek.`))return;
  try{
    const res=await api('/api/subscription/delete',{method:'POST',body:JSON.stringify({url})});
    toast(`Abonelik silindi; ${res.deleted_nodes||0} düğüm kaldırıldı`);
    S.subConfig=await api('/api/subscription/config');
    renderSettings();
  }catch(err){toast(err.message,'err')}
}
async function deleteImportSource(idx){
  const src=(S.importSources||[])[idx];
  if(!src){toast('İçe aktarma kaynağı bulunamadı','err');return}
  const label=importSourceText(src);
  if(!confirm(`İçe aktarma kaynağı silinsin mi?\n${label}\n\nBu kaynaktan içe aktarılan tüm düğümler de silinecek.`))return;
  try{
    const res=await api('/api/import/sources',{method:'POST',body:JSON.stringify({key:src.key})});
    if(src.mode==='url'&&src.source)await removeSubscriptionURLs(src.source.split(/\r?\n/).map(x=>x.trim()).filter(Boolean));
    toast(`İçe aktarma kaynağı silindi; ${res.deleted_nodes||0} düğüm kaldırıldı`);
    await Promise.all([loadSubscriptionConfig(),loadImportSources()]);
    renderSettings();
  }catch(err){toast(err.message,'err')}
}
async function refreshImportSource(idx){
  const src=(S.importSources||[])[idx];
  if(!src||!src.refreshable){toast('Bu kaynakta kullanılabilir etiket yok','err');return}
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
  if(!sources.length){toast('Silinecek içe aktarma kaynağı yok','err');return}
  const total=sources.reduce((sum,src)=>sum+(Number(src.total)||0),0);
  if(!confirm(`Tüm içe aktarmalar silinsin mi?\n\n${sources.length} içe aktarma kaynağı ve ${total} düğüm silinecek; tüm abonelik bağlantıları temizlenecek. Manuel yapılandırılmış düğümler silinmez.`))return;
  try{
    const res=await api('/api/import/sources',{method:'POST',body:JSON.stringify({all:true})});
    toast(`Tüm içe aktarmalar silindi; ${res.deleted_nodes||0} düğüm kaldırıldı`);
    await Promise.all([loadSubscriptionConfig(),loadImportSources(),loadImportSummary()]);
    renderNav();
    renderSettings();
  }catch(err){toast(err.message,'err')}
}
