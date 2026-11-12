(() => {
 const q=selector=>document.querySelector(selector);
 const dialog=q('#launcher-import');
 const openButton=q('#open-launcher-import');
 const list=q('#launcher-import-sources');
 const title=q('#launcher-import-title');
 const intro=q('#launcher-import-intro');
 const welcome=q('#launcher-import-welcome');
 const progress=q('#launcher-import-progress');
 const meter=progress.querySelector('progress');
 const status=q('#launcher-import-status');
 const errors=q('#launcher-import-errors');
 const start=q('#launcher-import-start');
 const skip=q('#launcher-import-skip');
 const browse=q('#launcher-import-browse');
 const restart=q('#launcher-import-restart');
 const close=q('#launcher-import-close');
 const brand={
  prism:['Prism Launcher','prism.svg'],multimc:['MultiMC','multimc.ico'],
  modrinth:['Modrinth App','modrinth.svg'],curseforge:['CurseForge','curseforge.svg'],
  legacy:['Legacy Launcher','legacy.png'],tlauncher:['TLauncher','tlauncher.ico'],
  minecraft:['Minecraft Launcher','minecraft.svg'],
 };
 let app, sources=new Map(), selected=new Set(), busy=false, choosing=false, finished=false, automatic=false, restarting=false, loaded=false;
 let resolveOnboarding;
 const onboarding=new Promise(resolve=>{resolveOnboarding=resolve});
 window.SintetImport={onboarding};
 const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms));
 const t=phrase=>window.SintetText.translate(phrase);
 const f=(phrase,...args)=>window.SintetText.format(phrase,...args);
 const loaderNames={vanilla:'Vanilla',fabric:'Fabric',forge:'Forge',neoforge:'NeoForge'};

 function node(tag,className,text){
  const element=document.createElement(tag);
  if(className)element.className=className;
  if(text)element.textContent=text;
  return element;
 }
 function setStatus(text){status.textContent=text;status.hidden=!text}
 function addSources(entries){
  for(const entry of entries||[]){
   if(!sources.has(entry.id))selected.add(entry.id);
   sources.set(entry.id,entry);
  }
 }
 function renderSources(){
  const groups=new Map();
  for(const entry of sources.values()){
   if(!groups.has(entry.launcher))groups.set(entry.launcher,[]);
   groups.get(entry.launcher).push(entry);
  }
  list.replaceChildren();
  for(const [id,entries] of groups){
   const [name,icon]=brand[id]||brand.minecraft;
   const section=node('section','import-source');
   const heading=node('div','import-source-heading');
   const logo=node('span','import-source-logo');
   const image=node('img');image.src='assets/launcher-import/'+icon;image.alt='';image.dataset.brand=id;
   image.onerror=()=>logo.replaceChildren(node('span','',name.split(/\s+/).map(word=>word[0]).join('').slice(0,2)));
   logo.append(image);heading.append(logo,node('strong','',name));section.append(heading);
   for(const entry of entries){
    const row=node('label','import-instance');
    const checkbox=node('input');checkbox.type='checkbox';checkbox.checked=selected.has(entry.id);
    checkbox.onchange=()=>{if(checkbox.checked)selected.add(entry.id);else selected.delete(entry.id);updateActions()};
    const info=node('span','import-instance-info');
    const title=node('strong','',entry.name);title.dataset.noTranslate='';
    const subtitle=node('small','',[entry.game,loaderNames[entry.loader],entry.loaderVersion].filter(Boolean).join(' · '));
    info.append(title,subtitle);row.append(checkbox,info);row.title=entry.folder;row.dataset.noTranslate='';section.append(row);
   }
   list.append(section);
  }
 }
 function updateActions(){
  const welcoming=automatic&&!busy&&!finished;
  const continueOnly=welcoming&&!sources.size;
  welcome.hidden=!welcoming;
  intro.hidden=automatic||busy||finished;
  list.hidden=busy||finished||!sources.size;
  progress.hidden=!busy&&!finished;
  start.hidden=finished||continueOnly;start.disabled=busy||choosing||!selected.size;
  skip.classList.toggle('primary',continueOnly);
  if(!finished)skip.textContent=continueOnly?'Продолжить':'Пропустить';
  browse.hidden=busy||finished;browse.disabled=choosing;
  close.disabled=busy||restarting;
  skip.disabled=busy||restarting;
  restart.hidden=!finished;restart.disabled=restarting;
 }
 function reset(){
  sources.clear();selected.clear();finished=false;busy=false;restarting=false;
  errors.replaceChildren();errors.hidden=true;meter.value=0;
  title.textContent=automatic?'Добро пожаловать в Sintet':'Перенос в Sintet';
  setStatus('Ищем сборки…');renderSources();updateActions();
 }
 async function acknowledge(){
  if(!automatic)return;
  try{await app.FinishLauncherImportOffer()}
  finally{automatic=false;resolveOnboarding()}
 }
 async function dismiss(){
  if(busy||restarting)return;
  dialog.close();
  try{await acknowledge()}catch(error){toast(String(error?.message||error))}
 }
 async function find(){
  try{
   addSources(await app.DetectLauncherImports());
   renderSources();
   setStatus(sources.size||automatic?'':'Сборки не найдены. Можно выбрать папку вручную.');
  }catch(error){setStatus(String(error?.message||error))}
  finally{loaded=true;updateActions()}
 }
 async function manualOpen(){
  if(!app||dialog.open)return;
  reset();dialog.showModal();await find();
 }
 openButton.onclick=()=>void manualOpen();
 close.onclick=skip.onclick=()=>void dismiss();
 dialog.addEventListener('cancel',event=>{event.preventDefault();void dismiss()});
 browse.onclick=async()=>{
  choosing=true;updateActions();
  try{
   const entries=await app.ChooseLauncherImportFolder(t('Выберите папку старого лаунчера'));
   if(entries==null)return;
   addSources(entries);renderSources();
   setStatus(entries.length?'':'В этой папке нет сборок, доступных для переноса.');
  }catch(error){setStatus(String(error?.message||error))}
  finally{choosing=false;updateActions()}
 };
 async function relaunch(){
  restarting=true;updateActions();setStatus('Перезапускаем Sintet…');
  try{await app.RestartAfterLauncherImport()}
  catch(error){restarting=false;updateActions();setStatus(String(error?.message||error))}
 }
 restart.onclick=()=>void relaunch();
 function renderProgress(value){
  const stages={scanning:'Проверяем файлы',copying:'Копируем файлы',installing:'Устанавливаем Minecraft и загрузчик'};
  const stage=progress.querySelector('.import-stage');
  stage.textContent=stages[value.phase]||'Перенос завершён';
  progress.querySelector('.import-current').textContent=value.current||'';
  meter.value=value.percent||0;
  progress.querySelector('.import-percent').textContent=meter.value+'%';
  const bytes=window.SintetLanguage.formatBytes;
  progress.querySelector('.import-bytes').textContent=value.phase==='copying'&&value.totalBytes
   ?bytes(value.copiedBytes)+' / '+bytes(value.totalBytes):'';
 }
 start.onclick=async()=>{
  busy=true;title.textContent='Перенос в Sintet';errors.replaceChildren();errors.hidden=true;setStatus('');updateActions();
  renderProgress({phase:'scanning',percent:0});
  try{
   await app.StartLauncherImport([...selected]);
   for(;;){
    const value=await app.LauncherImportProgress();
    renderProgress(value);
    if(['done','partial','failed'].includes(value.phase)){
     busy=false;finished=value.phase!=='failed';
     for(const item of value.failures||[]){
      const row=node('li','');
      if(item.name){const name=node('strong','',item.name);name.dataset.noTranslate='';row.append(name)}
      row.append(node('span','',item.reason));errors.append(row);
     }
     errors.hidden=!errors.childElementCount;
     setStatus(value.phase==='failed'?'Не удалось перенести сборки':f('Перенесено сборок: %d из %d',value.completed,value.total));
     if(finished){
      skip.textContent='Продолжить';selected.clear();
      window.dispatchEvent(new Event('sintet:builds'));
     }
     updateActions();
     if(value.phase==='done'){await pause(1100);if(dialog.open&&!restarting)await relaunch()}
     return;
    }
    await pause(220);
   }
  }catch(error){busy=false;updateActions();setStatus(String(error?.message||error))}
 };

 async function boot(){
  await window.SintetSettings.ready;
  app=window.go?.main?.App;
  if(!app?.LauncherImportOffer){resolveOnboarding();return}
  automatic=await app.LauncherImportOffer();
  if(!automatic){openButton.disabled=false;resolveOnboarding();return}
  if(dialog.open)return;
  reset();await find();
  openButton.disabled=false;
  while(document.hidden||!document.hasFocus()||document.querySelector('dialog[open]')){
   if(dialog.open||!automatic)return;
   await pause(300);
  }
  if(automatic&&loaded&&!dialog.open)dialog.showModal();
 }
 void boot().catch(()=>resolveOnboarding());
})();
