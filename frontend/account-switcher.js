(() => {
 const stack = document.querySelector('#account-stack');
 const toggle = document.querySelector('#account-stack-toggle');
 const choices = document.querySelector('#account-choices');
 const profile = document.querySelector('#account-dialog');
 const addDialog = document.querySelector('#account-add-dialog');
 const localForm = document.querySelector('#account-local-form');
 const localName = document.querySelector('#account-local-name');
 const localError = document.querySelector('#account-local-error');
 const entries = new Map();
 let activeKey = '';
 let expanded = false;
 let canAdd = true;
 let changing = false;

 function setExpanded(value) {
  expanded = value;
  stack.classList.toggle('expanded', value);
  for (const item of entries.values()) item.inert = !value;
  add.hidden = value && !canAdd;
  toggle.tabIndex = value ? -1 : 0;
  toggle.setAttribute('aria-expanded', String(value));
  if (value) entries.get(activeKey)?.querySelector('.account-choice').focus({preventScroll:true});
 }

 function createChoice(key) {
  const item = document.createElement('div');
  item.className = 'account-slot';
  item.inert = !expanded;
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'account-choice';
  const avatar = document.createElement('span');
  avatar.className = 'account-choice-avatar';
  const name = document.createElement('span');
  name.className = 'account-choice-name';
  button.append(avatar,name);
  const remove = document.createElement('button');
  remove.type = 'button';
  remove.className = 'account-choice-remove';
  remove.textContent = '×';
  item.append(button,remove);

  button.addEventListener('click', async () => {
   if (changing) return;
   changing = true;
   try {
    if (key !== activeKey) await window.SintetIdentity.selectAccount(key);
    setExpanded(false);
    toggle.focus();
   } catch (error) { toast(String(error)); }
   finally { changing = false; }
  });
  remove.addEventListener('click', async () => {
   if (changing) return;
   changing = true;
   remove.disabled = true;
   try {
    await window.SintetIdentity.removeAccount(key);
    entries.get(activeKey)?.querySelector('.account-choice').focus({preventScroll:true});
    toast('Аккаунт убран из лаунчера');
   } catch (error) { toast(String(error)); }
   finally { changing = false; remove.disabled = false; }
  });
  return item;
 }

 const add = document.createElement('div');
 add.className = 'account-slot account-slot-add';
 add.innerHTML = '<button type="button" class="account-choice account-choice-add" aria-label="Добавить аккаунт" title="Добавить аккаунт"><span class="account-add-icon" aria-hidden="true"><svg class="account-add-mark" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg></span><span class="account-choice-name">Добавить</span></button>';
 const addButton = add.firstElementChild;
 addButton.addEventListener('click', () => {
  if (changing || !canAdd) return;
  setExpanded(false);
  profile.close();
  localForm.reset();
  localForm.hidden = true;
  localError.textContent = '';
  document.querySelector('#microsoft-login-status').hidden = true;
  addDialog.showModal();
 });
 choices.append(add);

 function position(item, index, count) {
  item.style.setProperty('--offset', (index-(count-1)/2)*69+'px');
  item.style.setProperty('--order',String(index));
 }

 function update(state) {
  activeKey = state.active;
  const profiles = [...state.profiles];
  const index = profiles.findIndex(item => item.key === activeKey);
  if (index > 0) profiles.unshift(profiles.splice(index,1)[0]);
  const keys = new Set(profiles.map(item => item.key));
  for (const [key,item] of entries) {
   if (!keys.has(key)) { item.remove(); entries.delete(key); }
  }
  canAdd = state.canAdd;
  const count = profiles.length+Number(canAdd);
  stack.classList.toggle('ready',profiles.length>0);
  stack.classList.toggle('has-stacked',profiles.length>1);
  stack.classList.toggle('overflow',count>5);
  for (const [index,view] of profiles.entries()) {
   let item = entries.get(view.key);
   if (!item) {
    item = createChoice(view.key);
    entries.set(view.key,item);
    choices.insertBefore(item,add);
   }
   const {username,avatarURL} = view.account;
   const button = item.querySelector('.account-choice');
   const avatar = item.querySelector('.account-choice-avatar');
   window.SintetAccount.renderAvatar(avatar,username,view.provider==='sintet'?avatarURL:'');
   item.querySelector('.account-choice-name').textContent = username;
   const provider = {sintet:'Sintet ID',ely:'Ely.by',microsoft:'Microsoft',local:'Локальный профиль'}[view.provider];
   button.title = username+' · '+provider;
   button.setAttribute('aria-label',button.title);
   button.setAttribute('aria-pressed',String(view.key===activeKey));
   item.classList.toggle('front',index===0);
   item.classList.toggle('stacked',index>0&&index<3);
   item.style.setProperty('--depth',String(Math.min(index,2)));
   const remove = item.querySelector('.account-choice-remove');
   remove.hidden = view.key==='local-default'&&profiles.length===1;
   remove.title = 'Убрать '+username+' из лаунчера';
   remove.setAttribute('aria-label',remove.title);
   position(item,index,count);
  }
  add.hidden = expanded && !canAdd;
  addButton.disabled = !canAdd;
  addButton.title = canAdd?'Добавить аккаунт':'Достигнут лимит аккаунтов';
  position(add,profiles.length,count);
 }

 toggle.addEventListener('click',()=>setExpanded(!expanded));
 profile.addEventListener('close',()=>setExpanded(false));
 profile.addEventListener('cancel',event=>{
  if(expanded){event.preventDefault();setExpanded(false);toggle.focus()}
 });
 function backToProfile() {
  addDialog.close();
  profile.showModal();
  toggle.focus();
 }
 document.querySelector('#account-add-close').addEventListener('click',backToProfile);
 addDialog.addEventListener('cancel',event=>{event.preventDefault();backToProfile()});
 document.querySelector('#account-local-toggle').addEventListener('click',()=>{
  localForm.hidden=!localForm.hidden;
  if(!localForm.hidden)localName.focus();
 });
 localForm.addEventListener('submit',async event=>{
  event.preventDefault();
  const submit=localForm.querySelector('button[type=submit]');
  submit.disabled=true;
  localError.textContent='';
  try{
   await window.SintetIdentity.addLocal(localName.value);
   backToProfile();
  }catch(error){localError.textContent=String(error)}
  finally{submit.disabled=false}
 });
 window.SintetAccountSwitcher={update};
})();
