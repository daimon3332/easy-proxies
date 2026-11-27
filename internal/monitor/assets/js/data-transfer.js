async function reloadCore(){
  try{await api('/api/reload',{method:'POST'});openDialog('Yeniden yükleme tamamlandı','<p>Proxy çekirdeği yeniden yüklendi.</p>')}catch(err){openDialog('Yeniden yükleme başarısız',`<p>${esc(err.message)}</p>`)}
}
function openNodeExportDialog(ids){
  ids=Array.isArray(ids)?ids.filter(Boolean):[];
  if(!ids.length){toast('Önce düğüm seçin','err');return}
  S.pendingExportNodeIDs=ids;
  openDialog('Düğümleri dışa aktar',`<p>${ids.length} düğüm dışa aktarılacak. Dosya biçimini seçin.</p><div class="row"><button class="btn secondary" onclick="exportPendingNodes('uri')">URI</button><button class="btn secondary" onclick="exportPendingNodes('yaml')">YAML</button><button class="btn secondary" onclick="exportPendingNodes('base64')">Base64</button></div>`,{actions:'<button class="btn" type="button" onclick="closeDialog()">Vazgeç</button>'});
}
async function exportPendingNodes(format){
  const ids=S.pendingExportNodeIDs||[];
  closeDialog();
  await exportNodes(format,ids,false);
}
async function exportNodes(format,ids=[],all=false){
  try{
    await downloadRequest('/api/data-export/nodes',{all,node_ids:ids,format});
    toast(`Düğümler ${String(format).toUpperCase()} biçiminde dışa aktarıldı`);
  }catch(err){openDialog('Dışa aktarma başarısız',`<p>${esc(err.message)}</p>`)}
}
function updateExportTagControls(){
  const count=S.selectedExportTags.size;
  const total=qsa('[data-export-tag]').length;
  const badge=qs('#exportTagCount');
  if(badge)badge.textContent=`Seçilen ${count} / ${total}`;
  qsa('[data-tag-export-selected]').forEach(button=>{button.disabled=count===0;button.textContent=`Seçilenleri dışa aktar (${count})`});
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
  if(!tags.length){toast('En az bir etiket seçin','err');return}
  try{
    await downloadRequest('/api/data-export/tags',{tags});
    toast(tags.length===1?'Etiket verisi dışa aktarıldı':`${tags.length} etiket paketlenerek dışa aktarıldı`);
  }catch(err){openDialog('Dışa aktarma başarısız',`<p>${esc(err.message)}</p>`)}
}
async function downloadLocalBackup(){
  try{await downloadResponse('/api/backup/local');toast('Tam yedek dışa aktarıldı')}catch(err){openDialog('Yedekleme başarısız',`<p>${esc(err.message)}</p>`)}
}
async function restoreLocalBackup(file){
  if(!file)return;
  if(!confirm(`${file.name} yedeği geri yüklensin mi?\n\nMevcut ayarlar, abonelikler, düğümler ve proxy havuzu durumu yedekteki verilerle değiştirilecek.`)){qs('#localBackupFile').value='';return}
  const form=new FormData();form.append('file',file);
  try{
    const res=await api('/api/backup/local/restore',{method:'POST',body:form});
    const result=res.result||{};
    const restart=result.restart_required?'<p><strong>Bazı yönetim dinleme veya günlük ayarlarının tam uygulanması için programı yeniden başlatın.</strong></p>':'';
    openDialog('Geri yükleme tamamlandı',`<p>Düğüm: ${esc(result.node_count||0)} · Havuzda: ${esc(result.pool_node_count||0)} · Abonelik: ${esc(result.subscriptions||0)}</p>${restart}`);
    await refresh();
  }catch(err){openDialog('Geri yükleme başarısız',`<p>${esc(err.message)}</p>`)}
  finally{const input=qs('#localBackupFile');if(input)input.value=''}
}
function webDAVPayload(){
  return {address:qs('#webdavAddress')?.value.trim()||'',username:qs('#webdavUsername')?.value||'',password:qs('#webdavPassword')?.value||'',folder:qs('#webdavFolder')?.value.trim()||'/easy_proxies'};
}
async function saveWebDAV(silent=false){
  const payload=webDAVPayload();
  const res=await api('/api/backup/webdav/settings',{method:'PUT',body:JSON.stringify(payload)});
  S.webdav=res.webdav||payload;
  if(!silent)toast(res.message||'WebDAV ayarları kaydedildi');
  return payload;
}
async function testWebDAV(){
  try{await saveWebDAV(true);const res=await api('/api/backup/webdav/test',{method:'POST'});await loadWebDAVFiles(undefined,true);toast(`${res.message||'WebDAV bağlantısı başarılı'} · ${S.webdavFiles.length} yedek bulundu`)}catch(err){openDialog('Bağlantı başarısız',`<p>${esc(err.message)}</p>`)}
}
async function createWebDAVBackup(){
  try{await saveWebDAV(true);const res=await api('/api/backup/webdav/create',{method:'POST'});toast(res.message||'WebDAV yedeklemesi tamamlandı');await loadWebDAVFiles()}catch(err){openDialog('Yedekleme başarısız',`<p>${esc(err.message)}</p>`)}
}
async function downloadWebDAVBackup(name){
  try{await downloadResponse(`/api/backup/webdav/${encodeURIComponent(name)}/download`);toast('Uzak yedek indirildi')}catch(err){openDialog('İndirme başarısız',`<p>${esc(err.message)}</p>`)}
}
async function restoreWebDAVBackup(name){
  if(!confirm(`${name} uzak yedeği geri yüklensin mi?\n\nMevcut ayarlar, abonelikler, düğümler ve proxy havuzu durumu yedekteki verilerle değiştirilecek.`))return;
  try{
    const res=await api(`/api/backup/webdav/${encodeURIComponent(name)}/restore`,{method:'POST'});
    const result=res.result||{};
    const restart=result.restart_required?' Bazı ayarların tam uygulanması için programı yeniden başlatın.':'';
    openDialog('Geri yükleme tamamlandı',`<p>Düğüm: ${esc(result.node_count||0)} · Havuzda: ${esc(result.pool_node_count||0)} · Abonelik: ${esc(result.subscriptions||0)}.${esc(restart)}</p>`);
    await refresh();
  }catch(err){openDialog('Geri yükleme başarısız',`<p>${esc(err.message)}</p>`)}
}
async function deleteWebDAVBackup(name){
  if(!confirm(`${name} uzak yedeği silinsin mi?`))return;
  try{await api(`/api/backup/webdav/${encodeURIComponent(name)}/delete`,{method:'DELETE'});toast('Uzak yedek silindi');await loadWebDAVFiles()}catch(err){openDialog('Silme başarısız',`<p>${esc(err.message)}</p>`)}
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
    throw new Error(data.error||`İstek başarısız: ${res.status}`);
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
    if(!res.ok)throw new Error('Dışa aktarma başarısız: '+res.status);
    const text=await res.text();
    const blob=new Blob([text],{type:'text/plain;charset=utf-8'});
    const a=document.createElement('a');a.href=URL.createObjectURL(blob);a.download='easy-proxies-export.txt';a.click();URL.revokeObjectURL(a.href);
    openDialog('Dışa aktarma tamamlandı','<p>Dışa aktarma dosyası oluşturuldu.</p>');
  }catch(err){openDialog('Dışa aktarma başarısız',`<p>${esc(err.message)}</p>`)}
}
