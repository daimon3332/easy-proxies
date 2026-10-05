function renderConfig(){
  qs('#view').innerHTML=`<div class="grid">
    <div class="card"><div class="card-head"><h2>手动节点（高级）</h2></div><div class="card-body"><div class="list">${S.configNodes.length?S.configNodes.map(renderConfigNode).join(''):'<div class="empty">没有手动节点</div>'}</div></div></div>
    <div class="card"><div class="card-head"><h2>添加 / 更新</h2></div><div class="card-body stack">
      <div class="field"><label>名称</label><input id="cfgName"></div>
      <div class="field"><label>URI</label><textarea id="cfgUri"></textarea></div>
      <div class="field"><label>端口（0 自动）</label><input id="cfgPort" type="number" value="0"></div>
      <button class="btn primary" onclick="saveConfigNode()">保存节点</button>
      <p class="muted">保存后点击顶部“重载核心”生效。</p>
    </div></div>
  </div>`;
}
function renderConfigNode(n){
  return `<article class="node"><div class="node-main"><div><div class="node-name">${esc(n.name)}</div><div class="node-uri">${esc(n.uri)}</div></div>${n.port?`<span class="pill">端口 ${n.port}</span>`:''}</div><div class="row"><button class="btn small" onclick='editConfigNode(${JSON.stringify(n).replace(/'/g,"&#39;")})'>编辑</button><button class="btn small danger" onclick="deleteConfigNode('${esc(n.name)}')">删除</button></div></article>`;
}
function editConfigNode(n){qs('#cfgName').value=n.name||'';qs('#cfgUri').value=n.uri||'';qs('#cfgPort').value=n.port||0}
async function saveConfigNode(){
  const name=qs('#cfgName').value.trim(),uri=qs('#cfgUri').value.trim(),port=Number(qs('#cfgPort').value)||0;
  if(!uri){toast('URI 不能为空','err');return}
  const exists=S.configNodes.some(n=>n.name===name);
  try{
    if(exists)await api('/api/nodes/config/'+encodeURIComponent(name),{method:'PUT',body:JSON.stringify({name,uri,port})});
    else await api('/api/nodes/config',{method:'POST',body:JSON.stringify({name,uri,port})});
    toast('手动节点已保存');await loadConfigNodes();renderConfig();
  }catch(err){toast(err.message,'err')}
}
async function deleteConfigNode(name){
  if(!confirm('删除手动节点 '+name+' ?'))return;
  try{await api('/api/nodes/config/'+encodeURIComponent(name),{method:'DELETE'});toast('已删除');await loadConfigNodes();renderConfig()}catch(err){toast(err.message,'err')}
}
