async function loadConnectivityScopes(request){
  const data=await api('/api/connectivity/scopes',{signal:request?.signal});
  if(!isCurrentPageRequest(request))return null;
  S.connectivityScopes=data;
  const available=new Set((data.tags||[]).map(item=>item.tag));
  for(const tag of [...S.connectivitySelectedTags])if(!available.has(tag))S.connectivitySelectedTags.delete(tag);
  if(!S.connectivityTagsInitialized){
    for(const tag of available)S.connectivitySelectedTags.add(tag);
    S.connectivityTagsInitialized=true;
  }
  return data;
}

function connectivityTargetName(id){return {google:'Google',github:'GitHub',outlook:'Outlook',proxyspace:'ProxySpace'}[id]||id}
function verificationPolicyHTML(scope,test204,targets,disabled=false){
  const selected=targets instanceof Set?targets:new Set(targets||[]);
  const count=selected.size+(test204?1:0);
  const probeButton=`<button type="button" class="site-selector ${test204?'selected':''}" aria-pressed="${test204?'true':'false'}" ${disabled?'disabled':''} onclick="setVerification204('${scope}',${test204?'false':'true'})"><strong>204 hız testi</strong><span>${test204?'Başarılı olmalı':'Kontrol etme'}</span></button>`;
  const siteButtons=VERIFICATION_TARGETS.map(target=>`<button type="button" class="site-selector ${selected.has(target.id)?'selected':''}" aria-pressed="${selected.has(target.id)?'true':'false'}" ${disabled?'disabled':''} onclick="toggleVerificationTarget('${scope}','${target.id}')"><strong>${target.name}</strong><span>${selected.has(target.id)?'Başarılı olmalı':'Kontrol etme'}</span></button>`).join('');
  return `<div class="verification-policy"><div class="verification-policy-head"><div><strong>Düğüm kullanılabilirlik koşulları</strong><span>Seçilen tüm kontroller başarılı olmalı</span></div><span class="pill ${count?'green':'red'}">${count?`${count} kontrol seçildi`:'Seçim yapılmadı'}</span></div><div class="verification-targets">${probeButton}${siteButtons}</div></div>`;
}
function verificationPolicy(scope){
  if(scope==='import')return {test_204:!!S.importTest204,site_targets:[...S.importSiteTargets]};
  return {test_204:S.subConfig?.test_204!==false,site_targets:Array.isArray(S.subConfig?.site_targets)?[...S.subConfig.site_targets]:[]};
}
function verificationPolicyValid(policy,showError=true){
  const valid=!!policy.test_204||(policy.site_targets||[]).length>0;
  if(!valid&&showError)openDialog('Bir test yöntemi seçin','<p>204 hız testi ve site kontrolleri aynı anda kapatılamaz. Devam etmek için en az birini etkinleştirin.</p>');
  return valid;
}
function setVerification204(scope,enabled){
  if(scope==='import'){
    S.importTest204=!!enabled;
    localStorage.setItem(IMPORT_TEST_204_KEY,S.importTest204?'1':'0');
    renderImport();
    return;
  }
  S.subConfig={...(S.subConfig||{}),test_204:!!enabled};
  renderSettings();
}
function toggleVerificationTarget(scope,targetID){
  if(!VERIFICATION_TARGETS.some(target=>target.id===targetID))return;
  if(scope==='import'){
    if(S.importSiteTargets.has(targetID))S.importSiteTargets.delete(targetID);else S.importSiteTargets.add(targetID);
    localStorage.setItem(IMPORT_SITE_TARGETS_KEY,JSON.stringify([...S.importSiteTargets]));
    renderImport();
    return;
  }
  const selected=new Set(S.subConfig?.site_targets||[]);
  if(selected.has(targetID))selected.delete(targetID);else selected.add(targetID);
  S.subConfig={...(S.subConfig||{}),site_targets:[...selected]};
  renderSettings();
}
function subscriptionConfigPayload(overrides={}){
  return {enabled:S.subConfig?.enabled!==false,interval:S.subConfig?.interval||'24h',subscriptions:S.subConfig?.subscriptions||[],...verificationPolicy('refresh'),...overrides};
}
function setConnectivityTimeout(value){
  const parsed=parseConnectivityTimeout(value);
  S.connectivityTimeoutInput=String(value??'');
  S.connectivityTimeoutValid=!parsed.error;
  if(!parsed.error){
    S.connectivityTimeoutSeconds=parsed.seconds;
    localStorage.setItem(CONNECTIVITY_TIMEOUT_KEY,String(parsed.seconds));
  }
  const input=qs('#connectivityTimeout');
  if(input){input.setAttribute('aria-invalid',parsed.error?'true':'false');input.setCustomValidity(parsed.error)}
  const error=qs('#connectivityTimeoutError');if(error)error.textContent=parsed.error;
  const start=qs('#connectivityStartButton');if(start)start.disabled=!!parsed.error||!(S.connectivityScopes?.tags||[]).length;
  return !parsed.error;
}
function connectivityPhaseText(job){
  return {first_pass:'İlk kontrol turu',retry_wait:'Yeniden deneme bekleniyor',retry:'Başarısız kontroller yeniden deneniyor',saving_results:'Sonuçlar kaydediliyor',canceling:'Durduruluyor',finished:'Kontrol tamamlandı',failed:'Kontrol başarısız',canceled:'Durduruldu'}[job?.phase]||'Kontrol bekleniyor';
}
function connectivityStatusText(status){return {running:'Kontrol ediliyor',finished:'Tamamlandı',failed:'Başarısız',canceled:'Durduruldu'}[status]||'Bekliyor'}
function renderConnectivity(){
  const scopes=S.connectivityScopes||{tags:[],targets:[]};
  const tagRows=(scopes.tags||[]).map((item,index)=>`<label class="tag-option"><input type="checkbox" ${S.connectivitySelectedTags.has(item.tag)?'checked':''} onchange="toggleConnectivityTagAt(${index},this.checked)"><span title="${esc(item.tag)}">${esc(item.tag)}</span><small class="muted">${esc(item.nodes)} düğüm</small></label>`).join('');
  const running=S.connectivityJob?.status==='running';
  const timeoutValue=running?(Number(S.connectivityJob?.timeout_seconds)||CONNECTIVITY_DEFAULT_TIMEOUT_SECONDS):S.connectivityTimeoutInput;
  const sites=(scopes.targets||[]).map(target=>`<button class="site-selector ${S.connectivityTestTargets.has(target.id)?'selected':''}" ${running?'disabled':''} onclick="toggleConnectivityTestTarget('${esc(target.id)}')" title="${esc(target.url||'')}"><strong>${esc(target.name)}</strong><span>${S.connectivityTestTargets.has(target.id)?'Bu turda test et':'Test etme'}</span></button>`).join('');
  qs('#view').innerHTML=`<div class="connectivity-layout">
    <section class="settings-section"><div class="settings-section-head connectivity-scope-head"><h2>Test kapsamı</h2><div class="row"><button class="btn small secondary" onclick="setAllConnectivityTags(true)">Tümünü seç</button><button class="btn small ghost" onclick="setAllConnectivityTags(false)">Seçimi temizle</button></div></div><div class="settings-section-body"><div class="connectivity-scope-summary"><span>Seçilen <strong id="connectivityTagCount">${S.connectivitySelectedTags.size}</strong> / ${esc((scopes.tags||[]).length)} etiket</span><span>Toplam ${esc((scopes.tags||[]).reduce((sum,item)=>sum+(Number(item.nodes)||0),0))} düğüm</span></div><div class="tag-picker">${tagRows||'<div class="connectivity-idle" style="grid-column:1/-1"><span>Etiketli düğüm bulunamadı</span></div>'}</div><div class="connectivity-scope-footer"><div class="connectivity-test-controls"><div class="site-strip">${sites}</div><div class="connectivity-timeout-field"><label for="connectivityTimeout">Tek deneme zaman aşımı</label><div class="number-suffix"><input id="connectivityTimeout" type="number" min="1" max="60" step="1" value="${esc(timeoutValue)}" ${running?'disabled':''} aria-describedby="connectivityTimeoutError" aria-invalid="${S.connectivityTimeoutValid?'false':'true'}" oninput="setConnectivityTimeout(this.value)"><span>sn</span></div><small id="connectivityTimeoutError" class="field-error" aria-live="polite">${S.connectivityTimeoutValid?'':'1–60 arasında tam sayı girin'}</small></div></div><div id="connectivityActions" class="row">${connectivityActionsHTML()}</div></div></div></section>
    <section id="connectivityProgressHost"></section>
    <section id="connectivityCriteria"></section>
    <section id="connectivityMatrixHost"></section>
    <section id="connectivityHistoryHost"></section>
    <section id="connectivityResults"></section>
  </div>`;
  updateConnectivityJobView(S.connectivityJob);
}

