(() => {
 const q=s=>document.querySelector(s);
 let api,activeKey='',last='',pollTask,starting=false,elyBusy=false;
 const addDialog=q('#account-add-dialog');
 const microsoftDialog=q('#microsoft-device-dialog');
 const microsoftButton=q('#microsoft-login');
 const microsoftCode=q('#microsoft-device-code');
 const microsoftStatus=q('#microsoft-login-status');
 const deviceStatus=q('#microsoft-device-status');
 const offline=()=>window.SintetNetwork.offline;
 let loginBlocked=false;
 function syncNetworkButtons(){
  q('#account-network-note').hidden=!offline();
  q('#sintet-id-login').disabled=loginBlocked||offline();
  q('#ely-login').disabled=loginBlocked||offline();
  microsoftButton.disabled=microsoftStarting||loginBlocked||offline();
 }
 window.addEventListener('sintet:network',syncNetworkButtons);
 let microsoftStarting=false, microsoftCancelling=false;
 syncNetworkButtons();
 const wait=document.createElement('dialog');wait.className='identity-wait';wait.setAttribute('aria-labelledby','identity-wait-title');wait.innerHTML='<div class="identity-orbit" aria-hidden="true"><img src="assets/brand/sintet-id-icon-light.svg" alt=""></div><img class="identity-wait-brand" src="assets/brand/sintet-id-light.svg" alt="Sintet ID"><h1 id="identity-wait-title">Подтверди вход<br>в браузере.</h1><p>Мы открыли сайт Sintet. Проверь свой ник<br>и разреши вход в лаунчер.</p><span class="identity-wait-status" role="status">Ожидаем подтверждения<span>•••</span></span><button type="button" id="identity-cancel">Отменить вход</button>';document.body.append(wait);
 const elyDialog=document.createElement('dialog');elyDialog.className='account-dialog ely-auth-dialog';elyDialog.setAttribute('aria-labelledby','ely-auth-title');elyDialog.innerHTML='<img class="ely-form-brand" src="assets/brand/ely-light.svg" alt="Ely.by"><h1 id="ely-auth-title">Вход в Ely.by</h1><p class="ely-auth-intro">Твой аккаунт. Твой ник. Твой скин.</p><form id="ely-auth-form"><label for="ely-username">Почта или ник</label><input id="ely-username" name="username" autocomplete="username" required maxlength="320" spellcheck="false"><label for="ely-password">Пароль</label><input id="ely-password" name="password" type="password" autocomplete="current-password" required maxlength="1024"><div id="ely-totp-group" hidden><label for="ely-totp">Код из приложения-аутентификатора</label><input id="ely-totp" name="totp" autocomplete="one-time-code" inputmode="numeric" pattern="[0-9]{6}" maxlength="6"></div><p id="ely-auth-error" class="ely-auth-error" role="status"></p><button type="submit" id="ely-submit" class="account-done">Войти через Ely.by</button><button type="button" id="ely-auth-cancel" class="ely-auth-cancel">Отмена</button></form><div class="ely-signup">Нет аккаунта? <button type="button" id="ely-register">Создать аккаунт ↗</button></div><p class="ely-password-note">Данные отправляются напрямую в Ely.by.<br>Пароль не сохраняется.</p>';document.body.append(elyDialog);
 function showWait(){if(!wait.open){q('#account-add-dialog').close();try{q('#versions-dialog')?.hidePopover?.()}catch{};wait.showModal()}}
 wait.addEventListener('cancel',e=>e.preventDefault());wait.querySelector('#identity-cancel').onclick=async()=>{try{await api?.CancelSintetLogin()}finally{wait.close();starting=false}};
 let renameTask=null, savedName='';
 function defaultName(){return storage.get('nickname','Player')}
 function apply(state){
  const account=state.account, source=state.provider, ely=source==='ely', microsoft=source==='microsoft';
  const online=ely||microsoft||source==='sintet';
  const nickname=q('#nickname');
  nickname.readOnly=online;
  if(activeKey!==state.active||(!renameTask&&document.activeElement!==nickname)){
   const name=account?.username||defaultName();
   if(nickname.value!==name||q('#profile-name').textContent!==name){nickname.value=name;updateProfile(name)}
  }
  activeKey=state.active;
  savedName=account?.username||defaultName();
  const mark=ely?'ely':'sintet-id', label=microsoft?'Microsoft':ely?'Ely.by':'Sintet ID';
  q('#account-profile-kind').hidden=online;
  q('#account-identity-brand').hidden=!online||microsoft;
  q('#account-microsoft-brand').hidden=!microsoft;
  q('#account-identity-brand').src='assets/brand/'+mark+'-light.svg';
  q('#account-identity-brand').alt=label;
  q('#nickname-source').hidden=!online;
  q('#nickname-source').src=microsoft?'assets/brand/microsoft.svg':'assets/brand/'+mark+'-icon-light.svg';
  q('#nickname-source').alt='Вход через '+label;
  q('#nickname-source').title=label;
  q('.nickname-field').classList.toggle('has-identity',online);
  q('.parameter-line .badge').textContent=online?label:'Offline';
  window.SintetAccount.setAccount(account,source,state.error||'');
  window.SintetAccountSwitcher.update(state);
 }
 async function login(){if(starting||!api||offline())return;starting=true;showWait();try{await api.BeginSintetLogin();starting=false;await poll()}catch(e){wait.close();starting=false;toast(String(e))}}
 function showMicrosoftError(message){
  microsoftStatus.textContent=message;
  microsoftStatus.hidden=false;
 }
 microsoftButton.onclick=async()=>{
  if(!api||microsoftStarting||offline())return;
  microsoftStarting=true;
  microsoftButton.disabled=true;
  microsoftStatus.hidden=true;
  try{
   const code=await api.BeginMicrosoftLogin();
   if(!addDialog.open){await api.CancelMicrosoftLogin();return}
   microsoftCode.textContent=code;
   deviceStatus.textContent='Ожидаем подтверждения…';
   addDialog.close();
   microsoftDialog.showModal();
   q('#microsoft-open').focus();
  }catch(error){showMicrosoftError(String(error))}
  finally{microsoftStarting=false;await refreshAfterChange()}
 };
 q('#microsoft-copy-code').onclick=async()=>{
  try{await navigator.clipboard.writeText(microsoftCode.textContent);deviceStatus.textContent='Код скопирован. Вставь его на странице Microsoft.'}
  catch{deviceStatus.textContent='Скопируй код вручную.'}
 };
 q('#microsoft-open').onclick=async()=>{
  try{await api.OpenMicrosoftVerification()}
  catch(error){deviceStatus.textContent=String(error)}
 };
 async function cancelMicrosoft(){
  if(microsoftCancelling)return;
  microsoftCancelling=true;
  try{
   await api.CancelMicrosoftLogin();
   microsoftDialog.close();
   microsoftCode.textContent='';
   addDialog.showModal();
  }catch(error){deviceStatus.textContent=String(error)}
  finally{microsoftCancelling=false;await refreshAfterChange()}
 }
 q('#microsoft-cancel').onclick=cancelMicrosoft;
 microsoftDialog.addEventListener('cancel',event=>{event.preventDefault();void cancelMicrosoft()});
 function closeEly(){if(elyBusy)return;elyDialog.close();q('#ely-password').value='';q('#ely-totp').value=''}
 function loginEly(){if(!api||elyBusy)return;q('#account-add-dialog').close();q('#ely-auth-form').reset();q('#ely-totp-group').hidden=true;q('#ely-totp').required=false;q('#ely-auth-error').textContent='';elyDialog.showModal();q('#ely-username').focus()}
 q('#ely-register').onclick=async()=>{if(!api||elyBusy)return;try{await api.OpenElyRegistration()}catch(error){toast(String(error))}};
 q('#ely-auth-cancel').onclick=closeEly;elyDialog.addEventListener('cancel',e=>{e.preventDefault();closeEly()});
 q('#ely-auth-form').onsubmit=async e=>{e.preventDefault();if(elyBusy)return;if(offline()){q('#ely-auth-error').textContent=window.SintetNetwork.message;return;}elyBusy=true;q('#ely-submit').textContent='Входим…';q('#ely-auth-error').textContent='';elyDialog.querySelectorAll('input,button').forEach(el=>el.disabled=true);try{await api.LoginEly(q('#ely-username').value,q('#ely-password').value,q('#ely-totp').value);q('#ely-password').value='';q('#ely-totp').value='';elyDialog.close();await poll();if(!q('#account-dialog').open)q('#account-dialog').showModal();toast('Вход через Ely.by выполнен')}catch(error){const message=String(error);if(message.includes('ELY_TWO_FACTOR_REQUIRED')){q('#ely-totp-group').hidden=false;q('#ely-totp').required=true;q('#ely-auth-error').textContent='Аккаунт защищён. Введите код из приложения.'}else q('#ely-auth-error').textContent=message}finally{elyBusy=false;elyDialog.querySelectorAll('input,button').forEach(el=>el.disabled=false);q('#ely-submit').textContent='Войти через Ely.by';if(!q('#ely-totp-group').hidden)q('#ely-totp').focus()}};
 window.SintetIdentity={
  login,loginEly,
  async saveLocalNickname(name){
   const key=activeKey||'local-default';
   if(key==='local-default'){
    storage.set('nickname',name);
    updateProfile(name);
    return;
   }
   if(renameTask){
    if(renameTask.key===key&&renameTask.name===name)return renameTask.promise;
    await renameTask.promise.catch(()=>{});
   }
   if(key!==activeKey)throw Error('Выбран другой аккаунт');
   if(name===savedName)return;
   const task={key,name};
   task.promise=(async()=>{
    await api.RenameLocalAccount(key,name,defaultName());
    savedName=name;
    updateProfile(name);
    await refreshAfterChange();
   })();
   renameTask=task;
   try{await task.promise}finally{if(renameTask===task)renameTask=null}
  },
  async selectAccount(key){await api.SelectAccount(key);void api.RefreshProfiles().catch(()=>{});await refreshAfterChange()},
  async removeAccount(key){await api.RemoveAccount(key);await refreshAfterChange()},
  async addLocal(name){await api.AddLocalAccount(name,defaultName());await refreshAfterChange()},
 };
 async function refreshAfterChange(){
  if(pollTask)await pollTask;
  await poll();
 }
 function poll(){
  if(!api)return;
  if(!pollTask)pollTask=readState().finally(()=>{pollTask=undefined});
  return pollTask;
 }
 async function readState(){
  try{
   const state=await api.IdentityState(defaultName());
   if(state.pending&&state.pendingProvider!=='microsoft'){showWait()}
   else if(microsoftDialog.open&&!microsoftCancelling&&!state.pending){
    microsoftDialog.close();
    microsoftCode.textContent='';
    if(state.loginError){addDialog.showModal();showMicrosoftError(state.loginError)}
    else if(state.provider==='microsoft'){
     q('#account-dialog').showModal();
     toast('Вход через Microsoft выполнен');
    }
   }
   else if(wait.open&&(!starting||state.loginError||state.active!==activeKey)){
    wait.close();starting=false;
    if(state.loginError)toast(state.loginError);
    else if(state.account){
     if(!q('#account-dialog').open)q('#account-dialog').showModal();
     toast('Аккаунт выбран: '+state.account.username);
    }
   }
   const key=JSON.stringify(state);
   if(key!==last){last=key;apply(state)}
   loginBlocked=!!(state.checking||state.pending);
   syncNetworkButtons();
  }catch{}
 }
 (async()=>{for(let i=0;i<100&&!window.go?.main?.App;i++)await new Promise(r=>setTimeout(r,100));api=window.go?.main?.App;if(api){await poll();setInterval(poll,450)}})();
})();
