(()=>{
 const toggle=document.querySelector('#friends-toggle'),panel=document.querySelector('#friends-panel');
 const list=document.querySelector('#friends-list'),footer=panel.querySelector('footer');
 const presence=document.querySelector('#rich-presence');
 const presenceList=presence.querySelector('#presence-friends');
 const presenceEmpty=presence.querySelector('#presence-empty');
 const presenceOnline=presence.querySelector('#presence-online');
 const presenceActions=presence.querySelector('#presence-actions');
 const presenceLogin=presence.querySelector('#presence-login');
 const privacyButton=presence.querySelector('#presence-privacy');
 const titleActions=document.createElement('div');
 titleActions.className='friends-title-actions';
 const title=panel.querySelector('#friends-title');
 title.parentElement.append(titleActions);
 const requestsButton=document.createElement('button');
 requestsButton.type='button';
 requestsButton.className='friends-requests-toggle';
 requestsButton.setAttribute('aria-label','Заявки в друзья');
 requestsButton.setAttribute('aria-expanded','false');
 requestsButton.title='Заявки в друзья';
 requestsButton.innerHTML='<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M18 9a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9M10 21h4"/></svg><span class="friends-requests-count" hidden></span>';
 titleActions.append(requestsButton);
 const api=()=>window.go?.main?.App;
 const avatars=new Map();
 const guideKey='sintet-social-guide-v1';
 let guideAccepted=false;
 try{guideAccepted=localStorage.getItem(guideKey)==='accepted'}catch{}
 let continueAfterGuide;
 const guide=document.createElement('dialog');
 guide.className='social-guide';
 guide.setAttribute('aria-labelledby','social-guide-title');
 guide.innerHTML=`
  <header>
   <span class="social-guide-kicker">ПЕРЕД ПЕРВОЙ ИГРОЙ С ДРУЗЬЯМИ</span>
   <h2 id="social-guide-title">Как работают друзья и P2P</h2>
   <p>Настоятельно рекомендуем прочитать памятку целиком перед подключением.</p>
  </header>
  <div class="social-guide-scroll" tabindex="0" role="region" aria-label="Памятка о P2P и игровом оверлее">
   <section>
    <h3>Как друг попадает в твой мир</h3>
    <p>Создай лобби, выбери сборку и пригласи друзей. Подготовить файлы можно до запуска игры. Когда лидер откроет мир для сети, участники смогут запустить Minecraft и присоединиться. Между лаунчерами используется зашифрованное P2P-соединение.</p>
    <p>Участники прямого соединения могут узнать сетевые IP-адреса друг друга. Приглашай тех, кому доверяешь. Роутер или провайдер может помешать установить туннель. Тогда подключение к миру не состоится.</p>
   </section>
   <section>
    <h3>Как оверлей появляется в Minecraft</h3>
    <p>Если оверлей включён, Sintet запускает Java с параметром <code>-agentpath</code>. Сама Java загружает <code>sintet-overlay.dll</code> при старте игры. Это загрузка нативного модуля в процесс Minecraft. DLL перехватывает вывод кадра, чтобы рисовать панель поверх игры и обрабатывать Shift + Tab.</p>
    <p>Панель общается с лаунчером через локальный адрес <code>127.0.0.1</code> с токеном доступа. Автовход передаётся отдельному Java-мосту, который запускает подключение в игровом потоке. Оверлей можно отключить в настройках до запуска игры.</p>
   </section>
   <section>
    <h3>Как проверить DLL</h3>
    <p>DLL входит в сборку Sintet. Перед запуском лаунчер сравнивает SHA-256 файла с SHA-256 встроенной копии. Так можно обнаружить подмену файла на диске. Хеш подтверждает совпадение файлов, а безопасность кода оценивают отдельно.</p>
    <p>Текущая DLL без подписи Authenticode. Если хочешь проверить установленный файл, посмотри его SHA-256 через PowerShell и сравни с хешем этой сборки ниже.</p>
    <code class="social-guide-command">Get-FileHash -Algorithm SHA256 ./sintet-overlay.dll</code>
    <p class="social-guide-digest" data-overlay-hash hidden></p>
   </section>
  </div>
  <footer>
   <span class="social-guide-progress" aria-live="polite">Прокрути памятку до конца</span>
   <button type="button" class="social-guide-cancel">Позже</button>
   <button type="button" class="social-guide-accept" disabled>Прочитал, продолжить</button>
  </footer>`;
 document.body.append(guide);
 const guideScroll=guide.querySelector('.social-guide-scroll');
 const guideAccept=guide.querySelector('.social-guide-accept');
 const guideProgress=guide.querySelector('.social-guide-progress');
 function updateGuideProgress(){
  const reachedEnd=guideScroll.scrollTop+guideScroll.clientHeight>=guideScroll.scrollHeight-2;
  if(guide.open&&reachedEnd){
   guideAccept.disabled=false;
   guideProgress.textContent='Конец памятки';
  }
 }
 function showGuide(continuation){
  continueAfterGuide=continuation;
  guideAccept.disabled=true;
  guideProgress.textContent='Прокрути памятку до конца';
  guide.showModal();
  guideScroll.scrollTop=0;
  guideScroll.focus({preventScroll:true});
  requestAnimationFrame(updateGuideProgress);
  const hash=guide.querySelector('[data-overlay-hash]');
  api()?.OverlayBuildHash?.().then(value=>{
   if(!/^[a-f0-9]{64}$/.test(value))return;
   hash.textContent='SHA-256 встроенной DLL '+value;
   hash.hidden=false;
  }).catch(()=>{});
 }
 guideScroll.addEventListener('scroll',updateGuideProgress);
 guide.querySelector('.social-guide-cancel').onclick=()=>guide.close();
 guideAccept.onclick=()=>{
  if(guideAccept.disabled)return;
  guideAccepted=true;
  try{localStorage.setItem(guideKey,'accepted')}catch{}
  guide.close();
  const continuation=continueAfterGuide;
  continueAfterGuide=null;
  continuation?.();
 };
 toggle.addEventListener('click',event=>{
  if(guideAccepted)return;
  event.preventDefault();
  showGuide(()=>panel.showPopover());
 });
 const guideLink=document.createElement('button');
 guideLink.type='button';
 guideLink.className='social-guide-link';
 guideLink.textContent='О P2P и оверлее';
 guideLink.onclick=()=>{
  panel.hidePopover();
  showGuide(()=>panel.showPopover());
 };

 let busy=false,state=null,searchGeneration=0,activeTab='friends',showRequests=false,lastPackPhase='',lastPackProfile='';
 let acceptedInvite=null,inviteError='',inviteView='';
 const invitationBanner=document.createElement('aside');
 invitationBanner.className='launcher-invitation';
 invitationBanner.setAttribute('aria-label','Приглашение в лобби');
 invitationBanner.hidden=true;
 document.body.append(invitationBanner);
 function position(){const anchor=toggle.getBoundingClientRect(),top=anchor.bottom+10;panel.style.left=Math.max(12,Math.min(anchor.left,innerWidth-410))+'px';panel.style.top=top+'px';panel.style.maxHeight=Math.max(160,innerHeight-top-12)+'px'}
 panel.addEventListener('beforetoggle',event=>{if(event.newState==='open'){position();refresh()}});
 panel.addEventListener('toggle',event=>toggle.setAttribute('aria-expanded',String(event.newState==='open')));
 window.addEventListener('resize',()=>{if(panel.matches(':popover-open'))position()});
 const tools=document.createElement('div');tools.className='friends-tools';
 tools.innerHTML='<div class="friends-search"><input id="friend-search" placeholder="Поиск по друзьям" autocomplete="off" maxlength="36"><button type="button" id="friend-search-submit">Найти аккаунт</button></div><div id="friend-search-results"></div>';
 list.before(tools);
 const requestsPanel=element('div','friends-requests-list');
 requestsPanel.hidden=true;
 tools.before(requestsPanel);
 requestsButton.onclick=()=>{
  showRequests=!showRequests;
  requestsButton.setAttribute('aria-expanded',String(showRequests));
  render();
 };
 const tabs=element('div','social-tabs');tabs.setAttribute('role','tablist');tabs.setAttribute('aria-label','Друзья и лобби');
 const friendTab=element('button','','Друзья'),lobbyTab=element('button','','Лобби');
 const friendPage=element('section','social-tab-page'),lobbyPage=element('section','social-tab-page');
 friendPage.id='social-friends-page';lobbyPage.id='social-lobby-page';friendPage.setAttribute('role','tabpanel');lobbyPage.setAttribute('role','tabpanel');
 const offlineNotice=element('p','social-offline',window.SintetNetwork.message);
 offlineNotice.setAttribute('role','status');
 offlineNotice.hidden=true;
 panel.append(offlineNotice,tabs,friendPage,lobbyPage);friendPage.append(tools,list);lobbyPage.append(footer);
 function selectTab(name){activeTab=name;const friends=name==='friends';friendPage.hidden=!friends;lobbyPage.hidden=friends;friendTab.setAttribute('aria-selected',String(friends));lobbyTab.setAttribute('aria-selected',String(!friends));friendTab.tabIndex=friends?0:-1;lobbyTab.tabIndex=friends?-1:0;}
 for(const [button,name,page] of [[friendTab,'friends',friendPage],[lobbyTab,'lobby',lobbyPage]]){button.type='button';button.setAttribute('role','tab');button.id='social-tab-'+name;button.setAttribute('aria-controls',page.id);page.setAttribute('aria-labelledby',button.id);button.onclick=()=>selectTab(name);button.onkeydown=event=>{if(event.key==='ArrowLeft'||event.key==='ArrowRight'){event.preventDefault();selectTab(name==='friends'?'lobby':'friends');(name==='friends'?lobbyTab:friendTab).focus()}};tabs.append(button)}
 selectTab('friends');
 function openSocialTab(name){
  const open=()=>{
   showRequests=false;
   selectTab(name);
   render();
   if(!panel.matches(':popover-open'))panel.showPopover();
  };
  if(guideAccepted)open();else showGuide(open);
 }
 presence.querySelector('#presence-open-friends').onclick=()=>openSocialTab('friends');
 presence.querySelector('#presence-open-lobby').onclick=()=>openSocialTab('lobby');
 presenceLogin.onclick=()=>document.querySelector('#account-button').click();
 footer.innerHTML='<div id="friends-room" class="friends-room" hidden><span>Код комнаты</span><button type="button" id="friends-copy-room"></button></div><button type="button" id="friends-host" class="friends-create-lobby">Создать комнату</button><form id="friends-join" class="friends-search"><input id="friends-code" placeholder="Код комнаты · 6 цифр" inputmode="numeric" pattern="[0-9]{6}" maxlength="6" required><button type="submit">Войти в лобби</button></form><p id="friends-connection" role="status"></p><div class="lobby-address-actions"><button type="button" id="friends-endpoint" hidden></button><button type="button" id="friends-auto-join" hidden><svg viewBox="0 0 16 16" width="14" height="14" fill="currentColor" aria-hidden="true"><path d="M4 2v12l10-6z"/></svg></button></div><p id="friends-error" role="alert"></p>';
 function element(tag,cls,text){const node=document.createElement(tag);if(cls)node.className=cls;if(text)node.textContent=text;return node}
 function button(label,action,target){const node=element('button','friend-action',label);node.type='button';node.disabled=busy||state?.busy;node.onclick=()=>act(action,target);return node}
 async function act(action,target=''){
  if(busy||state?.busy||!api()||window.SintetNetwork.offline)return false;
  if(action==='invite-accept'){
   acceptedInvite=state?.invites?.find(invite=>invite.id===target&&invite.account)||
    (acceptedInvite?.id===target?acceptedInvite:null);
   inviteError='';
  }
  busy=true;render();
  try{
   await api().FriendsAction(action,target);
   if(action==='request')toast('Заявка в друзья отправлена');
   if(action==='friend-accept')toast('Заявка принята, вы теперь друзья');
   if(action==='friend-remove')toast(state?.friends?.some(friend=>friend.account?.id===target&&friend.status==='accepted')?'Друг удалён':'Заявка удалена');
   if(['host','join','invite-accept'].includes(action))selectTab('lobby');
   return true;
  }catch(error){
   const message=String(error?.message||error);
   if(action==='invite-accept'&&acceptedInvite?.id===target)inviteError=message;
   else toast(message);
   return false;
  }finally{busy=false;await refresh()}
 }
 function friendAvatar(account){
  const avatar=element('span','friend-avatar',account.username?.slice(0,1).toUpperCase()||'?');
  if(account.avatarUrl&&api()?.ResolveAvatar){
   if(!avatars.has(account.avatarUrl)){
    if(avatars.size>=256)avatars.delete(avatars.keys().next().value);
    avatars.set(account.avatarUrl,api().ResolveAvatar(account.avatarUrl).catch(()=>{avatars.delete(account.avatarUrl);return ''}));
   }
   avatars.get(account.avatarUrl).then(src=>{
    if(!src)return;
    const image=element('img','');image.alt='';image.src=src;
    avatar.replaceChildren(image);
   });
  }
  return avatar;
 }
 function row(account,status,buttons){
  const node=element('div','friend-row');node.append(friendAvatar(account));
  const content=element('div','friend-details');content.append(element('strong','',account.username));if(account.handle)content.append(element('small','friend-handle','@'+account.handle));content.append(element('span','',status));node.append(content);
  if(account.leader){
   const crown=element('span','lobby-crown');crown.title='Лидер лобби';
   crown.innerHTML='<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="m3 7 5 4 4-7 4 7 5-4-3 12H6Z" stroke-linejoin="round"/></svg>';
   content.querySelector('strong').append(crown);
  }
  const actions=element('div','friend-actions');actions.append(...buttons);node.append(actions);return node;
 }
 const preparationLabels={connecting:'Подключается',checking:'Сверяет сборку',required:'Нужно подготовить сборку',downloading:'Скачивает',installing:'Устанавливает Minecraft',stopping:'Перезапускает игру',launching:'Запускает игру',ready:'Готов',world:'В мире',error:'Нужна повторная попытка'};
 const presenceLabels={lobby:'В лобби',singleplayer:'Одиночный мир',multiplayer:'Мультиплеер',world:'В мире Minecraft',menu:'В главном меню',launcher:'В лаунчере',offline:'Не в сети'};
 const presenceOrder={lobby:0,multiplayer:1,singleplayer:1,world:1,menu:2,launcher:3};
 const loaderNames={fabric:'Fabric',forge:'Forge',neoforge:'NeoForge',quilt:'Quilt',vanilla:'Vanilla'};
 function invitationDetails(invite){
  const game=invite.game?'Minecraft '+invite.game:'';
  return [game,invite.build!==game?invite.build:'',loaderNames[invite.loader]].filter(Boolean).join(' · ');
 }

 function renderInvitation(offline){
  if(state?.gameRunning&&!busy&&!state.gameJoining){acceptedInvite=null;inviteError='';}
  const pending=(state?.invites||[]).find(invite=>invite.account&&invite.id!==acceptedInvite?.id);
  const invite=acceptedInvite||pending;
  if(offline||!state?.enabled||!invite||state.gameRunning&&(!acceptedInvite||!state.gameJoining)){
   invitationBanner.hidden=true;inviteView='';
   return;
  }
  const pack=acceptedInvite?state.pack:null;
  const phase=pack?.phase;
  const waiting=!!acceptedInvite&&!inviteError;
  const preparing=waiting&&['checking','downloading','stopping','installing','launching'].includes(phase);
  const needPack=waiting&&phase==='required';
  const connected=waiting&&state.lobbyConnected;
  const disconnected=waiting&&!busy&&!state.busy&&!pack&&!connected;
  const error=inviteError||(waiting&&phase==='error'?pack.error||'Не удалось подготовить сборку':disconnected?'Соединение с лобби закрыто':'');
  const joining=waiting&&!!state.gameJoining;
  const ready=connected&&!preparing&&!needPack&&!joining&&!error&&!busy&&!state.busy;
  const connectionStatus=state.joinAttempt>1?'Повторяем: '+(state.connection||'подключаемся к лобби'):state.connection||(connected?'Подключено к лобби':'Подключаемся к лобби…');
  const packStatus=preparing&&(pack.current||({checking:'Сверяем сборку',downloading:'Скачиваем сборку',
   stopping:'Закрываем Minecraft',installing:'Устанавливаем Minecraft',launching:'Запускаем Minecraft'}[phase]));
  const details=invitationDetails(invite);
  const description=error||(waiting&&!ready?(needPack?'Требуется сборка друга':packStatus||connectionStatus):connected&&(!state.worldReady||state.inWorld)?connectionStatus:details);
  const key=JSON.stringify([invite,waiting,preparing,needPack,ready,error,description,busy,!!state.busy]);
  if(!invitationBanner.hidden&&inviteView===key)return;
  inviteView=key;
  const focused=invitationBanner.contains(document.activeElement)?document.activeElement.dataset.inviteAction:'';
  const identity=element('div','launcher-invitation-copy');
  identity.append(element('strong','',connected?'Вы в лобби':invite.account.username+' приглашает в лобби'));
  if(description)identity.append(element('span','',description));
  if(waiting&&!ready&&details&&description!==details)identity.append(element('small','',details));
  const controls=element('div','launcher-invitation-actions');
  const control=(label,name,callback)=>{
   const item=element('button','',label);item.type='button';item.dataset.inviteAction=name;
   item.disabled=(busy||!!state?.busy)&&name!=='close';
   item.onclick=callback;controls.append(item);
  };
  const accept=()=>{
   const run=()=>void act('invite-accept',invite.id);
   if(guideAccepted)run();else showGuide(run);
  };
  const dismiss=()=>{acceptedInvite=null;inviteError='';renderInvitation(false);};
  if(error){
   control('Закрыть','close',dismiss);
   if(inviteError&&!state.localPort)control('Повторить','accept',accept);
  }else if(needPack){
   control('Отмена','decline',()=>{acceptedInvite=null;void act('close');});
   control('Подготовить сборку','accept',()=>confirmPack(pack));
  }else if(!waiting){
   control('Отклонить','decline',()=>void act('invite-decline',invite.id));
   control('Принять','accept',accept);
  }else if(ready){
   control('Закрыть','close',dismiss);
   control('Открыть лобби','open',()=>{dismiss();openSocialTab('lobby');});
  }else if(busy||preparing||joining||state.busy){
   const progress=element('span','launcher-invitation-spinner');progress.setAttribute('aria-hidden','true');
   controls.append(progress);
   if(['downloading','installing'].includes(phase))control('Отмена','decline',()=>void act('cancel-pack'));
  }
  const avatar=friendAvatar(invite.account);
  invitationBanner.replaceChildren(avatar,identity,controls);
  invitationBanner.hidden=false;
  if(focused)controls.querySelector('[data-invite-action="'+focused+'"]')?.focus({preventScroll:true});
 }
 const expandedPresence=new Set();
 let previousModes=new Map();
 let lastPresenceView='';
 const musicCovers=new Map();
 const privacyDialog=element('dialog','presence-dialog');
 privacyDialog.setAttribute('aria-labelledby','presence-privacy-title');
 privacyDialog.innerHTML=`<form>
  <header><h2 id="presence-privacy-title">Настройки видимости</h2><p>Sintet Rich Presence</p></header>
  <label class="presence-switch"><span><strong>Режим невидимки</strong><small>Текущий статус, игра и музыка скрываются от друзей.</small></span><input type="checkbox" name="invisible" role="switch"></label>
  <label class="presence-switch"><span><strong>Показывать музыку</strong><small>Друзья увидят трек из плеера Windows и его обложку.</small></span><input type="checkbox" name="shareMusic" role="switch"></label>
  <p class="presence-dialog-error" role="alert" hidden></p>
  <footer><button type="button" data-cancel>Отмена</button><button type="submit" class="primary">Сохранить</button></footer>
 </form>`;
 const removeDialog=element('dialog','presence-dialog');
 removeDialog.setAttribute('aria-labelledby','presence-remove-title');
 removeDialog.innerHTML=`<header><h2 id="presence-remove-title">Удалить из друзей?</h2><p></p></header>
 <footer><button type="button" data-cancel>Отмена</button><button type="button" class="presence-remove" data-remove>Удалить</button></footer>`;
 document.body.append(privacyDialog,removeDialog);
 const privacyForm=privacyDialog.querySelector('form');
 const privacyError=privacyDialog.querySelector('.presence-dialog-error');
 privacyButton.onclick=()=>{
  privacyForm.elements.invisible.checked=!!state?.privacy?.invisible;
  privacyForm.elements.shareMusic.checked=!!state?.privacy?.shareMusic;
  privacyError.hidden=true;
  privacyDialog.showModal();
 };
 privacyDialog.querySelector('[data-cancel]').onclick=()=>privacyDialog.close();
 privacyForm.onsubmit=async event=>{
  event.preventDefault();
  const submit=privacyForm.querySelector('[type="submit"]');
  submit.disabled=true;
  privacyError.hidden=true;
  try{
   state.privacy=await api().SavePresencePrivacy(privacyForm.elements.invisible.checked,privacyForm.elements.shareMusic.checked);
   renderPresence();
   privacyDialog.close();
   toast('Настройки видимости сохранены');
  }catch(error){privacyError.textContent=String(error?.message||error);privacyError.hidden=false}
  finally{submit.disabled=false}
 };
 let friendToRemove;
 removeDialog.querySelector('[data-cancel]').onclick=()=>removeDialog.close();
 removeDialog.querySelector('[data-remove]').onclick=()=>{
  const account=friendToRemove;
  removeDialog.close();
  if(account)void act('friend-remove',account.id);
 };
 removeDialog.addEventListener('close',()=>{friendToRemove=null});

 function displayGameVersion(version){
  if(!version)return '';
  const variant=version.match(/^(fabric|forge|neoforge|quilt)-.+?-(\d+\.\d+(?:\.\d+)?(?:-(?:snapshot|pre|rc)-?\d+)?|\d{2}w\d{2}[a-z])$/i);
  if(variant){
   const loader=loaderNames[variant[1].toLowerCase()];
   return variant[2]+' · '+loader;
  }
  return /^(?:\d+\.\d+(?:\.\d+)?(?:-(?:snapshot|pre|rc)-?\d+)?|\d{2}w\d{2}[a-z])$/i.test(version)?version:'';
 }

 function presenceButton(label,action,id){
  const control=button(label,action,id);
  control.dataset.presenceAction=action+'-'+id;
  control.onclick=()=>{
   if(guideAccepted)void act(action,id);
   else showGuide(()=>void act(action,id));
  };
  return control;
 }

 function friendMode(friend,inLobby){
  if(inLobby)return 'lobby';
  const mode=friend.presence?.activity?.mode||friend.presence?.status;
  return Object.hasOwn(presenceLabels,mode)?mode:'offline';
 }

 function formatPresenceTime(minutes){
  return window.SintetLanguage.formatDuration(minutes,{lessThanMinute:minutes===0,includeMinutes:true});
 }

 function totalPresenceTime(milliseconds){
  return window.SintetLanguage.formatDuration(Math.floor((milliseconds||0)/60000));
 }

 function updatePresenceTimers(){
  for(const timer of presenceList.querySelectorAll('.is-expanded [data-started-at]')){
   const elapsed=Math.max(0,Math.floor(Date.now()/1000)-Number(timer.dataset.startedAt));
   timer.textContent=formatPresenceTime(Math.floor(elapsed/60));
  }
 }

 function musicCard(music){
  const player=element('div','presence-music');
  const vinyl=element('div','presence-vinyl');vinyl.setAttribute('aria-hidden','true');
  const center=element('span','presence-vinyl-label');
  center.innerHTML='<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 18V6l11-2v12M9 18c0 1.1-1.3 2-3 2s-3-.9-3-2 1.3-2 3-2 3 .9 3 2Zm11-2c0 1.1-1.3 2-3 2s-3-.9-3-2 1.3-2 3-2 3 .9 3 2Z"/></svg>';
  const recordArt=element('img','presence-vinyl-cover');recordArt.alt='';recordArt.hidden=true;
  vinyl.append(center,recordArt);
  const track=element('div','presence-music-track');
  track.append(element('span','','Слушает сейчас'),element('strong','',music.title));
  if(music.artist)track.append(element('small','',music.artist));
  player.append(vinyl,track);
  return player;
 }

 function loadMusicCovers(){
  for(const player of presenceList.querySelectorAll('.is-expanded [data-cover-digest]')){
   if(player.dataset.coverLoading||Number(player.dataset.coverRetry||0)>Date.now())continue;
   const {coverAccount:account,coverDigest:digest}=player.dataset;
   const key=account+':'+digest;
   if(!api()?.FriendsMusicCover)continue;
   if(!musicCovers.has(key)){
    if(musicCovers.size>=32)musicCovers.delete(musicCovers.keys().next().value);
    const cover=api().FriendsMusicCover(account,digest).catch(()=>null);
    musicCovers.set(key,cover);
    cover.then(src=>{if(!src&&musicCovers.get(key)===cover)musicCovers.delete(key)});
   }
   player.dataset.coverLoading='true';
   musicCovers.get(key).then(src=>{
    if(!src){delete player.dataset.coverLoading;player.dataset.coverRetry=String(Date.now()+10000);return;}
    for(const img of player.querySelectorAll('.presence-music-backdrop,.presence-vinyl-cover')){img.src=src;img.hidden=false;}
    player.classList.add('has-music-cover');
   });
  }
 }

 function gamePresence(activity,version,mode){
  const server=mode==='multiplayer'?activity?.server:'';
  const hasTimer=Number.isSafeInteger(activity?.startedAt)&&activity.startedAt>0;
  if(!activity?.build&&!version&&!server&&!hasTimer)return null;
  const game=element('div','presence-game');
  const icon=element('span','presence-game-icon');
  icon.innerHTML='<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m12 3 8 4.5v9L12 21l-8-4.5v-9L12 3Zm0 9 8-4.5M12 12 4 7.5M12 12v9"/></svg>';
  const identity=element('div','presence-game-name');
  identity.append(element('strong','',activity?.build||'Minecraft'));
  const context=[version,server].filter(Boolean).join('  ·  ');
  if(context)identity.append(element('span','',context));
  game.append(icon,identity);
  if(hasTimer){
   const session=element('div','presence-session');
   const timer=element('span','presence-timer');timer.dataset.startedAt=activity.startedAt;
   session.append(element('small','','Сессия'),timer);
   game.append(session);
  }
  return game;
 }

 function presenceCard(friend){
  const {account,mode,activity,version,totals}=friend;
  const online=mode!=='offline';
  const card=element('article','presence-card');
  card.dataset.mode=mode;
  card.classList.toggle('friend-online',online);
  const summary=element('button','presence-summary');
  summary.type='button';
  summary.dataset.presenceAction='details-'+account.id;
  const identity=element('span','presence-identity');
  identity.append(element('strong','',account.username));
  if(account.handle)identity.append(element('small','presence-handle','@'+account.handle));
  const context=mode==='multiplayer'&&activity?.server?activity.server:activity?.build;
  if(context)identity.append(element('small','',context));
  const chevron=element('span','presence-chevron');chevron.setAttribute('aria-hidden','true');
  summary.append(friendAvatar(account),identity,element('span','presence-status',presenceLabels[mode]),chevron);
  const reveal=element('div','presence-reveal');
  reveal.id='presence-details-'+account.id;
  summary.setAttribute('aria-controls',reveal.id);
  const details=element('div','presence-details');
  const content=element('div','presence-detail-content');
  if(totals){
   const profile=element('div','presence-detail-profile');
   const portrait=friendAvatar(account);portrait.classList.add('presence-portrait');
   const totalsList=element('dl','presence-totals');
   for(const [label,time] of [['Всего в Minecraft',totals.minecraftMs],['Всего в лаунчере',totals.launcherMs]]){
    const stat=element('div','presence-total');
    stat.append(element('dt','',label),element('dd','',totalPresenceTime(time)));
    totalsList.append(stat);
   }
   profile.append(portrait,totalsList);content.append(profile);
  }
  const game=gamePresence(activity,version,mode);
  if(game)content.append(game);
  const music=activity?.music;
  if(music?.title){
   if(/^[a-f0-9]{64}$/.test(music.digest||'')){
    content.dataset.coverAccount=account.id;
    content.dataset.coverDigest=music.digest;
    const backdrop=element('img','presence-music-backdrop');backdrop.alt='';backdrop.hidden=true;
    content.prepend(backdrop);
   }
   content.append(musicCard(music));
  }
  const actions=element('div','presence-card-actions');
  if(online&&mode!=='lobby')actions.append(presenceButton('Пригласить','invite',account.id));
  if(mode==='lobby'){
   const lobby=element('button','friend-action','Лобби');lobby.type='button';
   lobby.onclick=()=>openSocialTab('lobby');actions.append(lobby);
  }
  const copy=element('button','friend-action','Скопировать ID');copy.type='button';
  copy.onclick=async()=>{try{await navigator.clipboard.writeText(account.id);toast('ID скопирован')}catch{toast('Не удалось скопировать ID')}};
  const remove=element('button','friend-action presence-remove','Удалить из друзей');remove.type='button';
  remove.disabled=busy||state?.busy;
  remove.onclick=()=>{
   friendToRemove=account;
   removeDialog.querySelector('p').textContent=account.username;
   removeDialog.showModal();
  };
  actions.append(copy,remove);
  content.append(actions);details.append(content);reveal.append(details);
  card.append(summary,reveal);

  function setExpanded(expanded){
   summary.setAttribute('aria-expanded',String(expanded));
   card.classList.toggle('is-expanded',expanded);
   reveal.inert=!expanded;
  }
  setExpanded(expandedPresence.has(account.id));
  summary.onclick=()=>{
   const next=!expandedPresence.has(account.id);
   if(next)expandedPresence.add(account.id);else expandedPresence.delete(account.id);
   setExpanded(next);
   if(next){updatePresenceTimers();loadMusicCovers();}
  };
  if(previousModes.has(account.id)&&previousModes.get(account.id)!==mode&&online)card.classList.add('presence-updated');
  return card;
 }

 function renderPresence(offline=window.SintetNetwork.offline||!!state?.offline){
  if(presence.inert)return;
  const enabled=!!state?.enabled&&!offline;
  const members=new Set((state?.members||[]).map(member=>member.id));
  const friends=enabled?(state.friends||[]).filter(friend=>friend.account&&friend.status==='accepted').map(friend=>{
   const activity=friend.presence?.activity;
   const loader=activity?.loader&&loaderNames[activity.loader];
   const version=activity?.version?[activity.version,loader].filter(Boolean).join(' · '):displayGameVersion(friend.presence?.version);
   return {account:friend.account,mode:friendMode(friend,!!friend.presence&&members.has(friend.account.id)),activity,version,totals:friend.totals};
  }):[];
  const invites=enabled?(state.invites||[]).filter(invite=>invite.account):[];
  const snapshot=JSON.stringify([offline,!!state,enabled,!!state?.error,!!state?.reconnecting,busy,!!state?.busy,friends,invites]);
  if(snapshot===lastPresenceView){updatePresenceTimers();loadMusicCovers();return;}
  lastPresenceView=snapshot;

  friends.sort((a,b)=>(presenceOrder[a.mode]??4)-(presenceOrder[b.mode]??4)||a.account.username.localeCompare(b.account.username));
  const online=friends.filter(friend=>friend.mode!=='offline').length;
  presenceOnline.hidden=!enabled;
  presenceOnline.textContent=state?.reconnecting?'Переподключение…':online+' в сети';
  presenceOnline.classList.toggle('has-online',online>0&&!state?.reconnecting);
  presenceActions.hidden=!enabled;
  privacyButton.hidden=!enabled;
  presenceLogin.hidden=offline||!state||enabled;
  presenceEmpty.hidden=invites.length+friends.length>0;
  presenceEmpty.querySelector('strong').textContent=offline?window.SintetNetwork.message:
   !state?'Загружаем друзей…':!enabled?'Друзья из Sintet ID':state.reconnecting?'Восстанавливаем связь с Sintet ID…':
   state.error?'Не удалось загрузить друзей':'Пока нет друзей';

  const focused=document.activeElement?.dataset.presenceAction;
  const rows=[];
  for(const invite of invites){
   const node=row(invite.account,'Приглашает в игру',[
    presenceButton('Принять','invite-accept',invite.id),presenceButton('Отклонить','invite-decline',invite.id),
   ]);
   node.classList.add('presence-friend','friend-online');
   rows.push(node);
  }
  rows.push(...friends.map(presenceCard));
  if(enabled){
   previousModes=new Map(friends.map(friend=>[friend.account.id,friend.mode]));
   for(const id of expandedPresence)if(!previousModes.has(id))expandedPresence.delete(id);
  }
  presenceList.replaceChildren(...rows);
  // Polling can replace a card mid-rotation. Every record shares the same timeline.
  for(const vinyl of presenceList.querySelectorAll('.presence-vinyl')){
   const spin=vinyl.getAnimations().find(animation=>animation.animationName==='presence-record-spin');
   if(spin)spin.startTime=0;
  }
  updatePresenceTimers();
  loadMusicCovers();
  if(focused){
   const next=Array.from(presenceList.querySelectorAll('button')).find(control=>control.dataset.presenceAction===focused);
   next?.focus({preventScroll:true});
  }
 }
 async function search(){
  if(window.SintetNetwork.offline)return;
  const query=document.querySelector('#friend-search').value.trim(),generation=++searchGeneration,result=document.querySelector('#friend-search-results');
  result.replaceChildren();if(query.length<2)return;
  try{const accounts=await api().FriendsSearch(query);if(generation!==searchGeneration)return;
   const existing=new Set((state?.friends||[]).map(friend=>friend.account?.id));
   const candidates=(accounts||[]).filter(account=>!existing.has(account.id));
   for(const account of candidates)result.append(row(account,'Sintet ID',[button('Добавить','request',account.id)]));
   if(!candidates.length)result.append(element('p','friends-hint','Никого не найдено'));
  }catch(error){if(generation===searchGeneration)result.append(element('p','friends-hint',String(error?.message||error)))}
 }
 document.querySelector('#friend-search').oninput=()=>{
  searchGeneration++;
  document.querySelector('#friend-search-results').replaceChildren();
  render();
 };
 document.querySelector('#friend-search-submit').onclick=search;
 document.querySelector('#friend-search').onkeydown=event=>{if(event.key==='Enter'){event.preventDefault();search()}};
 document.querySelector('#friends-join').onsubmit=event=>{event.preventDefault();act('join',document.querySelector('#friends-code').value.trim())};
 const lobbySetup=element('label','lobby-setup');lobbySetup.append(element('span','','Сборка лобби'));
 const lobbyProfile=element('select','');lobbyProfile.setAttribute('aria-label','Сборка лобби');lobbySetup.append(lobbyProfile);
 const changePack=element('button','pack-cancel','Сменить сборку');changePack.type='button';changePack.hidden=true;
 lobbySetup.append(changePack);document.querySelector('#friends-host').before(lobbySetup);
 let currentLobbyProfile='';
 lobbyProfile.addEventListener('change',()=>{changePack.disabled=busy||state?.busy||!lobbyProfile.value||lobbyProfile.value===state?.hostProfile;});
 changePack.onclick=()=>act('change-lobby-pack',lobbyProfile.value);
 let lobbyProfilesAt=0,lobbyProfilesKey='';
 async function refreshLobbyProfiles(){
  if(state?.room&&!state.leader||Date.now()-lobbyProfilesAt<15000)return;
  lobbyProfilesAt=Date.now();
  try{const profiles=await api().Installations(),key=JSON.stringify(profiles);if(key===lobbyProfilesKey)return;lobbyProfilesKey=key;
   let preferred=lobbyProfile.value||state?.hostProfile;try{const saved=JSON.parse(localStorage.getItem('sintet.selectedProfile'));if(!preferred&&saved)preferred=saved.id||(saved.loader==='vanilla'?saved.base:`${saved.loader}-${saved.loaderVersion}-${saved.base}`)}catch{}
   lobbyProfile.replaceChildren();for(const profile of profiles||[]){const option=element('option','',profile.name?profile.name+' · '+profile.base:profile.base+' · '+(loaderNames[profile.loader]||profile.loader));option.value=profile.id;lobbyProfile.append(option)}
   if([...lobbyProfile.options].some(option=>option.value===preferred))lobbyProfile.value=preferred;
   if(!lobbyProfile.options.length){const option=element('option','','Сначала установи Minecraft');option.value='';lobbyProfile.append(option)}window.SintetUI?.select.refresh(lobbyProfile);
  }catch{}
 }
 document.querySelector('#friends-host').onclick=()=>act(state?.room?'close':'host',state?.gameRunning?'':lobbyProfile.value);
 document.querySelector('#friends-copy-room').onclick=()=>{if(state?.room?.code)navigator.clipboard.writeText(state.room.code).then(()=>toast('Код скопирован'))};
 document.querySelector('#friends-auto-join').onclick=()=>act('connect-game');
 document.querySelector('#friends-endpoint').onclick=()=>{
  if(state?.localPort)navigator.clipboard.writeText('127.0.0.1:'+state.localPort).then(()=>toast('Адрес скопирован'));
 };
 const packDialog=document.createElement('dialog');packDialog.className='pack-sync-confirm';
 packDialog.innerHTML='<h2>Сборка друга</h2><p class="pack-confirm-details"></p><p class="pack-confirm-note">Моды запускают код на компьютере. Продолжай, если доверяешь хосту. Файлы сохранятся отдельной сборкой, твои моды и конфиги останутся на месте.</p><p class="pack-confirm-restart"></p><footer><button type="button" data-pack-back>Отмена</button><button type="button" data-pack-accept>Продолжить</button></footer>';
 document.body.append(packDialog);let consentDigest='';
 packDialog.querySelector('[data-pack-back]').onclick=()=>packDialog.close();
 packDialog.querySelector('[data-pack-accept]').onclick=()=>{const digest=consentDigest;packDialog.close();act('sync-pack',digest)};
 const packSize=n=>window.SintetLanguage.formatBytes(n);
 function confirmPack(pack){consentDigest=pack.digest;packDialog.querySelector('.pack-confirm-details').textContent=pack.name+' · Minecraft '+pack.game+' · '+pack.loader+(pack.missing?' · '+window.SintetText.format('%d файлов, %s',pack.missing,packSize(pack.bytes)):'');packDialog.querySelector('.pack-confirm-restart').textContent=state.preGameLobby?'Подготовим сборку отдельно. Игру можно будет запустить, когда лидер откроет мир для сети. Текущая игра продолжит работать.':pack.restart?'После загрузки Minecraft корректно закроется и запустится со сборкой друга. Сохрани текущий мир.':'После подготовки Minecraft запустится и подключится к миру друга.';packDialog.querySelector('[data-pack-accept]').textContent=state.preGameLobby?'Подготовить сборку':pack.restart?'Скачать и перезапустить':'Скачать и войти';packDialog.showModal()}
 function renderPack(){
  let card=document.querySelector('#lobby-pack');const pack=state?.pack;
  if(state?.hostNeedsPack){
   if(!card){card=element('section','lobby-pack');card.id='lobby-pack'}
   footer.prepend(card);card.className='lobby-pack requires-sync pack-error';
   const restore=button('Восстановить сборку','host',state.gameRunning?'':lobbyProfile.value);
   restore.classList.add('pack-sync-button');
   restore.onclick=()=>act('host',state.gameRunning?'':lobbyProfile.value);
   card.replaceChildren(element('strong','','Сборка лобби не загружена'),restore,element('p','','Выбери исходную сборку, чтобы гости снова могли её получить.'));
   return;
  }
  if(!pack){card?.remove();return}
  if(!card){card=element('section','lobby-pack');card.id='lobby-pack'}footer.prepend(card);const endpoint=document.querySelector('#lobby-address'),actions=footer.querySelector('.lobby-address-actions');if(endpoint&&actions)card.after(endpoint,actions);card.replaceChildren();
  const working=['checking','downloading','stopping','installing','launching'].includes(pack.phase),required=['required','error'].includes(pack.phase);
  card.classList.toggle('requires-sync',required);card.classList.toggle('pack-working',working);
  card.classList.toggle('pack-error',pack.phase==='error');
  const heading=({hosting:'Сборка доступна',checking:'Сверяем сборку',required:'Сборка отличается',downloading:'Скачиваем сборку',stopping:'Сохраняем и закрываем Minecraft',installing:'Готовим Minecraft',launching:'Запускаем Minecraft',ready:'Сборка готова',error:'Сборка не подготовлена'}[pack.phase]||'Сборка друга');
  card.append(element('strong','',heading));
  if(pack.game)card.append(element('span','pack-version',[pack.name,pack.game,loaderNames[pack.loader]||pack.loader].filter(Boolean).join(' · ')));
  if(required){
   if(pack.phase==='required')card.querySelector('strong').textContent=pack.missing?window.SintetText.format('Нужно %d файлов · %s',pack.missing,packSize(pack.bytes)):state.preGameLobby?'Подготовить сборку':'Нужен перезапуск';
   card.append(element('p','',pack.error||'Сборка сохраняется отдельно'));
   if(pack.digest){
    const action=element('button','pack-sync-button',state.preGameLobby?'Подготовить':pack.restart?'Скачать и перезапустить':'Скачать и войти');
    action.disabled=busy||state.busy;action.onclick=()=>confirmPack(pack);card.append(action);
   }else if(pack.phase==='error'&&state.lobbyConnected){
    const retry=button('Повторить проверку','retry-pack');
    retry.classList.add('pack-sync-button');card.append(retry);
   }
  }
  if(working){const progress=element('progress','');progress.max=pack.total||100;if(pack.total)progress.value=pack.done;card.append(progress);if(pack.current)card.append(element('span','pack-current',pack.current));if(pack.total)card.append(element('small','',pack.phase==='installing'?Math.round(pack.done)+'%':packSize(pack.done)+' / '+packSize(pack.total)));if(pack.phase!=='checking'&&pack.phase!=='launching'){const cancel=element('button','pack-cancel','Отменить');cancel.onclick=()=>act('cancel-pack');card.append(cancel)}}
  if(pack.phase==='hosting')card.append(element('small','','Передаются моды и обычные конфиги. Личные данные и скрипты не передаются.'));
  if(pack.phase==='hosting'&&state.preGameLobby){const launch=button(state.gameRunning?'Игра запущена':'Запустить Minecraft','launch-host');launch.disabled=busy||state.busy||state.gameRunning;launch.classList.add('pack-sync-button');card.append(launch);const renew=button('Обновить файлы сборки','refresh-pack');renew.classList.add('pack-cancel');card.append(renew)}
 }
 function render(){
  const offline=window.SintetNetwork.offline||!!state?.offline;
  renderInvitation(offline);
  renderPresence(offline);
  offlineNotice.hidden=!offline;
  tabs.hidden=offline;
  requestsButton.hidden=offline;
  if(offline){friendPage.hidden=true;lobbyPage.hidden=true;requestsPanel.hidden=true;return}
  selectTab(activeTab);
  if(!state)return;list.replaceChildren();
  const pending=(state.friends||[]).filter(friend=>friend.status==='pending'&&friend.account);
  document.querySelector('#friends-count').textContent=(state.friends||[]).filter(f=>f.status==='accepted').length;
  const incoming=pending.filter(friend=>friend.incoming).length;
  const requestCount=requestsButton.querySelector('.friends-requests-count');
  requestCount.textContent=incoming;
  requestCount.hidden=incoming===0;
  requestsPanel.hidden=!showRequests;
  requestsButton.classList.toggle('active',showRequests);
  tools.hidden=!state.enabled||showRequests;
  list.hidden=showRequests;
  requestsPanel.replaceChildren();
  if(showRequests){
   if(!pending.length)requestsPanel.append(element('div','friends-requests-empty','Новых заявок нет'));
   for(const friend of pending){
    const incoming=friend.incoming;
    requestsPanel.append(row(friend.account,incoming?'Хочет добавить тебя в друзья':'Заявка отправлена',incoming?[button('Принять','friend-accept',friend.account.id),button('Отклонить','friend-remove',friend.account.id)]:[button('Отменить','friend-remove',friend.account.id)]));
   }
  }
  const filter=document.querySelector('#friend-search').value.trim().replace(/^@/,'').toLowerCase();
  for(const invite of state.invites||[]){if(invite.account)list.append(row(invite.account,'Приглашает в игру',[button('Принять','invite-accept',invite.id),button('×','invite-decline',invite.id)]))}
  for(const friend of state.friends||[]){if(!friend.account)continue;
   if(friend.status==='pending')continue;
   if(filter&&![friend.account.username,friend.account.handle,friend.account.id].some(value=>value?.toLowerCase().includes(filter)))continue;
   let status=presenceLabels[friendMode(friend,false)];
   let buttons=[...(friend.presence?[button('Пригласить','invite',friend.account.id)]:[]),button('×','friend-remove',friend.account.id)];
   const inLobby=state.members?.some(member=>member.id===friend.account.id);
   if(inLobby){
    status='В лобби';
    const invite=button('Пригласить','invite',friend.account.id);
    invite.disabled=true;invite.title='Друг уже в лобби';
    buttons=[invite,button('×','friend-remove',friend.account.id)];
   }
   const node=row(friend.account,status,buttons);node.classList.toggle('friend-online',!!friend.presence);list.append(node);
  }
  if(!list.children.length&&!showRequests){const empty=element('div','friends-empty');empty.append(element('strong','',state.enabled?(filter?'Друзья не найдены':'Пока нет друзей'):'Войди через Sintet ID'),element('span','',state.enabled?(filter?'Можно поискать аккаунт по нику или ID':'Найди друга по нику или ID'):'Друзья и приглашения привязаны к аккаунту'));list.append(empty)}
  document.querySelector('#friends-join').hidden=!!state.room||!!state.localPort;
  const chooseLobbyProfile=state.hostNeedsPack||state.preGameLobby&&(!state.room||state.leader);
  lobbySetup.hidden=!chooseLobbyProfile||state.gameRunning&&!state.leader;
  if(state.hostProfile&&state.hostProfile!==currentLobbyProfile&&[...lobbyProfile.options].some(option=>option.value===state.hostProfile)){
   currentLobbyProfile=state.hostProfile;lobbyProfile.value=state.hostProfile;window.SintetUI?.select.refresh(lobbyProfile);
  }
  changePack.hidden=!state.leader;
  const host=document.querySelector('#friends-host');host.textContent=state.leader?'Закрыть лобби':state.room||state.localPort?'Выйти из лобби':'Создать лобби';host.classList.toggle('lobby-close',!!state.leader);host.disabled=busy||state.busy||!state.enabled;
  document.querySelector('#friends-room').hidden=!state.room;
  lobbyPage.classList.toggle('has-room',!!state.room||!!state.localPort);
  let members=document.querySelector('#lobby-members');if(!members){members=element('div','lobby-members');members.id='lobby-members';footer.prepend(members)}members.replaceChildren();for(const person of state.members||[]){
   const kick=state.leader&&!person.leader?button('×','kick',person.id):null;
   if(kick){kick.title='Исключить из лобби';kick.setAttribute('aria-label','Исключить '+person.username+' из лобби')}
   const status=person.leader?(state.worldReady?'Мир открыт':'Лидер лобби'):(preparationLabels[person.preparation]||'В лобби')+(['downloading','installing'].includes(person.preparation)?' · '+(person.percent||0)+'%':'');
   members.append(row(person,status,kick?[kick]:[]));
  }
  const title=document.querySelector('#friends-room>span');title.className='lobby-count';if(title)title.textContent=(state.members?.length||state.connected+1)+' / 4';document.querySelector('#friends-copy-room').textContent=state.room?.code||'';
  const connection=state.pack?.phase==='checking'?'Сверяем сборку':state.connection;
  document.querySelector('#friends-connection').textContent=state.joinAttempt?(state.joinAttempt>1?'Повторная попытка · ':'')+(connection||'Входим в лобби…'):connection||(busy||state.busy?'Подождите…':'');
  const endpoint=document.querySelector('#friends-endpoint');
  endpoint.hidden=!state.worldReady||!state.localPort;
  const overlayHidden=window.SintetSettings?.config?.settings?.overlayEnabled===false;
  endpoint.textContent=endpoint.hidden?'':overlayHidden?'127.0.0.1:'+state.localPort:'Скопировать адрес';
  endpoint.title='Скопировать адрес';
  let address=document.querySelector('#lobby-address');if(!address){address=element('p','');address.id='lobby-address';endpoint.parentElement.before(address)}address.hidden=endpoint.hidden||overlayHidden;address.textContent=address.hidden?'':'127.0.0.1:'+state.localPort;
  const autoJoin=document.querySelector('#friends-auto-join');
  autoJoin.hidden=!state.worldReady||!state.localPort||state.inWorld||state.gameJoining||!!state.pack&&state.pack.phase!=='ready';
  autoJoin.textContent=state.gameRunning?(state.pack?.restart?'Перезапустить и войти':'Присоединиться'):'Запустить и присоединиться';
  autoJoin.title=state.gameReady?'Автоматически войти в мир':'Запустить Minecraft и войти в мир';
  autoJoin.setAttribute('aria-label','Автоматически войти в мир');
  const syncMessage=document.querySelector('#friends-error');syncMessage.textContent=state.error||(state.reconnecting?'Восстанавливаем связь с Sintet ID…':'');syncMessage.classList.toggle('is-reconnecting',!state.error&&!!state.reconnecting);
  for(const field of footer.querySelectorAll('input,button'))field.disabled=busy||state.busy||!state.enabled||(field===autoJoin&&!state.gameReady&&state.pack?.phase!=='ready');
  changePack.disabled=busy||state.busy||!state.enabled||!lobbyProfile.value||lobbyProfile.value===state.hostProfile;
  let autoOption=document.querySelector('#lobby-auto-connect');if(!autoOption){autoOption=element('label','lobby-auto-option');autoOption.id='lobby-auto-connect';const check=element('input','');check.type='checkbox';check.onchange=()=>act('auto-connect',String(check.checked));autoOption.append(check,element('span','','Подключиться, когда мир будет готов'));document.querySelector('#friends-connection').after(autoOption)}autoOption.hidden=!state.preGameLobby||!state.lobbyConnected||state.leader||state.inWorld||state.pack?.phase==='error';autoOption.querySelector('input').checked=!!state.autoConnect;
  let reconnect=document.querySelector('#lobby-reconnect');if(!reconnect){reconnect=button('Восстановить соединение','reconnect-lobby');reconnect.id='lobby-reconnect';document.querySelector('#friends-error').after(reconnect)}reconnect.hidden=!state.room||state.leader||state.lobbyConnected||busy||state.busy;
  renderPack();
 }
 async function refresh(){if(!api())return;try{state=await api().FriendsState();const phase=state.pack?.phase||'';if(!acceptedInvite&&['downloading','stopping','installing','launching'].includes(phase)&&phase!==lastPackPhase){selectTab('lobby');if(!panel.matches(':popover-open')&&!document.querySelector('dialog[open]')){position();panel.showPopover()}}lastPackPhase=phase;if(state.pack?.profile&&state.pack.profile!==lastPackProfile){lastPackProfile=state.pack.profile;window.dispatchEvent(new CustomEvent('sintet:p2p-profile',{detail:{id:lastPackProfile}}))}render();await refreshLobbyProfiles()}catch{}}
 async function openCommunity(service){try{if(!api())throw Error('Запусти Sintet через Start.bat');await api().OpenCommunity(service)}catch(error){toast(String(error?.message||error))}}
 document.querySelector('#community-discord').onclick=()=>openCommunity('discord');
 document.querySelector('#community-telegram').onclick=()=>openCommunity('telegram');
 footer.append(guideLink);
 window.addEventListener('sintet:network',render);
 window.addEventListener('sintet:presence-layout',()=>renderPresence());
 setInterval(refresh,2000);render();refresh();
})();
