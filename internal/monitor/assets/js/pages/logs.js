function renderLogs(){
  qs('#view').innerHTML=`<div class="card log-card"><div class="card-head"><h2>Çalışma günlükleri</h2><div class="row"><button class="btn small" onclick="refresh()">${icon('refresh','sm')}Yenile</button><button class="btn small danger" onclick="clearLogs()">${icon('trash','sm')}Günlükleri temizle</button></div></div><div class="card-body"><p class="action-status ${S.logActionType==='err'?'err':''}" style="display:${S.logAction?'block':'none'}">${esc(S.logAction||'')}</p><pre class="log-view">${esc(S.logs||'Henüz günlük yok')}</pre></div></div>`;
}
async function clearLogs(){
  if(!confirm('Günlükleri temizlemek istediğinizden emin misiniz?'))return;
  try{
    await api('/api/logs/clear',{method:'POST'});
    S.logs='';
    S.logAction='Günlükler temizlendi';
    S.logActionType='ok';
    await loadLogs();
    renderLogs();
  }catch(err){
    S.logAction=err.message;
    S.logActionType='err';
    toast(err.message,'err');
    renderLogs();
  }
}