function connectivityActionsHTML(){
  const job=S.connectivityJob,running=job?.status==='running';
  if(running)return job.phase==='canceling'?'<button class="btn danger" disabled>Durduruluyor</button>':'<button class="btn danger" onclick="cancelConnectivityJob()">Testi durdur</button>';
  const disabled=!(S.connectivityScopes?.tags||[]).length||!S.connectivityTimeoutValid?'disabled':'';
  return `<button class="btn primary" id="connectivityStartButton" ${disabled} onclick="startConnectivityJob()">Testi başlat</button>`;
}

function toggleConnectivityTagAt(index,checked){
  const tag=S.connectivityScopes?.tags?.[index]?.tag;if(typeof tag!=='string')return;
  if(checked)S.connectivitySelectedTags.add(tag);else S.connectivitySelectedTags.delete(tag);
  const count=qs('#connectivityTagCount');if(count)count.textContent=S.connectivitySelectedTags.size;
}
function setAllConnectivityTags(selected){
  S.connectivitySelectedTags.clear();
  if(selected)for(const item of S.connectivityScopes?.tags||[])S.connectivitySelectedTags.add(item.tag);
  renderConnectivity();
}

function toggleConnectivityTestTarget(id){
  if(S.connectivityTestTargets.has(id)){
    if(S.connectivityTestTargets.size===1){toast('En az bir test sitesi seçili kalmalı','err');return}
    S.connectivityTestTargets.delete(id);
  }else S.connectivityTestTargets.add(id);
  renderConnectivity();
}

