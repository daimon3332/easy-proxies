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
    <div class="card"><div class="card-head"><h2>Toplu test</h2></div><div class="card-body stack">
      <div class="bulk-grid"><div class="bulk-group"><strong>Test kapsamı</strong>
        <label class="bulk-option"><input type="checkbox" id="bulkCandidate" ${sel.candidate?'checked':''} onchange="updateBulkSummary()"><span>Aday düğümler</span><span class="value-pill muted">${counts.candidate}</span></label>
        <label class="bulk-option"><input type="checkbox" id="bulkPool" ${sel.pool?'checked':''} onchange="updateBulkSummary()"><span>Proxy havuzu</span><span class="value-pill green">${counts.pool}</span></label>
        <label class="bulk-option"><input type="checkbox" id="bulkFailed" ${sel.failed?'checked':''} onchange="updateBulkSummary()"><span>Başarısız düğümler</span><span class="value-pill red">${counts.failed}</span></label>
      </div><div class="bulk-group"><strong>Yapılacak işlemler</strong>
        <label class="bulk-option"><input type="checkbox" id="bulkRetest" ${sel.retest?'checked':''} onchange="updateBulkSummary()"><span>Hız testini tekrarla</span></label>
        <label class="bulk-option"><input type="checkbox" id="bulkCountry" ${sel.country?'checked':''} onchange="updateBulkSummary()"><span>Ülkeyi belirle</span></label>
        <label class="bulk-option"><input type="checkbox" id="bulkPromote" ${sel.promote?'checked':''} onchange="updateBulkSummary()"><span>Başarılı olanları havuza ekle</span></label>
      </div></div>
      <div id="bulkExecutionSummary" class="settings-highlight"></div>
      <div class="row" style="justify-content:flex-end"><button class="btn primary" onclick="runBulkTest()">${icon('activity','sm')}Toplu testi başlat</button></div>
    </div></div>
  </div>`;
  updateBulkSummary();
}
function updateBulkSummary(){
  const selected=[];
  if(qs('#bulkCandidate')?.checked)selected.push('Aday düğümler');if(qs('#bulkPool')?.checked)selected.push('Proxy havuzu');if(qs('#bulkFailed')?.checked)selected.push('Başarısız düğümler');
  const actions=[];
  if(qs('#bulkRetest')?.checked)actions.push('Hız testini tekrarla');if(qs('#bulkCountry')?.checked)actions.push('Ülkeyi belirle');
  const host=qs('#bulkExecutionSummary');if(!host)return;
  host.innerHTML=`<span>${esc(selected.join(', ')||'Kapsam seçilmedi')} · ${esc(actions.join(', ')||'İşlem seçilmedi')}${qs('#bulkPromote')?.checked?' · Başarılı olanları havuza ekle':''}</span>`;
}
async function runBulkTest(){
  const scopes={candidate:qs('#bulkCandidate').checked,pool:qs('#bulkPool').checked,failed:qs('#bulkFailed').checked};
  const acts={retest:qs('#bulkRetest').checked,country:qs('#bulkCountry').checked,promote:qs('#bulkPromote').checked};
  S.bulkSel={...scopes,...acts};
  if(!(scopes.candidate||scopes.pool||scopes.failed)){toast('En az bir test kapsamı seçin','err');return}
  if(!(acts.retest||acts.country)){toast('En az bir işlem seçin','err');return}
  const selectedScopes=Object.entries(scopes).filter(([,enabled])=>enabled).map(([scope])=>scope);
  let job;
  try{
    job=await runAsyncBatchTest({scopes:selectedScopes,retest:acts.retest,country:acts.country,promote_passed:acts.promote,auto_reload:true,label:'Toplu test'});
  }catch(e){toast(e.message,'err');return}
  const parts=[];
  if(acts.retest)parts.push(`Hız testi: ${(job?.passed||0)}/${(job?.passed||0)+(job?.failed||0)} başarılı`);
  if(acts.country)parts.push(`Ülke: ${job?.country_ok||0} başarılı / ${job?.country_bad||0} başarısız`);
  if(acts.promote)parts.push(`Havuza eklenen: ${job?.promoted||0}`);
  toast('Toplu test tamamlandı: '+parts.join(' · '));
  await refresh();
}
