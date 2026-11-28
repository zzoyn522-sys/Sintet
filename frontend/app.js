const $=s=>document.querySelector(s);
// Internal drags are disabled. Files dropped from Explorer still use the Wails handler.
document.addEventListener('dragstart', event => event.preventDefault());
const storage={get(k,f){try{return localStorage.getItem('sintet.'+k)??f}catch{return f}},set(k,v){try{localStorage.setItem('sintet.'+k,v)}catch{}}};
let pageTransition;
const navTabs = [...document.querySelectorAll('.top-navigation .nav[data-page]')];
const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
function page(id) {
 const target = document.getElementById(id);
 if (!target?.classList.contains('page')) return;
 const changed = target.classList.contains('hidden');
 const galleryIndex = navTabs.length;
 const from = document.querySelector('#screenshots:not(.hidden)')
  ? galleryIndex : navTabs.findIndex(button=>button.classList.contains('active'));
 const to = id === 'screenshots' ? galleryIndex : navTabs.findIndex(button=>button.dataset.page === id);
 document.querySelectorAll('.page').forEach(view => view.classList.toggle('hidden',view !== target));
 document.querySelectorAll('.nav').forEach(button => button.classList.toggle('active',button.dataset.page === id));
 document.querySelector('#screenshots-toggle')?.setAttribute('aria-pressed',String(id === 'screenshots'));
 if (!changed) return;
 pageTransition?.cancel();
 // Opacity below 1 makes the page a new backdrop root, so its glass
 // panels can no longer sample the launcher background during the slide.
 if (from >= 0 && to >= 0 && !reduceMotion.matches && target.animate) {
  const offset = id === 'screenshots' ? '0 -18px' : `${Math.sign(to - from) * 18}px 0`;
  pageTransition = target.animate([
   {translate:offset},
   {translate:'none'}
  ],{duration:185,easing:'cubic-bezier(.2,.75,.3,1)'});
 }
}
reduceMotion.addEventListener('change',event=>{if(event.matches) pageTransition?.cancel()});
document.querySelectorAll('.nav').forEach(b=>b.onclick=()=>page(b.dataset.page));
$('#minimize').onclick=()=>{
 const app=window.go?.main?.App;
 if(app?.MinimizeLauncher)void app.MinimizeLauncher();else window.runtime?.WindowMinimise();
};
$('#close').onclick=()=>window.runtime?.Quit();
window.runtime?.EventsOn?.('sintet:tray-action',action=>{
 switch(action){
  case 'settings': $('.nav[data-page="settings"]').click();break;
  case 'workshop': $('.nav[data-page="workshop"]').click();break;
  case 'friends': if(!$('#friends-panel').matches(':popover-open'))$('#friends-toggle').click();break;
  case 'logs': if(!$('#logs-dialog').open)$('#open-logs').click();break;
 }
});
let toastTimer;
function toast(text, action = null) {
 const box = $('#toast');
 const button = $('#toast-action');
 const message = $('#toast-message');
 clearTimeout(toastTimer);
 const dismiss = () => {
  clearTimeout(toastTimer);
  box.classList.remove('visible');
  button.inert = true;
  button.onclick = null;
 };
 message.textContent = text;
 button.hidden = !action;
 button.inert = !action;
 button.textContent = action?.label || '';
 button.onclick = action ? () => {dismiss();void action.run();} : null;
 box.classList.add('visible');
 toastTimer = setTimeout(dismiss, action ? 6000 : 3500);
}
$('#nickname').value=storage.get('nickname','Player');
async function saveNick(){
 const field=$('#nickname');
 const nick=field.value.trim();
 const valid=/^[A-Za-z0-9_]{3,16}$/.test(nick);
 field.setAttribute('aria-invalid',String(!valid));
 $('#nick-error').textContent=valid?'':'3–16 символов: латиница, цифры и _';
 if(!valid||field.readOnly)return valid;
 try{
  if(window.SintetIdentity)await window.SintetIdentity.saveLocalNickname(nick);
  else{storage.set('nickname',nick);updateProfile(nick)}
  return true;
 }catch(error){
  field.setAttribute('aria-invalid','true');
  $('#nick-error').textContent=String(error);
  return false;
 }
}
$('#nickname').onchange=()=>{void saveNick()};
$('#nickname').oninput=()=>{$('#nick-error').textContent='';$('#nickname').removeAttribute('aria-invalid')};
$('#nickname').onkeydown=e=>{if(e.key==='Enter')$('#nickname').blur()};

function updateProfile(nick){$('#profile-name').textContent=nick;$('#avatar').textContent=nick[0].toUpperCase();window.dispatchEvent(new CustomEvent('sintet:profile',{detail:{nickname:nick}}))}updateProfile($('#nickname').value);