async function startConnectivityJob(){
  const tags=[...S.connectivitySelectedTags];
  const targets=[...S.connectivityTestTargets];
  if(!tags.length){toast('En az bir etiket seçin','err');return}
  if(!targets.length){toast('En az bir test sitesi seçin','err');return}
  const timeoutInput=qs('#connectivityTimeout');
  if(!setConnectivityTimeout(timeoutInput?.value??S.connectivityTimeoutInput)){timeoutInput?.focus();return}
  try{
    const timeout_seconds=S.connectivityTimeoutSeconds;
    const started=await api('/api/connectivity/jobs/start',{method:'POST',body:JSON.stringify({tags,targets,timeout_seconds})});
    S.connectivityJobID=started.job_id;S.connectivityJob={id:started.job_id,status:'running',phase:'first_pass',tags,targets,timeout_seconds,total_routes:0,total_checks:0,done_checks:0,retry_checks:0,retry_done:0,recovered:0};S.connectivityResultPage=null;S.connectivityHistory=null;S.connectivitySelectedTargets=new Set(targets);
    localStorage.setItem(CONNECTIVITY_JOB_KEY,started.job_id);
    renderConnectivity();
    await pollConnectivityJob(started.job_id,false);
  }catch(err){toast(err.message,'err')}
}

async function pollConnectivityJob(jobID,silent=false){
  if(!jobID||S.connectivityPollingJob===jobID)return;
  S.connectivityPollingJob=jobID;
  let last=null,failures=0;
  try{
    last=await api('/api/connectivity/jobs/status?id='+encodeURIComponent(jobID));
    if(last.targets?.length){
      S.connectivityTestTargets=new Set(last.targets);S.connectivitySelectedTargets=new Set(last.targets);
      const timeout=Number(last.timeout_seconds)||CONNECTIVITY_DEFAULT_TIMEOUT_SECONDS;
      S.connectivityTimeoutInput=String(timeout);S.connectivityTimeoutSeconds=timeout;S.connectivityTimeoutValid=true;localStorage.setItem(CONNECTIVITY_TIMEOUT_KEY,String(timeout));
      S.connectivityJob=last;if(S.page==='connectivity')renderConnectivity()
    }
    while(true){
      S.connectivityJob=last;updateConnectivityJobView(last);
      if(last.status!=='running')break;
      try{
        const pushed=await waitForJobEvent('connectivity',jobID,last.updated_at,30000);
        last=pushed||await api('/api/connectivity/jobs/status?id='+encodeURIComponent(jobID));
        failures=0;
      }catch(err){
        if(err.status===401||err.status===404)throw err;
        failures++;await new Promise(resolve=>setTimeout(resolve,Math.min(5000,500*Math.pow(2,Math.min(failures,3)))));
        last=await api('/api/connectivity/jobs/status?id='+encodeURIComponent(jobID));
      }
    }
    S.connectivityJob=last;updateConnectivityJobView(last);
    if(last.status==='finished'){
      await Promise.all([loadConnectivityResults(),loadConnectivityHistory()]);
      if(!silent)toast('Site bağlantı testi tamamlandı');
    }else if(!silent){toast(last.status==='canceled'?'Site bağlantı testi durduruldu':last.error||'Site bağlantı testi başarısız','err')}
  }finally{
    S.connectivityPollingJob='';
    if(last&&last.status!=='running')forgetJobEvent('connectivity',jobID);
  }
}

