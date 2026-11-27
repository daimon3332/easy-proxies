function importSourceType(s){
  if(s?.mode==='url')return 'Abonelik bağlantısı';
  const f=String(s?.format||'');
  if(f.includes('base64'))return 'Base64';
  if(f.includes('clash'))return 'Clash YAML';
  if(f.includes('uri'))return 'URI';
  return s?.mode==='content'?'İçerik içe aktarma':'İçe aktarma kaynağı';
}
function importSourceText(s){
  if(s?.mode==='url')return s.source||'';
  const f=String(s?.format||'');
  if(f.includes('base64'))return 'Base64 içerik içe aktarma';
  if(f.includes('clash'))return 'Clash YAML içerik içe aktarma';
  if(f.includes('uri'))return 'URI listesi içe aktarma';
  return s?.source&&s.source!=='content'?s.source:'İçerik içe aktarma';
}
function fmtDate(v){
  if(!v)return '-';
  const d=new Date(v);
  return Number.isNaN(d.getTime())?'-':d.toLocaleString('tr-TR');
}
function webDAVFilesHTML(files,loading){
  if(loading)return '<div class="empty">Uzak yedekler yükleniyor...</div>';
  if(!files.length)return '<div class="empty">Uzak yedek bulunamadı</div>';
  return files.map(file=>`<div class="backup-file"><div class="backup-file-main"><strong>${esc(file.name)}</strong><span class="muted">${esc(formatBytes(file.size))} · ${esc(fmtDate(file.mod_time))}</span></div><div class="row"><button class="btn small" data-name="${esc(file.name)}" onclick="downloadWebDAVBackup(this.dataset.name)">İndir</button><button class="btn small secondary" data-name="${esc(file.name)}" onclick="restoreWebDAVBackup(this.dataset.name)">Geri yükle</button><button class="btn small danger" data-name="${esc(file.name)}" onclick="deleteWebDAVBackup(this.dataset.name)">Sil</button></div></div>`).join('');
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
  qs('#chainProfileTitle').textContent=id?'Ön proxy profilini düzenle':'Ön proxy profili ekle';
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
  if(!profile.name)return 'Ön proxy profili için bir ad girin';
  if(!profile.hops[0].uri)return 'Düğüm URI adresini girin';
  if(!/^[a-z][a-z0-9+.-]*:\/\//i.test(profile.hops[0].uri))return 'Düğüm URI bir protokol içermeli (ör. vless:// veya socks5://)';
  return '';
}
function setChainEditorStatus(message,type=''){const host=qs('#chainEditStatus');if(!host)return;host.textContent=message||'';host.className='inline-status '+(type||'')}
function setChainEditorBusy(busy,action=''){
  for(const id of ['chainTestButton','chainSaveButton'])if(qs('#'+id))qs('#'+id).disabled=!!busy;
  if(qs('#chainTestButton'))qs('#chainTestButton').textContent=busy&&action==='test'?'Test ediliyor':'Bağlantıyı test et';
  if(qs('#chainSaveButton'))qs('#chainSaveButton').textContent=busy&&action==='save'?'Kaydediliyor':'Kaydet';
}
async function testChainProfileDraft(){
  const profile=chainProfileDraft(),error=validateChainProfileDraft(profile);if(error){setChainEditorStatus(error,'bad');return false}
  setChainEditorBusy(true,'test');setChainEditorStatus('Ön proxy bağlantısı kuruluyor...');
  try{const result=await api('/api/chain-profiles/test',{method:'POST',body:JSON.stringify({profile})});setChainEditorStatus(`Bağlantı başarılı, gecikme ${result.latency_ms||0} ms`,'good');return true}catch(err){setChainEditorStatus(err.message,'bad');return false}finally{setChainEditorBusy(false)}
}
async function saveChainProfileDraft(){
  const id=qs('#chainEditID')?.value||'',name=qs('#chainEditName')?.value.trim()||'',uri=qs('#chainEditURI')?.value.trim()||'';
  const profile={id,name,enabled:!!qs('#chainEditEnabled')?.checked,hops:[{uri}]},error=validateChainProfileDraft(profile);
  if(error){setChainEditorStatus(error,'bad');return}
  const usage=S.chainUsage?.[id]||{nodes:0,ports:0};
  const cascade=!!(id&&usage.nodes&&!profile.enabled);
  const oldProfile=S.chainProfiles.find(item=>item.id===id),routeChanged=oldProfile?.hops?.[0]?.uri!==uri;
  const impact=cascade?`Devre dışı bırakma, ilişkili ${usage.nodes} zincir düğümünü ve ${usage.ports||0} portu da geri alınamaz biçimde silecek.`:routeChanged?`${usage.nodes} zincir düğümü ve ${usage.ports||0} port etkilenecek; kaydettiğinizde otomatik olarak yeniden test edilecek.`:`Yalnızca ad değişecek; ilişkili düğümlerde kesinti veya yeniden test olmayacak.`;
  if(id&&usage.nodes&&!confirm(`“${name}” ön proxy profili değiştirilsin mi?\n\n${impact}`))return;
  const next=S.chainProfiles.map(profile=>({...profile,hops:(profile.hops||[]).map(hop=>({...hop}))}));
  const index=next.findIndex(item=>item.id===id&&id);
  if(index>=0)next[index]=profile;else next.push(profile);
  setChainEditorBusy(true,'save');setChainEditorStatus('Doğrulanıyor ve kaydediliyor...');
  try{
    const data=await api('/api/chain-profiles',{method:'PUT',body:JSON.stringify({profiles:next,cascade})});
    S.chainProfiles=data.profiles||[];S.chainUsage=data.usage||{};
    const saved=S.chainProfiles.find(item=>item.id===id)||(S.chainProfiles.find(item=>item.name===name&&item.hops?.[0]?.uri===uri));
    if(S.chainEditorOrigin==='import'&&saved?.enabled)selectImportChain(saved.id);
    qs('#chainProfileModal').classList.remove('show');
    toast(data.retest_job_id?'Ön proxy kaydedildi; ilişkili düğümler yeniden test ediliyor':'Ön proxy kaydedildi');
    if(S.page==='settings')renderSettings();else if(S.page==='import')renderImport();
  }catch(err){setChainEditorStatus(err.message,'bad')}finally{setChainEditorBusy(false)}
}
async function deleteChainProfile(id){
  const profile=S.chainProfiles.find(item=>item.id===id);if(!profile)return;
  const usage=S.chainUsage?.[id]||{nodes:0,ports:0};
  const warning=usage.nodes?`\n\nBu işlem ilişkili ${usage.nodes} zincir düğümünü ve ${usage.ports||0} portu da geri alınamaz biçimde silecek.`:'';
  if(!confirm(`“${profile.name}” ön proxy profili silinsin mi?${warning}`))return;
  try{const data=await api('/api/chain-profiles',{method:'PUT',body:JSON.stringify({profiles:S.chainProfiles.filter(item=>item.id!==id),cascade:usage.nodes>0})});S.chainProfiles=data.profiles||[];S.chainUsage=data.usage||{};if(S.chainProfileID===id)selectImportChain('');toast(data.deleted_nodes?`Ön proxy ve ilişkili ${data.deleted_nodes} düğüm silindi`:'Ön proxy silindi');renderSettings()}catch(err){toast(err.message,'err')}
}
async function testChainProfile(id){
  const button=document.querySelector(`[data-chain-test="${CSS.escape(id)}"]`),old=button?.textContent;if(button){button.disabled=true;button.textContent='Test ediliyor'}
  try{const result=await api('/api/chain-profiles/test',{method:'POST',body:JSON.stringify({id})});toast(`Ön proxy kullanılabilir, gecikme ${result.latency_ms||0} ms`)}catch(err){toast(err.message,'err')}finally{if(button){button.disabled=false;button.textContent=old}}
}
function chainProfilesHTML(){
  if(!S.chainProfiles.length)return '<div class="connectivity-idle"><span>Henüz ön proxy yapılandırılmadı</span><button class="btn small secondary" onclick="openChainProfileEditor(\'\', \'settings\')">Ön proxy ekle</button></div>';
  const rows=S.chainProfiles.map(profile=>{
    const disabled=profile.enabled?'':`<span class="pill amber">Devre dışı</span>`;
    return `<div class="chain-row"><div class="chain-main"><div class="row"><strong>${esc(profile.name)}</strong>${disabled}</div><code class="source-value">${esc(maskProxyURI(profile.hops?.[0]?.uri||''))}</code></div><div class="chain-row-actions"><button class="btn small green" data-chain-test="${esc(profile.id)}" data-id="${esc(profile.id)}" onclick="testChainProfile(this.dataset.id)">Test et</button><button class="btn small secondary" data-id="${esc(profile.id)}" onclick="openChainProfileEditor(this.dataset.id,'settings')">Düzenle</button><button class="btn small danger" data-id="${esc(profile.id)}" onclick="deleteChainProfile(this.dataset.id)">Sil</button></div></div>`;
  }).join('');
  return `<div class="chain-list">${rows}</div>`;
}
function chainBindingLabel(source){
  if(source?.chain_binding==='mixed')return {text:'Ön proxy: Karışık',className:'amber'};
  if(source?.chain_binding==='profile'&&source.chain_profile_id)return {text:`Ön proxy: ${chainProfileName(source.chain_profile_id)}`,className:'amber'};
  return {text:'Doğrudan bağlantı',className:'muted'};
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
          <h2>Ön proxy</h2>
          <button class="btn small primary" onclick="openChainProfileEditor('', 'settings')">Ekle</button>
        </div>
        <div class="settings-section-body">
          ${chainProfilesHTML()}
        </div>
      </section>

      <section class="settings-section" data-settings-column="right">
        <div class="settings-section-head">
          <h2>Genel çalışma ayarları</h2>
        </div>
        <div class="settings-section-body">
          <div class="settings-form">
            <div class="field"><label>Çalışma modu</label><select id="setMode"><option value="pool">Havuz</option><option value="multi-port">Çoklu port</option><option value="hybrid">Karma</option></select></div>
            <div class="field"><label>Test hedefi</label><select id="setProbe"><option value="https://www.gstatic.com/generate_204">Google bağlantı testi (204)</option><option value="https://cp.cloudflare.com/generate_204">Cloudflare bağlantı testi (204)</option></select></div>
            <div class="field"><label>Harici IP</label><input id="setExternal" value="${esc(s.external_ip||'')}" placeholder="Boş bırakılırsa mevcut erişim adresi kullanılır"></div>
            <div class="field"><label>Yönetim dinleme adresi</label><input id="setMgmtListen" value="${esc(management.listen||'')}"></div>
            <label class="toggle-row wide"><input id="setSkip" type="checkbox" ${s.skip_cert_verify?'checked':''} style="width:auto"><div><strong>Sertifika doğrulamasını atla</strong></div></label>
          </div>
        </div>
      </section>

      <section class="settings-section" data-settings-column="left">
        <div class="settings-section-head">
          <h2>Portlar ve dinleme</h2>
        </div>
        <div class="settings-section-body">
          <div class="settings-form">
            <div class="field"><label>Havuz dinleme adresi</label><input id="setListenAddr" value="${esc(listener.address||'')}"></div>
            <div class="field"><label>Havuz portu</label><input id="setListenPort" type="number" value="${esc(listener.port||0)}"></div>
            <div class="field"><label>Çoklu port adresi</label><input id="setMultiAddr" value="${esc(multi.address||'')}"></div>
            <div class="field"><label>İlk port</label><input id="setMultiPort" type="number" value="${esc(multi.base_port||0)}"></div>
            <div class="field"><label>Kullanıcı adı</label><input id="setMultiUser" value="${esc(multi.username||'')}"></div>
            <div class="field"><label>Parola</label><input id="setMultiPass" value="${esc(multi.password||'')}"></div>
          </div>
        </div>
      </section>

      <section class="settings-section" data-settings-column="right">
        <div class="settings-section-head">
          <h2>Proxy havuzu politikası</h2>
        </div>
        <div class="settings-section-body">
          <div class="settings-form">
            <div class="field"><label>Havuz seçim yöntemi</label><select id="setPoolMode"><option value="rotate">Döngüsel</option><option value="sequential">Sıralı</option><option value="random">Rastgele</option><option value="balance">Dengeli</option></select></div>
            <div class="field"><label>Başarısızlık eşiği</label><input id="setFail" type="number" min="1" value="${esc(pool.failure_threshold||2)}"></div>
            <div class="field"><label>Kara liste süresi (sn)</label><input id="setBlacklist" type="number" min="1" step="1" value="${esc(blacklistSeconds)}"></div>
            <div class="field"><label>Değişim aralığı (sn)</label><input id="setRotateInterval" type="number" min="1" step="1" value="${esc(rotationSeconds)}"></div>
          </div>
          <div class="settings-actions"><button class="btn primary" onclick="saveSettings()">Çalışma ayarlarını kaydet ve yeniden yükle</button></div>
        </div>
      </section>

      <section class="settings-section settings-wide">
        <div class="settings-section-head">
          <h2>Abonelik yenileme</h2>
          <span class="pill green">${esc((sub.subscriptions||[]).length)} abonelik</span>
        </div>
        <div class="settings-section-body">
          <div id="refreshStatusHost">${refreshStatusHTML(S.refreshJob)}</div>
          <label class="toggle-row"><input id="subEnabled" type="checkbox" ${sub.enabled!==false?'checked':''} style="width:auto"><div><strong>Otomatik yenilemeyi etkinleştir</strong></div></label>
          ${verificationPolicyHTML('refresh',sub.test_204!==false,new Set(sub.site_targets||[]),refreshing)}
          <div class="time-grid">
            <div class="field"><label>Gün</label><input id="subDays" type="number" min="0" value="${interval.days}"></div>
            <div class="field"><label>Saat</label><input id="subHours" type="number" min="0" max="23" value="${interval.hours}"></div>
            <div class="field"><label>Dakika</label><input id="subMinutes" type="number" min="0" max="59" value="${interval.minutes}"></div>
          </div>
          <button class="btn secondary block" onclick="saveAutoRefresh()">Otomatik yenileme ayarlarını kaydet</button>
        </div>
      </section>

      <section class="settings-section settings-wide">
        <div class="settings-section-head">
          <h2>İçe aktarma kaynakları</h2>
          <div class="row">
            <span class="pill" id="bindingTagCount">Seçilen: ${esc(S.selectedBindingTags.size)}</span>
            <button class="btn small secondary" data-binding-selected onclick="openTagBindingDialog([...S.selectedBindingTags])" ${S.selectedBindingTags.size&&!bindingRunning&&!refreshing?'':'disabled'}>Seçilenlerin ön proxy ayarı</button>
            <button class="btn small secondary" onclick="openTagBindingDialog(allBindingTags(),'all')" ${bindingTags.length&&!bindingRunning&&!refreshing?'':'disabled'}>Tüm ön proxy ayarları</button>
            <button class="btn small green" data-refresh-action onclick="refreshAllImportSources()" ${sources.length&&!refreshing&&!bindingRunning?'':'disabled'}>Tümünü yeniden test et</button>
            <button class="btn small danger" data-refresh-action onclick="deleteAllImportSources()" ${sources.length&&!refreshing&&!bindingRunning?'':'disabled'}>Tümünü sil</button>
          </div>
        </div>
        <div class="settings-section-body">
      ${sources.length?`<div class="stack">${sources.map((src,i)=>{const tag=String(src.tag_prefix||'').trim(),binding=chainBindingLabel(src);return `<div class="source-row">
        <label class="source-row-select"><input type="checkbox" data-binding-tag="${esc(tag)}" aria-label="${esc(tag)} etiketini seç" ${tag&&S.selectedBindingTags.has(tag)?'checked':''} ${tag&&!bindingRunning&&!refreshing?'':'disabled'} onchange="toggleBindingTag(this.dataset.bindingTag,this.checked)"></label>
        <div style="flex:1;min-width:0">
          <div class="row" style="gap:8px;align-items:center;flex-wrap:wrap">
            <span class="pill green">${esc(src.tag_prefix||'local')}</span>
            <span class="pill">${esc(importSourceType(src))}</span>
            <span class="value-pill ${binding.className}">${esc(binding.text)}</span>
            <span class="value-pill muted">Toplam ${esc(src.total||0)}</span>
            <span class="value-pill green">Havuzda ${esc(src.pool||0)}</span>
            <span class="value-pill red">Başarısız ${esc(src.failed||0)}</span>
            <span class="value-pill amber">Aday ${esc(src.candidate||0)}</span>
          </div>
          <div style="margin-top:6px"><code class="source-value">${esc(importSourceText(src))}</code></div>
          <div class="muted" style="margin-top:6px">Güncelleme: ${esc(fmtDate(src.updated_at))}</div>
        </div>
        <div class="chain-row-actions">
          ${tag?`<button class="btn small secondary" data-tag="${esc(tag)}" onclick="openTagBindingDialog([this.dataset.tag])" ${bindingRunning||refreshing?'disabled':''}>Ön proxy değiştir</button>`:''}
          ${src.refreshable?`<button class="btn small green" data-refresh-action onclick="refreshImportSource(${i})" ${refreshing||bindingRunning?'disabled':''}>Yeniden test et</button>`:''}
          <button class="btn small danger" data-refresh-action onclick="deleteImportSource(${i})" ${refreshing||bindingRunning?'disabled':''}>Sil</button>
        </div>
      </div>`}).join('')}</div>`:`<div class="empty">Henüz içe aktarma kaynağı yok</div>`}
        </div>
      </section>

      <section class="settings-section settings-wide">
        <div class="settings-section-head">
          <h2>Veri dışa aktarma</h2>
          <span class="pill" id="exportTagCount">Seçilen ${esc(S.selectedExportTags.size)} / ${esc(exportTags.length)}</span>
        </div>
        <div class="settings-section-body">
          <div class="row"><button class="btn small secondary" onclick="setAllExportTags(true)" ${exportTags.length?'':'disabled'}>Tüm etiketleri seç</button><button class="btn small ghost" id="clearExportTags" onclick="setAllExportTags(false)" ${S.selectedExportTags.size?'':'disabled'}>Seçimi temizle</button><button class="btn" data-tag-export-selected onclick="exportSelectedTags()" ${S.selectedExportTags.size?'':'disabled'}>Seçilenleri dışa aktar (${esc(S.selectedExportTags.size)})</button></div>
          ${exportTags.length?`<div class="subscription-export-list">${exportTags.map(group=>`<div class="export-tag-row"><input type="checkbox" data-export-tag data-tag="${esc(group.tag)}" aria-label="${esc(group.tag)} etiketini seç" ${S.selectedExportTags.has(group.tag)?'checked':''} onchange="toggleExportTag(this.dataset.tag,this.checked)"><div class="export-tag-main"><strong>${esc(group.tag)}</strong><span class="muted">${esc(group.types.join(' / '))} · ${esc(group.total)} düğüm</span></div><button class="btn small secondary" data-tag="${esc(group.tag)}" onclick="exportSourceTags([this.dataset.tag])">Dışa aktar</button></div>`).join('')}</div>`:'<div class="empty">Etiketli içe aktarma kaynağı yok</div>'}
        </div>
      </section>

      <section class="settings-section settings-wide">
        <div class="settings-section-head">
          <h2>Yedekleme ve geri yükleme</h2>
          <span class="pill ${webdav.address?'green':''}">${webdav.address?'WebDAV yapılandırıldı':'Yalnızca yerel'}</span>
        </div>
        <div class="settings-section-body">
          <div class="backup-settings-grid">
            <div class="backup-pane">
              <h3>Yerel yedek</h3>
              <button class="btn primary block" onclick="downloadLocalBackup()">Tam yedeği dışa aktar</button>
              <button class="btn secondary block" onclick="qs('#localBackupFile').click()">Yerel yedekten geri yükle</button>
              <input id="localBackupFile" type="file" accept=".zip,application/zip" onchange="restoreLocalBackup(this.files?.[0])" hidden>
            </div>
            <div class="backup-pane">
              <h3>WebDAV yedeği</h3>
              <div class="field"><label>WebDAV adresi</label><input id="webdavAddress" type="text" value="${esc(webdav.address||'')}" placeholder="https://dav.example.com/remote.php/dav/files/user"></div>
              <div class="field"><label>Yedek klasörü</label><input id="webdavFolder" type="text" value="${esc(webdav.folder||'/easy_proxies')}" placeholder="/easy_proxies"><div class="field-help">Varsayılan: /easy_proxies</div></div>
              <div class="settings-form">
                <div class="field"><label>Kullanıcı adı</label><input id="webdavUsername" type="text" value="${esc(webdav.username||'')}"></div>
                <div class="field"><label>Parola</label><input id="webdavPassword" type="password" value="${esc(webdav.password||'')}"></div>
              </div>
              <div class="row"><button class="btn" onclick="saveWebDAV(false)">Kaydet</button><button class="btn secondary" onclick="testWebDAV()">Bağlantıyı test et</button><button class="btn green" onclick="createWebDAVBackup()">Şimdi yedekle</button></div>
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
  const count=qs('#bindingTagCount');if(count)count.textContent=`Seçilen: ${S.selectedBindingTags.size}`;
  const button=qs('[data-binding-selected]');if(button)button.disabled=!S.selectedBindingTags.size||!!S.activeTagBindingJob||!!S.activeRefreshJob;
}
function openTagBindingDialog(tags,scope='selected'){
  const available=new Set(allBindingTags());
  tags=[...new Set((tags||[]).map(tag=>String(tag||'').trim()).filter(tag=>available.has(tag)))];
  if(!tags.length){toast('Önce bir etiket seçin','err');return}
  S.pendingTagBindingTags=tags;
  const source=tags.length===1?(S.importSources||[]).find(item=>String(item.tag_prefix||'').trim()===tags[0]):null;
  const current=source?.chain_binding==='profile'?source.chain_profile_id:'';
  const options=S.chainProfiles.filter(profile=>profile.enabled).map(profile=>`<option value="${esc(profile.id)}" ${profile.id===current?'selected':''}>${esc(profile.name)}</option>`).join('');
  const title=scope==='all'?'Tüm etiketlere ön proxy ata':tags.length===1?`${tags[0]} etiketine ön proxy ata`:'Seçilen etiketlere ön proxy ata';
  openDialog(title,`<div class="field"><label for="tagBindingProfile">Ön proxy</label><select id="tagBindingProfile"><option value="">Doğrudan bağlantı (ön proxy kaldırılır)</option>${options}</select></div><div class="meta"><span class="pill green">${esc(tags.length)} etiket</span>${source?.chain_binding==='mixed'?'<span class="pill amber">Mevcut bağlantı karışık</span>':''}</div>`,{actions:'<button class="btn" type="button" onclick="closeDialog()">Vazgeç</button><button class="btn primary" id="applyTagBinding" type="button" onclick="submitTagBinding()">Yeniden test et ve uygula</button>'});
}
async function submitTagBinding(){
  const tags=[...S.pendingTagBindingTags],profileID=qs('#tagBindingProfile')?.value||'';
  if(!tags.length)return;
  const policy=verificationPolicy('refresh');if(!verificationPolicyValid(policy))return;
  const button=qs('#applyTagBinding');if(button){button.disabled=true;button.textContent='Görev oluşturuluyor'}
  try{
    const started=await api('/api/import/bindings',{method:'POST',body:JSON.stringify({tags,chain_profile_id:profileID,...policy})});
    S.activeTagBindingJob=started.job_id;localStorage.setItem(ACTIVE_TAG_BINDING_JOB_KEY,started.job_id);
    closeDialog();
    await pollTagBindingJob(started.job_id,true);
  }catch(err){if(button){button.disabled=false;button.textContent='Yeniden test et ve uygula'}toast(err.message,'err')}
}
function tagBindingStatusLabel(status){return {waiting:'Bekliyor',preparing:'Hazırlanıyor',testing:'Test ediliyor',completed:'Uygulandı',failed:'Başarısız',skipped:'Değişiklik gerekmiyor',canceled:'İptal edildi',running:'Devam ediyor',partial:'Kısmen tamamlandı'}[status]||status||'Bilinmiyor'}
function openTagBindingProgress(job){
  openDialog('Etiketlerin ön proxy ayarını değiştir','<div id="tagBindingProgress"></div>',{wide:true,actions:'<button class="btn danger" id="cancelTagBinding" type="button" onclick="cancelTagBindingJob()">Görevi durdur</button><button class="btn" type="button" onclick="closeDialog()">Kapat</button>'});
  const dialog=qs('#resultDialog');if(dialog)dialog.dataset.tagBindingShell='1';
  renderTagBindingProgress(job);
}
function renderTagBindingProgress(job){
  const host=qs('#tagBindingProgress');if(!host||qs('#resultDialog')?.dataset.tagBindingShell!=='1')return;
  const total=Number(job?.total_tags)||0,done=Number(job?.done_tags)||0,pct=total?Math.min(100,Math.round(done*100/total)):0;
  const rows=(job?.items||[]).map(item=>`<div class="binding-progress-row"><strong>${esc(item.tag_prefix)}</strong><div class="meta"><span class="pill ${item.status==='completed'||item.status==='skipped'?'green':item.status==='failed'||item.status==='canceled'?'red':'amber'}">${esc(tagBindingStatusLabel(item.status))}</span>${item.total?`<span class="value-pill muted">${esc((item.passed||0)+(item.failed||0))}/${esc(item.total)}</span>`:''}</div>${item.error?`<div class="muted" style="grid-column:1/-1">${esc(item.error)}</div>`:''}</div>`).join('');
  host.innerHTML=`<div class="binding-progress"><div class="progress-bar"><div class="progress-fill" style="width:${pct}%"></div></div><div class="meta"><span>Etiket ${done}/${total}</span><span class="pill green">Başarılı ${esc(job?.successful||0)}</span><span class="pill red">Başarısız ${esc(job?.failed||0)}</span><span class="pill">Değişiklik yok ${esc(job?.skipped||0)}</span></div>${job?.error?`<div class="inline-status bad">${esc(job.error)}</div>`:''}<div class="binding-progress-list">${rows}</div></div>`;
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
    toast(failed?`Ön proxy değişikliği tamamlandı; ${failed} etiketin eski bağlantısı korundu`:'Ön proxy değişikliği tamamlandı',failed?'err':'ok');
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
  try{const job=await api('/api/import/bindings/jobs/'+encodeURIComponent(id),{method:'DELETE'});S.tagBindingJob=job;renderTagBindingProgress(job);toast('Durdurma isteği gönderildi')}catch(err){toast(err.message,'err')}
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
    toast(res?.message||'Ayarlar kaydedildi');
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
  try{S.subConfig=await api('/api/subscription/config',{method:'PUT',body:JSON.stringify(body)});toast('Abonelik otomatik yenileme ayarları kaydedildi');renderSettings()}catch(err){toast(err.message,'err')}
}
