const $=s=>document.querySelector(s);
const storage={get(k,f){try{return localStorage.getItem('sintet.'+k)??f}catch{return f}},set(k,v){try{localStorage.setItem('sintet.'+k,v)}catch{}}};
function page(id){document.querySelectorAll('.page').forEach(p=>p.classList.toggle('hidden',p.id!==id));document.querySelectorAll('.nav').forEach(b=>b.classList.toggle('active',b.dataset.page===id))}
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
let toastTimer;function toast(text){$('#toast').textContent=text;$('#toast').classList.add('visible');clearTimeout(toastTimer);toastTimer=setTimeout(()=>$('#toast').classList.remove('visible'),3500)}
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