function handleConnectivityPollError(err){
  S.connectivityPollingJob='';
  if(err.status===404){S.connectivityJobID='';localStorage.removeItem(CONNECTIVITY_JOB_KEY)}
  toast(err.status===404?'Site testi görevi artık geçerli değil':err.message,'err');
}

async function cancelConnectivityJob(){
  const id=S.connectivityJob?.id||S.connectivityJobID;if(!id)return;
  if(!confirm('Geçerli site testini durdurmak istiyor musunuz?\n\nTamamlanmış sonuçlar korunur ancak port oluşturmak için kullanılamaz.'))return;
  try{S.connectivityJob=await api('/api/connectivity/jobs/cancel?id='+encodeURIComponent(id),{method:'POST'});updateConnectivityJobView(S.connectivityJob);toast('Test durduruluyor')}catch(err){toast(err.message,'err')}
}

function connectivityProgressHTML(job){
  if(!job)return '<div class="connectivity-idle"><span>Henüz site bağlantı testi çalıştırılmadı</span></div>';
  const timeout=Number(job.timeout_seconds)||CONNECTIVITY_DEFAULT_TIMEOUT_SECONDS;
  if(job.status!=='running')return `<div class="connectivity-progress"><div class="connectivity-progress-head"><div><strong>${esc(connectivityPhaseText(job))}</strong><div class="muted">Test edilen rota: ${esc(job.total_routes||0)} · ${(job.targets||[]).map(connectivityTargetName).map(esc).join(' / ')} · Tek deneme zaman aşımı: ${esc(timeout)} sn</div></div><span class="pill ${job.status==='finished'?'green':'red'}">${esc(connectivityStatusText(job.status))}</span></div>${job.error?`<div class="action-status err">${esc(job.error)}</div>`:''}${job.history_error?`<div class="action-status err">${esc(job.history_error)}</div>`:''}</div>`;
  const retry=job.phase==='retry'||job.phase==='retry_wait';
  const done=retry?(job.retry_done||0):(job.done_checks||0),total=retry?(job.retry_checks||0):(job.total_checks||0);
  const pct=total?Math.min(100,Math.round(done*100/total)):0;
  return `<div class="connectivity-progress"><div class="connectivity-progress-head"><div><strong>${esc(connectivityPhaseText(job))}</strong><div class="muted">Rota ${esc(job.total_routes||0)} · Siteler ${(job.targets||[]).map(connectivityTargetName).map(esc).join(' / ')} · Zaman aşımı ${esc(timeout)} sn · İlk tur ${esc(job.done_checks||0)}/${esc(job.total_checks||0)} · Yeniden deneme ${esc(job.retry_done||0)}/${esc(job.retry_checks||0)}</div></div><span class="pill amber">${esc(connectivityStatusText(job.status))}</span></div><div class="progress"><div style="width:${pct}%"></div></div>${job.error?`<div class="action-status err">${esc(job.error)}</div>`:''}</div>`;
}

