function bulkScopeIds(scopes){
  const nodes=S.nodes||[];
  const ids=new Set();
  for(const n of nodes){
    const isCandidate=!inPool(n)&&n.state==='passed';
    const isPool=inPool(n);
    const isFailed=n.state==='failed';
    if((scopes.candidate&&isCandidate)||(scopes.pool&&isPool)||(scopes.failed&&isFailed))ids.add(n.id);
  }
  return [...ids];
}
function renderBulk(){
  const sel=S.bulkSel=S.bulkSel||{candidate:true,pool:true,failed:true,retest:true,country:false,promote:true};
  const summary=S.summary||{};
  const counts={
    candidate:Number(summary.passed)||0,
    pool:Number(summary.in_pool)||0,
    failed:Number(summary.failed)||0,
  };
  qs('#view').innerHTML=`<div class="grid">
    <div class="card"><div class="card-head"><h2>批量测试</h2></div><div class="card-body stack">
      <div class="bulk-grid"><div class="bulk-group"><strong>测试范围</strong>
        <label class="bulk-option"><input type="checkbox" id="bulkCandidate" ${sel.candidate?'checked':''} onchange="updateBulkSummary()"><span>候选节点</span><span class="value-pill muted">${counts.candidate}</span></label>
        <label class="bulk-option"><input type="checkbox" id="bulkPool" ${sel.pool?'checked':''} onchange="updateBulkSummary()"><span>节点池</span><span class="value-pill green">${counts.pool}</span></label>
        <label class="bulk-option"><input type="checkbox" id="bulkFailed" ${sel.failed?'checked':''} onchange="updateBulkSummary()"><span>失败节点</span><span class="value-pill red">${counts.failed}</span></label>
      </div><div class="bulk-group"><strong>执行操作</strong>
        <label class="bulk-option"><input type="checkbox" id="bulkRetest" ${sel.retest?'checked':''} onchange="updateBulkSummary()"><span>重新测速</span></label>
        <label class="bulk-option"><input type="checkbox" id="bulkCountry" ${sel.country?'checked':''} onchange="updateBulkSummary()"><span>检测国家</span></label>
        <label class="bulk-option"><input type="checkbox" id="bulkPromote" ${sel.promote?'checked':''} onchange="updateBulkSummary()"><span>成功后加入节点池</span></label>
      </div></div>
      <div id="bulkExecutionSummary" class="settings-highlight"></div>
      <div class="row" style="justify-content:flex-end"><button class="btn primary" onclick="runBulkTest()">${icon('activity','sm')}执行批量测试</button></div>
    </div></div>
  </div>`;
  updateBulkSummary();
}
function updateBulkSummary(){
  const selected=[];
  if(qs('#bulkCandidate')?.checked)selected.push('候选节点');if(qs('#bulkPool')?.checked)selected.push('节点池');if(qs('#bulkFailed')?.checked)selected.push('失败节点');
  const actions=[];
  if(qs('#bulkRetest')?.checked)actions.push('重新测速');if(qs('#bulkCountry')?.checked)actions.push('检测国家');
  const host=qs('#bulkExecutionSummary');if(!host)return;
  host.innerHTML=`<span>${esc(selected.join('、')||'未选择范围')} · ${esc(actions.join('、')||'未选择操作')}${qs('#bulkPromote')?.checked?' · 成功后入池':''}</span>`;
}
async function runBulkTest(){
  const scopes={candidate:qs('#bulkCandidate').checked,pool:qs('#bulkPool').checked,failed:qs('#bulkFailed').checked};
  const acts={retest:qs('#bulkRetest').checked,country:qs('#bulkCountry').checked,promote:qs('#bulkPromote').checked};
  S.bulkSel={...scopes,...acts};
  if(!(scopes.candidate||scopes.pool||scopes.failed)){toast('请至少选择一个范围','err');return}
  if(!(acts.retest||acts.country)){toast('请至少选择一种操作','err');return}
  const selectedScopes=Object.entries(scopes).filter(([,enabled])=>enabled).map(([scope])=>scope);
  let job;
  try{
    job=await runAsyncBatchTest({scopes:selectedScopes,retest:acts.retest,country:acts.country,promote_passed:acts.promote,auto_reload:true,label:'批量测试'});
  }catch(e){toast(e.message,'err');return}
  const parts=[];
  if(acts.retest)parts.push(`测速 ${job?.passed||0}/${(job?.passed||0)+(job?.failed||0)} 成功`);
  if(acts.country)parts.push(`国家 ${job?.country_ok||0} 成功 / ${job?.country_bad||0} 失败`);
  if(acts.promote)parts.push(`入池 ${job?.promoted||0}`);
  toast('批量测试完成：'+parts.join(' · '));
  await refresh();
}
