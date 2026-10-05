function renderLogs(){
  qs('#view').innerHTML=`<div class="card log-card"><div class="card-head"><h2>运行日志</h2><div class="row"><button class="btn small" onclick="refresh()">${icon('refresh','sm')}刷新</button><button class="btn small danger" onclick="clearLogs()">${icon('trash','sm')}清空日志</button></div></div><div class="card-body"><p class="action-status ${S.logActionType==='err'?'err':''}" style="display:${S.logAction?'block':'none'}">${esc(S.logAction||'')}</p><pre class="log-view">${esc(S.logs||'暂无日志')}</pre></div></div>`;
}
async function clearLogs(){
  if(!confirm('确定清空日志吗？'))return;
  try{
    await api('/api/logs/clear',{method:'POST'});
    S.logs='';
    S.logAction='日志已清空';
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
