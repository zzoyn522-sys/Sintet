(() => {
 const q=s=>document.querySelector(s),popover=q('#versions-dialog');
 let api,catalog=[],variants=[],installs=[],selected=null,busy=false,running=false,resolving=false,previousStage='',catalogLoading=false,selectionRequest=0;
 try{selected=JSON.parse(localStorage.getItem('sintet.selectedProfile'))}catch{}
 const names={vanilla:'Vanilla',fabric:'Fabric',forge:'Forge',neoforge:'NeoForge'};
 const profileID=p=>p.id|| (p.loader==='vanilla'?p.base:`${p.loader}-${p.loaderVersion}-${p.base}`);
 const isBuild=p=>p?.id?.startsWith('build-');
 const installed=p=>!!p&&installs.some(item=>item.id===profileID(p));
 const label=p=>p.name?`${p.name} · ${p.base}`:`${p.base} · ${names[p.loader]}`;
 const isOpen=()=>popover.matches(':popover-open');
 function closeVersions(){if(isOpen())popover.hidePopover();q('#version').setAttribute('aria-expanded','false')}
 function positionVersions(){if(!isOpen())return;const rect=q('#version').getBoundingClientRect(),gap=7;const width=Math.min(Math.max(rect.width,280),innerWidth-24);const height=Math.max(0,Math.min(350,innerHeight-rect.bottom-gap-12));popover.style.width=width+'px';popover.style.height=height+'px';popover.style.left=Math.max(12,Math.min(rect.right-width,innerWidth-width-12))+'px';popover.style.top=(rect.bottom+gap)+'px'}
 function updateButton(){q('#open-profile-folder').disabled=!api||!selected||!installed(selected)||resolving;q('#play').disabled=!selected||busy||running||resolving;q('#play').textContent=running?'Игра запущена':busy?'Подождите…':resolving?'Выбор версии…':selected?(installed(selected)?'Играть':'Установить'):'Выберите версию';if(selected){q('#version-text').textContent=label(selected);const icon=q('#version-icon');icon.hidden=!isBuild(selected);window.SintetBuildIcon.render(icon,isBuild(selected)?selected.icon||'grass':'');q('#selected-version').textContent=selected.base;q('#version').classList.toggle('installed',installed(selected));q('.current-game>div>span').textContent=`${names[selected.loader]} · Java Edition`}}
 function seedVariants(){const keys=new Set();const list=[];for(const installedProfile of installs){const p=installedProfile.id?.startsWith('build-')?{...installedProfile}:{base:installedProfile.base,loader:installedProfile.loader,loaderVersion:installedProfile.loaderVersion};const key=p.id||p.base+'/'+p.loader;if(!keys.has(key)){keys.add(key);list.push(p)}}for(const p of [...variants,...catalog.map(v=>({base:v.id,loader:'vanilla',loaderVersion:''}))]){const key=p.base+'/'+p.loader;if(!keys.has(key)){keys.add(key);list.push(p)}}const order=new Map(catalog.map((v,i)=>[v.id,i])),rank={forge:0,fabric:1,neoforge:2,vanilla:3};list.sort((a,b)=>(order.get(a.base)??9999)-(order.get(b.base)??9999)||rank[a.loader]-rank[b.loader]||String(a.name||'').localeCompare(String(b.name||''),'ru'));variants=list}
 async function choose(entry){const request=++selectionRequest;closeVersions();resolving=true;updateButton();try{const value={...entry};if(value.loader!=='vanilla'&&!value.loaderVersion){const builds=await api.LoaderVersions(value.base,value.loader);if(!builds?.length)throw Error('Для этой версии загрузчик недоступен');value.loaderVersion=builds[0]}if(request!==selectionRequest)return;selected=value;try{localStorage.setItem('sintet.selectedProfile',JSON.stringify(selected))}catch{}q('#version').focus()}catch(e){if(request===selectionRequest)toast(String(e))}finally{if(request===selectionRequest){resolving=false;updateButton()}}}
 function visibleVersion(version,type,settings){
  if(isBuild(version))return true;
  const id=version.base.toLowerCase();
  if(type==='old_alpha'||type==='old_beta'||/^(?:a\d|b\d|c0\.|inf-|in-|rd-)/.test(id))return !!settings.oldVersions;
  if(/(?:^|[-_. ])(?:rc|release[ -]?candidate)(?:[ ._-]?\d+)?$/i.test(id))return !!settings.releaseCandidates;
  if(/(?:^|[-_. ])(?:pre|pre[ -]?release)(?:[ ._-]?\d+)?$/i.test(id))return !!settings.preReleases;
  if(/experimental|combat[ _-]?test|deep[ _-]?dark/.test(id))return !!settings.experimentalVersions;
  if(type==='snapshot'||/^\d{2}w\d{2}[a-z]$|[-_.]snapshot(?:[-_.]\d+)?$/.test(id))return !!settings.snapshots;
  if(type==='release'||/^\d+\.\d+(?:\.\d+)?$/.test(id))return true;
  return !!settings.experimentalVersions;
 }
 function renderVersions(){
  const types=new Map(catalog.map(v=>[v.id,v.type]));
  const settings=window.SintetSettings?.config?.settings||{};
  const words=q('#version-search').value.toLowerCase().trim().split(/\s+/).filter(Boolean);
  const fragment=document.createDocumentFragment(),rows=[];
  const matches=variants.filter(v=>{
   if(!visibleVersion(v,types.get(v.base),settings))return false;
   const text=`${v.name||''} ${v.base} ${names[v.loader]} ${v.loaderVersion}`.toLowerCase();
   return words.every(word=>text.includes(word));
  });
  for(const [heading,entries] of [['Сборки',matches.filter(isBuild)],['Версии Minecraft',matches.filter(v=>!isBuild(v))]]){
   if(!entries.length)continue;
   const group=document.createElement('div');
   group.className='version-group';
   group.setAttribute('role','group');
   group.setAttribute('aria-label',heading);
   const caption=document.createElement('div');
   caption.className='version-group-title';
   caption.textContent=heading;
   caption.setAttribute('aria-hidden','true');
   group.append(caption);
   for(const entry of entries){
    const button=document.createElement('button');
    button.type='button';
    button.className='version-row';
    button.dataset.version=entry.base;
    button.dataset.loader=entry.loader;
    button.setAttribute('role','option');
    const isSelected=selected&&profileID(selected)===profileID(entry);
    button.setAttribute('aria-selected',String(!!isSelected));
    button.classList.toggle('installed',installed(entry));
    button.classList.toggle('selected',!!isSelected);
    button.title=label(entry)+(entry.loaderVersion?' · '+entry.loaderVersion:'')+(' · '+window.SintetText.translate(installed(entry)?'Установлено':'Не установлено'));
    const content=document.createElement('span');
    if(isBuild(entry)){
     const icon=document.createElement('span');
     icon.className='version-build-icon';
     icon.setAttribute('aria-hidden','true');
     window.SintetBuildIcon.render(icon,entry.icon||'grass');
     content.append(icon);
    }
    const title=document.createElement('span');
    title.textContent=label(entry);
    content.append(title);
    const mark=document.createElement('span');
    mark.className='version-choice-mark';
    mark.setAttribute('aria-hidden','true');
    mark.textContent=isSelected?'✓':'';
    button.append(content,mark);
    button.onclick=()=>choose(entry);
    const index=rows.push(button)-1;
    button.onkeydown=event=>{
     if(event.key!=='ArrowDown'&&event.key!=='ArrowUp')return;
     event.preventDefault();
     rows[index+(event.key==='ArrowDown'?1:-1)]?.focus();
    };
    group.append(button);
   }
   fragment.append(group);
  }
  if(!rows.length){
   const empty=document.createElement('div');
   empty.className='launcher-error';
   empty.textContent='Версии не найдены';
   fragment.append(empty);
  }
  q('#version-list').replaceChildren(fragment);
  q('#version-catalog-status').hidden=!catalogLoading;
  q('#version-catalog-status').textContent=catalogLoading?'Загрузка списка модлоадеров…':'';
 }
 q('#version').onclick=()=>{if(!api||!variants.length)return;if(isOpen()){closeVersions();return}q('#version-search').value='';renderVersions();popover.showPopover();q('#version').setAttribute('aria-expanded','true');positionVersions();q('#version-search').focus()};
 q('#version-search').oninput=renderVersions;q('#version-search').onkeydown=event=>{if(event.key==='ArrowDown'){event.preventDefault();q('#version-list .version-row')?.focus()}};
 async function loadVariantCatalog(){if(catalogLoading||window.SintetNetwork.offline)return;catalogLoading=true;if(isOpen())renderVersions();try{const result=await api.VersionVariants();variants=result.variants||[];seedVariants();if(result.warnings?.length)toast(result.warnings.join('; '))}catch(e){toast('Не удалось обновить список модлоадеров: '+String(e))}finally{catalogLoading=false;if(isOpen())renderVersions()}}
 async function refreshInstalled(){
  installs=await api.Installations();
  variants=variants.filter(v=>!isBuild(v));
  if(isBuild(selected)){
   selected=installs.find(item=>item.id===selected.id)||null;
   if(!selected&&installs.length)selected=installs[0];
   try{localStorage.setItem('sintet.selectedProfile',JSON.stringify(selected))}catch{}
  }
  seedVariants();updateButton();
  if(isOpen())renderVersions();
 }
 window.addEventListener('sintet:builds',()=>{if(api)refreshInstalled().catch(e=>toast(String(e)))});
 window.addEventListener('sintet:p2p-profile',async event=>{if(!api)return;try{await refreshInstalled();const entry=installs.find(p=>p.id===event.detail.id);if(entry){selected=entry;localStorage.setItem('sintet.selectedProfile',JSON.stringify(selected));updateButton()}}catch(e){toast(String(e))}});
 async function install(profile=selected){
  if(window.SintetNetwork.offline){toast(window.SintetNetwork.message);return}
  busy=true;updateButton();
  try{
   if(isBuild(profile))await api.InstallWorkshopBuild(profile.id);
   else await api.Install(profile.base,profile.loader,profile.loaderVersion||'');
   await poll();
  }catch(error){busy=false;updateButton();toast(String(error))}
 }
 q('#open-profile-folder').onclick=async()=>{if(!api||!selected||!installed(selected)||resolving)return;try{await api.OpenProfileFolder(profileID(selected))}catch(error){toast(String(error))}};
 q('#play').onclick=async()=>{if(!selected||busy||running||resolving)return;if(!await saveNick()||busy||running||resolving)return;if(!installed(selected)){await install();return}busy=true;updateButton();try{await api.Launch(profileID(selected),q('#nickname').value.trim());await poll()}catch(e){busy=false;updateButton();toast(String(e))}};
 const logDialog=q('#logs-dialog'),gameLog=q('#game-log'),analysis=q('#journal-analysis');
 const failureDialog=document.createElement('dialog');failureDialog.className='install-error-dialog';
 failureDialog.innerHTML='<header><h2>Ошибка установки или запуска</h2><button type="button" data-error-close aria-label="Закрыть">×</button></header><pre tabindex="0"></pre><footer><button type="button" data-error-copy>Скопировать ошибку</button><button type="button" data-error-done>Закрыть</button></footer>';
 document.body.append(failureDialog);let fullFailure='';
 q('#install-error-details').onclick=()=>{failureDialog.querySelector('pre').textContent=fullFailure;failureDialog.showModal()};
 failureDialog.querySelector('[data-error-close]').onclick=failureDialog.querySelector('[data-error-done]').onclick=()=>failureDialog.close();
 failureDialog.querySelector('[data-error-copy]').onclick=async()=>{try{await navigator.clipboard.writeText(failureDialog.querySelector('pre').textContent);toast('Ошибка скопирована')}catch{toast('Выдели текст ошибки и скопируй вручную')}};
 let journal=null,readingJournal=false;

 function journalTab(raw){
  analysis.hidden=raw;
  gameLog.hidden=!raw;
  q('#journal-analysis-tab').setAttribute('aria-pressed',String(!raw));
  q('#journal-text-tab').setAttribute('aria-pressed',String(raw));
 }
 q('#journal-analysis-tab').onclick=()=>journalTab(false);
 q('#journal-text-tab').onclick=()=>journalTab(true);

 function journalMessage(message){
  const element=document.createElement('p');
  element.className='journal-message';
  element.textContent=message;
  return element;
 }
 function showEvidence(line){
  journalTab(true);
  const text=gameLog.firstChild;
  if(!text||text.nodeType!==Node.TEXT_NODE)return;
  let start=0;
  for(let current=1;current<line;current++){
   const end=text.data.indexOf('\n',start);
   if(end<0)return;
   start=end+1;
  }
  const end=text.data.indexOf('\n',start);
  const range=document.createRange();
  range.setStart(text,start);
  range.setEnd(text,end<0?text.length:end);
  gameLog.focus();
  gameLog.scrollTop+=range.getBoundingClientRect().top-gameLog.getBoundingClientRect().top-24;
  const selection=window.getSelection();
  selection.removeAllRanges();
  selection.addRange(range);
 }
 function journalProfile(){
  if(!journal?.profile)return selected;
  if(selected&&profileID(selected)===journal.profile)return selected;
  if(journal.profile.startsWith('build-'))return {id:journal.profile};
  return installs.find(item=>item.id===journal.profile)||variants.find(item=>profileID(item)===journal.profile)||null;
 }
 function updateJournalActions(){
  q('#repair-install').disabled=!api||!journalProfile()||busy||running;
 }
 async function refreshGameLog(){
  if(readingJournal)return;
  readingJournal=true;
  try{
   if(!api){analysis.replaceChildren(journalMessage('Нет соединения с лаунчером'));return}
   const result=await api.Journal(journal?.revision||'');
   if(journal?.revision===result.revision)return;
   journal=result;
   const content=result.text||'Игра ещё не запускалась.';
   if(gameLog.textContent!==content)gameLog.textContent=content;
   const items=[];
   for(const finding of result.findings||[]){
    const card=document.createElement('article');
    card.className='journal-finding';
    const title=document.createElement('h2');
    title.textContent=finding.title;
    const advice=document.createElement('p');
    advice.textContent=finding.advice;
    const evidence=document.createElement('button');
    evidence.type='button';
    evidence.className='journal-evidence';
    evidence.title=finding.evidence;
    evidence.onclick=()=>showEvidence(finding.line);
    const snippet=document.createElement('code');
    snippet.textContent=finding.evidence;
    const link=document.createElement('span');
    link.textContent='Показать в логе';
    evidence.append(snippet,link);
    card.append(title,advice,evidence);
    items.push(card);
   }
   if(!items.length)items.push(journalMessage(result.text?'Известных причин сбоя пока не найдено.':'Игра ещё не запускалась.'));
   if(result.partial)items.push(journalMessage('Журнал длинный. Показаны и разобраны начало и конец файла.'));
   analysis.replaceChildren(...items);
   updateJournalActions();
  }catch(error){analysis.replaceChildren(journalMessage(window.SintetText.format('Не удалось прочитать журнал: %s',String(error))))}
  finally{readingJournal=false}
 }
 q('#cancel-install').onclick=()=>api?.CancelInstall();
 q('#repair-install').onclick=()=>{
  const profile=journalProfile();
  if(!api||!profile||busy||running)return;
  logDialog.close();
  void install(profile);
 };
 q('#open-logs').onclick=()=>{journalTab(false);updateJournalActions();logDialog.showModal();void refreshGameLog()};
 q('#logs-close').onclick=()=>logDialog.close();

 const mb=n=>window.SintetLanguage.formatSize(n/1048576,'megabyte',1);
 let polling=false;
 async function poll(){
  if(!api||polling)return;
  polling=true;
  try{
   const state=await api.State(),wasBusy=busy,wasRunning=running;
   busy=state.busy;
   running=state.running;
   updateButton();
   if(logDialog.open)updateJournalActions();
   q('.app-footer').classList.toggle('has-launch-error',!!state.error);
   q('#install-status').hidden=!busy&&!state.error;
   const t=window.SintetText.translate, stage=t(state.stage||''), error=t(state.error||'');
   q('#install-stage').textContent=error?stage+': '+error:stage;
   q('#install-stage').title=state.error||state.stage;
   q('#install-error-details').hidden=!state.error;
   fullFailure=state.error?state.stage+'\n'+state.error+(state.errorDetails&&state.errorDetails!==state.error?'\n\nТехнические подробности:\n'+state.errorDetails:''):'';
   q('#install-percent').textContent=busy&&state.total>0?Math.floor(state.percent)+'%':'';
   const progress=q('#install-progress');
   progress.hidden=!busy;
   if(state.total>0)progress.value=state.percent||0;
   else progress.removeAttribute('value');
   q('#install-detail').textContent=busy&&state.total>0?window.SintetText.format('%s / %s · %d / %d файлов',mb(state.done),mb(state.total),state.files,state.fileCount):'';
   q('#cancel-install').hidden=!busy;
   if(!busy&&(wasBusy||state.stage!==previousStage)&&state.stage==='Установлено'){
    await refreshInstalled();
    toast('Установка завершена. Можно играть.');
   }
   if(state.stage!==previousStage&&state.error)toast(state.error);
   if(logDialog.open&&(state.log||state.error)&&(busy||running||wasBusy||wasRunning||state.stage!==previousStage))await refreshGameLog();
   previousStage=state.stage;
  }catch(error){console.error(error)}finally{polling=false}
 }
 async function refreshRemoteVersions(){
  const data=await api.ListVersions();
  catalog=data.versions;
  seedVariants();
  if(!selected&&data.latest)selected={base:data.latest,loader:'vanilla',loaderVersion:''};
  updateButton();
  if(isOpen())renderVersions();
  if(!window.SintetNetwork.offline)void loadVariantCatalog();
 }
 async function init(){
  for(let i=0;i<100&&!window.go?.main?.App;i++)await new Promise(r=>setTimeout(r,100));
  await window.SintetSettings?.ready;
  api=window.go?.main?.App;
  if(!api){q('#version-text').textContent='Запустите Sintet через Start.bat';return}
  try{
   await refreshInstalled();
   if(!selected||!names[selected.loader]||!installed(selected)&&window.SintetNetwork.offline)selected=installs.find(p=>p.loader==='vanilla')||installs[0]||null;
   updateButton();
   await poll();
   setInterval(poll,500);
   await refreshRemoteVersions();
  }catch(e){
   if(!selected)q('#version-text').textContent=window.SintetNetwork.offline?window.SintetNetwork.message:'Не удалось получить версии';
   if(!window.SintetNetwork.offline&&!installs.length)toast(String(e));
  }
 }
 window.addEventListener('sintet:network',event=>{
  if(!api)return;
  if(!event.detail.offline)refreshRemoteVersions().catch(e=>{if(!selected)toast(String(e))});
  else if(!selected)q('#version-text').textContent=window.SintetNetwork.message;
 });
 document.addEventListener('pointerdown',event=>{if(isOpen()&&!popover.contains(event.target)&&!q('#version').contains(event.target))closeVersions()});document.addEventListener('keydown',event=>{if(event.key==='Escape'&&isOpen()){event.preventDefault();closeVersions();q('#version').focus()}});q('#version').addEventListener('keydown',event=>{if(event.key==='ArrowDown'&&!isOpen()){event.preventDefault();q('#version').click()}});window.addEventListener('resize',positionVersions);document.addEventListener('scroll',event=>{if(isOpen()&&!popover.contains(event.target))positionVersions()},true);
 window.addEventListener('sintet:news-layout',closeVersions);
 window.addEventListener('sintet:settings',()=>{if(isOpen())renderVersions()});
 window.addEventListener('sintet:settings',async event=>{if(event.detail.rootChanged&&api){closeVersions();previousStage='';try{const data=await api.ListVersions();catalog=data.versions;variants=[];await refreshInstalled();await poll();loadVariantCatalog()}catch(e){toast(String(e))}}});init();
})();
