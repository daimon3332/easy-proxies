function restoreActiveJobs(){
  const importId=localStorage.getItem(ACTIVE_IMPORT_JOB_KEY);
  if(importId && !S.importPollingJob){
    S.activeImportJob=importId;
    if(S.page==='import' && !S.importSummary){
      S.importSummary={status:'running',title:'恢复导入任务',detail:'正在读取后端进度'};
      renderImport();
    }
    pollJob(importId).catch(err=>{
      S.activeImportJob='';
      localStorage.removeItem(ACTIVE_IMPORT_JOB_KEY);
      toast(err.message,'err');
    });
  }
  const batchId=localStorage.getItem(ACTIVE_BATCH_JOB_KEY);
  if(batchId && !S.batchPollingJob){
    S.activeBatchJob=batchId;
    pollBatchJob(batchId,'恢复测速进度').catch(err=>{
      S.activeBatchJob='';
      localStorage.removeItem(ACTIVE_BATCH_JOB_KEY);
      toast(err.message,'err');
    });
  }
  const refreshId=localStorage.getItem(ACTIVE_REFRESH_JOB_KEY);
  if(refreshId && !S.refreshPollingJob){
    S.activeRefreshJob=refreshId;
    if(!S.refreshJob||S.refreshJob.id!==refreshId){
      S.refreshJob={id:refreshId,status:'running',phase:'waiting',total_urls:0,done_urls:0,total_nodes:0,done_nodes:0,started_at:new Date().toISOString()};
    }
    renderRefreshStatusHost();
    pollRefreshSourceJob(refreshId).catch(handleRefreshPollError);
  }
  const bindingId=localStorage.getItem(ACTIVE_TAG_BINDING_JOB_KEY);
  if(bindingId&&!S.tagBindingPollingJob){
    S.activeTagBindingJob=bindingId;
    pollTagBindingJob(bindingId,S.page==='settings').catch(err=>{
      S.activeTagBindingJob='';localStorage.removeItem(ACTIVE_TAG_BINDING_JOB_KEY);toast(err.message,'err');
      if(S.page==='settings')renderSettings();
    });
  }
  const connectivityId=localStorage.getItem(CONNECTIVITY_JOB_KEY);
  if(connectivityId&&!S.connectivityPollingJob&&(!S.connectivityJob||S.connectivityJob.id!==connectivityId||S.connectivityJob.status==='running')){
    S.connectivityJobID=connectivityId;
    pollConnectivityJob(connectivityId,true).catch(handleConnectivityPollError);
  }
}
document.addEventListener('keydown',event=>{
  if(event.key==='Escape'&&qs('#chainProfileModal')?.classList.contains('show'))closeChainProfileEditor();
  if(event.key==='Escape')qsa('details.row-menu[open]').forEach(menu=>menu.removeAttribute('open'));
});
document.addEventListener('click',event=>{
  qsa('details.row-menu[open]').forEach(menu=>{if(!menu.contains(event.target))menu.removeAttribute('open')});
});
function releasePageResources(){
  S.pageController?.abort();
  S.jobStreamAbort?.abort();
  if(S.filterTimer)clearTimeout(S.filterTimer);
  for(const timer of Object.values(S.autoTimers))if(timer)clearInterval(timer);
  S.autoTimers={};
  stopDashboardClock();
  wakeJobEventWaiters();
  S.jobWaiters.clear();
  S.jobEvents.clear();
}
window.addEventListener('pagehide',releasePageResources);
window.addEventListener('pageshow',event=>{
  if(!event.persisted)return;
  startJobEventStream(true);
  setupAllAutoProbe();
  refresh().catch(()=>{});
});
(async function boot(){
  if(localStorage.getItem('easy_proxies_sidebar_collapsed')==='1')qs('#app').classList.add('collapsed');
  syncLogoutButton();
  renderNav();
  try{await api('/api/ui/summary');qs('#authModal').classList.remove('show');startJobEventStream()}catch(e){}
  setupAllAutoProbe();
  await go(pageFromHash(),true);
})();
