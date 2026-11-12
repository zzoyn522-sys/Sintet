(() => {
 const telegram=document.querySelector('#community-telegram');
 if(!telegram)return;
 const entry=document.createElement('button');
 entry.id='community-support';
 entry.type='button';
 entry.className='community-button support-button';
 entry.title='Поддержать Sintet';
 entry.setAttribute('aria-label','Поддержать Sintet');
 entry.setAttribute('aria-haspopup','dialog');
 entry.setAttribute('aria-controls','support-dialog');
 entry.innerHTML='<span class="monobank-icon" aria-hidden="true"></span>';
 telegram.after(entry);

 const dialog=document.createElement('dialog');
 dialog.id='support-dialog';
 dialog.className='support-dialog';
 dialog.setAttribute('aria-labelledby','support-title');
 dialog.innerHTML=`
  <button type="button" class="support-close" aria-label="Закрыть">×</button>
  <header class="support-author">
   <img src="assets/brand/zzoyn.webp" alt="zzoyn" width="64" height="64" draggable="false">
   <div><strong>zzoyn</strong><span>Автор Sintet · Украина</span></div>
  </header>
  <h2 id="support-title"></h2>
  <p class="support-intro"></p>
  <div class="support-story" tabindex="0" role="region" aria-label="Обращение автора Sintet">
   <p>Я zzoyn, энтузиаст из Украины. В свободное время делаю Sintet, потому что хочу подарить каждому бесплатный, удобный и красивый лаунчер. Мне хочется, чтобы здесь было приятно собирать свой Minecraft, находить друзей и просто играть вместе.</p>
   <p>За каждой новой возможностью стоят часы работы, поиски ошибок и переделки. Иногда приходится долго разбираться с проблемой, которую в итоге никто даже не заметит. Но ради того, чтобы у тебя всё заработало с первого раза, хочется стараться дальше.</p>
   <p>У проекта есть постоянные расходы. Домены, базу данных и серверы нужно оплачивать, чтобы аккаунты, обновления и наши сервисы оставались доступными. Донаты помогают покрывать эти расходы и дают мне возможность продолжать развивать Sintet.</p>
   <p>Если этот лаунчер уже стал маленькой частью твоих вечеров с друзьями, буду очень благодарен за поддержку. Даже небольшая посильная сумма для меня многое значит. За ней я вижу человека, которому пригодился мой труд, и это помогает снова садиться за работу и верить в проект.</p>
   <div class="support-feedback">
    <p>Тёплые слова тоже очень важны. Оставь комментарий, честный отзыв или идею в Telegram. Мне хочется знать, что тебе понравилось и что ещё стоит улучшить.</p>
    <button type="button" class="support-comment">Оставить комментарий</button>
   </div>
  </div>
  <button type="button" class="support-donate"><span class="monobank-icon" aria-hidden="true"></span><span>Поддержать через Monobank</span></button>
  <p class="support-payment-note">При переводе оставь комментарий к платежу – свой ник, пожелание или пару тёплых слов. Буду рад узнать, кому пригодился Sintet.</p>
  <p class="support-error" role="alert" hidden></p>
  <footer><span>Спасибо, что ты здесь.</span><button type="button" class="support-continue">Продолжить</button></footer>`;
 document.body.append(dialog);

 const copy={
  welcome:{title:'Помоги Sintet расти',intro:'Рад, что ты здесь. Спасибо, что выбрал мой лаунчер.'},
  update:{title:'Спасибо, что ты с Sintet',intro:'Спасибо, что помогаешь проекту расти.'},
  manual:{title:'Помоги Sintet расти',intro:'Этот проект растёт благодаря людям, которым он нужен.'},
 };
 let app=null,automaticHandled=false,pollTimer=0;
 const wait=ms=>new Promise(resolve=>setTimeout(resolve,ms));
 const ready=(async()=>{
  for(let attempt=0;attempt<120&&!window.go?.main?.App;attempt++)await wait(100);
  return window.go?.main?.App;
 })();

 async function remember(version){
  if(!app||!version)return;
  try{await app.AcknowledgeSupportNotice(version)}
  catch(error){console.info('Sintet support notice:',String(error?.message||error))}
 }
 function open(kind,version){
  automaticHandled=true;
  clearTimeout(pollTimer);
  const text=copy[kind]||copy.manual;
  dialog.querySelector('h2').textContent=text.title;
  dialog.querySelector('.support-intro').textContent=kind==='update'&&version?'Sintet '+version+' · '+text.intro:text.intro;
  dialog.querySelector('.support-error').hidden=true;
  if(!dialog.open)dialog.showModal();
  dialog.querySelector('.support-story').scrollTop=0;
  void ready.then(async value=>{
   app=value;
   if(app)await remember(version||await app.LauncherVersion());
  }).catch(()=>{});
 }
 entry.onclick=async()=>{await window.SintetImport?.onboarding;open('manual')};
 dialog.querySelector('.support-close').onclick=()=>dialog.close();
 dialog.querySelector('.support-continue').onclick=()=>dialog.close();
 async function community(service,button){
  const error=dialog.querySelector('.support-error');
  error.hidden=true;
  button.disabled=true;
  try{
   app=app||await ready;
   if(!app)throw Error('Дождись запуска Sintet и попробуй ещё раз.');
   await app.OpenCommunity(service);
  }catch(problem){error.textContent=String(problem?.message||problem);error.hidden=false}
  finally{button.disabled=false}
 }
 const donate=dialog.querySelector('.support-donate'),comment=dialog.querySelector('.support-comment');
 donate.onclick=()=>void community('monobank',donate);
 comment.onclick=()=>void community('telegram',comment);

 async function check(){
  clearTimeout(pollTimer);
  if(automaticHandled||!app)return;
  try{
   await window.SintetImport?.onboarding;
   const notice=await app.SupportNotice();
   if(automaticHandled||!notice?.kind)return;
   const occupied=!!document.querySelector('dialog[open],.launcher-invitation:not([hidden])');
   if(document.hidden||!document.hasFocus()||notice.deferred||occupied){
    pollTimer=setTimeout(check,2000);
    return;
   }
   open(notice.kind,notice.version);
  }catch{}
 }
 void ready.then(value=>{
  app=value;
  if(app)pollTimer=setTimeout(check,1000);
 });
 window.addEventListener('focus',()=>{if(!automaticHandled)void check()});
 document.addEventListener('visibilitychange',()=>{if(!document.hidden&&!automaticHandled)void check()});
})();