function connectivityMatrixHTML(job){
  if(!job||!(job.summaries||[]).length)return '';
  const targets=job.targets||[];
  const columns=`grid-template-columns:minmax(150px,1fr) repeat(${targets.length},minmax(150px,1fr))`;
  const head=`<div class="connectivity-matrix-row connectivity-matrix-head" style="${columns}"><div>Tag</div>${targets.map(id=>`<div>${esc(connectivityTargetName(id))}</div>`).join('')}</div>`;
  const rows=(job.summaries||[]).map(summary=>{
    const byTarget=Object.fromEntries((summary.targets||[]).map(item=>[item.target_id,item]));
    const cells=targets.map(id=>{
      const item=byTarget[id]||{},total=Number(item.total)||0,passed=Number(item.passed)||0,partial=Number(item.partial)||0,rate=total?Math.round(passed*1000/total)/10:0;
      const cls=total&&passed===total?'good':partial?'partial':total&&passed===0?'bad':'';
      return `<div class="connectivity-cell ${cls}"><strong>${passed}/${total} · ${rate}%</strong><span>Kısmen kullanılabilir ${partial} · Başarısız ${item.failed||0}</span><span>İlk tur ${item.first_passed||0} · Kurtarılan ${item.recovered||0} · Medyan ${item.median_latency_ms?esc(item.median_latency_ms)+' ms':'-'}</span></div>`;
    }).join('');
    return `<div class="connectivity-matrix-row" style="${columns}"><div><strong>${esc(summary.tag)}</strong><div class="muted">${esc(summary.routes||0)} rota</div></div>${cells}</div>`;
  }).join('');
  return `<section class="settings-section"><details><summary class="section-summary">Başarı oranı matrisi</summary><div class="settings-section-body"><div class="connectivity-matrix">${head}${rows}</div></div></details></section>`;
}

function updateConnectivityJobView(job){
  if(S.page!=='connectivity')return;
  const progress=qs('#connectivityProgressHost'),matrix=qs('#connectivityMatrixHost'),actions=qs('#connectivityActions');
  if(progress)progress.innerHTML=connectivityProgressHTML(job);
  if(matrix)matrix.innerHTML=connectivityMatrixHTML(job);
  if(actions)actions.innerHTML=connectivityActionsHTML();
  const running=job?.status==='running';qsa('.site-selector').forEach(button=>button.disabled=running);const timeout=qs('#connectivityTimeout');if(timeout)timeout.disabled=running;
  renderConnectivityHistory();
  renderConnectivityCriteria();
  renderConnectivityResults();
}

async function loadConnectivityHistory(){
  const jobID=S.connectivityJob?.id||S.connectivityJobID;if(!jobID)return;
  S.connectivityHistory=await api('/api/connectivity/history?id='+encodeURIComponent(jobID));
  renderConnectivityHistory();
}

function connectivityVerdictText(value){return {usable:'Başarılı',partial:'Kısmen kullanılabilir',failed:'Başarısız',missing:'Kayıt yok'}[value]||value||'-'}
function renderConnectivityHistory(){
  const host=qs('#connectivityHistoryHost');if(!host)return;
  const job=S.connectivityJob,history=S.connectivityHistory;
  if(!job||job.status!=='finished'){host.innerHTML='';return}
  if(!history){host.innerHTML='<div class="connectivity-idle"><span>Geçmiş karşılaştırma yükleniyor...</span></div>';return}
  if(!history.available){host.innerHTML='<div class="connectivity-idle"><span>Aynı kapsamda geçmiş sonuç yok. Bu tur, sonraki karşılaştırmalar için temel olarak kaydedildi.</span></div>';return}
  const c=history.overall||{};
  const metric=(label,value,cls='')=>`<div class="metric"><span>${label}</span><strong class="${cls}">${esc(value||0)}</strong></div>`;
  const targets=(history.targets||[]).map(item=>`<span class="pill">${esc(connectivityTargetName(item.target_id))}: Başarılı kalan ${esc(item.continued_success||0)} / Yeni başarılı ${esc(item.newly_successful||0)} / Yeni başarısız ${esc(item.newly_failed||0)}</span>`).join('');
  const changes=(history.changes||[]).map(item=>`<div class="connectivity-change-row"><div><strong>${esc(item.node_name||item.route_fingerprint)}</strong><div class="muted">${(item.tags||[]).map(esc).join(' · ')}</div></div><span>${esc(connectivityVerdictText(item.previous))} → ${esc(connectivityVerdictText(item.current))}</span><span class="pill ${item.current==='usable'?'green':item.current==='partial'?'amber':item.current==='missing'?'':'red'}">${esc(connectivityVerdictText(item.current))}</span></div>`).join('');
  host.innerHTML=`<section class="settings-section"><details><summary class="section-summary">Önceki aynı kapsamlı testle karşılaştırma<span>Yeni başarılı ${esc(c.newly_successful||0)} · Yeni başarısız ${esc(c.newly_failed||0)}</span></summary><div class="settings-section-body"><div class="connectivity-history-grid">${metric('Başarılı kalan',c.continued_success,'')}${metric('Yeni başarılı',c.newly_successful,'good')}${metric('Yeni başarısız',c.newly_failed,'')}${metric('Başarısız kalan',c.continued_unsuccessful,'')}${metric('Yeni, geçmişi olmayan',c.no_history,'')}${metric('Kapsamdan çıkarılan',c.removed,'')}</div><div class="site-strip">${targets}</div>${changes?`<details><summary>Durumu değişen düğümleri gör (${esc((history.changes||[]).length)})</summary><div class="connectivity-change-list">${changes}</div></details>`:'<div class="muted">Başarılı, kısmen kullanılabilir veya başarısız durumları arasında değişen düğüm yok.</div>'}</div></details></section>`;
}

