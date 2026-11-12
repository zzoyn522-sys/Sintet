(() => {
 const preview=window.SintetThemePreview;
 if(!preview)return;
 const screen=location.pathname.split('/').at(-1);
 const account={id:'00000000-0000-4000-8000-000000000001',username:'Design',handle:'design'};
 const game={id:'1.21.1',base:'1.21.1',loader:'vanilla',type:'release'};
 const settings={interfaceLanguage:'ru',gameDirectory:'C:\\Users\\Design\\Documents\\Sintet\\Minecraft',instanceMode:'shared',width:1280,height:720,javaPath:'',memoryMB:4096,autoMemory:true,workshopLanguage:'ru'};
 const methods={
  GetSettings:()=>({interfaceLanguage:'ru',settings,defaults:settings,systemMemoryMB:16384,effectiveMemoryMB:4096}),
  DefaultAppearance:()=>preview.defaults,
  GetAppearanceLibrary:()=>({current:preview.current,slots:[{name:'Design',settings:preview.current}],activeSlot:1}),
  Installations:()=>[game],
  ListVersions:()=>({versions:[game],latest:game.id}),
  VersionVariants:()=>({variants:[]}),
  State:()=>({busy:false}),
  GetNetworkStatus:()=>({online:true}),
  InternetAvailable:()=>true,
  IdentityState:()=>({provider:'sintet',account,active:'design',profiles:[{key:'design',provider:'sintet',username:'Design',account,active:true}]}),
  LauncherImportState:()=>({seen:true}),
  ProfileActivity:()=>({minecraftMs:0,launcherMs:0}),
  LauncherVersion:()=>preview.version,
  LauncherDisplayVersion:()=>preview.version,
  WorkshopTargets:()=>[],
  FriendsState:()=>({friends:[],requests:[],invites:[]}),
  MinecraftNews:()=>[{title:'Minecraft Java Edition'}],
 };
 window.go={main:{App:new Proxy(methods,{get:(target,key)=>key==='then'?undefined:async(...args)=>target[key]?.(...args)})}};
 localStorage.setItem('sintet-social-guide-v1','accepted');
 localStorage.setItem('sintet.selectedProfile',JSON.stringify(game));
 localStorage.setItem('sintet.newsCollapsed','true');
 window.addEventListener('load',async()=>{
  const css=document.createElement('style');
  css.textContent='*,*::before,*::after{animation:none!important;transition:none!important;caret-color:transparent!important}';
  document.head.append(css);
  await window.SintetSettings.ready;
  await window.SintetAppearance.ready;
  window.SintetAccount.setAccount(account);
  if(screen==='settings'){
   document.querySelector('#settings-nav').click();
   document.querySelector('#settings-tab-appearance').click();
  }else if(screen==='profile')document.querySelector('#account-button').click();
  const background=document.querySelector('#appearance-background img');
  if(background?.src&&preview.current.background)await background.decode().catch(()=>{});
  if(preview.current.fontFile)await document.fonts.load('14px SintetImported').catch(()=>[]);
  await document.fonts.ready;
  setTimeout(()=>{document.documentElement.dataset.themePreviewReady='true'},350);
 });
})();
