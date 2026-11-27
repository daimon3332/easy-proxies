function renderConfig(){
  qs('#view').innerHTML=`<div class="grid">
    <div class="card"><div class="card-head"><h2>Manuel düğümler (gelişmiş)</h2></div><div class="card-body"><div class="list">${S.configNodes.length?S.configNodes.map(renderConfigNode).join(''):'<div class="empty">Manuel düğüm yok</div>'}</div></div></div>
    <div class="card"><div class="card-head"><h2>Ekle / güncelle</h2></div><div class="card-body stack">
      <div class="field"><label>Ad</label><input id="cfgName"></div>
      <div class="field"><label>URI</label><textarea id="cfgUri"></textarea></div>
      <div class="field"><label>Port (0 = otomatik)</label><input id="cfgPort" type="number" value="0"></div>
      <button class="btn primary" onclick="saveConfigNode()">Düğümü kaydet</button>
      <p class="muted">Değişiklikleri etkinleştirmek için kaydettikten sonra üst menüden “Çekirdeği yeniden yükle” seçeneğine tıklayın.</p>
    </div></div>
  </div>`;
}
function renderConfigNode(n){
  return `<article class="node"><div class="node-main"><div><div class="node-name">${esc(n.name)}</div><div class="node-uri">${esc(n.uri)}</div></div>${n.port?`<span class="pill">Port ${n.port}</span>`:''}</div><div class="row"><button class="btn small" onclick='editConfigNode(${JSON.stringify(n).replace(/'/g,"&#39;")})'>Düzenle</button><button class="btn small danger" onclick="deleteConfigNode('${esc(n.name)}')">Sil</button></div></article>`;
}
function editConfigNode(n){qs('#cfgName').value=n.name||'';qs('#cfgUri').value=n.uri||'';qs('#cfgPort').value=n.port||0}
async function saveConfigNode(){
  const name=qs('#cfgName').value.trim(),uri=qs('#cfgUri').value.trim(),port=Number(qs('#cfgPort').value)||0;
  if(!uri){toast('URI boş olamaz','err');return}
  const exists=S.configNodes.some(n=>n.name===name);
  try{
    if(exists)await api('/api/nodes/config/'+encodeURIComponent(name),{method:'PUT',body:JSON.stringify({name,uri,port})});
    else await api('/api/nodes/config',{method:'POST',body:JSON.stringify({name,uri,port})});
    toast('Manuel düğüm kaydedildi');await loadConfigNodes();renderConfig();
  }catch(err){toast(err.message,'err')}
}
async function deleteConfigNode(name){
  if(!confirm(name+' adlı manuel düğüm silinsin mi?'))return;
  try{await api('/api/nodes/config/'+encodeURIComponent(name),{method:'DELETE'});toast('Düğüm silindi');await loadConfigNodes();renderConfig()}catch(err){toast(err.message,'err')}
}
