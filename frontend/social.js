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
 const lobbySettingsButton=element('button','friends-requests-toggle lobby-settings-toggle');
 lobbySettingsButton.type='button';
 lobbySettingsButton.setAttribute('aria-label','Настройки лобби');
 lobbySettingsButton.setAttribute('aria-controls','social-lobby-settings');
 lobbySettingsButton.setAttribute('aria-expanded','false');
 lobbySettingsButton.innerHTML='<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h5m6 0h5M4 17h9m6 0h1"/><circle cx="12" cy="7" r="3"/><circle cx="16" cy="17" r="3"/></svg>';
 titleActions.append(requestsButton,lobbySettingsButton);
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
    <p>Участники прямого соединения могут узнать сетевые IP-адреса друг друга. Приглашай тех, кому доверяешь.</p>
    <p>Если прямое соединение не удалось, Sintet попробует резервный канал TURN. На аккаунт доступно 5 ГБ каждые два месяца, включая игру и передачу сборок. Остаток можно посмотреть в настройках.</p>
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
  guide.addEventListener('close',()=>{
   panel.showPopover();
   guideLink.focus({preventScroll:true});
  },{once:true});
  showGuide();
 };

 let busy=false,state=null,searchGeneration=0,activeTab='friends',showRequests=false,lastPackPhase='',lastPackProfile='';
 let lobbySettingsOpen=false;
 let acceptedInvite=null,inviteError='',inviteView='',autoConnectPending=null,stateRead=0,stateApplied=0;
 const invitationBanner=document.createElement('aside');
 invitationBanner.className='launcher-invitation';
 invitationBanner.setAttribute('aria-label','Приглашение в лобби');
 invitationBanner.hidden=true;
 document.querySelector('.launcher-invitations').append(invitationBanner);
 const friendRequestBanner=element('aside','launcher-invitation');
 friendRequestBanner.setAttribute('aria-label','Заявка в друзья');
 friendRequestBanner.hidden=true;
 invitationBanner.after(friendRequestBanner);
 const dismissedFriendRequests=new Set();
 let friendRequestView='';
 const turnNotice=element('aside','launcher-invitation turn-notice');
 turnNotice.setAttribute('role','status');
 turnNotice.hidden=true;
 document.querySelector('.launcher-invitations').append(turnNotice);
 const turnLobby=turnMeter(false),turnSettings=turnMeter(true);
 let shownTURNNotice=0,turnNoticeTimer,settingsUsageLoading=false,settingsUsageGeneration=0;
 document.querySelector('#launcher-settings-social').append(turnSettings);
 function turnMeter(settings){
  const root=element(settings?'article':'section',settings?'preference-card wide turn-meter':'turn-meter turn-meter-lobby');
  root.hidden=!settings;
  const header=element('div','turn-meter-head');
  header.append(element('strong','','Лимит TURN'),element('span','turn-meter-value'));
  const progress=element('progress','turn-meter-progress');
  progress.max=1;progress.value=0;progress.setAttribute('aria-label','Лимит TURN');progress.hidden=true;
  const hint=element('p','turn-meter-hint','Расход резервного подключения. Прямой P2P трафик в лимит не входит.');
  root.append(header,progress,hint,element('p','turn-meter-reset'));
  if(settings){
   const subscription=element('div','turn-subscription');
   const desc=element('div','');
   desc.append(element('strong','','Подписка TURN'),element('p','','Больше трафика для игры и передачи сборок.'));
   subscription.append(desc,element('span','turn-subscription-badge','Скоро'));
   root.append(subscription);
  }
  return root;
 }
 function updateTurnMeter(root,usage,fallback=''){
  const progress=root.querySelector('progress'),value=root.querySelector('.turn-meter-value');
  const reset=root.querySelector('.turn-meter-reset');
  const valid=usage?.limit>0;
  progress.hidden=!valid;
  if(valid){
   const used=Math.min(usage.used,usage.limit);
   const formatUsed=used>=1e9?window.SintetLanguage.formatSize(used/1e9,'gigabyte',2):
    used>=1e6?window.SintetLanguage.formatSize(used/1e6,'megabyte',2):
    used>=1e3?window.SintetLanguage.formatSize(used/1e3,'kilobyte',1):window.SintetLanguage.formatSize(used,'byte');
   value.textContent=formatUsed+' / '+window.SintetLanguage.formatSize(usage.limit/1e9,'gigabyte',usage.limit%1e9?2:0);
   progress.value=Math.min(1,usage.used/usage.limit);
   const greenToAmber=Math.min(1,progress.value*2),amberToRed=Math.max(0,progress.value*2-1);
   const rgb=[.45+.48*greenToAmber,.78+.02*greenToAmber-.42*amberToRed,.54-.25*greenToAmber-.06*amberToRed];
   progress.style.setProperty('--turn-progress-color','rgb('+rgb.map(channel=>Math.round(channel*255)).join(' ')+')');
   if(usage.periodEnd){
    const date=new Date(usage.periodEnd+'T00:00:00Z');
    const formatted=new Intl.DateTimeFormat(document.documentElement.lang||undefined,{day:'numeric',month:'long',timeZone:'UTC'}).format(date);
    reset.textContent=window.SintetText.format('Лимит обновится %s',formatted);
   }
  }else{
   value.textContent=window.SintetText.translate(fallback);
   reset.textContent='';
   progress.style.removeProperty('--turn-progress-color');
  }
  reset.hidden=!reset.textContent;
 }
 function renderTurn(state){
  turnLobby.hidden=!state?.turnActive;
  if(state?.turnActive)updateTurnMeter(turnLobby,state.lobbyTurnUsage||state.turnUsage,'Считаем расход…');
  if(state?.turnNotice&&shownTURNNotice!==state.turnNotice){
   shownTURNNotice=state.turnNotice;
   if(Date.now()-shownTURNNotice<12000){
    turnNotice.textContent='Лимит TURN подключения закончился';
    turnNotice.hidden=false;
    clearTimeout(turnNoticeTimer);
    turnNoticeTimer=setTimeout(()=>{turnNotice.hidden=true},8500);
   }
  }
 }
 async function refreshSettingsTurn(){
  if(!api()?.TurnUsage||settingsUsageLoading||turnSettings.getClientRects().length===0)return;
  if(!state?.enabled){
   updateTurnMeter(turnSettings,null,'Выбери профиль для лобби');
   return;
  }
  settingsUsageLoading=true;
  const generation=settingsUsageGeneration;
  try{
   const usage=await api().TurnUsage();
   if(generation===settingsUsageGeneration)updateTurnMeter(turnSettings,usage);
  }catch{
   if(generation===settingsUsageGeneration)updateTurnMeter(turnSettings,null,'Расход временно недоступен');
  }finally{settingsUsageLoading=false}
 }
 document.querySelector('#settings-nav').addEventListener('click',()=>requestAnimationFrame(refreshSettingsTurn));
 document.querySelector('#settings-tab-launcher').addEventListener('click',()=>requestAnimationFrame(refreshSettingsTurn));
 window.addEventListener('sintet:identity',()=>{
  settingsUsageGeneration++;
  updateTurnMeter(turnSettings,null,'Считаем расход…');
  refresh();
 });
 setInterval(refreshSettingsTurn,15000);
 updateTurnMeter(turnSettings,null,'Считаем расход…');
 function position(){const anchor=toggle.getBoundingClientRect(),top=anchor.bottom+10;panel.style.left=Math.max(12,Math.min(anchor.left,innerWidth-410))+'px';panel.style.top=top+'px';panel.style.maxHeight=Math.max(160,innerHeight-top-12)+'px'}
 panel.addEventListener('beforetoggle',event=>{if(event.newState==='open'){position();refresh()}});
 panel.addEventListener('toggle',event=>toggle.setAttribute('aria-expanded',String(event.newState==='open')));
 window.addEventListener('resize',()=>{if(panel.matches(':popover-open')||panel.hasAttribute('data-tour-preview'))position()});
 const tools=document.createElement('div');tools.className='friends-tools';
 tools.innerHTML='<div class="friends-search"><input id="friend-search" placeholder="Поиск по друзьям" autocomplete="off" maxlength="36"><button type="button" id="friend-search-submit">Найти аккаунт</button></div><div id="friend-search-results"></div>';
 list.before(tools);
 const requestsPanel=element('div','friends-requests-list');
 requestsPanel.hidden=true;
 tools.before(requestsPanel);
 requestsButton.onclick=()=>{
  setLobbySettings(false,false);
  showRequests=!showRequests;
  requestsButton.setAttribute('aria-expanded',String(showRequests));
  render();
 };
 const tabs=element('div','social-tabs');tabs.setAttribute('role','tablist');tabs.setAttribute('aria-label','Друзья и лобби');
 const friendTab=element('button','','Друзья'),lobbyTab=element('button','','Лобби');
 const friendPage=element('section','social-tab-page'),lobbyPage=element('section','social-tab-page');
 friendPage.id='social-friends-page';lobbyPage.id='social-lobby-page';friendPage.setAttribute('role','tabpanel');lobbyPage.setAttribute('role','tabpanel');
 const lobbySettings=element('section','social-tab-page lobby-settings-view');
 lobbySettings.id='social-lobby-settings';
 lobbySettings.hidden=true;
 lobbySettings.setAttribute('aria-labelledby','social-lobby-settings-title');
 const lobbySettingsHeading=element('div','lobby-settings-heading');
 const lobbySettingsBack=element('button','friends-requests-toggle');
 lobbySettingsBack.type='button';
 lobbySettingsBack.setAttribute('aria-label','Назад');
 lobbySettingsBack.innerHTML='<svg viewBox="0 0 24 24" aria-hidden="true"><path d="m14 6-6 6 6 6"/></svg>';
 const lobbySettingsTitle=element('h3','','Настройки лобби');
 lobbySettingsTitle.id='social-lobby-settings-title';
 lobbySettingsHeading.append(lobbySettingsBack,lobbySettingsTitle);
 lobbySettings.append(lobbySettingsHeading);
 const offlineNotice=element('p','social-offline',window.SintetNetwork.message);
 offlineNotice.setAttribute('role','status');
 offlineNotice.hidden=true;
 panel.append(offlineNotice,tabs,friendPage,lobbyPage,lobbySettings);friendPage.append(tools,list);lobbyPage.append(footer);
 function selectTab(name){activeTab=name;const friends=name==='friends';friendPage.hidden=lobbySettingsOpen||!friends;lobbyPage.hidden=lobbySettingsOpen||friends;friendTab.setAttribute('aria-selected',String(friends));lobbyTab.setAttribute('aria-selected',String(!friends));friendTab.tabIndex=friends?0:-1;lobbyTab.tabIndex=friends?-1:0;}
 function setLobbySettings(open,focus=true){
  if(lobbySettingsOpen===open)return;
  lobbySettingsOpen=open;
  if(open){
   showRequests=false;
   requestsButton.setAttribute('aria-expanded','false');
   panel.style.setProperty('--lobby-settings-height',Math.max(380,panel.offsetHeight)+'px');
  }
  panel.toggleAttribute('data-lobby-settings',open);
  lobbySettingsButton.setAttribute('aria-expanded',String(open));
  lobbySettingsButton.classList.toggle('active',open);
  if(focus){
   render();
   if(open)lobbySettings.scrollTop=0;
   (open?lobbySettingsBack:lobbySettingsButton).focus({preventScroll:true});
  }
 }
 lobbySettingsButton.onclick=()=>setLobbySettings(!lobbySettingsOpen);
 lobbySettingsBack.onclick=()=>setLobbySettings(false);
 panel.addEventListener('keydown',event=>{
  if(event.key!=='Escape'||!lobbySettingsOpen)return;
  event.preventDefault();
  event.stopPropagation();
  setLobbySettings(false);
 });
 for(const [button,name,page] of [[friendTab,'friends',friendPage],[lobbyTab,'lobby',lobbyPage]]){button.type='button';button.setAttribute('role','tab');button.id='social-tab-'+name;button.setAttribute('aria-controls',page.id);page.setAttribute('aria-labelledby',button.id);button.onclick=()=>selectTab(name);button.onkeydown=event=>{if(event.key==='ArrowLeft'||event.key==='ArrowRight'){event.preventDefault();selectTab(name==='friends'?'lobby':'friends');(name==='friends'?lobbyTab:friendTab).focus()}};tabs.append(button)}
 selectTab('friends');
 window.SintetLobbyTour={
  open(){
   showRequests=false;
   setLobbySettings(false,false);
   panel.dataset.tourPreview='';
   selectTab('lobby');
   position();
   render();
  },
  close(){
   delete panel.dataset.tourPreview;
   selectTab('friends');
   render();
  },
 };
 function openSocialTab(name){
  const open=()=>{
   showRequests=false;
   setLobbySettings(false,false);
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
 footer.prepend(turnLobby);
 const lobbyWarning=element('details','lobby-profile-warning');
 const lobbyWarningButton=element('summary','');
 lobbyWarningButton.innerHTML='<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3 2 20h20L12 3Z"/><path d="M12 9v4m0 3v.1"/></svg>';
 lobbyWarningButton.append(element('span','','Лобби без Sintet ID'));
 lobbyWarning.append(lobbyWarningButton,element('p','','Для игры с друзьями рекомендуем Sintet ID. Без него доступен 1 ГБ TURN на два месяца. Прямое P2P не тратит этот лимит.'));
 lobbyWarning.hidden=true;
 footer.prepend(lobbyWarning);
 function element(tag,cls,text){const node=document.createElement(tag);if(cls)node.className=cls;if(text)node.textContent=text;return node}
 function button(label,action,target){const node=element('button','friend-action',label);node.type='button';node.disabled=busy||state?.busy;node.onclick=()=>act(action,target);return node}
 async function act(action,target=''){
  if(busy||state?.busy||!api()||window.SintetNetwork.offline&&!action.startsWith('voice-'))return false;
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
   if(['host','join','join-friend','invite-accept'].includes(action))selectTab('lobby');
   return true;
  }catch(error){
   const message=String(error?.message||error);
   if(action==='invite-accept'&&acceptedInvite?.id===target)inviteError=message;
   else if(!action.startsWith('lobby-capacity'))toast(message);
   if(action.startsWith('lobby-capacity'))throw error;
   return false;
  }finally{busy=false;await refresh()}
 }
 async function setAutoConnect(checked){
  if(autoConnectPending!==null||!api()||window.SintetNetwork.offline)return;
  autoConnectPending=checked;
  render();
  try{await api().FriendsAction('auto-connect',String(checked))}
  catch(error){toast(String(error?.message||error))}
  finally{
   await refresh();
   autoConnectPending=null;
   render();
  }
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
  const content=element('div','friend-details');
  const name=element('strong','',account.username);
  window.SintetAccount.renderName(name,account.username,account);
  content.append(name);if(account.handle)content.append(element('small','friend-handle','@'+account.handle));content.append(element('span','',status));node.append(content);
  if(account.leader){
   const crown=element('span','lobby-crown');crown.title='Лидер лобби';
   crown.innerHTML='<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="m3 7 5 4 4-7 4 7 5-4-3 12H6Z" stroke-linejoin="round"/></svg>';
   name.append(crown);
  }
  const actions=element('div','friend-actions');actions.append(...buttons);node.append(actions);return node;
 }
 function profileMark(provider){
  const providers={microsoft:['assets/brand/microsoft.svg','Microsoft'],ely:['assets/brand/ely-icon-light.svg','Ely.by'],sintet:['assets/brand/sintet-id-icon-light.svg','Sintet ID']};
  const mark=providers[provider];
  if(!mark){const local=element('span','presence-provider-icon local-profile-mark','◉');local.title='Локальный профиль';local.setAttribute('aria-label',local.title);return local;}
  const node=element('img','presence-provider-icon');node.src=mark[0];node.alt=mark[1];node.title=mark[1];return node;
 }
 const preparationLabels={connecting:'Подключается',checking:'Сверяет сборку',required:'Нужно подготовить сборку',downloading:'Скачивает',installing:'Устанавливает Minecraft',stopping:'Перезапускает игру',launching:'Запускает игру',ready:'Готов',world:'В мире',error:'Нужна повторная попытка'};
 const presenceLabels={lobby:'В лобби',singleplayer:'Одиночный мир',multiplayer:'Мультиплеер',world:'В мире Minecraft',menu:'В главном меню',launcher:'В лаунчере',offline:'Не в сети'};
 const presenceOrder={lobby:0,multiplayer:1,singleplayer:1,world:1,menu:2,launcher:3};
 const loaderNames={fabric:'Fabric',forge:'Forge',neoforge:'NeoForge',quilt:'Quilt',vanilla:'Vanilla'};
 const manualLobbyProfile='sintet:manual';
 function invitationDetails(invite){
  if(invite.game===manualLobbyProfile)return 'Ничего не ставить';
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
 function renderFriendRequest(offline){
  const request=state?.enabled&&!offline&&(state.friends||[]).find(friend=>
   friend.status==='pending'&&friend.incoming&&friend.account&&!dismissedFriendRequests.has(friend.id));
  if(!request){
   friendRequestBanner.hidden=true;
   friendRequestView='';
   return;
  }
  const key=JSON.stringify([request.id,request.account,busy,!!state.busy]);
  if(friendRequestView===key)return;
  friendRequestView=key;
  const focused=friendRequestBanner.contains(document.activeElement)?document.activeElement.dataset.inviteAction:'';
  const copy=element('div','launcher-invitation-copy');
  copy.setAttribute('role','status');
  copy.append(element('strong','','Заявка в друзья'));
  copy.append(element('span','',window.SintetText.format('%s хочет добавить тебя в друзья',request.account.username)));
  const controls=element('div','launcher-invitation-actions');
  const decline=button('Отклонить','friend-remove',request.account.id);
  decline.dataset.inviteAction='decline';
  const accept=button('Принять','friend-accept',request.account.id);
  accept.dataset.inviteAction='accept';
  const dismiss=element('button','','×');
  dismiss.type='button';dismiss.dataset.inviteAction='close';
  dismiss.setAttribute('aria-label','Скрыть уведомление');
  dismiss.onclick=()=>{
   dismissedFriendRequests.add(request.id);
   renderFriendRequest(offline);
  };
  controls.append(decline,accept,dismiss);
  friendRequestBanner.replaceChildren(friendAvatar(request.account),copy,controls);
  friendRequestBanner.hidden=false;
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
  const playerName=element('strong','presence-player-name');
  window.SintetAccount.renderName(playerName,account.username,account);
  playerName.append(profileMark(account.provider||'sintet'));
  identity.append(playerName);
  const publicID=/^[0-9]{9,16}$/.test(account.publicID||'')?account.publicID:'';
  if(account.handle||publicID){
   const metadata=element('span','presence-profile-meta');
   if(account.handle)metadata.append(element('small','presence-handle','@'+account.handle));
   if(publicID)metadata.append(element('code','presence-public-id','ID · '+publicID));
   identity.append(metadata);
  }
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
  if(friend.joinableLobby&&!state.room)actions.append(presenceButton('Присоединиться','join-friend',account.id));
  if(online&&mode!=='lobby')actions.append(presenceButton('Пригласить','invite',account.id));
  if(mode==='lobby'){
   const lobby=element('button','friend-action','Лобби');lobby.type='button';
   lobby.onclick=()=>openSocialTab('lobby');actions.append(lobby);
  }
  const copy=element('button','friend-action','Скопировать ID');copy.type='button';
  copy.onclick=async()=>{try{await navigator.clipboard.writeText(publicID);toast('ID скопирован')}catch{toast('Не удалось скопировать ID')}};
  const remove=element('button','friend-action presence-remove','Удалить из друзей');remove.type='button';
  remove.disabled=busy||state?.busy;
  remove.onclick=()=>{
   friendToRemove=account;
   removeDialog.querySelector('p').textContent=account.username;
   removeDialog.showModal();
  };
  if(publicID)actions.append(copy);
  actions.append(remove);
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
  const enabled=!!state?.enabled&&(!state.lobbyProvider||state.lobbyProvider==='sintet'||!!state.lobbyProfile)&&!offline;
  const members=new Set((state?.members||[]).map(member=>member.id));
  const friends=enabled?(state.friends||[]).filter(friend=>friend.account&&friend.status==='accepted').map(friend=>{
   const activity=friend.presence?.activity;
   const loader=activity?.loader&&loaderNames[activity.loader];
   const version=activity?.version?[activity.version,loader].filter(Boolean).join(' · '):displayGameVersion(friend.presence?.version);
   return {joinableLobby:!!friend.joinableLobby&&!state.room,account:friend.account,mode:friendMode(friend,!!friend.presence&&members.has(friend.account.id)),activity,version,totals:friend.totals};
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
  presenceActions.hidden=offline||!state?.enabled;
  presence.querySelector('#presence-open-friends').hidden=!enabled;
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
 const lobbySetup=element('label','lobby-setup');lobbySetup.append(element('span','','Версия или сборка'));
 const lobbyRoom=document.querySelector('#friends-room');
 const lobbyCount=lobbyRoom.querySelector('span');
 const lobbyCountTools=element('div','lobby-count-tools');
 lobbyCount.before(lobbyCountTools);
 const capacityToggle=element('button','lobby-capacity-toggle','+');
 capacityToggle.type='button';capacityToggle.title='Изменить размер лобби';
 capacityToggle.setAttribute('aria-label','Изменить размер лобби');
 lobbyCountTools.append(lobbyCount,capacityToggle);
 lobbyCount.className='lobby-count';

 const capacityDialog=element('dialog','ws-dialog ws-build-dialog ws-build-manage ws-utility-dialog lobby-capacity-dialog');
 capacityDialog.setAttribute('aria-labelledby','lobby-capacity-title');
 capacityDialog.innerHTML=`<header class="ws-build-header">
    <div><h1 id="lobby-capacity-title">Размер лобби</h1><span>До 150 участников вместе с лидером</span></div>
    <button type="button" class="account-close" aria-label="Закрыть">×</button>
   </header>
   <div class="ws-utility-body">
    <div class="lobby-capacity-warning"><strong>Больше 4 участников – выше нагрузка</strong>
     <p>Мир работает на компьютере лидера. При большом числе игроков возможны сильные лаги и обрывы. Плавная игра зависит от компьютера и интернета лидера.</p>
    </div>
    <label class="lobby-capacity-field">Максимум участников<input type="number" min="4" max="150" step="1" required></label>
    <p class="lobby-capacity-warning lobby-capacity-overflow" role="status" hidden></p>
    <p class="lobby-capacity-note">С 9 участниками лимит TURN для этого лобби – 1 ГБ на аккаунт за текущий двухмесячный период. Прямое P2P продолжит работать.</p>
    <p class="lobby-capacity-note lobby-capacity-error" role="alert" hidden></p>
   </div>
   <footer class="ws-build-footer"><button type="button" data-capacity-cancel>Отмена</button><button type="button" class="primary" data-capacity-save>Применить</button></footer>`;
 document.body.append(capacityDialog);
 const capacityInput=capacityDialog.querySelector('input'),capacitySave=capacityDialog.querySelector('[data-capacity-save]');
 const capacityOverflow=capacityDialog.querySelector('.lobby-capacity-overflow');
 let reopenLobby=false, resizingLobby=false;
 function renderCapacityWarning(){
  if(!capacityDialog.open||resizingLobby)return;
  const occupied=state.members?.length||1;
  const excess=capacityInput.validity.valid?Math.max(0,occupied-capacityInput.valueAsNumber):0;
  capacityOverflow.hidden=!excess;
  if(excess)capacityOverflow.textContent=window.SintetText.format('Будут отключены участники, которые вошли последними. Количество – %s.',String(excess));
  capacitySave.textContent=excess?'Отключить лишних и применить':'Применить';
  capacitySave.classList.toggle('danger',!!excess);
  capacitySave.classList.toggle('primary',!excess);
  return excess;
 }
 capacityToggle.onclick=()=>{
  reopenLobby=panel.matches(':popover-open');
  capacityInput.value=state.room.capacity||4;
  capacityDialog.querySelector('.lobby-capacity-error').hidden=true;
  capacityDialog.showModal();
  renderCapacityWarning();
  capacityInput.focus();
 };
 for(const control of capacityDialog.querySelectorAll('.account-close,[data-capacity-cancel]')){
  control.onclick=()=>{if(!resizingLobby)capacityDialog.close()};
 }
 capacityDialog.addEventListener('cancel',event=>{if(resizingLobby)event.preventDefault()});
 capacityDialog.addEventListener('close',()=>{
  if(reopenLobby&&state?.room&&!panel.matches(':popover-open')){selectTab('lobby');position();panel.showPopover()}
  if(panel.matches(':popover-open'))capacityToggle.focus();
  reopenLobby=false;
 });
 capacitySave.onclick=async()=>{
  const capacity=capacityInput.valueAsNumber;
  const error=capacityDialog.querySelector('.lobby-capacity-error');
  if(!capacityInput.checkValidity()){
   error.textContent='Укажи число от 4 до 150.';
   error.hidden=false;
   return;
  }
  const disconnect=renderCapacityWarning()>0;
  error.hidden=true;
  resizingLobby=true;
  for(const control of capacityDialog.querySelectorAll('input,button'))control.disabled=true;
  try{
   if(await act(disconnect?'lobby-capacity-confirm':'lobby-capacity',String(capacity)))capacityDialog.close();
  }catch(reason){
   error.textContent=String(reason?.message||reason);
   error.hidden=false;
  }finally{
   resizingLobby=false;
   for(const control of capacityDialog.querySelectorAll('input,button'))control.disabled=false;
   renderCapacityWarning();
  }
 };
 capacityInput.oninput=renderCapacityWarning;
 capacityInput.onkeydown=event=>{if(event.key==='Enter'){event.preventDefault();if(renderCapacityWarning())capacitySave.focus();else capacitySave.click()}};

 const memberPane=element('section','lobby-member-pane');
 const members=element('div','lobby-members');members.id='lobby-members';
 const memberSearch=element('div','lobby-member-search'),searchButton=element('button','lobby-search-toggle');
 searchButton.type='button';
 searchButton.setAttribute('aria-label','Поиск участников');
 searchButton.setAttribute('aria-expanded','false');
 searchButton.innerHTML='<svg viewBox="0 0 20 20" aria-hidden="true"><circle cx="8.5" cy="8.5" r="4.5"/><path d="m12 12 4 4"/></svg>';
 const searchInput=element('input','');searchInput.type='search';searchInput.placeholder='Ник или @юзернейм';
 searchInput.setAttribute('aria-label','Поиск участников');
 const searchSlot=element('div','friends-search lobby-search-slot');searchSlot.append(searchInput);
 memberSearch.append(searchButton,searchSlot);
 const showMore=element('button','lobby-show-more','Показать ещё');showMore.type='button';
 const noMembers=element('p','lobby-member-empty','Участники не найдены');
 memberPane.append(memberSearch,members,noMembers,showMore);footer.prepend(memberPane);
 let lobbyKey='',memberLimit=12,membersKey='';
 function setLobbySearch(open){
  memberSearch.classList.toggle('is-open',open);
  searchButton.setAttribute('aria-expanded',String(open));
  searchInput.inert=!open;
  if(open)searchInput.focus();
  else searchInput.value='';
 }
 setLobbySearch(false);
 searchButton.onclick=()=>{setLobbySearch(searchInput.inert);memberLimit=12;renderLobbyMembers()};
 searchInput.oninput=()=>{memberLimit=12;renderLobbyMembers()};
 searchInput.onkeydown=event=>{if(event.key==='Escape'){event.preventDefault();event.stopPropagation();searchButton.click();searchButton.focus()}};
 showMore.onclick=()=>{memberLimit+=12;renderLobbyMembers()};
 function renderLobbyMembers(){
  const code=state.room?.code||'';
  if(code!==lobbyKey){lobbyKey=code;memberLimit=12;membersKey='';setLobbySearch(false)}
  const people=state.members||[];
  lobbyCount.textContent=(people.length||state.connected+1)+' / '+(state.room?.capacity||4);
  memberPane.hidden=!code||!people.length;
  memberSearch.hidden=people.length<=4;
  if(memberSearch.hidden&&!searchInput.inert)setLobbySearch(false);
  const query=searchInput.value.trim().replace(/^@/,'').toLowerCase();
  const matches=query?people.filter(person=>[person.username,person.handle].some(value=>value?.toLowerCase().includes(query))):people;
  const shown=matches.slice(0,memberLimit);
  showMore.hidden=matches.length<=memberLimit;
  noMembers.hidden=!!matches.length;
  const key=JSON.stringify([state.leader,state.worldReady,state.voiceEnabled,state.lobbyProfile?.id,busy,!!state.busy,shown]);
  if(key===membersKey)return;
  membersKey=key;
  members.replaceChildren(...shown.map(person=>{
   const kick=state.leader&&!person.leader?button('×','kick',person.id):null;
   if(kick){kick.title='Исключить из лобби';kick.setAttribute('aria-label','Исключить '+person.username+' из лобби')}
   const status=person.leader?(state.worldReady?'Мир открыт':'Лидер лобби'):(preparationLabels[person.preparation]||'В лобби')+(['downloading','installing'].includes(person.preparation)?' · '+(person.percent||0)+'%':'');
   const actions=[];
   const own=person.id===state.lobbyProfile?.id;
   if(state.voiceEnabled){
    const micLabel=own?(person.voiceMic?'Выключить микрофон':'Включить микрофон'):(person.voiceMic?'Микрофон включён':'Микрофон выключен');
    const mic=own?button('','voice-mic',String(!person.voiceMic)):element('span','');
    mic.classList.add('lobby-voice-icon');mic.setAttribute('aria-label',micLabel);mic.title=micLabel;
    mic.dataset.active=String(!!person.voiceMic);mic.dataset.speaking=String(!!person.voiceSpeaking);
    mic.innerHTML='<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" aria-hidden="true"><rect x="9" y="3" width="6" height="12" rx="3"/><path d="M5 11a7 7 0 0 0 14 0m-7 7v3"/>'+(!person.voiceMic?'<path d="m4 4 16 16"/>':'')+'</svg>';
    actions.push(mic);
    if(!own){
     const speaker=button('','voice-mute',person.id);speaker.classList.add('lobby-voice-icon');speaker.dataset.active=String(!person.voiceMuted);
     const label=person.voiceMuted?'Включить звук игрока':'Заглушить игрока';speaker.setAttribute('aria-label',label);speaker.title=label;
     speaker.innerHTML='<svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 9h4l5-4v14l-5-4H3Z"/>'+(person.voiceMuted?'<path d="m16 9 5 6m0-6-5 6"/>':'<path d="M16 9a4 4 0 0 1 0 6m3-9a8 8 0 0 1 0 12"/>')+'</svg>';
     actions.push(speaker);
    }
   }
   if(kick)actions.push(kick);
   const memberRow=row(person,status,actions);
   memberRow.classList.add('lobby-member-row');
   return memberRow;
  }));
 }
 function lobbySwitch(title,hint){
  const label=element('label','presence-switch lobby-settings-switch');
  const copy=element('span','');
  copy.append(element('strong','',title));
  if(hint)copy.append(element('small','',hint));
  const input=element('input','');input.type='checkbox';input.setAttribute('role','switch');
  label.append(copy,input);
  return {label,input};
 }
 const {label:friendsOpen,input:friendsOpenInput}=lobbySwitch('Открыть лобби для друзей','Друзья смогут присоединиться из списка друзей.');
 friendsOpenInput.onchange=()=>{const open=friendsOpenInput.checked;friendsOpenInput.checked=!!state.room?.friends_open;void act('lobby-friends-open',String(open))};
 const {label:voiceToggle,input:voiceToggleInput}=lobbySwitch('Голосовой чат');
 voiceToggleInput.onchange=()=>{const enabled=voiceToggleInput.checked;voiceToggleInput.checked=!!state.voiceEnabled;void act('voice-enable',String(enabled))};
 const {label:spatialToggle,input:spatialToggleInput}=lobbySwitch('Пространственный звук','Голос затихает с расстоянием и звучит со стороны игрока.');
 spatialToggleInput.onchange=()=>{const enabled=spatialToggleInput.checked;spatialToggleInput.checked=state.voiceSpatial!==false;void act('voice-spatial',String(enabled))};
 const voiceRange=element('label','lobby-settings-range');
 const voiceRangeTitle=element('strong','','Дальность голоса');
 const voiceRangeValue=element('output','lobby-settings-range-value');
 const voiceRangeInput=element('input','');
 voiceRangeInput.type='range';voiceRangeInput.min='8';voiceRangeInput.max='128';voiceRangeInput.value='48';
 voiceRangeInput.setAttribute('aria-label','Дальность голоса в блоках');
 voiceRange.append(voiceRangeTitle,voiceRangeValue,voiceRangeInput,element('small','','Дальность для всех участников меняет лидер.'));
 voiceRangeInput.oninput=()=>{voiceRangeValue.value=voiceRangeInput.value;};
 voiceRangeInput.onchange=()=>void act('voice-range',voiceRangeInput.value);
 const voiceError=element('p','lobby-voice-error');voiceError.setAttribute('role','status');
 const lobbySettingsHint=element('p','lobby-settings-hint','Голосовой чат доступен в лобби');
 const lobbySettingsOptions=element('div','lobby-settings-options');
 lobbySettingsOptions.append(friendsOpen,voiceToggle,spatialToggle,voiceRange,lobbySettingsHint,voiceError);
 const lobbyConnection=element('section','lobby-settings-connection');
 const guideArrow=element('span','lobby-settings-guide-arrow','↗');guideArrow.setAttribute('aria-hidden','true');
 guideLink.append(guideArrow);
 lobbyConnection.append(
  element('h4','','P2P и оверлей'),
  element('p','','Лаунчер сначала пробует прямое подключение. Если оно недоступно, используется ретранслятор TURN.'),
  guideLink,
 );
 lobbySettings.append(lobbySettingsOptions,lobbyConnection);
 const lobbyProfile=element('select','');lobbyProfile.setAttribute('aria-label','Версия или сборка');lobbySetup.append(lobbyProfile);
 const manualLobbyOption=()=>{const option=element('option','','Версия из запущенной игры');option.value=manualLobbyProfile;return option};
 lobbyProfile.append(manualLobbyOption());
 const changePack=element('button','pack-cancel','Сменить сборку');changePack.type='button';changePack.hidden=true;
 lobbySetup.append(changePack);document.querySelector('#friends-host').before(lobbySetup);
 let currentLobbyProfile='',lobbyProfileChosen=false;
 lobbyProfile.addEventListener('change',()=>{lobbyProfileChosen=true;changePack.disabled=busy||state?.busy||!lobbyProfile.value||lobbyProfile.value===state?.hostProfile;});
 changePack.onclick=()=>act('change-lobby-pack',lobbyProfile.value);
 let lobbyProfilesAt=0,lobbyProfilesKey='';
 function defaultLobbyVersion(){
  if(lobbyProfileChosen||state?.room||state?.gameRunning)return;
  try{
   const selected=JSON.parse(localStorage.getItem('sintet.selectedProfile'));
   if(!selected)return;
   const id=selected.loader==='vanilla'?selected.base:`${selected.loader}-${selected.loaderVersion}-${selected.base}`;
   if((!selected.id||selected.id===id)&&[...lobbyProfile.options].some(option=>option.value===id)&&lobbyProfile.value!==id){
    lobbyProfile.value=id;
    window.SintetUI?.select.refresh(lobbyProfile);
   }
  }catch{}
 }
 async function refreshLobbyProfiles(){
  if(state?.room&&!state.leader||Date.now()-lobbyProfilesAt<15000)return;
  lobbyProfilesAt=Date.now();
  try{const profiles=await api().Installations(),key=JSON.stringify(profiles);if(key===lobbyProfilesKey)return;lobbyProfilesKey=key;
   const preferred=lobbyProfile.value;
   lobbyProfile.replaceChildren(manualLobbyOption());for(const profile of profiles||[]){const option=element('option','',profile.name?profile.name+' · '+profile.base:profile.base+' · '+(loaderNames[profile.loader]||profile.loader));option.value=profile.id;lobbyProfile.append(option)}
   if([...lobbyProfile.options].some(option=>option.value===preferred))lobbyProfile.value=preferred;
   defaultLobbyVersion();
   window.SintetUI?.select.refresh(lobbyProfile);
  }catch{}
 }
 document.querySelector('#friends-host').onclick=()=>{defaultLobbyVersion();return act(state?.room?'close':'host',lobbyProfile.value===manualLobbyProfile||!state?.gameRunning?lobbyProfile.value:'')};
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
  const packHeadings={hosting:'Сборка доступна',checking:'Сверяем сборку',required:'Сборка отличается',downloading:'Скачиваем сборку',stopping:'Сохраняем и закрываем Minecraft',installing:'Готовим Minecraft',launching:'Запускаем Minecraft',ready:'Сборка готова',error:'Сборка не подготовлена'};
  const versionHeadings={hosting:'Версия лидера',checking:'Проверяем версию Minecraft',required:'Нужно установить версию',downloading:'Готовим версию',ready:'Версия готова',error:'Версия не подготовлена'};
  const heading=(pack.versionOnly&&versionHeadings[pack.phase])||packHeadings[pack.phase]||'Сборка друга';
  card.append(element('strong','',heading));
  if(pack.game){
   const loader=loaderNames[pack.loader]||pack.loader;
   const details=pack.versionOnly?[pack.name,[loader,pack.loaderVersion].filter(Boolean).join(' ')]:[pack.name,pack.game,loader];
   card.append(element('span','pack-version',details.filter(Boolean).join(' · ')));
  }
  if(required){
   if(pack.phase==='required'&&!pack.versionOnly)card.querySelector('strong').textContent=pack.missing?window.SintetText.format('Нужно %d файлов · %s',pack.missing,packSize(pack.bytes)):state.preGameLobby?'Подготовить сборку':'Нужен перезапуск';
   card.append(element('p','',pack.error||(pack.versionOnly?'Лаунчер установит ту же версию Minecraft, что у лидера.':'Сборка сохраняется отдельно')));
   if(pack.digest){
    const action=element('button','pack-sync-button',pack.versionOnly?'Подготовить версию':state.preGameLobby?'Подготовить':pack.restart?'Скачать и перезапустить':'Скачать и войти');
    action.disabled=busy||state.busy;action.onclick=()=>pack.versionOnly?act('sync-pack',pack.digest):confirmPack(pack);card.append(action);
   }else if(pack.phase==='error'&&state.lobbyConnected){
    const retry=button('Повторить проверку','retry-pack');
    retry.classList.add('pack-sync-button');card.append(retry);
   }
  }
  if(working){const progress=element('progress','');progress.max=pack.total||100;if(pack.total)progress.value=pack.done;card.append(progress);if(pack.current)card.append(element('span','pack-current',pack.current));if(pack.total)card.append(element('small','',pack.phase==='installing'?Math.round(pack.done)+'%':packSize(pack.done)+' / '+packSize(pack.total)));if(pack.phase!=='checking'&&pack.phase!=='launching'){const cancel=element('button','pack-cancel','Отменить');cancel.onclick=()=>act('cancel-pack');card.append(cancel)}}
  if(pack.phase==='hosting'&&!pack.versionOnly)card.append(element('small','','Передаются моды и обычные конфиги. Личные данные и скрипты не передаются.'));
  if(pack.phase==='hosting'&&state.preGameLobby&&state.hostProfile!==manualLobbyProfile){const launch=button(state.gameRunning?'Игра запущена':'Запустить Minecraft','launch-host');launch.disabled=busy||state.busy||state.gameRunning;launch.classList.add('pack-sync-button');card.append(launch);if(!pack.versionOnly){const renew=button('Обновить файлы сборки','refresh-pack');renew.classList.add('pack-cancel');card.append(renew)}}
 }
 function render(){
  const offline=window.SintetNetwork.offline||!!state?.offline;
  const tourPreview=panel.hasAttribute('data-tour-preview');
  renderInvitation(offline);
  renderFriendRequest(offline);
  renderPresence(offline);
  lobbySettings.hidden=!lobbySettingsOpen;
  friendsOpen.hidden=!state?.leader||!state.friendLobbiesAvailable;
  friendsOpenInput.checked=!!state?.room?.friends_open;
  friendsOpenInput.disabled=offline||busy||!!state?.busy;
  voiceToggleInput.checked=!!state?.voiceEnabled;
  voiceToggleInput.disabled=!state?.room||busy||!!state.busy;
  spatialToggleInput.checked=state?.voiceSpatial!==false;
  spatialToggleInput.disabled=!state?.room||busy||!!state.busy;
  voiceRange.hidden=!state?.room;
  voiceRangeInput.disabled=!state?.leader||busy||!!state?.busy;
  if(!voiceRangeInput.matches(':active')){
   voiceRangeInput.value=state?.voiceRange||48;
   voiceRangeValue.value=voiceRangeInput.value;
  }
  lobbySettingsHint.hidden=!!state?.room;
  voiceError.hidden=!state?.voiceError;
  voiceError.textContent=state?.voiceError||'';
  offlineNotice.hidden=!offline||lobbySettingsOpen;
  tabs.hidden=lobbySettingsOpen||(offline&&!tourPreview);
  requestsButton.hidden=offline;
  if(offline&&!tourPreview){friendPage.hidden=true;lobbyPage.hidden=true;requestsPanel.hidden=true;return}
  selectTab(activeTab);
  if(!state)return;list.replaceChildren();
  const external=!!state.lobbyProvider&&state.lobbyProvider!=='sintet';
  const friendsEnabled=state.enabled&&(!external||!!state.lobbyProfile);
  requestsButton.hidden=!friendsEnabled;
  lobbyWarning.hidden=!external;
  if(!external)lobbyWarning.open=false;
  const pending=(state.friends||[]).filter(friend=>friend.status==='pending'&&friend.account);
  document.querySelector('#friends-count').textContent=(state.friends||[]).filter(f=>f.status==='accepted').length;
  const incoming=pending.filter(friend=>friend.incoming).length;
  const requestCount=requestsButton.querySelector('.friends-requests-count');
  requestCount.textContent=incoming;
  requestCount.hidden=incoming===0;
  requestsPanel.hidden=!showRequests;
  requestsButton.classList.toggle('active',showRequests);
  tools.hidden=!friendsEnabled||showRequests;
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
   if(friend.joinableLobby&&!state.room)buttons.unshift(button('Присоединиться','join-friend',friend.account.id));
   const node=row(friend.account,status,buttons);node.classList.toggle('friend-online',!!friend.presence);list.append(node);
  }
  if(!list.children.length&&!showRequests){const empty=element('div','friends-empty');empty.append(element('strong','',friendsEnabled?(filter?'Друзья не найдены':'Пока нет друзей'):'Подключаем профиль…'),element('span','',friendsEnabled?'Найди друга по нику, @юзернейму или ID':'В лобби можно войти по коду'));list.append(empty)}
  document.querySelector('#friends-join').hidden=!!state.room||!!state.localPort;
  const chooseLobbyProfile=state.hostNeedsPack||state.preGameLobby&&(!state.room||state.leader);
 lobbySetup.hidden=!chooseLobbyProfile;
 defaultLobbyVersion();
 if(!state.hostProfile)currentLobbyProfile='';
  if(state.hostProfile&&state.hostProfile!==currentLobbyProfile&&[...lobbyProfile.options].some(option=>option.value===state.hostProfile)){
   currentLobbyProfile=state.hostProfile;lobbyProfile.value=state.hostProfile;window.SintetUI?.select.refresh(lobbyProfile);
  }
  changePack.hidden=!state.leader;
  const host=document.querySelector('#friends-host');host.textContent=state.leader?'Закрыть лобби':state.room||state.localPort?'Выйти из лобби':'Создать лобби';host.classList.toggle('lobby-close',!!state.leader);host.disabled=busy||state.busy||!state.enabled;
  document.querySelector('#friends-room').hidden=!state.room;
  lobbyPage.classList.toggle('has-room',!!state.room||!!state.localPort);
  renderLobbyMembers();
  renderCapacityWarning();
  capacityToggle.hidden=!state.leader;
  document.querySelector('#friends-copy-room').textContent=state.room?.code||'';
  const connection=state.pack?.phase==='checking'?(state.pack.versionOnly?'Проверяем версию Minecraft':'Сверяем сборку'):state.connection;
  document.querySelector('#friends-connection').textContent=state.joinAttempt?(state.joinAttempt>1?'Повторная попытка · ':'')+(connection||'Входим в лобби…'):connection||(busy||state.busy?'Подождите…':'');
  const endpoint=document.querySelector('#friends-endpoint');
  endpoint.hidden=!state.worldReady||!state.localPort;
  const overlayHidden=window.SintetSettings?.config?.settings?.overlayEnabled===false;
  endpoint.textContent=endpoint.hidden?'':overlayHidden?'127.0.0.1:'+state.localPort:'Скопировать адрес';
  endpoint.title='Скопировать адрес';
  let address=document.querySelector('#lobby-address');if(!address){address=element('p','');address.id='lobby-address';endpoint.parentElement.before(address)}address.hidden=endpoint.hidden||overlayHidden;address.textContent=address.hidden?'':'127.0.0.1:'+state.localPort;
  const autoJoin=document.querySelector('#friends-auto-join');
  const worldJoin=state.worldReady&&state.localPort;
  const launchVersion=!state.leader&&state.lobbyConnected&&state.pack?.versionOnly&&state.pack.phase==='ready'&&(!state.gameRunning||state.pack.restart);
  autoJoin.hidden=(!worldJoin&&!launchVersion)||state.inWorld||state.gameJoining||state.noPack&&!state.gameReady||!!state.pack&&state.pack.phase!=='ready';
  autoJoin.textContent=worldJoin?(state.gameRunning?(state.pack?.restart?'Перезапустить и войти':'Присоединиться'):'Запустить и присоединиться'):(state.pack?.restart?'Перезапустить Minecraft':'Запустить Minecraft');
  autoJoin.title=autoJoin.textContent;
  autoJoin.setAttribute('aria-label',autoJoin.textContent);
  let manualNote=document.querySelector('#lobby-manual-note');
  if(!manualNote){manualNote=element('p','');manualNote.id='lobby-manual-note';document.querySelector('#friends-connection').after(manualNote);}
  manualNote.hidden=!state.noPack||state.leader;
  manualNote.textContent='Запусти ту же версию Minecraft, что и у лидера';
  const syncMessage=document.querySelector('#friends-error');syncMessage.textContent=state.error||(state.reconnecting?'Восстанавливаем связь с Sintet ID…':'');syncMessage.classList.toggle('is-reconnecting',!state.error&&!!state.reconnecting);
  for(const field of footer.querySelectorAll('input,button'))field.disabled=busy||state.busy||!state.enabled||(field===autoJoin&&!state.gameReady&&state.pack?.phase!=='ready');
  changePack.disabled=busy||state.busy||!state.enabled||!lobbyProfile.value||lobbyProfile.value===state.hostProfile;
  let autoOption=document.querySelector('#lobby-auto-connect');
  if(!autoOption){
   autoOption=element('label','lobby-auto-option');autoOption.id='lobby-auto-connect';
   const check=element('input','');check.type='checkbox';
   check.onchange=()=>void setAutoConnect(check.checked);
   autoOption.append(check,element('span','','Подключиться, когда мир будет готов'));
   document.querySelector('#friends-connection').after(autoOption);
  }
  autoOption.hidden=!state.preGameLobby||!state.lobbyConnected||state.leader||state.inWorld;
  const autoCheck=autoOption.querySelector('input');
  autoCheck.disabled=autoConnectPending!==null||!state.enabled;
  autoCheck.checked=autoConnectPending??!!state.autoConnect;
  let reconnect=document.querySelector('#lobby-reconnect');if(!reconnect){reconnect=button('Восстановить соединение','reconnect-lobby');reconnect.id='lobby-reconnect';document.querySelector('#friends-error').after(reconnect)}reconnect.hidden=!state.room||state.leader||state.lobbyConnected||busy||state.busy;
  renderPack();
 }
 window.addEventListener('sintet:profile-badges',()=>{
  lastPresenceView='';
  membersKey='';
  if(state)render();
 });
 let lobbyProfileSnapshot='';
 let discordJoinNotice=0;
 window.addEventListener('sintet:badge-assignments',()=>void refresh(true));
 async function refresh(badges=false){
  if(!api())return;
  const request=++stateRead;
  try{
   const next=await (badges&&api().RefreshProfileBadges?api().RefreshProfileBadges():api().FriendsState());
   if(request<stateApplied)return;
   stateApplied=request;
   const enabledChanged=state?.enabled!==next.enabled||state?.lobbyProvider!==next.lobbyProvider;
   state=next;
   if(next.discordJoinNotice&&next.discordJoinNotice!==discordJoinNotice){
    discordJoinNotice=next.discordJoinNotice;
    selectTab('lobby');
    if(!panel.matches(':popover-open')&&!document.querySelector('dialog[open]')){position();panel.showPopover()}
    if(next.discordJoinError)toast(window.SintetText.translate(next.discordJoinError));
   }
   window.SintetBadgeLive?.setEnabled(!!next.enabled&&!!next.lobbyProfile&&!next.offline);
   const profileKey=JSON.stringify(next.lobbyProfile||null);
   if(profileKey!==lobbyProfileSnapshot){
    lobbyProfileSnapshot=profileKey;
    window.SintetLobbyProfile=next.lobbyProfile||null;
    window.dispatchEvent(new CustomEvent('sintet:lobby-profile',{detail:window.SintetLobbyProfile}));
   }
   renderTurn(state);
   if(enabledChanged)refreshSettingsTurn();
   const phase=state.pack?.phase||'';
   if(!acceptedInvite&&['downloading','stopping','installing','launching'].includes(phase)&&phase!==lastPackPhase){
    selectTab('lobby');
    if(!panel.matches(':popover-open')&&!document.querySelector('dialog[open]')){position();panel.showPopover()}
   }
   lastPackPhase=phase;
   if(state.pack?.profile&&state.pack.profile!==lastPackProfile){
    lastPackProfile=state.pack.profile;
    window.dispatchEvent(new CustomEvent('sintet:p2p-profile',{detail:{id:lastPackProfile}}));
   }
   render();
   await refreshLobbyProfiles();
  }catch{}
 }
 async function openCommunity(service){try{if(!api())throw Error('Запусти Sintet через Start.bat');await api().OpenCommunity(service)}catch(error){toast(String(error?.message||error))}}
 document.querySelector('#community-discord').onclick=()=>openCommunity('discord');
 document.querySelector('#community-telegram').onclick=()=>openCommunity('telegram');
 window.addEventListener('sintet:network',render);
 window.addEventListener('sintet:presence-layout',()=>renderPresence());
 setInterval(refresh,2000);render();refresh();
})();
