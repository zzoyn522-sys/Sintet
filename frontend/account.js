(() => {
 const dialog=document.querySelector('#account-dialog'),trigger=document.querySelector('#account-button'),status=document.querySelector('#account-status');
 let account=null,provider='sintet',accountError='';
 const defaultAvatar='assets/brand/default-avatar.webp';
 let badgeCatalog=new Map(),badgeCatalogSnapshot='',badgeCatalogRequest=0;
 async function refreshBadges(force=false){
  const request=++badgeCatalogRequest;
  try{
   const response=await fetch('https://sintet-identity.zzoyn522.workers.dev/api/profile-badges',force?{cache:'no-store'}:undefined);
   if(!response.ok)return;
   const {items}=await response.json();
   if(request!==badgeCatalogRequest||!Array.isArray(items))return;
   const next=new Map();
   for(const item of items.slice(0,256)){
    if(!item||typeof item.id!=='string'||!/^[a-z0-9][a-z0-9-]{0,39}$/.test(item.id)||
       typeof item.label!=='string'||item.label.length>80||typeof item.png!=='string'||
       item.png.length>32000||!/^iVBORw0KGgo[A-Za-z0-9+/]*={0,2}$/.test(item.png))continue;
    next.set(item.id,{src:'data:image/png;base64,'+item.png,label:item.label});
   }
   const snapshot=JSON.stringify([...next]);
   if(snapshot===badgeCatalogSnapshot)return;
   badgeCatalogSnapshot=snapshot;
   badgeCatalog=next;
   window.dispatchEvent(new Event('sintet:profile-badges'));
  }catch{ /* Keep the last catalog while the network is unavailable. */ }
 }
 function renderName(target,name,person){
  let label=target.firstElementChild;
  if(!label?.classList.contains('account-display-label')){
   label=document.createElement('span');
   label.className='account-display-label';
   target.classList.add('account-display-name');
   target.replaceChildren(label);
  }
  if(label.textContent!==name)label.textContent=name;
  const badges=Array.isArray(person?.badges)?person.badges:[];
  const wanted=[...new Set(badges)].filter(id=>badgeCatalog.has(id));
  const current=[...target.querySelectorAll('.account-role-badge')];
  if(current.length===wanted.length&&current.every((image,i)=>{
   const item=badgeCatalog.get(wanted[i]);
   return image.dataset.badge===wanted[i]&&image.getAttribute('src')===item.src;
  }))return;
  current.forEach(image=>image.remove());
  label.after(...wanted.map(id=>{
   const item=badgeCatalog.get(id);
   const image=document.createElement('img');
   image.className='account-role-badge';
   image.dataset.badge=id;
   image.src=item.src;
   image.alt=item.label;
   image.draggable=false;
   return image;
  }));
 }
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
 let avatarHighlight='#ffffff';
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
 document.querySelector('#account-handle').addEventListener('click',async()=>{
  if(provider==='sintet'){settingsLink.click();return;}
  const handle=window.SintetLobbyProfile?.handle;
  if(!handle)return;
  try{await navigator.clipboard.writeText('@'+handle);toast('Юзернейм скопирован')}catch{}
 });
 const avatarChange=document.querySelector('#account-lobby-avatar');
 avatarChange.addEventListener('click',async()=>{
  avatarChange.disabled=true;
  try{
   const address=await window.go?.main?.App?.ChangeLobbyAvatar();
   if(address){toast('Аватарка обновлена');void syncProfile();}
  }catch(error){toast(String(error))}
  finally{avatarChange.disabled=false}
 });
 settingsLink.addEventListener('click',()=>window.go?.main?.App?.OpenAccountSettings().catch(()=>toast('Не удалось открыть настройки')));
 function applyGlow(colors,highlight='#ffffff'){
  avatarHighlight=highlight;
  ['start','middle','end'].forEach((stop,index)=>{
   dialog.style.setProperty('--avatar-glow-'+stop,colors[index]);
  });
 }
 async function syncAvatarGlow(url){
  if(!dialog.open||!dialog.style||requestedAvatar===url)return;
  requestedAvatar=url;
  const cached=avatarPalettes.get(url);
  if(cached){applyGlow(cached.gradient,cached.highlight);return}
  applyGlow(defaultGlow);
  if(!url)return;
  try{
   const palette=await window.go?.main?.App?.AvatarGlow(url);
   if(palette?.gradient?.length!==3||![...palette.gradient,palette.highlight].every(color=>/^#[0-9a-f]{6}$/i.test(color)))return;
   if(avatarPalettes.size>=16)avatarPalettes.delete(avatarPalettes.keys().next().value);
   avatarPalettes.set(url,palette);
   if(dialog.open&&requestedAvatar===url)applyGlow(palette.gradient,palette.highlight);
  }catch{
   if(requestedAvatar===url)requestedAvatar=undefined;
  }
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
  const handle=document.querySelector('#account-handle');
  const candidate=window.SintetLobbyProfile;
  const lobby=candidate?.provider===provider&&(provider!=='sintet'||candidate.id===account?.id)?candidate:null;
  renderName(document.querySelector('#account-title'),name,lobby);
  const username=provider==='sintet'?account?.handle:lobby?.handle;
  handle.hidden=provider==='sintet'?!account?.id:!username;
  handle.textContent=username?'@'+username:'Задать юзернейм на сайте';
  handle.title=provider==='sintet'?'Настройки Sintet ID':'Скопировать юзернейм для лобби';
  avatarChange.hidden=provider!=='microsoft'&&provider!=='ely';
  const idButton=document.querySelector('#account-id');
  idButton.hidden=!account?.publicID;
  document.querySelector('#account-id-value').textContent=account?.publicID||'';
  const avatarURL=(provider==='sintet'?account?.avatarURL:lobby?.avatarUrl)||'';
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
   const skinLogo=document.querySelector('#account-skin-logo');
   const skinLogoSrc=microsoft?'assets/brand/microsoft.svg':'assets/brand/'+(ely?'ely':'sintet-id')+'-icon-light.svg';
   if(skinLogo.getAttribute('src')!==skinLogoSrc)skinLogo.src=skinLogoSrc;
   document.querySelector('#account-skin-provider').textContent=microsoft?'Microsoft':ely?'Ely.by':'Sintet ID';
   document.querySelector('#account-skin-description').textContent=microsoft?'Скин из профиля Minecraft':ely?'Скин из профиля Ely.by':'Скин из профиля Sintet ID';
  }
  syncNetworkStatus();
 }
 document.querySelector('#account-id-copy').addEventListener('click',async()=>{
  if(!account?.publicID)return;
  try{await navigator.clipboard.writeText(account.publicID);toast('ID скопирован')}
  catch{toast('Не удалось скопировать ID')}
 });
 const content=document.querySelector('#account-dialog-content');
 const spectrum=document.createElement('canvas');
 spectrum.className='account-audio-wave';
 spectrum.setAttribute('aria-hidden','true');
 dialog.prepend(spectrum);
 const wave=spectrum.getContext('2d');
 let drawProfileWave;
 import('./background-wave.js').then(module=>{drawProfileWave=module.drawProfileWave}).catch(()=>{});
 const motion=matchMedia('(prefers-reduced-motion: reduce)');
 const bands=new Float32Array(8);
 let targetBands=[],spectrumFrame=0,spectrumRead=false,spectrumActive=false,nextSample=0;
 const appearance=document.documentElement;
 function spectrumVisible(){return !!wave&&dialog.open&&!document.hidden&&!motion.matches&&!appearance.hasAttribute('data-profile-wave-off')&&!appearance.hasAttribute('data-profile-wave-transparent')}
 async function sampleSpectrum(){
  const api=window.go?.main?.App;
  if(spectrumRead||!api?.ProfileAudioSpectrum)return;
  const active=spectrumVisible();
  if(!active&&!spectrumActive)return;
  spectrumRead=true;
  spectrumActive=active;
  try{
   const values=await api.ProfileAudioSpectrum(active);
   if(active&&spectrumVisible())targetBands=values||[];
  }catch{targetBands=[]}
  finally{
   spectrumRead=false;
   if(active!==spectrumVisible())void sampleSpectrum();
  }
 }
 function drawSpectrum(now){
  spectrumFrame=0;
  if(!spectrumVisible())return;
  if(now>=nextSample){nextSample=now+25;void sampleSpectrum()}
  const ratio=Math.min(devicePixelRatio||1,2);
  const width=spectrum.clientWidth,height=spectrum.clientHeight;
  if(width&&height){
   const w=Math.round(width*ratio),h=Math.round(height*ratio);
   if(spectrum.width!==w||spectrum.height!==h){spectrum.width=w;spectrum.height=h}
   wave.setTransform(ratio,0,0,ratio,0,0);
   wave.clearRect(0,0,width,height);
   for(let i=0;i<bands.length;i++){
    const level=(targetBands[i]||0)/1000;
    bands[i]+=(level-bands[i])*(level>bands[i]?.68:.19);
   }
   const bass=Math.max(bands[0],bands[1],bands[2]);
   if(bands.some(level=>level>.005)){
    const levelAt=(x)=>{
     const position=x*(bands.length-1);
     const left=Math.floor(position);
     const fraction=position-left;
     const blend=fraction*fraction*(3-2*fraction);
     const local=bands[left]+(bands[Math.min(left+1,bands.length-1)]-bands[left])*blend;
     const ripple=.84+.16*Math.sin(x*19-now*.007);
     // Keep the original amplitude without flattening peaks at a fixed depth.
     return 54*amplitude*(local*ripple*1.18+bass*(.21+.075*Math.sin(x*13+now*.004)));
    };
    const amplitude=Number(appearance.dataset.profileWaveHeight)||1;
    const color=appearance.dataset.profileWaveColor||avatarHighlight;
    const alpha=Math.round(255*(.11+.12*bass)*(Number(appearance.dataset.profileWaveOpacity)||1));
    if(appearance.dataset.profileWaveStyle==='line'){
     const intensity=(Number(appearance.dataset.profileWaveOpacity)||1)*Math.min(1,Math.max(...bands)*10);
     drawProfileWave?.(wave,{color,amplitude,intensity:100*intensity,bands,trail:appearance.dataset.profileWaveTrail});
    }else{
    const gradient=wave.createLinearGradient(0,0,0,90*amplitude);
    gradient.addColorStop(0,color+alpha.toString(16).padStart(2,'0'));
    gradient.addColorStop(1,color+'00');
    wave.fillStyle=gradient;
    wave.beginPath();
    wave.moveTo(0,0);
    for(let i=0;i<=64;i++){
     const x=i/64;
     wave.lineTo(x*width,levelAt(x));
    }
    wave.lineTo(width,0);
    wave.closePath();
    wave.fill();
    }
   }
  }
  spectrumFrame=requestAnimationFrame(drawSpectrum);
 }
 function syncSpectrum(){
  if(spectrumVisible()){
   if(!spectrumFrame)spectrumFrame=requestAnimationFrame(drawSpectrum);
  }else{
   cancelAnimationFrame(spectrumFrame);
   spectrumFrame=0;
   targetBands=[];
   bands.fill(0);
   wave?.clearRect(0,0,spectrum.width,spectrum.height);
   void sampleSpectrum();
  }
 }
 motion.addEventListener('change',syncSpectrum);
 document.addEventListener('visibilitychange',syncSpectrum);
 new MutationObserver(syncSpectrum).observe(appearance,{attributes:true,attributeFilter:['data-profile-wave-off','data-profile-wave-transparent']});
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

 let closing;
 function close(){
  if(closing)return closing;
  if(!dialog.open)return Promise.resolve();
  const animation=dialog.getAnimations?.().find(item=>item.animationName==='account-profile-fade');
  if(!animation){
   dialog.close();
   return Promise.resolve();
  }
  animation.reverse();
  closing=animation.finished.catch(()=>{}).then(()=>{
   if(dialog.open)dialog.close();
   animation.cancel();
   closing=undefined;
  });
  return closing;
 }
 dialog.addEventListener('cancel',event=>{event.preventDefault();void close()});
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
   syncSpectrum();
   if(!dialog.open)return;
   void window.go?.main?.App?.RefreshProfiles().catch(()=>{});
  }).observe(dialog,{attributes:true,attributeFilter:['open']});
 }
 document.querySelector('#account-close').addEventListener('click',close);document.querySelector('#account-done').addEventListener('click',close);
 let backdropDown=false;function outside(e){const r=dialog.getBoundingClientRect();return e.clientX<r.left||e.clientX>r.right||e.clientY<r.top||e.clientY>r.bottom}
 dialog.addEventListener('pointerdown',e=>{backdropDown=e.target===dialog&&outside(e)});dialog.addEventListener('click',e=>{if(backdropDown&&e.target===dialog&&outside(e))close();backdropDown=false});
 document.querySelector('#sintet-id-login').addEventListener('click',()=>window.SintetIdentity?.login());document.querySelector('#ely-login').addEventListener('click',()=>window.SintetIdentity?.loginEly());
 window.addEventListener('sintet:profile',syncProfile);
 window.addEventListener('sintet:lobby-profile',syncProfile);
 window.addEventListener('sintet:profile-badges',syncProfile);
 window.addEventListener('sintet:badge-catalog',()=>void refreshBadges(true));
 refreshBadges();
 setInterval(refreshBadges,60000);
 window.SintetAccount={renderAvatar,renderName,close,setAccount(value,source='sintet',error=''){account=value;provider=source;accountError=error;syncProfile()}};syncProfile();
})();
