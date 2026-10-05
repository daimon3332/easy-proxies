const IMPORT_KINDS=[['sub_url','订阅链接'],['uri','URI'],['base64','Base64'],['clash','Clash YAML'],['host_port','Host:Port']];
function renderImport(){
  const summary=S.importSummary;
  const dashboard=S.summary||{};
  const running=summary?.status==='running';
  const importCanceling=running && String(summary?.detail||'').includes('正在终止');
  qs('#view').innerHTML=`<div class="import-layout">
    <div class="card"><div class="card-head"><h2>导入节点</h2><div class="segmented" role="tablist" aria-label="导入格式">${IMPORT_KINDS.map(([kind,label])=>`<button type="button" role="tab" aria-selected="${S.importKind===kind}" class="${S.importKind===kind?'active':''}" onclick="chooseImportKind('${kind}')" title="${esc(importKindLabel(kind))}">${label}</button>`).join('')}</div></div>
      <div class="card-body import-form">
        <div class="import-meta">
          <div class="field"><label for="tagPrefix">Tag 前缀</label><input id="tagPrefix" value="" placeholder="例如：Glados" oninput="S.tagPrefix=this.value" onchange="syncImportChainForTag(this.value)"></div>
          <div class="chain-import-row">
            <div class="field"><label for="importChain">前置代理（可选）</label><select id="importChain" onchange="selectImportChain(this.value)"><option value="">不使用前置代理</option>${S.chainProfiles.filter(profile=>profile.enabled).map(profile=>`<option value="${esc(profile.id)}">${esc(profile.name)}</option>`).join('')}</select></div>
            <div class="chain-import-actions"><button class="btn secondary" type="button" onclick="openChainProfileEditor('', 'import')">添加</button><button class="btn" id="editImportChain" type="button" onclick="openChainProfileEditor(S.chainProfileID,'import')" ${S.chainProfileID?'':'disabled'}>编辑</button></div>
          </div>
        </div>
        ${importFields()}
        ${verificationPolicyHTML('import',S.importTest204,S.importSiteTargets,running)}
        <div class="import-footer">
          <div class="progress"><div id="jobBar"></div></div>
          <div class="import-footer-row">
            <p class="status" id="importStatus" aria-live="polite"></p>
            <label class="inline-check"><input id="importAutoPromote" type="checkbox" ${S.importAutoPromote?'checked':''} onchange="setImportAutoPromote(this.checked)">测速成功后自动加入节点池</label>
            ${running?(importCanceling?'<button class="btn danger" disabled>正在终止</button>':'<button class="btn danger" onclick="cancelImportJob()">终止</button>'):''}
            <button class="btn primary" onclick="directImport()" ${running?'disabled':''}>${icon('import','sm')}导入并测试</button>
          </div>
          ${summary?`<div class="job-summary"><div><strong>${esc(summary.title)}</strong><span>${esc(summary.detail)}</span></div>${statePill(summary.status)}</div>`:''}
        </div>
      </div>
    </div>
    <div class="card"><div class="card-head"><h2>本地端口</h2><button class="btn small ghost" onclick="refreshHomeSummary()">${icon('refresh','sm')}刷新</button></div><div class="card-body stack">
      <div class="port-summary">
        <div class="mini-stat green"><span>已生成</span><strong>${esc(dashboard.in_pool||0)}</strong></div>
        <div class="mini-stat"><span>候选</span><strong>${esc(dashboard.passed||0)}</strong></div>
        <div class="mini-stat red"><span>失败</span><strong>${esc(dashboard.failed||0)}</strong></div>
        <div class="mini-stat amber"><span>测试中</span><strong>${esc(dashboard.testing||0)}</strong></div>
      </div>
      ${renderGeneratedPorts(S.portPreview||[],Number(dashboard.in_pool)||0)}
    </div></div>
  </div>`;
  qs('#tagPrefix').value=S.tagPrefix||'';
  if(qs('#importContent'))qs('#importContent').value=S.importText||'';
  if(qs('#importChain'))qs('#importChain').value=S.chainProfileID||'';
  if(qs('#proxyProtocol'))qs('#proxyProtocol').value=S.proxyProtocol||'http';
}
function selectImportChain(value){
  S.chainProfileID=value||'';
  if(S.chainProfileID)localStorage.setItem(IMPORT_CHAIN_PROFILE_KEY,S.chainProfileID);else localStorage.removeItem(IMPORT_CHAIN_PROFILE_KEY);
  const select=qs('#importChain');if(select&&select.value!==S.chainProfileID)select.value=S.chainProfileID;
  const button=qs('#editImportChain');if(button)button.disabled=!S.chainProfileID;
}
function syncImportChainForTag(value){
  const tag=String(value||'').trim();if(!tag)return;
  const source=(S.importSources||[]).find(item=>String(item.tag_prefix||'').trim()===tag);
  if(!source)return;
  if(source.chain_binding==='mixed'){toast('该 Tag 当前包含多种前置代理，请在导入前明确选择','err');return}
  const profileID=source.chain_binding==='profile'&&S.chainProfiles.some(profile=>profile.id===source.chain_profile_id&&profile.enabled)?source.chain_profile_id:'';
  selectImportChain(profileID);
}
function renderGeneratedPorts(ports,total=ports.length){
  if(!ports.length)return '<div class="empty"><strong>还没有生成端口</strong><span>粘贴订阅链接后点击“导入并测试”。</span></div>';
  return `<div class="port-list">${ports.map(n=>`<div class="port"><b>${esc(n.port||'-')}</b><span class="muted" title="${esc(nodeName(n))}">${esc(nodeName(n))}</span><button class="btn small ghost" onclick="copyPort('${esc(n.port||'')}')" title="复制地址" aria-label="复制端口 ${esc(n.port||'')}">${icon('copy','sm')}</button></div>`).join('')}</div>${total>ports.length?`<p class="muted">已显示前 ${ports.length} 个端口，其余可在“可用端口”页面查看。</p>`:''}`;
}
async function copyPort(port){
  if(!port)return;
  const host=location.hostname||'127.0.0.1';
  const text=`${host}:${port}`;
  try{await navigator.clipboard.writeText(text);toast('已复制 '+text)}catch(e){toast(text)}
}
async function refreshHomeSummary(){
  await Promise.all([loadImportSummary(),loadPortPreview()]);
  renderImport();
}
function setImportAutoPromote(v){
  S.importAutoPromote=!!v;
  localStorage.setItem('easy_proxies_import_auto_promote',S.importAutoPromote?'1':'0');
  renderImport();
}
function chooseImportKind(kind){
  S.importText=qs('#importContent')?.value??S.importText;
  S.tagPrefix=qs('#tagPrefix')?.value??S.tagPrefix;
  S.importKind=kind;
  S.importSummary=null;
  renderImport();
}
function importKindLabel(kind=S.importKind){return {sub_url:'订阅链接格式',uri:'URI 格式',base64:'Base64 格式',clash:'Clash YAML 格式',host_port:'Host:Port 代理列表'}[kind]||'URI 格式'}
function importInputLabel(){return S.importKind==='sub_url'?'订阅链接（每行一个）':S.importKind==='uri'?'URI 节点（每行一个）':S.importKind==='base64'?'Base64 内容':S.importKind==='host_port'?'代理地址（每行一个）':'Clash YAML 内容'}
function importPlaceholder(){
  if(S.importKind==='sub_url')return 'https://example.com/sub1\nhttps://example.com/sub2';
  if(S.importKind==='uri')return 'ss://...\ntrojan://...\nvmess://...\nhttp://1.2.3.4:8080\nsocks5://1.2.3.4:1080\nsocks4://1.2.3.4:1080';
  if(S.importKind==='base64')return '粘贴 Base64 编码后的节点列表';
  if(S.importKind==='host_port')return '1.2.3.4:8080\nuser:pass@1.2.3.4:8080\nuser:pass@[2001:db8::1]:8080';
  return 'proxies:\n  - name: ...';
}
function importFields(){
  return `${S.importKind==='host_port'?`<div class="row"><div class="field" style="min-width:220px"><label for="proxyProtocol">代理协议</label><select id="proxyProtocol" onchange="S.proxyProtocol=this.value"><option value="http">HTTP</option><option value="socks5">SOCKS5</option></select></div><button class="btn secondary" onclick="qs('#hostPortFile').click()">选择 TXT 文件</button><input id="hostPortFile" type="file" accept=".txt,text/plain" hidden onchange="loadHostPortFile(this.files?.[0])"></div>`:''}<div class="field"><label for="importContent">${esc(importInputLabel())}</label><textarea id="importContent" placeholder="${esc(importPlaceholder())}" oninput="S.importText=this.value"></textarea></div>`;
}
async function loadHostPortFile(file){if(!file)return;S.importText=await file.text();renderImport()}
function importPayload(){
  S.tagPrefix=(qs('#tagPrefix')?.value||'').trim();
  S.importText=qs('#importContent')?.value||'';
  S.chainProfileID=qs('#importChain')?.value||'';
  if(S.chainProfileID)localStorage.setItem(IMPORT_CHAIN_PROFILE_KEY,S.chainProfileID);else localStorage.removeItem(IMPORT_CHAIN_PROFILE_KEY);
  const body={mode:S.importKind==='sub_url'?'url':'content',tag_prefix:S.tagPrefix,chain_profile_id:S.chainProfileID||''};
  if(S.importKind==='host_port'){body.content_format='host_port';body.proxy_protocol=S.proxyProtocol||'http'}
  if(body.mode==='url')body.url=S.importText.trim();else body.content=S.importText.trim();
  return body;
}
async function directImport(){
  try{
    const body=importPayload();
    const policy=verificationPolicy('import');
    if(!verificationPolicyValid(policy))return;
    if(!body.tag_prefix){
      openDialog('需要填写 Tag 前缀','<p>请先填写 Tag 前缀，再导入并测速节点。</p>');
      requestAnimationFrame(()=>qs('#tagPrefix')?.focus());
      return;
    }
    if(S.importKind==='sub_url')return await importSubscriptionURLs(body);
    if(body.mode==='content'&&!body.content){toast('订阅内容不能为空','err');return}
    await importOne(body, importKindLabel());
  }catch(err){setStatus('#importStatus',err.message);toast(err.message,'err')}
}
async function importSubscriptionURLs(body){
  const urls=body.url.split(/\r?\n/).map(x=>x.trim()).filter(Boolean);
  if(!urls.length){toast('订阅链接不能为空','err');return}
  const invalid=urls.find(u=>!/^https?:\/\//i.test(u));
  if(invalid){toast('订阅链接必须以 http:// 或 https:// 开头: '+invalid,'err');return}
  await importOne({...body,url:urls.join('\n')},`订阅 ${urls.length} 个`);
  await registerSubscriptionURLs(urls,body.tag_prefix);
}
async function importOne(body,label){
  let replacedSubscriptionURLs=[];
  if(body.mode==='content'){
    await loadSubscriptionConfig();
    const names=S.subConfig?.names||{};
    replacedSubscriptionURLs=(S.subConfig?.subscriptions||[]).filter(url=>names[url]===S.tagPrefix);
  }
  S.importSummary={status:'running',title:'正在解析 '+label,detail:'正在解析并准备测试节点'};
  renderImport();
  setStatus('#importStatus','解析中...');
  const parsed=await api('/api/import/parse',{method:'POST',body:JSON.stringify(body)});
  const nodes=Array.isArray(parsed.nodes)?parsed.nodes:[];
  if(!nodes.length)throw new Error('未解析到任何节点');
  const front=body.chain_profile_id?`前置 ${chainProfileName(body.chain_profile_id)}`:'直连';
  S.importSummary={status:'running',title:`已解析 ${nodes.length} 个节点`,detail:`来源 ${label} · ${front} · ${parsed.format || 'unknown'}，正在测试；成功节点${S.importAutoPromote?'会自动加入节点池':'会进入候选池'}`};
  renderImport();
  const ids=nodes.map(n=>n.id).filter(Boolean);
  const res=await api('/api/import/'+parsed.import_id+'/commit',{method:'POST',body:JSON.stringify({node_ids:ids,auto_reload:true,promote_passed:S.importAutoPromote,...verificationPolicy('import')})});
  S.lastJob=res.job_id;
  S.activeImportJob=res.job_id;
  localStorage.setItem(ACTIVE_IMPORT_JOB_KEY,res.job_id);
  const job=await pollJob(res.job_id);
  if(body.mode==='content'&&job?.status==='completed'&&replacedSubscriptionURLs.length){
    await removeSubscriptionURLs(replacedSubscriptionURLs);
    toast(`已用内容导入替换 ${replacedSubscriptionURLs.length} 个旧订阅`);
  }
}
async function registerSubscriptionURLs(urls,tagPrefix){
  await loadSubscriptionConfig();
  const names=S.subConfig?.names||{};
  const oldUrls=S.subConfig?.subscriptions||[];
  const kept=oldUrls.filter(u=>names[u]!==tagPrefix && !urls.includes(u));
  const next=[...new Set([...kept,...urls])];
  S.subConfig=await api('/api/subscription/config',{method:'PUT',body:JSON.stringify(subscriptionConfigPayload({subscriptions:next,refresh:false}))});
  toast(`已记录 ${urls.length} 个订阅链接；${tagPrefix} 采用最新快照`);
}
async function pollJob(id){
  if(!id || S.importPollingJob===id)return;
  S.importPollingJob=id;
  S.activeImportJob=id;
  localStorage.setItem(ACTIVE_IMPORT_JOB_KEY,id);
  let last=null;
  try{
    while(true){
      const bar=qs('#jobBar'),status=qs('#importStatus');
      const j=await api('/api/import/jobs/'+id);
      last=j;
      const done=(j.passed||0)+(j.failed||0);
      const siteDone=(j.site_progress||[]).reduce((sum,item)=>sum+(Number(item.done)||0),0),siteTotal=(j.site_progress||[]).reduce((sum,item)=>sum+(Number(item.total)||0),0);
      const probeDone=Number(j.probe_round_done)||0,probeTotal=Number(j.probe_round_total)||0;
      const pct=siteTotal?Math.min(100,Math.round(siteDone*100/siteTotal)):probeTotal?Math.min(100,Math.round(probeDone*100/probeTotal)):j.total?Math.min(100,Math.round(done*100/j.total)):0;
      const front=j.chain_probe?(j.chain_probe.error?'前置失败':`前置 ${j.chain_probe.latency_ms||0} ms`):'';
      const sites=siteProgressSummary(j.site_progress),probe=probeTotal?`204 第 ${j.probe_round||0}/${j.probe_rounds||3} 轮 ${probeDone}/${probeTotal}`:'';
      const line=`${j.status}: ${front?front+'，':''}${sites?sites+'；':probe?probe+'；':''}${done}/${j.total}，成功 ${j.passed||0}，失败 ${j.failed||0}，入池 ${j.promoted||0}`;
      if(bar)bar.style.width=pct+'%';
      if(status)status.textContent=line;
      S.importSummary={status:j.status,title:`导入任务 ${j.status}`,detail:`${front?front+'；':''}${sites?sites+'；':probe?probe+'；':''}完整链路 ${done}/${j.total}，成功 ${j.passed||0}，失败 ${j.failed||0}，入池 ${j.promoted||0}${j.error?'，错误: '+j.error:''}`};
      if(j.status==='completed'||j.status==='failed'||j.status==='canceled'){
        toast(j.status==='canceled'?'导入任务已终止':'导入任务结束');
        await refresh();
        break;
      }
      await new Promise(r=>setTimeout(r,500));
    }
  }finally{
    S.importPollingJob='';
    if(last && last.status!=='running'){
      S.activeImportJob='';
      localStorage.removeItem(ACTIVE_IMPORT_JOB_KEY);
    }
  }
  return last;
}
async function cancelImportJob(){
  const id=S.activeImportJob||S.lastJob||localStorage.getItem(ACTIVE_IMPORT_JOB_KEY);
  if(!id){toast('没有正在运行的导入任务','err');return}
  try{
    const j=await api('/api/import/jobs/'+encodeURIComponent(id)+'/cancel',{method:'POST'});
    const done=(j.passed||0)+(j.failed||0);
    S.importSummary={status:j.status,title:`导入任务 ${j.status}`,detail:`进度 ${done}/${j.total}，成功 ${j.passed||0}，失败 ${j.failed||0}，入池 ${j.promoted||0}${j.error?'，错误: '+j.error:''}`};
    renderImport();
    toast('已发送终止请求');
  }catch(err){toast(err.message,'err')}
}