function toggleConnectivityTarget(id){
  if(S.connectivitySelectedTargets.has(id)){if(S.connectivitySelectedTargets.size===1){toast('En az bir site koşulu seçili kalmalı','err');return}S.connectivitySelectedTargets.delete(id)}else S.connectivitySelectedTargets.add(id);
  renderConnectivityCriteria();refreshConnectivityPreviewSummary();
}

function renderConnectivityCriteria(){
  const host=qs('#connectivityCriteria');if(!host)return;
  const job=S.connectivityJob;
  if(!job||job.status!=='finished'){host.innerHTML='';return}
  const buttons=(job.targets||[]).map(id=>`<button class="site-selector ${S.connectivitySelectedTargets.has(id)?'selected':''}" onclick="toggleConnectivityTarget('${id}')"><strong>${connectivityTargetName(id)}</strong><span>${S.connectivitySelectedTargets.has(id)?'Başarılı olmalı':'Filtreye katma'}</span></button>`).join('');
  host.innerHTML=`<section class="settings-section"><div class="settings-section-head"><h2>Test sonucu ve portları uygula</h2><button class="btn secondary" onclick="openConnectivityPortPreview()">Önizle ve uygula</button></div><div class="settings-section-body"><div class="site-strip">${buttons}</div><div id="connectivityPreviewSummary" class="settings-highlight"><span class="muted">Test sonuçları hesaplanıyor...</span></div></div></section>`;
  refreshConnectivityPreviewSummary();
}

function connectivityPortRequest(allowEmpty=false){return {job_id:S.connectivityJob?.id||'',tags:S.connectivityJob?.tags||[],targets:[...S.connectivitySelectedTargets],allow_empty:allowEmpty}}
async function refreshConnectivityPreviewSummary(){
  const host=qs('#connectivityPreviewSummary');if(!host||S.connectivityJob?.status!=='finished')return;
  const request=connectivityPortRequest();
  try{
    const preview=await api('/api/connectivity/ports/preview',{method:'POST',body:JSON.stringify(request)});
    if(!qs('#connectivityPreviewSummary'))return;
    const total=Number(preview.qualifying||0)+Number(preview.non_qualifying||0);
    const metric=(label,value,cls='')=>`<div class="metric"><span>${label}</span><strong class="${cls}">${esc(value||0)}</strong></div>`;
    host.innerHTML=`<div class="result-metrics">${metric('Test edilen toplam',total)}${metric('Başarılı',preview.qualifying,'good')}${metric('Başarısız',preview.non_qualifying,'bad')}${metric('Mevcut port',preview.current_pool)}${metric('Uygulama sonrası port',preview.projected_pool)}</div><span class="muted">Uygulandığında: ${esc(preview.added||0)} yeni port oluşturulacak, ${esc(preview.removed||0)} port durdurulacak ve ${esc(preview.will_fail||0)} düğüm başarısız listesine taşınacak${preview.shared_retained?`; ${esc(preview.shared_retained)} ortak etiketli düğüm korunacak`:''}.</span><span class="pill amber">Henüz uygulanmadı</span>`;
  }catch(err){host.innerHTML=`<span style="color:var(--red)">${esc(err.message)}</span>`}
}

