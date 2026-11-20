(() => {
 const dialog=document.querySelector('#launcher-tour');
 const card=dialog.querySelector('.tour-card');
 const spotlight=dialog.querySelector('.tour-spotlight');
 const title=dialog.querySelector('#tour-title');
 const description=dialog.querySelector('#tour-description');
 const stepNumber=dialog.querySelector('.tour-count');
 const progress=dialog.querySelector('.tour-progress');
 const back=dialog.querySelector('.tour-back');
 const next=dialog.querySelector('.tour-next');
 const main=document.querySelector('main');
 const profile=document.querySelector('#account-dialog');
 const reducedMotion=matchMedia('(prefers-reduced-motion: reduce)');
 const steps=[
  {page:'settings',tab:'#settings-tab-launcher',target:'.launcher-settings-nav',side:'right',title:'Настройки под себя',description:'Выбирай разделы слева, чтобы перейти к памяти, интерфейсу Minecraft и настройкам оверлея.'},
  {page:'settings',tab:'#settings-tab-launcher',target:'#launcher-settings-minecraft .behavior-card',scroll:true,title:'Интерфейс и звук',description:'Задай язык, масштаб интерфейса, громкость и поле зрения сразу для всех версий Minecraft.'},
  {page:'settings',tab:'#settings-tab-appearance',target:'#appearance-pane-background',height:240,title:'Твоё оформление',description:'Выбери цвет и высоту анимированной волны или добавь свой фон.'},
  {page:'workshop',workshop:['catalog','mods'],target:'#workshop .ws-main',height:260,title:'Каталог мастерской',description:'Ищи моды, шейдеры и ресурс-паки. Фильтры помогают подобрать файлы для твоей версии Minecraft.'},
  {page:'workshop',workshop:['local','modpacks'],target:'#workshop .ws-main',height:240,title:'Мои сборки',description:'Создай сборку с нуля или импортируй готовый файл .mrpack. Здесь же можно открыть настройки каждой сборки.'},
  {page:'home',preview:'profile',target:'#account-dialog',side:'right',cardWidth:295,title:'Меню профиля',description:'Отсюда можно войти в аккаунт или переключиться между сохранёнными профилями.'},
  {page:'home',preview:'lobby',target:'#friends-panel',side:'right',title:'Друзья и лобби',description:'Выбери сборку и создай лобби или введи шестизначный код друга. Когда хозяин откроет мир для сети, к нему можно будет присоединиться.'},
  {page:'home',target:'#home .launchbar',side:'above',title:'Пора играть',description:'Выбери версию Minecraft в параметрах и нажми «Играть». Лаунчер скачает нужные файлы.'},
 ];
 const indicators=steps.map(()=>progress.appendChild(document.createElement('span')));
 const navigate=page=>{
  const button=document.querySelector('.nav[data-page="'+page+'"]');
  if(button.classList.contains('active'))return;
  button.click();
  main.scrollTop=0;
 };
 let resolveFinished;
 window.SintetTour={finished:new Promise(resolve=>{resolveFinished=resolve})};
 let app,index=0,closing=false,frame=0,contentAnimation,preview='';

 function showPreview(name=''){
  if(preview===name)return;
  if(preview==='lobby')window.SintetLobbyTour?.close();
  if(preview==='profile'){
   profile.close();
   delete profile.dataset.tourPreview;
  }
  preview=name;
  if(name==='lobby')window.SintetLobbyTour?.open();
  if(name==='profile'){
   profile.dataset.tourPreview='';
   profile.show();
  }
 }

 function place(){
  const entry=steps[index],target=document.querySelector(entry.target);
  const bounds=target.getBoundingClientRect();
  const scrollBounds=main.contains(target)?main.getBoundingClientRect():null;
  const margin=8, gap=17, padding=6;
  const width=dialog.clientWidth,height=dialog.clientHeight;
  const clamp=(value,min,max)=>Math.max(min,Math.min(value,max));
  const visibleTop=Math.max(margin,scrollBounds?.top??0);
  const visibleBottom=Math.min(height-margin,scrollBounds?.bottom??height);
  const left=Math.max(margin,bounds.left-padding),right=Math.min(width-margin,bounds.right+padding);
  const top=clamp(bounds.top-padding,visibleTop,visibleBottom);
  const bottom=clamp(bounds.top+Math.min(bounds.height,entry.height??bounds.height)+padding,top,visibleBottom);
  spotlight.style.setProperty('--tour-x',left+'px');
  spotlight.style.setProperty('--tour-y',top+'px');
  spotlight.style.setProperty('--tour-w',Math.max(0,right-left)+'px');
  spotlight.style.setProperty('--tour-h',bottom-top+'px');

  card.style.setProperty('--tour-card-width',(entry.cardWidth??352)+'px');
  const cardWidth=card.offsetWidth,cardHeight=card.offsetHeight;
  const centerX=(left+right)/2,centerY=(top+bottom)/2;
  const space={
   below:height-margin-bottom-gap-cardHeight,
   above:top-margin-gap-cardHeight,
   right:width-margin-right-gap-cardWidth,
   left:left-margin-gap-cardWidth,
  };
  const choices=[entry.side??'below','below','above','right','left'];
  const side=choices.find(side=>space[side]>=0)??choices.reduce((best,side)=>space[side]>space[best]?side:best);
  const cardLeft=clamp(side==='right'?right+gap:side==='left'?left-gap-cardWidth:centerX-cardWidth/2,margin,width-cardWidth-margin);
  const cardTop=clamp(side==='below'?bottom+gap:side==='above'?top-gap-cardHeight:centerY-cardHeight/2,margin,height-cardHeight-margin);
  const horizontal=side==='below'||side==='above';
  card.dataset.side=side;
  card.style.setProperty('--tour-x',cardLeft+'px');
  card.style.setProperty('--tour-y',cardTop+'px');
  card.style.setProperty('--tour-pointer',clamp(horizontal?centerX-cardLeft:centerY-cardTop,20,(horizontal?cardWidth:cardHeight)-20)+'px');
 }

 function schedulePlacement(){
  if(!dialog.open||frame)return;
  frame=requestAnimationFrame(()=>{
   frame=0;
   if(dialog.open)place();
  });
 }

 function showStep(step){
  index=step;
  const entry=steps[index];
  if(preview&&preview!==entry.preview)showPreview();
  navigate(entry.page);
  if(entry.tab){
   const tab=document.querySelector(entry.tab);
   if(tab.getAttribute('aria-selected')!=='true')tab.click();
   if(!entry.scroll)main.scrollTop=0;
  }
  if(entry.workshop)window.SintetWorkshop?.showTourSection(...entry.workshop);
  title.textContent=entry.title;
  description.textContent=entry.description;
  stepNumber.textContent=(index+1)+' из '+steps.length;
  indicators.forEach((indicator,i)=>{indicator.dataset.filled=String(i<=index)});
  back.hidden=index===0;
  next.textContent=index===steps.length-1?'Готово':'Далее';
  const first=!dialog.open;
  if(first)dialog.showModal();
  showPreview(entry.preview);
  if(entry.scroll){
   document.querySelector(entry.target).scrollIntoView({
    block:'center',inline:'nearest',behavior:reducedMotion.matches?'instant':'smooth',
   });
  }
  if(first){
   place();
   requestAnimationFrame(()=>{if(dialog.open)dialog.dataset.positioned='true'});
  }else{
   place();
   contentAnimation?.cancel();
   if(!reducedMotion.matches)contentAnimation=card.querySelector('.tour-copy').animate(
    [{opacity:.45,transform:'translateY(4px)'},{opacity:1,transform:'translateY(0)'}],
    {duration:240,easing:'ease-out'},
   );
  }
  if(!card.contains(document.activeElement)||document.activeElement===back&&back.hidden)next.focus({preventScroll:true});
 }

 async function finish(){
  if(closing)return;
  closing=true;
  if(!reducedMotion.matches){
   await dialog.animate([{opacity:1},{opacity:0}],{duration:155,easing:'ease-in',fill:'forwards'}).finished;
  }
  showPreview();
  dialog.close();
  navigate('home');
  try{await app.FinishLauncherTour()}
  catch(error){toast(String(error?.message||error))}
  finally{resolveFinished()}
 }
 function advance(){
  if(closing)return;
  if(index===steps.length-1)void finish();
  else showStep(index+1);
 }
 function previous(){
  if(!closing&&index>0)showStep(index-1);
 }
 back.onclick=previous;
 next.onclick=advance;
 dialog.querySelector('.tour-skip').onclick=()=>void finish();
 dialog.addEventListener('cancel',event=>{event.preventDefault();void finish()});
 dialog.addEventListener('keydown',event=>{
  if(event.key==='ArrowLeft'){event.preventDefault();previous()}
  if(event.key==='ArrowRight'){event.preventDefault();advance()}
 });
 window.addEventListener('resize',schedulePlacement);
 main.addEventListener('scroll',schedulePlacement,{passive:true});
 window.addEventListener('sintet:settings',schedulePlacement);
 const resize=new ResizeObserver(schedulePlacement);
 resize.observe(card);
 for(const {target} of steps)resize.observe(document.querySelector(target));

 async function boot(){
  await window.SintetSettings.ready;
  app=window.go?.main?.App;
  if(!await app?.LauncherTourPending?.()){resolveFinished();return}
  while(document.querySelector('dialog[open]'))await new Promise(resolve=>setTimeout(resolve,250));
  showStep(0);
 }
 void boot().catch(()=>{
  showPreview();
  if(dialog.open)dialog.close();
  resolveFinished();
 });
})();
