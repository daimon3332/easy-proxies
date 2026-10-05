function importSourceType(s){
  if(s?.mode==='url')return '订阅链接';
  const f=String(s?.format||'');
  if(f.includes('base64'))return 'Base64';
  if(f.includes('clash'))return 'Clash YAML';
  if(f.includes('uri'))return 'URI';
  return s?.mode==='content'?'内容导入':'导入来源';
}
function importSourceText(s){
  if(s?.mode==='url')return s.source||'';
  const f=String(s?.format||'');
  if(f.includes('base64'))return 'Base64 内容导入';
  if(f.includes('clash'))return 'Clash YAML 内容导入';
  if(f.includes('uri'))return 'URI 列表内容导入';
  return s?.source&&s.source!=='content'?s.source:'内容导入';
}
function fmtDate(v){
  if(!v)return '-';
  const d=new Date(v);
  return Number.isNaN(d.getTime())?'-':d.toLocaleString();
}
function webDAVFilesHTML(files,loading){
  if(loading)return '<div class="empty">正在加载远程备份...</div>';
  if(!files.length)return '<div class="empty">没有远程备份</div>';
  return files.map(file=>`<div class="backup-file"><div class="backup-file-main"><strong>${esc(file.name)}</strong><span class="muted">${esc(formatBytes(file.size))} · ${esc(fmtDate(file.mod_time))}</span></div><div class="row"><button class="btn small" data-name="${esc(file.name)}" onclick="downloadWebDAVBackup(this.dataset.name)">下载</button><button class="btn small secondary" data-name="${esc(file.name)}" onclick="restoreWebDAVBackup(this.dataset.name)">恢复</button><button class="btn small danger" data-name="${esc(file.name)}" onclick="deleteWebDAVBackup(this.dataset.name)">删除</button></div></div>`).join('');
}
function exportTagGroups(sources){
  const groups=new Map();
  for(const source of sources||[]){
    const tag=String(source?.tag_prefix||'').trim();
    if(!tag)continue;
    let group=groups.get(tag);
    if(!group){group={tag,total:0,types:new Set()};groups.set(tag,group)}
    group.total+=Number(source.total)||0;
    group.types.add(importSourceType(source));
  }
  return [...groups.values()].map(group=>({...group,types:[...group.types]})).sort((a,b)=>a.tag.localeCompare(b.tag));
}
function chainProfileName(id){return S.chainProfiles.find(profile=>profile.id===id)?.name||id}
function maskProxyURI(uri){return String(uri||'').replace(/^(\w[\w+.-]*:\/\/)[^@/]+@/,'$1***@').replace(/#.*$/,'')}
function openChainProfileEditor(id='',origin=S.page){
  const profile=S.chainProfiles.find(item=>item.id===id)||{id:'',name:'',enabled:true,hops:[{uri:''}]};
  S.chainEditorOrigin=origin||S.page;
  qs('#chainProfileTitle').textContent=id?'编辑前置代理':'添加前置代理';
  qs('#chainEditID').value=profile.id||'';
  qs('#chainEditName').value=profile.name||'';
  qs('#chainEditURI').value=profile.hops?.[0]?.uri||'';
  qs('#chainEditEnabled').checked=profile.enabled!==false;
  setChainEditorStatus('');
  setChainEditorBusy(false);
  qs('#chainProfileModal').classList.add('show');
  requestAnimationFrame(()=>qs('#chainEditName')?.focus());
}
function closeChainProfileEditor(){if(qs('#chainSaveButton')?.disabled)return;qs('#chainProfileModal')?.classList.remove('show')}
function chainProfileDraft(){return {id:qs('#chainEditID')?.value||'',name:qs('#chainEditName')?.value.trim()||'',enabled:!!qs('#chainEditEnabled')?.checked,hops:[{uri:qs('#chainEditURI')?.value.trim()||''}]}}
function validateChainProfileDraft(profile){
  if(!profile.name)return '请填写前置代理名称';
  if(!profile.hops[0].uri)return '请填写节点 URI';
  if(!/^[a-z][a-z0-9+.-]*:\/\//i.test(profile.hops[0].uri))return '节点 URI 必须包含协议，例如 vless:// 或 socks5://';
  return '';
}
function setChainEditorStatus(message,type=''){const host=qs('#chainEditStatus');if(!host)return;host.textContent=message||'';host.className='inline-status '+(type||'')}
function setChainEditorBusy(busy,action=''){
  for(const id of ['chainTestButton','chainSaveButton'])if(qs('#'+id))qs('#'+id).disabled=!!busy;
  if(qs('#chainTestButton'))qs('#chainTestButton').textContent=busy&&action==='test'?'正在测试':'测试连接';
  if(qs('#chainSaveButton'))qs('#chainSaveButton').textContent=busy&&action==='save'?'正在保存':'保存';
}
async function testChainProfileDraft(){
  const profile=chainProfileDraft(),error=validateChainProfileDraft(profile);if(error){setChainEditorStatus(error,'bad');return false}
  setChainEditorBusy(true,'test');setChainEditorStatus('正在建立前置代理连接...');
  try{const result=await api('/api/chain-profiles/test',{method:'POST',body:JSON.stringify({profile})});setChainEditorStatus(`连接成功，延迟 ${result.latency_ms||0} ms`,'good');return true}catch(err){setChainEditorStatus(err.message,'bad');return false}finally{setChainEditorBusy(false)}
}
async function saveChainProfileDraft(){
  const id=qs('#chainEditID')?.value||'',name=qs('#chainEditName')?.value.trim()||'',uri=qs('#chainEditURI')?.value.trim()||'';
  const profile={id,name,enabled:!!qs('#chainEditEnabled')?.checked,hops:[{uri}]},error=validateChainProfileDraft(profile);
  if(error){setChainEditorStatus(error,'bad');return}
  const usage=S.chainUsage?.[id]||{nodes:0,ports:0};
  const cascade=!!(id&&usage.nodes&&!profile.enabled);
  const oldProfile=S.chainProfiles.find(item=>item.id===id),routeChanged=oldProfile?.hops?.[0]?.uri!==uri;
  const impact=cascade?`禁用会同时删除 ${usage.nodes} 个关联链式节点和 ${usage.ports||0} 个端口，无法撤销。`:routeChanged?`将影响 ${usage.nodes} 个链式节点和 ${usage.ports||0} 个端口，保存后会自动重新检测。`:`只修改名称，不会中断或重新检测关联节点。`;
  if(id&&usage.nodes&&!confirm(`确认修改前置代理“${name}”？\n\n${impact}`))return;
  const next=S.chainProfiles.map(profile=>({...profile,hops:(profile.hops||[]).map(hop=>({...hop}))}));
  const index=next.findIndex(item=>item.id===id&&id);
  if(index>=0)next[index]=profile;else next.push(profile);
  setChainEditorBusy(true,'save');setChainEditorStatus('正在验证并保存...');
  try{
    const data=await api('/api/chain-profiles',{method:'PUT',body:JSON.stringify({profiles:next,cascade})});
    S.chainProfiles=data.profiles||[];S.chainUsage=data.usage||{};
    const saved=S.chainProfiles.find(item=>item.id===id)||(S.chainProfiles.find(item=>item.name===name&&item.hops?.[0]?.uri===uri));
    if(S.chainEditorOrigin==='import'&&saved?.enabled)selectImportChain(saved.id);
    qs('#chainProfileModal').classList.remove('show');
    toast(data.retest_job_id?'前置代理已保存，关联节点正在重新检测':'前置代理已保存');
    if(S.page==='settings')renderSettings();else if(S.page==='import')renderImport();
  }catch(err){setChainEditorStatus(err.message,'bad')}finally{setChainEditorBusy(false)}
}
async function deleteChainProfile(id){
  const profile=S.chainProfiles.find(item=>item.id===id);if(!profile)return;
  const usage=S.chainUsage?.[id]||{nodes:0,ports:0};
  const warning=usage.nodes?`\n\n这会同时删除 ${usage.nodes} 个关联链式节点和 ${usage.ports||0} 个端口，无法撤销。`:'';
  if(!confirm(`确认删除前置代理“${profile.name}”？${warning}`))return;
  try{const data=await api('/api/chain-profiles',{method:'PUT',body:JSON.stringify({profiles:S.chainProfiles.filter(item=>item.id!==id),cascade:usage.nodes>0})});S.chainProfiles=data.profiles||[];S.chainUsage=data.usage||{};if(S.chainProfileID===id)selectImportChain('');toast(data.deleted_nodes?`已删除前置代理和 ${data.deleted_nodes} 个关联节点`:'前置代理已删除');renderSettings()}catch(err){toast(err.message,'err')}
}
async function testChainProfile(id){
  const button=document.querySelector(`[data-chain-test="${CSS.escape(id)}"]`),old=button?.textContent;if(button){button.disabled=true;button.textContent='测试中'}
  try{const result=await api('/api/chain-profiles/test',{method:'POST',body:JSON.stringify({id})});toast(`前置代理可用，延迟 ${result.latency_ms||0} ms`)}catch(err){toast(err.message,'err')}finally{if(button){button.disabled=false;button.textContent=old}}
}
function chainProfilesHTML(){
  if(!S.chainProfiles.length)return '<div class="connectivity-idle"><span>尚未配置前置代理</span><button class="btn small secondary" onclick="openChainProfileEditor(\'\', \'settings\')">添加前置代理</button></div>';
  const rows=S.chainProfiles.map(profile=>{
    const disabled=profile.enabled?'':`<span class="pill amber">已停用</span>`;
    return `<div class="chain-row"><div class="chain-main"><div class="row"><strong>${esc(profile.name)}</strong>${disabled}</div><code class="source-value">${esc(maskProxyURI(profile.hops?.[0]?.uri||''))}</code></div><div class="chain-row-actions"><button class="btn small green" data-chain-test="${esc(profile.id)}" data-id="${esc(profile.id)}" onclick="testChainProfile(this.dataset.id)">测试</button><button class="btn small secondary" data-id="${esc(profile.id)}" onclick="openChainProfileEditor(this.dataset.id,'settings')">编辑</button><button class="btn small danger" data-id="${esc(profile.id)}" onclick="deleteChainProfile(this.dataset.id)">删除</button></div></div>`;
  }).join('');
  return `<div class="chain-list">${rows}</div>`;
}
function chainBindingLabel(source){
  if(source?.chain_binding==='mixed')return {text:'前置 混合',className:'amber'};
  if(source?.chain_binding==='profile'&&source.chain_profile_id)return {text:`前置 ${chainProfileName(source.chain_profile_id)}`,className:'amber'};
  return {text:'直连',className:'muted'};
}
function renderSettings(){
  const s=S.settings||{};
  const listener=s.listener||{},multi=s.multi_port||{},pool=s.pool||{},management=s.management||{};
  const sub=S.subConfig||{subscriptions:[],enabled:true,interval:'24h'};
  const sources=S.importSources||[];
  const webdav=S.webdav||{address:'',username:'',password:'',folder:'/easy_proxies'};
  const webdavFiles=S.webdavFiles||[];
  const webdavLoading=!!S.webdavLoading;
  const interval=durationParts(sub.interval||'24h');
  const probeTarget=allowedProbeTarget(s.probe_target);
  const blacklistSeconds=durationSeconds(pool.blacklist_duration,600);
  const rotationSeconds=durationSeconds(pool.rotation_interval,120);
  const refreshing=S.refreshJob?.status==='running'||!!S.activeRefreshJob;
  const bindingRunning=S.tagBindingJob?.status==='running'||!!S.activeTagBindingJob;
  const bindingTags=[...new Set(sources.map(source=>String(source.tag_prefix||'').trim()).filter(Boolean))];
  const availableBindingTags=new Set(bindingTags);
  for(const tag of S.selectedBindingTags)if(!availableBindingTags.has(tag))S.selectedBindingTags.delete(tag);
  const exportTags=exportTagGroups(sources);
  const availableTags=new Set(exportTags.map(group=>group.tag));
  for(const tag of S.selectedExportTags)if(!availableTags.has(tag))S.selectedExportTags.delete(tag);
  qs('#view').innerHTML=`<div class="settings-grid">
      <section class="settings-section settings-compact" data-settings-column="left">
        <div class="settings-section-head">
          <h2>前置代理</h2>
          <button class="btn small primary" onclick="openChainProfileEditor('', 'settings')">添加</button>
        </div>
        <div class="settings-section-body">
          ${chainProfilesHTML()}
        </div>
      </section>

      <section class="settings-section" data-settings-column="right">
        <div class="settings-section-head">
          <h2>基础运行</h2>
        </div>
        <div class="settings-section-body">
          <div class="settings-form">
            <div class="field"><label>运行模式</label><select id="setMode"><option value="pool">pool</option><option value="multi-port">multi-port</option><option value="hybrid">hybrid</option></select></div>
            <div class="field"><label>探测目标</label><select id="setProbe"><option value="https://www.gstatic.com/generate_204">gstatic generate_204</option><option value="https://cp.cloudflare.com/generate_204">cloudflare generate_204</option></select></div>
            <div class="field"><label>外部 IP</label><input id="setExternal" value="${esc(s.external_ip||'')}" placeholder="留空则使用当前访问地址"></div>
            <div class="field"><label>管理监听</label><input id="setMgmtListen" value="${esc(management.listen||'')}"></div>
            <label class="toggle-row wide"><input id="setSkip" type="checkbox" ${s.skip_cert_verify?'checked':''} style="width:auto"><div><strong>跳过证书验证</strong></div></label>
          </div>
        </div>
      </section>

      <section class="settings-section" data-settings-column="left">
        <div class="settings-section-head">
          <h2>端口与监听</h2>
        </div>
        <div class="settings-section-body">
          <div class="settings-form">
            <div class="field"><label>Pool 监听地址</label><input id="setListenAddr" value="${esc(listener.address||'')}"></div>
            <div class="field"><label>Pool 端口</label><input id="setListenPort" type="number" value="${esc(listener.port||0)}"></div>
            <div class="field"><label>Multi 地址</label><input id="setMultiAddr" value="${esc(multi.address||'')}"></div>
            <div class="field"><label>Multi 起始端口</label><input id="setMultiPort" type="number" value="${esc(multi.base_port||0)}"></div>
            <div class="field"><label>Multi 用户名</label><input id="setMultiUser" value="${esc(multi.username||'')}"></div>
            <div class="field"><label>Multi 密码</label><input id="setMultiPass" value="${esc(multi.password||'')}"></div>
          </div>
        </div>
      </section>

      <section class="settings-section" data-settings-column="right">
        <div class="settings-section-head">
          <h2>节点池策略</h2>
        </div>
        <div class="settings-section-body">
          <div class="settings-form">
            <div class="field"><label>池模式</label><select id="setPoolMode"><option value="rotate">rotate</option><option value="sequential">sequential</option><option value="random">random</option><option value="balance">balance</option></select></div>
            <div class="field"><label>失败次数</label><input id="setFail" type="number" min="1" value="${esc(pool.failure_threshold||2)}"></div>
            <div class="field"><label>黑名单时间（秒）</label><input id="setBlacklist" type="number" min="1" step="1" value="${esc(blacklistSeconds)}"></div>
            <div class="field"><label>轮换间隔（秒）</label><input id="setRotateInterval" type="number" min="1" step="1" value="${esc(rotationSeconds)}"></div>
          </div>
          <div class="settings-actions"><button class="btn primary" onclick="saveSettings()">保存运行设置并重载</button></div>
        </div>
      </section>

      <section class="settings-section settings-wide">
        <div class="settings-section-head">
          <h2>订阅刷新</h2>
          <span class="pill green">${esc((sub.subscriptions||[]).length)} 个订阅</span>
        </div>
        <div class="settings-section-body">
          <div id="refreshStatusHost">${refreshStatusHTML(S.refreshJob)}</div>
          <label class="toggle-row"><input id="subEnabled" type="checkbox" ${sub.enabled!==false?'checked':''} style="width:auto"><div><strong>启用自动刷新</strong></div></label>
          ${verificationPolicyHTML('refresh',sub.test_204!==false,new Set(sub.site_targets||[]),refreshing)}
          <div class="time-grid">
            <div class="field"><label>天</label><input id="subDays" type="number" min="0" value="${interval.days}"></div>
            <div class="field"><label>小时</label><input id="subHours" type="number" min="0" max="23" value="${interval.hours}"></div>
            <div class="field"><label>分钟</label><input id="subMinutes" type="number" min="0" max="59" value="${interval.minutes}"></div>
          </div>
          <button class="btn secondary block" onclick="saveAutoRefresh()">保存自动刷新设置</button>
        </div>
      </section>

      <section class="settings-section settings-wide">
        <div class="settings-section-head">
          <h2>导入来源</h2>
          <div class="row">
            <span class="pill" id="bindingTagCount">已选 ${esc(S.selectedBindingTags.size)}</span>
            <button class="btn small secondary" data-binding-selected onclick="openTagBindingDialog([...S.selectedBindingTags])" ${S.selectedBindingTags.size&&!bindingRunning&&!refreshing?'':'disabled'}>设置所选前置代理</button>
            <button class="btn small secondary" onclick="openTagBindingDialog(allBindingTags(),'all')" ${bindingTags.length&&!bindingRunning&&!refreshing?'':'disabled'}>设置全部前置代理</button>
            <button class="btn small green" data-refresh-action onclick="refreshAllImportSources()" ${sources.length&&!refreshing&&!bindingRunning?'':'disabled'}>重新检测全部</button>
            <button class="btn small danger" data-refresh-action onclick="deleteAllImportSources()" ${sources.length&&!refreshing&&!bindingRunning?'':'disabled'}>删除全部</button>
          </div>
        </div>
        <div class="settings-section-body">
      ${sources.length?`<div class="stack">${sources.map((src,i)=>{const tag=String(src.tag_prefix||'').trim(),binding=chainBindingLabel(src);return `<div class="source-row">
        <label class="source-row-select"><input type="checkbox" data-binding-tag="${esc(tag)}" aria-label="选择 ${esc(tag)}" ${tag&&S.selectedBindingTags.has(tag)?'checked':''} ${tag&&!bindingRunning&&!refreshing?'':'disabled'} onchange="toggleBindingTag(this.dataset.bindingTag,this.checked)"></label>
        <div style="flex:1;min-width:0">
          <div class="row" style="gap:8px;align-items:center;flex-wrap:wrap">
            <span class="pill green">${esc(src.tag_prefix||'local')}</span>
            <span class="pill">${esc(importSourceType(src))}</span>
            <span class="value-pill ${binding.className}">${esc(binding.text)}</span>
            <span class="value-pill muted">总数 ${esc(src.total||0)}</span>
            <span class="value-pill green">池内 ${esc(src.pool||0)}</span>
            <span class="value-pill red">失败 ${esc(src.failed||0)}</span>
            <span class="value-pill amber">候选 ${esc(src.candidate||0)}</span>
          </div>
          <div style="margin-top:6px"><code class="source-value">${esc(importSourceText(src))}</code></div>
          <div class="muted" style="margin-top:6px">更新：${esc(fmtDate(src.updated_at))}</div>
        </div>
        <div class="chain-row-actions">
          ${tag?`<button class="btn small secondary" data-tag="${esc(tag)}" onclick="openTagBindingDialog([this.dataset.tag])" ${bindingRunning||refreshing?'disabled':''}>更改前置代理</button>`:''}
          ${src.refreshable?`<button class="btn small green" data-refresh-action onclick="refreshImportSource(${i})" ${refreshing||bindingRunning?'disabled':''}>重新检测</button>`:''}
          <button class="btn small danger" data-refresh-action onclick="deleteImportSource(${i})" ${refreshing||bindingRunning?'disabled':''}>删除</button>
        </div>
      </div>`}).join('')}</div>`:`<div class="empty">还没有导入来源</div>`}
        </div>
      </section>

      <section class="settings-section settings-wide">
        <div class="settings-section-head">
          <h2>数据导出</h2>
          <span class="pill" id="exportTagCount">已选 ${esc(S.selectedExportTags.size)} / ${esc(exportTags.length)}</span>
        </div>
        <div class="settings-section-body">
          <div class="row"><button class="btn small secondary" onclick="setAllExportTags(true)" ${exportTags.length?'':'disabled'}>选择全部 Tag</button><button class="btn small ghost" id="clearExportTags" onclick="setAllExportTags(false)" ${S.selectedExportTags.size?'':'disabled'}>清空选择</button><button class="btn" data-tag-export-selected onclick="exportSelectedTags()" ${S.selectedExportTags.size?'':'disabled'}>导出选中 (${esc(S.selectedExportTags.size)})</button></div>
          ${exportTags.length?`<div class="subscription-export-list">${exportTags.map(group=>`<div class="export-tag-row"><input type="checkbox" data-export-tag data-tag="${esc(group.tag)}" aria-label="选择 ${esc(group.tag)}" ${S.selectedExportTags.has(group.tag)?'checked':''} onchange="toggleExportTag(this.dataset.tag,this.checked)"><div class="export-tag-main"><strong>${esc(group.tag)}</strong><span class="muted">${esc(group.types.join(' / '))} · ${esc(group.total)} 个节点</span></div><button class="btn small secondary" data-tag="${esc(group.tag)}" onclick="exportSourceTags([this.dataset.tag])">导出</button></div>`).join('')}</div>`:'<div class="empty">没有带 Tag 的导入来源</div>'}
        </div>
      </section>

      <section class="settings-section settings-wide">
        <div class="settings-section-head">
          <h2>备份与恢复</h2>
          <span class="pill ${webdav.address?'green':''}">${webdav.address?'WebDAV 已配置':'仅本地'}</span>
        </div>
        <div class="settings-section-body">
          <div class="backup-settings-grid">
            <div class="backup-pane">
              <h3>本地备份</h3>
              <button class="btn primary block" onclick="downloadLocalBackup()">导出完整备份</button>
              <button class="btn secondary block" onclick="qs('#localBackupFile').click()">从本地备份恢复</button>
              <input id="localBackupFile" type="file" accept=".zip,application/zip" onchange="restoreLocalBackup(this.files?.[0])" hidden>
            </div>
            <div class="backup-pane">
              <h3>WebDAV 备份</h3>
              <div class="field"><label>WebDAV 地址</label><input id="webdavAddress" type="text" value="${esc(webdav.address||'')}" placeholder="https://dav.example.com/remote.php/dav/files/user"></div>
              <div class="field"><label>备份文件夹</label><input id="webdavFolder" type="text" value="${esc(webdav.folder||'/easy_proxies')}" placeholder="/easy_proxies"><div class="field-help">默认 /easy_proxies</div></div>
              <div class="settings-form">
                <div class="field"><label>账号</label><input id="webdavUsername" type="text" value="${esc(webdav.username||'')}"></div>
                <div class="field"><label>密码</label><input id="webdavPassword" type="text" value="${esc(webdav.password||'')}"></div>
              </div>
              <div class="row"><button class="btn" onclick="saveWebDAV(false)">保存</button><button class="btn secondary" onclick="testWebDAV()">测试连接</button><button class="btn green" onclick="createWebDAVBackup()">立即备份</button></div>
              <div class="stack" id="webdavFiles">${webDAVFilesHTML(webdavFiles,webdavLoading)}</div>
            </div>
          </div>
        </div>
      </section>
  </div>`;
  arrangeSettingsLayout();
  qs('#setMode').value=s.mode||'pool';
  qs('#setProbe').value=probeTarget;
  qs('#setPoolMode').value=pool.mode||'rotate';
}
function allBindingTags(){return [...new Set((S.importSources||[]).map(source=>String(source.tag_prefix||'').trim()).filter(Boolean))]}
function toggleBindingTag(tag,checked){
  tag=String(tag||'').trim();if(!tag)return;
  if(checked)S.selectedBindingTags.add(tag);else S.selectedBindingTags.delete(tag);
  const count=qs('#bindingTagCount');if(count)count.textContent=`已选 ${S.selectedBindingTags.size}`;
  const button=qs('[data-binding-selected]');if(button)button.disabled=!S.selectedBindingTags.size||!!S.activeTagBindingJob||!!S.activeRefreshJob;
}
function openTagBindingDialog(tags,scope='selected'){
  const available=new Set(allBindingTags());
  tags=[...new Set((tags||[]).map(tag=>String(tag||'').trim()).filter(tag=>available.has(tag)))];
  if(!tags.length){toast('请先选择 Tag','err');return}
  S.pendingTagBindingTags=tags;
  const source=tags.length===1?(S.importSources||[]).find(item=>String(item.tag_prefix||'').trim()===tags[0]):null;
  const current=source?.chain_binding==='profile'?source.chain_profile_id:'';
  const options=S.chainProfiles.filter(profile=>profile.enabled).map(profile=>`<option value="${esc(profile.id)}" ${profile.id===current?'selected':''}>${esc(profile.name)}</option>`).join('');
  const title=scope==='all'?'设置全部 Tag 前置代理':tags.length===1?`设置 ${tags[0]} 的前置代理`:'设置所选 Tag 前置代理';
  openDialog(title,`<div class="field"><label for="tagBindingProfile">前置代理</label><select id="tagBindingProfile"><option value="">直连（解除前置代理）</option>${options}</select></div><div class="meta"><span class="pill green">${esc(tags.length)} 个 Tag</span>${source?.chain_binding==='mixed'?'<span class="pill amber">当前为混合绑定</span>':''}</div>`,{actions:'<button class="btn" type="button" onclick="closeDialog()">取消</button><button class="btn primary" id="applyTagBinding" type="button" onclick="submitTagBinding()">重新检测并应用</button>'});
}
async function submitTagBinding(){
  const tags=[...S.pendingTagBindingTags],profileID=qs('#tagBindingProfile')?.value||'';
  if(!tags.length)return;
  const policy=verificationPolicy('refresh');if(!verificationPolicyValid(policy))return;
  const button=qs('#applyTagBinding');if(button){button.disabled=true;button.textContent='正在创建任务'}
  try{
    const started=await api('/api/import/bindings',{method:'POST',body:JSON.stringify({tags,chain_profile_id:profileID,...policy})});
    S.activeTagBindingJob=started.job_id;localStorage.setItem(ACTIVE_TAG_BINDING_JOB_KEY,started.job_id);
    closeDialog();
    await pollTagBindingJob(started.job_id,true);
  }catch(err){if(button){button.disabled=false;button.textContent='重新检测并应用'}toast(err.message,'err')}
}
function tagBindingStatusLabel(status){return {waiting:'等待',preparing:'准备中',testing:'检测中',completed:'已应用',failed:'失败',skipped:'无需修改',canceled:'已取消',running:'进行中',partial:'部分完成'}[status]||status||'未知'}
function openTagBindingProgress(job){
  openDialog('修改 Tag 前置代理','<div id="tagBindingProgress"></div>',{wide:true,actions:'<button class="btn danger" id="cancelTagBinding" type="button" onclick="cancelTagBindingJob()">终止任务</button><button class="btn" type="button" onclick="closeDialog()">关闭</button>'});
  const dialog=qs('#resultDialog');if(dialog)dialog.dataset.tagBindingShell='1';
  renderTagBindingProgress(job);
}
function renderTagBindingProgress(job){
  const host=qs('#tagBindingProgress');if(!host||qs('#resultDialog')?.dataset.tagBindingShell!=='1')return;
  const total=Number(job?.total_tags)||0,done=Number(job?.done_tags)||0,pct=total?Math.min(100,Math.round(done*100/total)):0;
  const rows=(job?.items||[]).map(item=>`<div class="binding-progress-row"><strong>${esc(item.tag_prefix)}</strong><div class="meta"><span class="pill ${item.status==='completed'||item.status==='skipped'?'green':item.status==='failed'||item.status==='canceled'?'red':'amber'}">${esc(tagBindingStatusLabel(item.status))}</span>${item.total?`<span class="value-pill muted">${esc((item.passed||0)+(item.failed||0))}/${esc(item.total)}</span>`:''}</div>${item.error?`<div class="muted" style="grid-column:1/-1">${esc(item.error)}</div>`:''}</div>`).join('');
  host.innerHTML=`<div class="binding-progress"><div class="progress-bar"><div class="progress-fill" style="width:${pct}%"></div></div><div class="meta"><span>Tag ${done}/${total}</span><span class="pill green">成功 ${esc(job?.successful||0)}</span><span class="pill red">失败 ${esc(job?.failed||0)}</span><span class="pill">无需修改 ${esc(job?.skipped||0)}</span></div>${job?.error?`<div class="inline-status bad">${esc(job.error)}</div>`:''}<div class="binding-progress-list">${rows}</div></div>`;
  const cancel=qs('#cancelTagBinding');if(cancel)cancel.disabled=job?.status!=='running';
}
async function pollTagBindingJob(jobID,openProgress=false){
  if(!jobID||S.tagBindingPollingJob===jobID)return;
  S.tagBindingPollingJob=jobID;S.activeTagBindingJob=jobID;localStorage.setItem(ACTIVE_TAG_BINDING_JOB_KEY,jobID);
  let last=null;
  try{
    while(true){
      last=await api('/api/import/bindings/jobs/'+encodeURIComponent(jobID));S.tagBindingJob=last;
      if(openProgress){openTagBindingProgress(last);openProgress=false}else renderTagBindingProgress(last);
      if(last.status!=='running')break;
      await new Promise(resolve=>setTimeout(resolve,600));
    }
    S.activeTagBindingJob='';localStorage.removeItem(ACTIVE_TAG_BINDING_JOB_KEY);
    const failed=Number(last.failed)||0;
    toast(failed?`前置代理修改完成，${failed} 个 Tag 保留原绑定`:'前置代理修改完成',failed?'err':'ok');
    await Promise.all([loadImportSources(),loadImportSummary()]);
    if(S.page==='settings')renderSettings();
  }finally{
    S.tagBindingPollingJob='';
    if(last?.status&&last.status!=='running'){S.activeTagBindingJob='';localStorage.removeItem(ACTIVE_TAG_BINDING_JOB_KEY)}
  }
  return last;
}
async function cancelTagBindingJob(){
  const id=S.activeTagBindingJob||localStorage.getItem(ACTIVE_TAG_BINDING_JOB_KEY);if(!id)return;
  try{const job=await api('/api/import/bindings/jobs/'+encodeURIComponent(id),{method:'DELETE'});S.tagBindingJob=job;renderTagBindingProgress(job);toast('已发送终止请求')}catch(err){toast(err.message,'err')}
}
function arrangeSettingsLayout(){
  const grid=qs('#view .settings-grid');if(!grid)return;
  const sections=qsa('#view [data-settings-column]');if(!sections.length)return;
  const columns=document.createElement('div');columns.className='settings-columns';
  const left=document.createElement('div'),right=document.createElement('div');
  left.className='settings-column';right.className='settings-column';columns.append(left,right);grid.insertBefore(columns,grid.firstChild);
  for(const section of sections)(section.dataset.settingsColumn==='left'?left:right).appendChild(section);
}
function allowedProbeTarget(value){
  const allowed=['https://www.gstatic.com/generate_204','https://cp.cloudflare.com/generate_204'];
  return allowed.includes(String(value||'').trim())?String(value).trim():allowed[0];
}
async function saveSettings(){
  const current=S.settings||{},currentListener=current.listener||{},currentMulti=current.multi_port||{},currentMgmt=current.management||{},currentGeo=current.geoip||{};
  const body={
    mode:qs('#setMode').value,
    external_ip:qs('#setExternal').value.trim(),
    probe_target:qs('#setProbe').value.trim(),
    skip_cert_verify:qs('#setSkip').checked,
    listener:{address:qs('#setListenAddr').value.trim(),port:Number(qs('#setListenPort').value)||0,username:currentListener.username||'',password:currentListener.password||''},
    multi_port:{address:qs('#setMultiAddr').value.trim(),base_port:Number(qs('#setMultiPort').value)||0,username:qs('#setMultiUser').value,password:qs('#setMultiPass').value},
    pool:{mode:qs('#setPoolMode').value.trim()||'rotate',failure_threshold:Number(qs('#setFail').value)||2,blacklist_duration:secondsDuration(qs('#setBlacklist').value,600),rotation_interval:secondsDuration(qs('#setRotateInterval').value,120)},
    management:{listen:qs('#setMgmtListen').value.trim(),password:currentMgmt.password||''},
    geoip:{
      enabled:!!currentGeo.enabled,
      database_path:currentGeo.database_path||'',
      listen:currentGeo.listen||'',
      port:currentGeo.port||0,
      auto_update_enabled:!!currentGeo.auto_update_enabled,
      auto_update_interval:currentGeo.auto_update_interval||''
    }
  };
  try{
    const res=await api('/api/settings',{method:'PUT',body:JSON.stringify(body)});
    toast(res?.message||'设置已保存');
    await api('/api/reload',{method:'POST'});
    await refresh();
  }catch(err){toast(err.message,'err')}
}
function durationSeconds(value,fallback){
  value=String(value||'').trim();
  if(!value)return fallback;
  if(/^\d+(\.\d+)?$/.test(value))return Math.max(1,Math.round(Number(value)));
  const re=/(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/g;
  let total=0,matched=false,m;
  while((m=re.exec(value))){
    matched=true;
    const n=Number(m[1]);
    const unit=m[2];
    if(unit==='h')total+=n*3600;
    else if(unit==='m')total+=n*60;
    else if(unit==='s')total+=n;
    else if(unit==='ms')total+=n/1000;
  }
  if(!matched || !Number.isFinite(total) || total<=0)return fallback;
  return Math.max(1,Math.round(total));
}
function secondsDuration(value,fallback){
  const n=Math.max(1,Math.round(Number(value)||fallback));
  return `${n}s`;
}
function durationParts(v){
  v=String(v||'24h');
  let days=0,hours=0,minutes=0;
  const h=v.match(/(\d+)h/),m=v.match(/(\d+)m/);
  if(h){hours=Number(h[1])||0;days=Math.floor(hours/24);hours=hours%24}
  if(m)minutes=Number(m[1])||0;
  if(!h&&!m){days=1}
  if(days===0&&hours===0&&minutes===0)days=1;
  return {days,hours,minutes};
}
function subscriptionInterval(){
  const d=Number(qs('#subDays')?.value)||0,h=Number(qs('#subHours')?.value)||0,m=Number(qs('#subMinutes')?.value)||0;
  const totalMinutes=d*24*60+h*60+m;
  if(totalMinutes<=0)return '24h';
  if(totalMinutes%60===0)return `${Math.floor(totalMinutes/60)}h`;
  const hh=Math.floor(totalMinutes/60),mm=totalMinutes%60;
  return `${hh}h${mm}m`;
}
async function saveAutoRefresh(){
  const policy=verificationPolicy('refresh');
  if(!verificationPolicyValid(policy))return;
  const body=subscriptionConfigPayload({enabled:qs('#subEnabled').checked,interval:subscriptionInterval(),refresh:false});
  try{S.subConfig=await api('/api/subscription/config',{method:'PUT',body:JSON.stringify(body)});toast('订阅自动刷新设置已保存');renderSettings()}catch(err){toast(err.message,'err')}
}