async function openConnectivityPortPreview(){
  try{
    const request=connectivityPortRequest(),preview=await api('/api/connectivity/ports/preview',{method:'POST',body:JSON.stringify(request)});
    S.connectivityPreview=preview;S.connectivityPreviewRequest={...request,preview_token:preview.preview_token};
    const metric=(label,value,cls='')=>`<div class="metric"><span>${label}</span><strong class="${cls}">${esc(value)}</strong></div>`;
    const empty=preview.empty_blocked?'<div class="action-status err">Koşulları karşılayan düğüm yok. Onaylarsanız seçilen etiketlerin ortak olmayan portları temizlenecek.</div>':'';
    const list=(title,items,total)=>!(items||[]).length?'':`<details><summary>${title} (toplam ${esc(total||items.length)}, ilk ${esc(Math.min(items.length,10))} gösteriliyor)</summary><div class="connectivity-change-list">${items.slice(0,10).map(item=>`<div class="connectivity-change-row"><div><strong>${esc(item.node_name||'-')}</strong><div class="muted">${(item.tags||[]).map(esc).join(' · ')}</div></div><span>${item.port?`Mevcut port ${esc(item.port)}`:'Henüz port yok'}</span>${statePill(item.state)}</div>`).join('')}</div></details>`;
    const warning=preview.removed?`<div class="action-status err">Uygulama sonrasında mevcut ${esc(preview.removed)} port durdurulacak. Önce ayrıntıları kontrol edin.</div>`:'';
    const actions=`<button class="btn" type="button" onclick="closeDialog()">Vazgeç</button><button id="connectivityApplyButton" class="btn ${preview.removed||preview.empty_blocked?'danger':'primary'}" type="button" onclick="applyConnectivityPorts(${preview.empty_blocked?'true':'false'})">${preview.empty_blocked?'Seçilen etiketleri temizlemeyi onayla':'Uygulamayı onayla'}</button>`;
    openDialog('Port değişikliklerini önizle',`<div class="port-preview-grid">${metric('Test edilen toplam',(preview.qualifying||0)+(preview.non_qualifying||0))}${metric('Başarılı',preview.qualifying,'good')}${metric('Başarısız',preview.non_qualifying,'bad')}${metric('Mevcut port',preview.current_pool)}${metric('Uygulama sonrası port',preview.projected_pool)}${metric('Başarısız listesine',preview.will_fail)}</div><div class="settings-highlight"><strong>Port havuzu henüz değiştirilmedi</strong><span class="muted">Yalnızca onaylarsanız uygulanır. İptal etmek veya pencereyi kapatmak mevcut portları değiştirmez.</span></div>${warning}${empty}${list('Yeni port örnekleri',preview.added_items,preview.added)}${list('Durdurulacak port örnekleri',preview.removed_items,preview.removed)}`,{wide:true,actions});
  }catch(err){toast(err.message,'err')}
}

async function applyConnectivityPorts(allowEmpty){
  const button=qs('#connectivityApplyButton'),oldText=button?.textContent;
  if(button){button.disabled=true;button.textContent='Uygulanıyor...'}
  try{
    const request={...(S.connectivityPreviewRequest||connectivityPortRequest()),allow_empty:!!allowEmpty};
    const result=await api('/api/connectivity/ports/apply',{method:'POST',body:JSON.stringify(request)});
    closeDialog();toast(result.build_failed?`Portlar güncellendi; havuzda ${result.pool_count||0} düğüm var. ${result.build_failed} düğüm için çalışma yapılandırması oluşturulamadı ve başarısız listesine taşındı.`:`Portlar güncellendi; havuzda ${result.pool_count||0} düğüm var.`,result.build_failed?'err':'ok');
    S.connectivityPreview=null;S.connectivityPreviewRequest=null;
    await loadImportSummary();renderNav();refreshConnectivityPreviewSummary();
  }catch(err){toast(err.message,'err');if(button){button.disabled=false;button.textContent=oldText}}
}

async function loadConnectivityResults(){
  const jobID=S.connectivityJob?.id||S.connectivityJobID;if(!jobID)return;
  const f=S.connectivityResultFilters,params=new URLSearchParams({id:jobID,page:String(f.page||1),page_size:'100'});
  if(f.tag)params.set('tag',f.tag);if(f.target)params.set('target',f.target);if(f.status)params.set('status',f.status);
  S.connectivityResultPage=await api('/api/connectivity/results?'+params.toString());
  renderConnectivityResults();
}

