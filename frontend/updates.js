(() => {
 const button=document.querySelector('#update-button');
 if(!button)return;
 const q=selector=>document.querySelector(selector),wait=ms=>new Promise(resolve=>setTimeout(resolve,ms));
 let api=null,checking=false,pollTimer=0,lastState=null;
 const wave=q('#update-bubble-shape');
 const reducedMotion=matchMedia('(prefers-reduced-motion: reduce)');
 let waveAnimation;
 function syncWaveMotion(){
  if(button.hidden||document.hidden||reducedMotion.matches){
   waveAnimation?.cancel();
   waveAnimation=undefined;
   return;
  }
  if(waveAnimation||!wave)return;
  const frameCount=96, pointCount=12, turn=2*Math.PI;
  const format=point=>point.map(value=>value.toFixed(2)).join(' ');
  // Interpolate the circular samples with shared tangents so the seam stays smooth.
  const frames=Array.from({length:frameCount+1},(_,frame)=>{
   const phase=turn*(frame%frameCount)/frameCount;
   const breathing=.35*Math.sin(2*phase);
   const points=Array.from({length:pointCount},(_,index)=>{
    const angle=turn*index/pointCount;
    const shift=angle-phase;
    const radius=16+breathing+1.35*Math.sin(3*shift)+.72*Math.cos(2*shift+.7)+.32*Math.sin(shift);
    return [20+radius*Math.cos(angle),20+radius*Math.sin(angle)];
   });
   const curves=points.map((point,index)=>{
    const previous=points[(index+pointCount-1)%pointCount];
    const next=points[(index+1)%pointCount];
    const after=points[(index+2)%pointCount];
    const start=point.map((value,axis)=>value+(next[axis]-previous[axis])/6);
    const end=next.map((value,axis)=>value-(after[axis]-point[axis])/6);
    return 'C'+format(start)+' '+format(end)+' '+format(next);
   });
   return {d:'path("M'+format(points[0])+curves.join('')+'Z")'};
  });
  waveAnimation=wave.animate(frames,{duration:8400,iterations:Infinity});
 }
 reducedMotion.addEventListener('change',syncWaveMotion);
 document.addEventListener('visibilitychange',syncWaveMotion);
 const ready=async()=>{for(let i=0;i<120&&!window.go?.main?.App;i++)await wait(100);return window.go?.main?.App};
 function render(state){
  if(!state)return;
  lastState=state;
  const active=!!state.available||!!state.downloading;
  button.hidden=!active;
  syncWaveMotion();
  button.disabled=!!state.downloading;
  button.classList.toggle('downloading',!!state.downloading);
  button.classList.toggle('update-error',!!state.error);
  const percent=Math.max(0,Math.min(100,state.percent||0));
  button.style.setProperty('--update-progress',String(state.downloading?percent/100:1));
  q('#update-percent').textContent=state.downloading?Math.floor(percent)+'%':'';
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
