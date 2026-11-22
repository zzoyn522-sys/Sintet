
(() => {
 const panel=document.querySelector('#skin-wardrobe');
 const get=id=>document.getElementById('wardrobe-'+id);
 const app=()=>window.go?.main?.App;
 const open=()=>panel.matches(':popover-open');
 const defaultSkin='assets/brand/default-skin.png';
 const reducedMotion=matchMedia('(prefers-reduced-motion: reduce)');
 const canvas=get('model');
 const preview=canvas.parentElement;
 const blankIcons={
  skin:'<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="5" y="5" width="14" height="14" rx="2"/><path d="M9 11h1m4 0h1m-5 4h4"/></svg>',
  cape:'<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 5-3 14h12L15 5m-8 0 12 14"/></svg>'
 };
 let id='', skins=null, capes=null, showCatalog=false, busy=false, wantsBack=false;
 let generation=0, previewJob=0, viewer, viewerPromise, loadedSkin='', loadedCape='', transitionFrame=0;
 const pendingSaves=new Map();
 const images=new Map();

 function message(element,text){
  element.textContent=text;
  element.hidden=!text;
 }
 function current(g){return generation===g&&open()}
 function cancelTurn(){
  cancelAnimationFrame(transitionFrame);
  transitionFrame=0;
 }
 function turn(back,animate=true){
  wantsBack=back;
  cancelTurn();
  if(!viewer)return;
  const from=viewer.playerObject.rotation.y;
  const target=(back?Math.PI:0)-.34;
  const delta=Math.atan2(Math.sin(target-from),Math.cos(target-from));
  if(!animate||reducedMotion.matches||Math.abs(delta)<.01){
   viewer.playerObject.rotation.y=target;
   return;
  }
  const start=performance.now();
  function frame(now){
   const progress=Math.min(1,(now-start)/360);
   viewer.playerObject.rotation.y=from+delta*(1-Math.pow(1-progress,3));
   if(progress<1)transitionFrame=requestAnimationFrame(frame);
  }
  transitionFrame=requestAnimationFrame(frame);
 }
 function getViewer(){
  if(viewer)return Promise.resolve(viewer);
  if(viewerPromise)return viewerPromise;
  viewerPromise=new Promise((resolve,reject)=>{
   const script=document.createElement('script');
   script.src='vendor/skinview3d/skinview3d.bundle.js';
   script.onload=resolve;
   script.onerror=()=>{
    script.remove();
    reject(new Error('Skin viewer script failed'));
   };
   document.head.append(script);
  }).then(()=>{
   viewer=new window.skinview3d.SkinViewer({
    canvas,width:preview.clientWidth,height:preview.clientHeight,
    pixelRatio:Math.min(devicePixelRatio,2),zoom:.86,fov:42
   });
   viewer.renderPaused=!open();
   viewer.controls.enableZoom=false;
   viewer.controls.enablePan=false;
   viewer.controls.minPolarAngle=.7;
   viewer.controls.maxPolarAngle=2.35;
   viewer.playerObject.rotation.y=(wantsBack?Math.PI:0)-.34;
   new ResizeObserver(()=>{
    if(preview.clientWidth&&preview.clientHeight)viewer.setSize(preview.clientWidth,preview.clientHeight);
   }).observe(preview);
   return viewer;
  }).catch(error=>{
   viewerPromise=null;
   throw error;
  });
  return viewerPromise;
 }
 function motion(){
  if(!viewer||viewer.renderPaused)return;
  viewer.animation=reducedMotion.matches?null:new window.skinview3d.IdleAnimation();
  if(viewer.animation)viewer.animation.speed=.6;
 }
 reducedMotion.addEventListener('change',()=>{
  if(reducedMotion.matches)turn(wantsBack,false);
  motion();
 });
 function image(account,url){
  if(images.has(url))return images.get(url);
  const source=url===defaultSkin?Promise.resolve(url):app().GetWardrobeTexture(account,url);
  const pending=source.then(address=>new Promise((resolve,reject)=>{
   const texture=new Image();
   texture.onload=()=>resolve(texture);
   texture.onerror=reject;
   texture.src=address;
  })).catch(error=>{
   if(images.get(url)===pending)images.delete(url);
   throw error;
  });
  if(images.size>=24)images.delete(images.keys().next().value);
  images.set(url,pending);
  return pending;
 }
 async function updatePreview(back){
  const job=++previewJob, account=id;
  const valid=()=>previewJob===job&&open()&&id===account;
  const skin=skins?.skin;
  const cape=[...(capes?.capes||[]),...(capes?.catalog||[])].find(item=>item.id===capes?.equipped);
  let create;
  try{
   create=await getViewer();
  }catch{
   if(valid())message(get('model-status'),'3D-просмотр недоступен');
   return;
  }
  if(!valid())return;
  if(create.renderPaused){
   create.renderPaused=false;
   motion();
  } else if(!create.animation&&!reducedMotion.matches)motion();
  const skinURL=skin?.key?skin.url:defaultSkin;
  const skinKey=(skinURL||defaultSkin)+':'+(skin?.model||'default');
  const capeURL=cape?.url||'';
  const updates=await Promise.allSettled([
   loadedSkin===skinKey?null:image(account,skinURL||defaultSkin),
   loadedCape===capeURL||!capeURL?null:image(account,capeURL)
  ]);
  if(!valid())return;
  const [nextSkin,nextCape]=updates;
  const usesFallback=nextSkin.status==='rejected'&&!loadedSkin;
  if(usesFallback){
   try{
    const fallback=await image(account,defaultSkin);
    if(!valid())return;
    create.loadSkin(fallback,{model:'default'});
   }catch{/* Keep the texture error visible. */}
  }
  if(nextSkin.status==='fulfilled'&&nextSkin.value){
   create.loadSkin(nextSkin.value,{model:skin?.model==='slim'?'slim':'default'});
   loadedSkin=skinKey;
  }
  if(loadedCape!==capeURL){
   create.resetCape();
   loadedCape='';
   if(nextCape.status==='fulfilled'&&nextCape.value){
    create.loadCape(nextCape.value);
    loadedCape=capeURL;
   }
  }
  get('turn').hidden=nextSkin.status==='rejected'&&!loadedSkin&&!usesFallback;
  turn(back);
  message(get('model-status'),updates.some(update=>update.status==='rejected')?'Не удалось загрузить текстуру.':'');
 }
 function card({key,label,url,selected,cape,action}){
  const button=document.createElement('button');
  button.type='button';
  button.className='wardrobe-card';
  button.dataset.key=key;
  button.title=label;
  button.setAttribute('aria-label',label);
  button.setAttribute('aria-pressed',String(selected));
  if(url){
   const swatch=document.createElement('span');
   swatch.className='wardrobe-swatch'+(cape?' is-cape':'');
   const img=new Image();
   img.alt='';
   img.loading='lazy';
   img.addEventListener('error',()=>{swatch.innerHTML=blankIcons[cape?'cape':'skin']},{once:true});
   img.src=url;
   swatch.append(img);
   button.append(swatch);
  }else button.innerHTML=blankIcons[cape?'cape':'skin'];
  button.onclick=action;
  return button;
 }
 function replaceCards(list,cards){
  const focused=list.contains(document.activeElement)?document.activeElement.dataset.key:null;
  const scroll=list.scrollLeft;
  list.replaceChildren(...cards);
  list.scrollLeft=scroll;
  if(focused!==null)cards.find(item=>item.dataset.key===focused)?.focus({preventScroll:true});
 }
 function renderSkins(){
  const active=skins.skin;
  const saved=new Map((skins.wardrobe||[]).map(item=>[item.key,item]));
  if(active.key&&!saved.has(active.key))saved.set(active.key,active);
  const cards=[card({key:'',label:'Стандартный скин',url:defaultSkin,selected:!active.key,action:()=>applySkin('')})];
  [...saved.values()].forEach((item,index)=>cards.push(card({
   key:item.key,label:window.SintetText.format('Скин %s',index+1),
   url:item.url,selected:item.key===active.key,action:()=>applySkin(item.key)
  })));
  replaceCards(get('skins'),cards);
  get('skin-type').value=active.model==='slim'?'slim':'default';
  window.SintetUI?.select.refresh(get('skin-type'));
 }
 function renderCapes(){
  const items=(showCatalog?capes.catalog:capes.capes)||[];
  const cards=[card({key:'',label:'Без плаща',cape:true,selected:!capes.equipped,action:()=>applyCape('')})];
  items.forEach(item=>cards.push(card({
   key:item.id,label:item.name,url:item.url,cape:true,
   selected:item.id===capes.equipped,action:()=>applyCape(item.id)
  })));
  replaceCards(get('capes'),cards);
  get('catalog').querySelector('span').textContent=showCatalog?'Мои плащи':'Каталог';
  get('catalog').setAttribute('aria-pressed',String(showCatalog));
  get('catalog').hidden=!capes.catalog?.length;
 }
 function setBusy(value){
  busy=value;
  panel.setAttribute('aria-busy',String(value));
  panel.querySelectorAll('.wardrobe-card,#wardrobe-upload,#wardrobe-catalog,#wardrobe-skin-type,#wardrobe-retry').forEach(control=>{
   control.disabled=value;
  });
 }
 async function refresh(){
  const g=++generation;
  setBusy(false);
  get('signin').hidden=Boolean(id);
  get('content').hidden=true;
  get('retry').hidden=true;
  get('skins-section').hidden=true;
  get('capes-section').hidden=true;
  message(get('status'),'');
  if(!id)return;
  message(get('status'),'Загрузка…');
  await pendingSaves.get(id);
  if(!current(g))return;
  const api=app();
  if(!api?.GetSkinWardrobe){
   message(get('status'),'Не удалось открыть гардероб Sintet ID.');
   return;
  }
  const results=await Promise.allSettled([api.GetSkinWardrobe(id),api.GetCapeWardrobe(id)]);
  if(!current(g))return;
  const [skinResult,capeResult]=results;
  skins=skinResult.status==='fulfilled'?skinResult.value:null;
  capes=capeResult.status==='fulfilled'?capeResult.value:null;
  if(skins)renderSkins();
  if(capes)renderCapes();
  get('skins-section').hidden=!skins;
  get('capes-section').hidden=!capes;
  get('content').hidden=!skins&&!capes;
  get('retry').hidden=Boolean(skins&&capes);
  message(get('status'),skins&&capes?'':String(results.find(result=>result.status==='rejected').reason));
  if(skins||capes)void updatePreview(Boolean(capes?.equipped));
 }
 async function save(action,commit){
  if(!id||!open()||busy)return;
  const g=generation, account=id;
  setBusy(true);
  message(get('status'),'');
  const operation=Promise.resolve().then(()=>action(app(),account));
  const settled=operation.then(()=>{},()=>{});
  pendingSaves.set(account,settled);
  try{
   const result=await operation;
   if(current(g)&&result)commit(result);
  }catch(error){
   if(current(g))message(get('status'),String(error));
  }finally{
   if(pendingSaves.get(account)===settled)pendingSaves.delete(account);
   if(current(g))setBusy(false);
  }
 }
 function commitSkin(result){
  skins=result;
  renderSkins();
  void updatePreview(false);
 }
 function applySkin(key){
  if((skins.skin.key||'')!==key)void save((api,id)=>api.ApplyWardrobeSkin(id,key),commitSkin);
 }
 function applyCape(cape){
  if(capes.equipped===cape){
   turn(Boolean(cape));
   return;
  }
  void save((api,id)=>api.EquipWardrobeCape(id,cape),result=>{
   capes=result;
   renderCapes();
   void updatePreview(Boolean(cape));
  });
 }
 function account(state){
  const next=state?.provider==='sintet'?state.account?.id||'':'';
  if(id===next)return;
  id=next;
  ++generation;
  ++previewJob;
  cancelTurn();
  skins=capes=null;
  showCatalog=false;
  loadedSkin=loadedCape='';
  images.clear();
  viewer?.resetCape();
  viewer?.resetSkin();
  message(get('model-status'),'');
  get('turn').hidden=true;
  get('skins').replaceChildren();
  get('capes').replaceChildren();
  if(open())void refresh();
 }
 get('toggle').addEventListener('click',()=>account(window.SintetIdentity?.current()));
 get('retry').onclick=()=>void refresh();
 get('upload').onclick=()=>void save((api,id)=>api.PickWardrobeSkin(id,get('skin-type').value),commitSkin);
 get('catalog').onclick=()=>{
  showCatalog=!showCatalog;
  renderCapes();
  get('capes').scrollLeft=0;
 };
 get('turn').onclick=()=>turn(!wantsBack);
 panel.addEventListener('toggle',()=>{
  get('toggle').setAttribute('aria-expanded',String(open()));
  if(open())void refresh();
  else{
   ++generation;
   ++previewJob;
   cancelTurn();
   if(viewer)viewer.renderPaused=true;
  }
 });
 window.addEventListener('sintet:identity',event=>account(event.detail));
})();
