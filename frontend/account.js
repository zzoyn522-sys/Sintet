(() => {
 const dialog=document.querySelector('#account-dialog'),trigger=document.querySelector('#account-button'),status=document.querySelector('#account-status');
 let account=null,provider='sintet',accountError='';
 const defaultAvatar='assets/brand/default-avatar.webp';
 function showAvatar(target,name,url){
  if(!target)return;
  const initial=name[0]?.toUpperCase()||'P';
  url=url||defaultAvatar;
  if(target.querySelector('img')?.getAttribute('src')===url)return;
  const image=document.createElement('img');image.src=url;image.alt='';image.draggable=false;
  image.onerror=()=>{
   if(image.parentNode!==target)return;
   if(image.getAttribute('src')!==defaultAvatar) image.src=defaultAvatar;
   else target.textContent=initial;
  };
  target.replaceChildren(image);
 }
 const avatarRequests = new WeakMap();
 const avatarSources = new Map();
 function renderAvatar(target,name,url){
  if(!target)return;
  const resolver=window.go?.main?.App?.ResolveAvatar;
  if(!url||!resolver){
   avatarRequests.delete(target);
   showAvatar(target,name,url);
   return;
  }
  if(avatarRequests.get(target)?.url===url)return;
  const request={url,attempts:0};
  avatarRequests.set(target,request);
  showAvatar(target,name,'');
  async function resolve(){
   if(avatarRequests.get(target)!==request)return;
   request.attempts++;
   let task=avatarSources.get(url);
   if(!task){
    task=resolver(url);
    avatarSources.set(url,task);
    task.catch(()=>{if(avatarSources.get(url)===task)avatarSources.delete(url)});
   }
   try{
    const local=await task;
    if(avatarRequests.get(target)===request)showAvatar(target,name,local);
   }catch{
    if(avatarRequests.get(target)===request&&request.attempts<3)setTimeout(resolve,15000);
   }
  }
  void resolve();
 }
 const defaultGlow=['#a167b5','#9c5fb3','#78378c'];
 const avatarPalettes=new Map();
 let requestedAvatar;
 const activity=document.querySelector('#account-activity');
 const settingsLink=document.querySelector('#account-web-settings');
 let activityAccount='',activityPending=false,activityTimer;

 function formatTime(milliseconds){
  const minutes=Math.floor(milliseconds/60000);
  return window.SintetLanguage.formatDuration(minutes,{lessThanMinute:milliseconds>0&&minutes===0});
 }

 async function refreshActivity(){
  if(activityPending||!dialog.open||!activityAccount)return;
  const id=activityAccount;
  activityPending=true;
  try{
   const totals=await window.go?.main?.App?.ProfileActivity(id);
   if(totals&&activityAccount===id){
    document.querySelector('#account-time-minecraft').textContent=formatTime(totals.minecraftMs);
    document.querySelector('#account-time-launcher').textContent=formatTime(totals.launcherMs);
   }
  }catch{}finally{
   activityPending=false;
   if(activityAccount!==id)void refreshActivity();
  }
 }
 window.addEventListener('sintet:settings',()=>void refreshActivity());
 document.querySelector('#account-handle').addEventListener('click',()=>settingsLink.click());
 settingsLink.addEventListener('click',()=>window.go?.main?.App?.OpenAccountSettings().catch(()=>toast('Не удалось открыть настройки')));
 function applyGlow(colors){
  ['start','middle','end'].forEach((stop,index)=>{
   dialog.style.setProperty('--avatar-glow-'+stop,colors[index]);
  });
 }
 async function syncAvatarGlow(url){
  if(!dialog.open||!dialog.style||requestedAvatar===url)return;
  requestedAvatar=url;
  const cached=avatarPalettes.get(url);
  if(cached){applyGlow(cached);return}
  applyGlow(defaultGlow);
  if(!url)return;
  try{
   const palette=await window.go?.main?.App?.AvatarGlow(url);
   if(palette?.length!==3||!palette.every(color=>/^#[0-9a-f]{6}$/i.test(color)))return;
   if(avatarPalettes.size>=16)avatarPalettes.delete(avatarPalettes.keys().next().value);
   avatarPalettes.set(url,palette);
   if(dialog.open&&requestedAvatar===url)applyGlow(palette);
  }catch{ /* Keep the default glow if the avatar is unavailable. */ }
 }
 function syncNetworkStatus(){
  const offline=window.SintetNetwork.offline;
  status.textContent=offline?window.SintetNetwork.message:accountError;
  status.hidden=!offline&&!accountError;
 }
 window.addEventListener('sintet:network',syncNetworkStatus);
 function syncProfile(){
  const name=document.querySelector('#profile-name').textContent;
  const online=!!account&&provider!=='local';
  document.querySelector('#account-title').textContent=name;
  const handle=document.querySelector('#account-handle');
  handle.hidden=provider!=='sintet'||!account?.id;
  handle.textContent=account?.handle?'@'+account.handle:'Задать юзернейм на сайте';
  handle.title='Настройки Sintet ID';
  const idButton=document.querySelector('#account-id');
  idButton.hidden=!account?.id;
  document.querySelector('#account-id-value').textContent=account?.id||'';
  const avatarURL=provider==='sintet'?account?.avatarURL||'':'';
  const statsID=provider==='sintet'?account?.id||'':'';
  document.querySelector('#account-email').hidden=!statsID;
  document.querySelector('#account-email-value').textContent=statsID?(account.email||'Не указана'):'';
  settingsLink.hidden=!statsID;
  activity.hidden=!statsID;
  if(activityAccount!==statsID){
   activityAccount=statsID;
   for(const value of activity.querySelectorAll('dd'))value.textContent='–';
   void refreshActivity();
  }
  void syncAvatarGlow(avatarURL);
  for(const selector of ['#avatar','#account-avatar'])renderAvatar(document.querySelector(selector),name,avatarURL);
  document.querySelector('#account-provider-details').hidden=!online;
  if(online){
   const ely=provider==='ely', microsoft=provider==='microsoft';
   document.querySelector('#account-skin-logo').src=microsoft?'assets/brand/microsoft.svg':'assets/brand/'+(ely?'ely':'sintet-id')+'-icon-light.svg';
   document.querySelector('#account-skin-provider').textContent=microsoft?'Microsoft':ely?'Ely.by':'Sintet ID';
   document.querySelector('#account-skin-description').textContent=microsoft?'Скин из профиля Minecraft':ely?'Скин из профиля Ely.by':'Скин из профиля Sintet ID';
  }
  syncNetworkStatus();
 }
 document.querySelector('#account-id-copy').addEventListener('click',async()=>{
  if(!account?.id)return;
  try{await navigator.clipboard.writeText(account.id);toast('ID скопирован')}
  catch{toast('Не удалось скопировать ID')}
 });
 const content=document.querySelector('#account-dialog-content');
 const stack=document.querySelector('#account-stack');
 if(typeof ResizeObserver==='function'&&typeof dialog.animate==='function'){
  let previousHeight=0;
  let resizeAnimation;

  function stopResize(){
   resizeAnimation?.cancel();
   resizeAnimation=undefined;
   dialog.classList.remove('resizing');
  }

  new ResizeObserver(()=>{
   if(!dialog.open)return;
   const style=getComputedStyle(dialog);
   const padding=parseFloat(style.paddingTop)+parseFloat(style.paddingBottom);
   const limit=parseFloat(style.maxHeight)||window.innerHeight-32;
   const target=Math.min(content.offsetHeight+padding,limit);
   const start=resizeAnimation?dialog.offsetHeight:previousHeight;
   previousHeight=target;

   // The avatar strip already animates its own height. Let the dialog follow it.
   const stackMoving=stack.getAnimations().some(animation=>animation.transitionProperty==='height');
   if(!start||stackMoving||Math.abs(target-start)<2||matchMedia('(prefers-reduced-motion: reduce)').matches){
    stopResize();
    return;
   }

   stopResize();
   dialog.classList.add('resizing');
   resizeAnimation=dialog.animate(
    [{height:start+'px'},{height:target+'px'}],
    {duration:240,easing:'cubic-bezier(.2,.75,.25,1)'},
   );
   resizeAnimation.onfinish=stopResize;
  }).observe(content);
  dialog.addEventListener('close',()=>{
   previousHeight=0;
   stopResize();
  });
 }

 function close(){dialog.close()}
 trigger.addEventListener('click',()=>{
  dialog.showModal();
  syncProfile();
  void refreshActivity();
  clearInterval(activityTimer);
  activityTimer=setInterval(refreshActivity,3000);
 });
 dialog.addEventListener('close',()=>{requestedAvatar=undefined;clearInterval(activityTimer)});
 if(typeof MutationObserver==='function'){
  new MutationObserver(()=>{
   if(!dialog.open)return;
   void syncAvatarGlow(provider==='sintet'?account?.avatarURL||'':'');
   void window.go?.main?.App?.RefreshProfiles().catch(()=>{});
  }).observe(dialog,{attributes:true,attributeFilter:['open']});
 }
 document.querySelector('#account-close').addEventListener('click',close);document.querySelector('#account-done').addEventListener('click',close);
 let backdropDown=false;function outside(e){const r=dialog.getBoundingClientRect();return e.clientX<r.left||e.clientX>r.right||e.clientY<r.top||e.clientY>r.bottom}
 dialog.addEventListener('pointerdown',e=>{backdropDown=e.target===dialog&&outside(e)});dialog.addEventListener('click',e=>{if(backdropDown&&e.target===dialog&&outside(e))close();backdropDown=false});
 document.querySelector('#sintet-id-login').addEventListener('click',()=>window.SintetIdentity?.login());document.querySelector('#ely-login').addEventListener('click',()=>window.SintetIdentity?.loginEly());
 window.addEventListener('sintet:profile',syncProfile);
 window.SintetAccount={renderAvatar,setAccount(value,source='sintet',error=''){account=value;provider=source;accountError=error;syncProfile()}};syncProfile();
})();
