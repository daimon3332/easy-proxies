async function reloadCore(){
  try{await api('/api/reload',{method:'POST'});openDialog('重载完成','<p>核心已重载。</p>')}catch(err){openDialog('重载失败',`<p>${esc(err.message)}</p>`)}
}
function openNodeExportDialog(ids){
  ids=Array.isArray(ids)?ids.filter(Boolean):[];
  if(!ids.length){toast('请先选择节点','err');return}
  S.pendingExportNodeIDs=ids;
  openDialog('导出节点',`<p>将导出 ${ids.length} 个节点，请选择格式。</p><div class="row"><button class="btn secondary" onclick="exportPendingNodes('uri')">URI</button><button class="btn secondary" onclick="exportPendingNodes('yaml')">YAML</button><button class="btn secondary" onclick="exportPendingNodes('base64')">Base64</button></div>`,{actions:'<button class="btn" type="button" onclick="closeDialog()">取消</button>'});
}
async function exportPendingNodes(format){
  const ids=S.pendingExportNodeIDs||[];
  closeDialog();
  await exportNodes(format,ids,false);
}
async function exportNodes(format,ids=[],all=false){
  try{
    await downloadRequest('/api/data-export/nodes',{all,node_ids:ids,format});
    toast(`节点已导出为 ${String(format).toUpperCase()}`);
  }catch(err){openDialog('导出失败',`<p>${esc(err.message)}</p>`)}
}
function updateExportTagControls(){
  const count=S.selectedExportTags.size;
  const total=qsa('[data-export-tag]').length;
  const badge=qs('#exportTagCount');
  if(badge)badge.textContent=`已选 ${count} / ${total}`;
  qsa('[data-tag-export-selected]').forEach(button=>{button.disabled=count===0;button.textContent=`导出选中 (${count})`});
  const clear=qs('#clearExportTags');
  if(clear)clear.disabled=count===0;
}
function toggleExportTag(tag,checked){
  tag=String(tag||'').trim();
  if(!tag)return;
  if(checked)S.selectedExportTags.add(tag);else S.selectedExportTags.delete(tag);
  updateExportTagControls();
}
function setAllExportTags(selected){
  qsa('[data-export-tag]').forEach(input=>{
    input.checked=selected;
    const tag=String(input.dataset.tag||'').trim();
    if(selected&&tag)S.selectedExportTags.add(tag);else S.selectedExportTags.delete(tag);
  });
  updateExportTagControls();
}
async function exportSelectedTags(){
  await exportSourceTags([...S.selectedExportTags]);
}
async function exportSourceTags(tags){
  tags=(tags||[]).map(tag=>String(tag||'').trim()).filter(Boolean);
  if(!tags.length){toast('请先选择至少一个 Tag','err');return}
  try{
    await downloadRequest('/api/data-export/tags',{tags});
    toast(tags.length===1?'Tag 数据已导出':`${tags.length} 个 Tag 已打包导出`);
  }catch(err){openDialog('导出失败',`<p>${esc(err.message)}</p>`)}
}
async function downloadLocalBackup(){
  try{await downloadResponse('/api/backup/local');toast('完整备份已导出')}catch(err){openDialog('备份失败',`<p>${esc(err.message)}</p>`)}
}
async function restoreLocalBackup(file){
  if(!file)return;
  if(!confirm(`确认恢复备份 ${file.name}？\n\n当前设置、订阅、节点和节点池状态将被替换。`)){qs('#localBackupFile').value='';return}
  const form=new FormData();form.append('file',file);
  try{
    const res=await api('/api/backup/local/restore',{method:'POST',body:form});
    const result=res.result||{};
    const restart=result.restart_required?'<p><strong>部分管理监听或日志设置需要重启程序后完全生效。</strong></p>':'';
    openDialog('恢复完成',`<p>节点 ${esc(result.node_count||0)} 个，池内 ${esc(result.pool_node_count||0)} 个，订阅 ${esc(result.subscriptions||0)} 个。</p>${restart}`);
    await refresh();
  }catch(err){openDialog('恢复失败',`<p>${esc(err.message)}</p>`)}
  finally{const input=qs('#localBackupFile');if(input)input.value=''}
}
function webDAVPayload(){
  return {address:qs('#webdavAddress')?.value.trim()||'',username:qs('#webdavUsername')?.value||'',password:qs('#webdavPassword')?.value||'',folder:qs('#webdavFolder')?.value.trim()||'/easy_proxies'};
}
async function saveWebDAV(silent=false){
  const payload=webDAVPayload();
  const res=await api('/api/backup/webdav/settings',{method:'PUT',body:JSON.stringify(payload)});
  S.webdav=res.webdav||payload;
  if(!silent)toast(res.message||'WebDAV 设置已保存');
  return payload;
}
async function testWebDAV(){
  try{await saveWebDAV(true);const res=await api('/api/backup/webdav/test',{method:'POST'});await loadWebDAVFiles(undefined,true);toast(`${res.message||'WebDAV 连接成功'}，发现 ${S.webdavFiles.length} 个备份`)}catch(err){openDialog('连接失败',`<p>${esc(err.message)}</p>`)}
}
async function createWebDAVBackup(){
  try{await saveWebDAV(true);const res=await api('/api/backup/webdav/create',{method:'POST'});toast(res.message||'WebDAV 备份完成');await loadWebDAVFiles()}catch(err){openDialog('备份失败',`<p>${esc(err.message)}</p>`)}
}
async function downloadWebDAVBackup(name){
  try{await downloadResponse(`/api/backup/webdav/${encodeURIComponent(name)}/download`);toast('远程备份已下载')}catch(err){openDialog('下载失败',`<p>${esc(err.message)}</p>`)}
}
async function restoreWebDAVBackup(name){
  if(!confirm(`确认恢复远程备份 ${name}？\n\n当前设置、订阅、节点和节点池状态将被替换。`))return;
  try{
    const res=await api(`/api/backup/webdav/${encodeURIComponent(name)}/restore`,{method:'POST'});
    const result=res.result||{};
    const restart=result.restart_required?' 部分设置需要重启程序后完全生效。':'';
    openDialog('恢复完成',`<p>节点 ${esc(result.node_count||0)} 个，池内 ${esc(result.pool_node_count||0)} 个，订阅 ${esc(result.subscriptions||0)} 个。${esc(restart)}</p>`);
    await refresh();
  }catch(err){openDialog('恢复失败',`<p>${esc(err.message)}</p>`)}
}
async function deleteWebDAVBackup(name){
  if(!confirm(`确认删除远程备份 ${name}？`))return;
  try{await api(`/api/backup/webdav/${encodeURIComponent(name)}/delete`,{method:'DELETE'});toast('远程备份已删除');await loadWebDAVFiles()}catch(err){openDialog('删除失败',`<p>${esc(err.message)}</p>`)}
}
async function downloadRequest(path,body){
  const headers={'Content-Type':'application/json'};
  if(S.token)headers.Authorization='Bearer '+S.token;
  const res=await fetch(path,{method:'POST',headers,body:JSON.stringify(body)});
  await consumeDownload(res);
}
async function downloadResponse(path){
  const headers=S.token?{Authorization:'Bearer '+S.token}:{};
  const res=await fetch(path,{headers});
  await consumeDownload(res);
}
async function consumeDownload(res){
  if(!res.ok){
    const data=await res.json().catch(()=>({}));
    throw new Error(data.error||`请求失败: ${res.status}`);
  }
  const blob=await res.blob();
  const disposition=res.headers.get('content-disposition')||'';
  const match=disposition.match(/filename="?([^";]+)"?/i);
  const name=match?.[1]||'easy_proxies_export';
  const a=document.createElement('a');
  a.href=URL.createObjectURL(blob);a.download=name;a.click();
  setTimeout(()=>URL.revokeObjectURL(a.href),1000);
}
function formatBytes(value){
  const bytes=Number(value)||0;
  if(bytes<1024)return `${bytes} B`;
  if(bytes<1024*1024)return `${(bytes/1024).toFixed(1)} KiB`;
  return `${(bytes/1024/1024).toFixed(1)} MiB`;
}
async function downloadExport(){
  try{
    const headers=S.token?{Authorization:'Bearer '+S.token}:{};
    const res=await fetch('/api/export?scheme=all',{headers});
    if(!res.ok)throw new Error('导出失败: '+res.status);
    const text=await res.text();
    const blob=new Blob([text],{type:'text/plain;charset=utf-8'});
    const a=document.createElement('a');a.href=URL.createObjectURL(blob);a.download='easy-proxies-export.txt';a.click();URL.revokeObjectURL(a.href);
    openDialog('导出完成','<p>已生成导出文件。</p>');
  }catch(err){openDialog('导出失败',`<p>${esc(err.message)}</p>`)}
}
