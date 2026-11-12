(() => {
 const button=document.querySelector('#update-button');
 if(!button)return;
 const q=selector=>document.querySelector(selector),wait=ms=>new Promise(resolve=>setTimeout(resolve,ms));
 let api=null,checking=false,pollTimer=0,lastState=null;
 const ready=async()=>{for(let i=0;i<120&&!window.go?.main?.App;i++)await wait(100);return window.go?.main?.App};
 function render(state){
  if(!state)return;
  lastState=state;
  const active=!!state.available||!!state.downloading;
  button.hidden=!active;
  button.disabled=!!state.downloading;
  button.classList.toggle('downloading',!!state.downloading);
  button.classList.toggle('update-error',!!state.error);
  button.style.setProperty('--update-progress',`${Math.max(0,Math.min(100,state.percent||0))}%`);
  q('#update-percent').textContent=state.downloading?`${Math.floor(state.percent||0)}`:'';
  const {format,translate}=window.SintetText;
  const title=state.error?translate(state.error):state.downloading
   ?state.stage&&state.stage!=='Скачивание Sintet'?translate(state.stage):format('Загрузка Sintet %s%%',Math.floor(state.percent||0))
   :state.latestVersion?format('Установить обновление Sintet %s',state.latestLabel||state.latestVersion):format('Доступно обновление Sintet');
  button.title=title;button.setAttribute('aria-label',title);
  if(state.downloading&&!pollTimer)pollTimer=setInterval(sync,400);
  if(!state.downloading&&pollTimer){clearInterval(pollTimer);pollTimer=0}
 }
 async function sync(){
  if(!api)return;
  try{
   const state=await api.UpdateState();
   if(lastState?.downloading&&state.error)window.toast?.(window.SintetText.translate(state.error));
   render(state);
  }catch{}
 }
 async function check(){
  if(!api||checking||lastState?.downloading)return;
  checking=true;
  try{render(await api.CheckForUpdates())}catch(error){console.info('Sintet update check failed:',String(error))}
  finally{checking=false}
 }
 button.addEventListener('click',async()=>{
  if(!api||!lastState?.available||lastState.downloading)return;
  button.disabled=true;
  try{await api.InstallUpdate();await sync()}catch(error){button.disabled=false;button.title=String(error?.message||error);setTimeout(()=>{if(lastState)render(lastState)},2500)}
 });
 window.addEventListener('sintet:settings',()=>{if(lastState)render(lastState)});
 void ready().then(value=>{
  api=value;if(!api)return;
  (api.LauncherDisplayVersion?api.LauncherDisplayVersion():api.LauncherVersion()).then(version=>{const node=q('#app-version');if(node)node.textContent=`Sintet ${version}`}).catch(()=>{});
  void check();
  setInterval(check,5*60*1000);
  document.addEventListener('visibilitychange',()=>{if(!document.hidden)void check()});
  window.addEventListener('focus',()=>void check());
 });
})();