function renderConnectivityResults(){
  const host=qs('#connectivityResults');if(!host)return;
  const job=S.connectivityJob,page=S.connectivityResultPage;
  if(!job||job.status!=='finished'){host.innerHTML='';return}
  const tags=(job.tags||[]).map(tag=>`<option value="${esc(tag)}" ${S.connectivityResultFilters.tag===tag?'selected':''}>${esc(tag)}</option>`).join('');
  const targetOptions=(job.targets||[]).map(id=>`<option value="${id}" ${S.connectivityResultFilters.target===id?'selected':''}>${connectivityTargetName(id)}</option>`).join('');
  const rows=(page?.items||[]).map(item=>{
    const verdict=item.verdict||(item.success?'usable':'failed');
    const badge=verdict==='usable'?'<span class="pill green">Tamamen kullanılabilir</span>':verdict==='partial'?'<span class="pill amber">Kısmen kullanılabilir</span>':'<span class="pill red">Başarısız</span>';
    const http=[item.http_status||'',item.final_host||''].filter(Boolean).join(' · ')||item.failure_stage||'-';
    const components=(item.components||[]).map(component=>`${component.name||component.id}: ${component.success?'Başarılı':component.error||'Başarısız'}`).join('; ');
    const detail=components||item.error||'-';
    return `<div class="connectivity-detail-row"><div><strong>${esc(item.node_name||item.node_id)}</strong><div class="muted">${(item.tags||[]).map(tag=>esc(tag)).join(' · ')}</div></div><div>${esc(connectivityTargetName(item.target_id))}</div><div>${badge}</div><div>${item.latency_ms?esc(item.latency_ms)+' ms':'-'}</div><div title="${esc(item.content_type||'')}">${esc(http)}</div><div class="muted" title="${esc(detail)}">${esc(item.error||components||'-')}</div></div>`;
  }).join('');
  const total=page?.total||0,current=page?.page||1,pages=Math.max(1,Math.ceil(total/(page?.page_size||100)));
  host.innerHTML=`<section class="settings-section"><div class="settings-section-head"><h2>Düğüm ayrıntıları<span class="section-count"> · ${esc(total)} sonuç</span></h2><button class="btn small secondary" onclick="toggleConnectivityDetails()">${S.connectivityDetailsOpen?'Ayrıntıları gizle':'Ayrıntıları göster'}</button></div>${S.connectivityDetailsOpen?`<div class="settings-section-body"><div class="row"><select style="width:170px" onchange="setConnectivityResultFilter('tag',this.value)"><option value="">Tüm etiketler</option>${tags}</select><select style="width:150px" onchange="setConnectivityResultFilter('target',this.value)"><option value="">Tüm siteler</option>${targetOptions}</select><select style="width:140px" onchange="setConnectivityResultFilter('status',this.value)"><option value="">Tüm sonuçlar</option><option value="success" ${S.connectivityResultFilters.status==='success'?'selected':''}>Tamamen kullanılabilir</option><option value="partial" ${S.connectivityResultFilters.status==='partial'?'selected':''}>Kısmen kullanılabilir</option><option value="failed" ${S.connectivityResultFilters.status==='failed'?'selected':''}>Başarısız</option></select></div><div class="connectivity-table"><div class="connectivity-detail-row connectivity-detail-head"><div>Düğüm / Etiket</div><div>Site</div><div>Sonuç</div><div>Süre</div><div>HTTP / Aşama</div><div>Sayfa kontrolü</div></div>${rows||'<div class="empty">Filtrelerle eşleşen sonuç yok</div>'}</div><div class="row" style="justify-content:flex-end"><button class="btn small" ${current<=1?'disabled':''} onclick="changeConnectivityResultPage(-1)">Önceki</button><span class="muted">${current} / ${pages}</span><button class="btn small" ${current>=pages?'disabled':''} onclick="changeConnectivityResultPage(1)">Sonraki</button></div></div>`:''}</section>`;
}
function toggleConnectivityDetails(){S.connectivityDetailsOpen=!S.connectivityDetailsOpen;renderConnectivityResults()}

async function setConnectivityResultFilter(name,value){S.connectivityResultFilters[name]=value;S.connectivityResultFilters.page=1;await loadConnectivityResults()}
async function changeConnectivityResultPage(delta){S.connectivityResultFilters.page=Math.max(1,(S.connectivityResultFilters.page||1)+delta);await loadConnectivityResults()}
