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
    <div class="metric"><span>Tarama aralığı</span><strong>${esc(basePort)}–${esc(endPort)}</strong></div>
    <div class="metric"><span>Havuz tarafından kullanılan</span><strong class="good">${esc(occupied.length)}</strong></div>
    <div class="metric"><span>Çakışma</span><strong class="${conflicts.length?'bad':''}">${esc(conflicts.length)}</strong></div>
    <div class="metric"><span>Boşta</span><strong>${esc(free.length)}</strong></div>
  </div>
  <div class="card"><div class="card-head"><h2>Port durumu</h2><button class="btn small secondary" onclick="refreshPorts()">${icon('refresh','sm')}Yeniden tara</button></div><div class="card-body stack">
    ${skipped.length?`<p class="action-status err">Port atanırken başka işlemler tarafından kullanılan ${esc(skipped.length)} port atlandı: ${esc(skipped.join(' / '))}</p>`:''}
    <div class="section"><h3>Proxy havuzunun dinlediği portlar</h3><div class="table port-list-limited">${visibleOccupied.length?visibleOccupied.map(p=>`<div class="port"><b>${p.port}</b><span class="muted">${esc(p.node_name||'')}</span></div>`).join(''):'<div class="empty">Proxy havuzu boş</div>'}</div>${occupied.length>visibleOccupied.length?`<p class="muted">İlk ${visibleOccupied.length} port gösteriliyor. Tüm düğümler için “Kullanılabilir portlar” sayfasına bakın.</p>`:''}</div>
    ${conflicts.length?`<div class="section"><h3>Çakışan portlar</h3><div class="table">${conflicts.map(p=>`<div class="port"><b>${p.port}</b><span class="pill red">${esc(reasonText(p.reason))}</span><span class="muted">${esc(reasonDetail(p.reason))}</span></div>`).join('')}</div></div>`:''}
  </div></div>`;
  maybeNotifySkippedPorts(skipped, S.ports&&S.ports.skipped_at);
}
function maybeNotifySkippedPorts(list, at){
  if(!list||!list.length||!at)return;
  if(S.lastSkipSeen===at)return;
  S.lastSkipSeen=at;
  openDialog('Port başka bir işlem tarafından kullanılıyor',`<p>Port ataması sırasında aşağıdaki portların başka işlemler tarafından kullanıldığı görüldü. Bu portlar otomatik olarak atlandı ve sonraki kullanılabilir portlar seçildi:</p><p><b>${esc(list.join(' / '))}</b></p><p class="muted">Kesintisiz port aralığı istiyorsanız bu portları serbest bırakın veya Ayarlar bölümünde multi_port.base_port değerini değiştirin.</p>`);
}
async function refreshPorts(){
  try{
    await loadPorts();
    renderPorts();
    const ports=(S.ports?.ports)||[];
    const occupied=ports.filter(p=>p.reason==='used_by_pool').length;
    const conflicts=ports.filter(p=>!p.available && p.reason!=='used_by_pool').length;
    const free=ports.filter(p=>p.available).length;
    openDialog('Port taraması tamamlandı',`<p>Proxy havuzunun kullandığı: ${esc(occupied)} · Diğer işlemlerin kullandığı: ${esc(conflicts)} · Boşta: ${esc(free)}</p>`);
  }catch(err){
    openDialog('Port taraması başarısız',`<p>${esc(err.message)}</p>`);
  }
}
function reasonText(r){return {used_by_pool:'Proxy havuzu kullanıyor',used_by_config:'Yapılandırma kullanıyor',listener_conflict:'Giriş portuyla çakışıyor',occupied_by_os:'Sistem kullanıyor'}[r]||r}
function reasonDetail(r){return {used_by_pool:'Havuzdaki bir düğüme atanmış',used_by_config:'Havuz veya yapılandırma tarafından kullanılıyor',listener_conflict:'Dinleme giriş portuyla çakışıyor',occupied_by_os:'İşletim sistemi veya başka bir işlem kullanıyor'}[r]||r}
function bestPortRun(ports){
  let best=null,cur=null;
  for(const p of ports){
    if(p.available){if(!cur)cur={start:p.port,end:p.port,len:1};else{cur.end=p.port;cur.len++}}
    else{if(cur&&(!best||cur.len>best.len))best=cur;cur=null}
  }
  if(cur&&(!best||cur.len>best.len))best=cur;
  return best;
}
