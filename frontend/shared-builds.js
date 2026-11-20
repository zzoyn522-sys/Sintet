import {mergeWorkshopResults,projectSources} from './data/workshop-sources.js';
import {orderModVersions} from './data/mod-versions.js';
import {showWorkshopLoading} from './workshop-loading.js';

const el=(tag,cls,text)=>{
 const node=document.createElement(tag);
 if(cls)node.className=cls;
 if(text!==undefined)node.textContent=text;
 return node;
};
const q=(root,selector)=>root.querySelector(selector);
const api=()=>window.go?.main?.App;
const roleName={owner:'Лидер',editor:'Редактор',invited:'Приглашение'};
const loaders={fabric:'Fabric',forge:'Forge',neoforge:'NeoForge'};
let account='', lastRefresh=0, fetching, builds=[], refreshList, current=null, busy=false, progressPoll=0;
let activeTab='mods';
let view='build',detailRequest=0,liveReading=false,modsQuery='',memberQuery='';
let settingsDialog, settingsBuild, settingsArea, openSettings, openWorkshopCatalog, inviteControl;
let inviteView='', invitePending='', inviteError='';
const invitation=el('aside','launcher-invitation');
invitation.setAttribute('aria-label','Приглашение в сборку');
invitation.hidden=true;
document.querySelector('.launcher-invitations').append(invitation);
let iconObserver, iconTimer;
const previewIcons=new Map(), requestedIcons=new Set();
const linking=new Set();
const avatars=new Map();
const dialog=el('dialog','ws-dialog ws-build-dialog shared-build-dialog');
dialog.setAttribute('aria-labelledby','shared-build-title');
dialog.innerHTML='<button class="account-close" type="button" aria-label="Закрыть">×</button><header class="shared-build-header"><div class="shared-build-mark" aria-hidden="true"></div><div><h1 id="shared-build-title"></h1><p class="shared-build-meta"></p></div></header><div class="shared-build-tabs" aria-label="Разделы общей сборки" hidden></div><div class="shared-build-main"></div><footer class="shared-build-footer"><div class="shared-build-feedback"><div class="shared-build-status" role="status"></div><progress max="100" hidden></progress></div><div class="shared-build-footer-actions"></div></footer>';
document.body.append(dialog);
const confirmation=el('dialog','ws-dialog ws-build-delete-dialog');
confirmation.setAttribute('aria-labelledby','shared-build-confirm-title');
confirmation.innerHTML='<h2 id="shared-build-confirm-title"></h2><p></p><form method="dialog"><footer><button value="cancel" autofocus>Отмена</button><button class="primary" value="accept">Подтвердить</button></footer></form>';
document.body.append(confirmation);
const body=q(dialog,'.shared-build-main');
const status=q(dialog,'.shared-build-status');
const progress=q(dialog,'progress');
const tabs=q(dialog,'.shared-build-tabs');
const footerActions=q(dialog,'.shared-build-footer-actions');
const footerCaption=el('span','shared-build-footer-caption');
const footerButtons=el('div','shared-build-footer-buttons');
footerActions.append(footerCaption,footerButtons);
const footer=q(dialog,'.shared-build-footer');
const embedded=el('div','shared-build-embedded');
const layersIcon='<svg viewBox="0 0 24 24" width="23" height="23" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m12 3-9 5 9 5 9-5-9-5Zm-9 9 9 5 9-5M3 16l9 5 9-5"/></svg>';
q(dialog,'.shared-build-mark').innerHTML=layersIcon;

function button(label,action,classes='') {
 const item=el('button','ws-action'+(classes?' '+classes:''),label);
 item.type='button';
 item.addEventListener('click',action);
 return item;
}
function avatar(person,classes='') {
 const node=el('span','shared-person-avatar'+(classes?' '+classes:''),(person.name||person.username||'?').slice(0,1).toUpperCase());
 const fallback=el('img');
 fallback.src='assets/brand/default-avatar.webp';
 fallback.alt='';
 fallback.draggable=false;
 node.replaceChildren(fallback);
 const url=person.avatarUrl||person.authorAvatar||'';
 if(!url||!api()?.ResolveAvatar)return node;
 if(!avatars.has(url)){
  if(avatars.size>=100)avatars.clear();
  avatars.set(url,api().ResolveAvatar(url).catch(()=>{avatars.delete(url);return ''}));
 }
 avatars.get(url).then(src=>{
  if(!src)return;
  const image=el('img');
  image.src=src;
  image.alt='';
  image.draggable=false;
  node.replaceChildren(image);
 });
 return node;
}
function projectIcon(title,url) {
 const icon=el('span','shared-catalog-icon',(title||'?').slice(0,2));
 if(!url)return icon;
 try{
  const address=new URL(url,location.href);
  if(address.protocol==='https:'||(/^\/workshop-icons\/[a-f0-9]{64}\.png$/.test(url)&&address.origin===location.origin)){
   const image=el('img');
   image.src=address.href;
   image.alt='';
   image.draggable=false;
   image.loading='lazy';
   image.referrerPolicy='no-referrer';
   image.onload=()=>icon.replaceChildren(image);
   image.onerror=()=>image.remove();
   icon.append(image);
  }
 }catch{}
 return icon;
}
function confirmAction(title,description,action) {
 q(confirmation,'h2').textContent=title;
 q(confirmation,'p').textContent=description;
 confirmation.returnValue='cancel';
 confirmation.addEventListener('close',()=>{
  if(confirmation.returnValue==='accept')void action();
 },{once:true});
 confirmation.showModal();
}
function clearStatus() {
 status.textContent='';
 progress.hidden=true;
}
function errorMessage(error) {
 return String(error?.message||error).replace(/^Error:\s*/,'');
}
function roleBadge(role) {
 const badge=el('span','ws-build-state',roleName[role]);
 badge.dataset.state=role==='invited'?'busy':'ready';
 return badge;
}
function setHeader(title,meta='') {
 q(dialog,'h1').textContent=title;
 q(dialog,'.shared-build-meta').textContent=meta;
}
function description(build) {
 return [build.game,loaders[build.loader],build.loader_version||build.loaderVersion].filter(Boolean).join(' · ');
}
function setControls(disabled) {
 const controls=settingsDialog?[...embedded.querySelectorAll('button,input,select'),...footerButtons.querySelectorAll('button')]:dialog.querySelectorAll('button,input,select');
 for(const control of controls) {
  if(disabled){
   control.dataset.sharedWasDisabled=String(control.disabled);
   control.disabled=true;
  }else if(control.dataset.sharedWasDisabled!==undefined) {
   control.disabled=control.dataset.sharedWasDisabled==='true';
   delete control.dataset.sharedWasDisabled;
  }
 }
}
function updateProgress(stage) {
 if(!busy||!stage?.stage)return;
 status.textContent=[stage.stage,stage.file].filter(Boolean).join(' · ');
 progress.hidden=false;
 if(stage.total>0)progress.value=Math.max(0,Math.min(100,stage.done*100/stage.total));
 else progress.removeAttribute('value');
}
async function perform(action,options={}) {
 if(busy)return;
 busy=true;
 const id=options.id||current?.id;
 clearStatus();
 setControls(true);
 status.textContent=options.label||'Сохраняем…';
 progressPoll=setInterval(async()=>{
  if(!id||!busy)return;
  try{const state=await api().SharedBuildStatus(id);if(current?.id===id)updateProgress(state)}catch{}
 },500);
 try{
  const result=await action();
  lastRefresh=0;
  if(options.close)(settingsDialog||dialog).close();
  else if(id&&options.reload!==false)await detail(id);
  status.textContent=options.success||'';
  if(id)window.dispatchEvent(new Event('sintet:shared-mods'));
  return result;
 }catch(error){
  const message=errorMessage(error);
  if(id)await detail(id).catch(()=>{});
  status.textContent=message;
 }finally{
  clearInterval(progressPoll);
  progress.hidden=true;
  busy=false;
  setControls(false);
  void refreshBuilds();
  if(refreshList)void refreshList();
 }
}

q(dialog,'.account-close').onclick=()=>{if(!busy)dialog.close()};
dialog.addEventListener('cancel',event=>{if(busy)event.preventDefault()});
dialog.addEventListener('close',reset);
function reset(){
 detailRequest++;
 current=null;
 view='build';
 modsQuery='';
 memberQuery='';
 inviteControl=null;
 iconObserver?.disconnect();
 clearTimeout(iconTimer);
 previewIcons.clear();
 requestedIcons.clear();
 activeTab='mods';
 tabs.hidden=true;
 tabs.replaceChildren();
 clearFooterActions();
 body.replaceChildren();
 clearStatus();
}
function opened(){return (settingsDialog||dialog).open}

function clearFooterActions(){
 footerCaption.textContent='';
 footerButtons.replaceChildren();
}

function renderInvitation(){
 const build=builds.find(item=>item.role==='invited'&&!item.offlineReason);
 if(!build||window.SintetNetwork.offline){
  invitation.hidden=true;
  inviteView='';
  return;
 }
 const pending=invitePending===build.id;
 const failure=inviteError===build.id;
 const snapshot=JSON.stringify([build.id,build.name,build.owner_name,build.ownerAvatar,pending,failure]);
 if(snapshot===inviteView)return;
 inviteView=snapshot;
 const copy=el('div','launcher-invitation-copy');
 copy.append(el('strong','',window.SintetText.format('%s приглашает в сборку',build.owner_name||'Sintet ID')),
  el('span','',[build.name,description(build)].filter(Boolean).join(' · ')));
 if(failure)copy.append(el('small','','Не удалось ответить на приглашение. Попробуй ещё раз.'));
 const controls=el('div','launcher-invitation-actions');
 for(const [action,label] of [['decline','Отклонить'],['accept','Принять']]){
  const control=button(label,async()=>{
   invitePending=build.id;
   inviteError='';
   renderInvitation();
   try{await answerSharedBuild(build.id,action)}
   catch{if(invitePending===build.id)inviteError=build.id}
   finally{if(invitePending===build.id)invitePending='';renderInvitation()}
  });
  control.dataset.inviteAction=action;
  control.disabled=pending;
  controls.append(control);
 }
 invitation.replaceChildren(avatar({name:build.owner_name,avatarUrl:build.ownerAvatar}),copy,controls);
 invitation.hidden=false;
}

export async function answerSharedBuild(id,action){
 const identity=account;
 await api().SharedBuildChange(id,action,'',0,false);
 if(identity!==account)return;
 fetching=null;
 lastRefresh=0;
 builds=action==='accept'
  ?builds.map(build=>build.id===id?{...build,role:'editor'}:build)
  :builds.filter(build=>build.id!==id);
 renderInvitation();
 void refreshList?.();
 void refreshBuilds();
}

export function refreshSharedBuilds(){
 fetching=null;
 lastRefresh=0;
 return refreshBuilds();
}

function refreshBuilds(){
 if(!account||!api()?.SharedBuilds||window.SintetNetwork.offline||Date.now()-lastRefresh<5000)return;
 if(fetching)return fetching;
 const identity=account;
 lastRefresh=Date.now();
 const task=api().SharedBuilds().then(result=>{
  if(fetching!==task||account!==identity)return;
  const next=result||[];
  if(JSON.stringify(next)===JSON.stringify(builds))return;
  builds=next;
  renderInvitation();
  void refreshList?.();
 }).catch(()=>{}).finally(()=>{if(fetching===task)fetching=null});
 fetching=task;
 return task;
}

function setAccount(state) {
 const id=state?.provider==='sintet'?state.account?.id||'':
  window.SintetLobbyProfile?.provider===state?.provider?window.SintetLobbyProfile?.id||'':'';
 if(account===id)return;
 account=id;
 builds=[];
 lastRefresh=0;
 fetching=null;
 invitePending=inviteError='';
 if(opened())(settingsDialog||dialog).close();
 renderInvitation();
 void refreshBuilds();
 if(refreshList)void refreshList();
}
window.addEventListener('sintet:identity',event=>setAccount(event.detail));
window.addEventListener('sintet:lobby-profile',()=>setAccount(window.SintetIdentity?.current()));
setAccount(window.SintetIdentity?.current());

export function canShareBuild(build) {
 return !!account && build.id.startsWith('build-') && build.installed && loaders[build.loader];
}
export function sharedBuildSettings(onOpen,onCatalog){
 openSettings=onOpen;
 openWorkshopCatalog=onCatalog;
}
export function activeSharedBuild(){
 return settingsDialog?.open?current:null;
}
export function sharedSettingsBusy(){return !!settingsDialog&&busy}
export function mountSharedSettings(host,local,shared) {
 settingsDialog=host;
 settingsBuild=local;
 current=shared||null;
 view='build';
 host.addEventListener('cancel',preventBusy);
 host.addEventListener('close',()=>{
  host.removeEventListener('cancel',preventBusy);
  settingsDialog=settingsBuild=settingsArea=null;
  dialog.append(tabs,body,footer);
  footerActions.append(footerButtons);
  embedded.remove();
  reset();
 },{once:true});
 if(shared)void detail(shared.id).catch(showDetailFailure);
}
function preventBusy(event){if(busy)event.preventDefault()}
function showDetailFailure(error) {
 if(!opened()||!current)return;
 if(errorMessage(error).toLocaleLowerCase()==='сборка недоступна'){
  (settingsDialog||dialog).close();
  window.toast?.('Сборка недоступна');
  void refreshSharedBuilds();
  return;
 }
 current={...current,offlineReason:window.SintetNetwork.offline?'Нет доступа к интернету':errorMessage(error)};
 renderBuild(current);
}
export function selectSharedPanel(tab){
 if(!settingsDialog)return;
 settingsArea=tab;
 const access=q(settingsDialog,'#ws-shared-access');
 const mods=q(settingsDialog,'#ws-shared-mods');
 const localMods=q(settingsDialog,'#ws-build-local-mods');
 const actions=q(settingsDialog,'#ws-build-shared-actions');
 const buttons=q(settingsDialog,'#ws-build-shared-buttons');
 mods.hidden=!current;
 localMods.hidden=!!current;
 actions.hidden=tab!=='access'&&(tab!=='mods'||!current);
 buttons.hidden=actions.hidden;
 if(actions.hidden)return;
 (tab==='mods'?mods:access).append(embedded);
 embedded.append(tabs,body);
 actions.append(footer);
 buttons.append(footerButtons);
 if(current){
  activeTab=tab==='mods'?'mods':'members';
  renderBuild(current);
 }else if(canShareBuild(settingsBuild)){
  tabs.hidden=true;
  clearFooterActions();
  const panel=group('Общий доступ');
  panel.classList.add('shared-build-sharing');
  const heading=el('div','shared-build-section-heading');
  const enable=button('Поделиться',async()=>{
   const id=await perform(()=>api().SharedBuildCreate(settingsBuild.id,settingsBuild.name),{label:'Подготовка модов…',reload:false});
   if(!id||!opened())return;
   current={id,role:'owner'};
   try{await detail(id);selectSharedPanel(settingsArea);clearStatus()}catch(error){showDetailFailure(error)}
  },'shared-build-primary');
  enable.disabled=window.SintetNetwork.offline;
  heading.append(panel.firstChild,enable);
  panel.append(heading,el('p','shared-build-hint','Пригласи друзей редактировать эту сборку, пока твой лаунчер открыт.'));
  body.replaceChildren(panel);
 }
}
export function handleSharedDrop(x,y,paths){
 const zone=q(embedded,'.shared-build-dropzone');
 if(!zone||!zone.getClientRects().length||!current||busy||readOnly(current))return false;
 const {left,right,top,bottom}=zone.getBoundingClientRect();
 if(x<left||x>right||y<top||y>bottom)return false;
 const build=current;
 void perform(()=>api().SharedBuildAddFiles(build.id,build.draft_version,paths),{label:'Добавление JAR…'});
 return true;
}

export function sharedBuildList(query,local,refresh) {
 refreshList=refresh;
 if(!account||!api()?.SharedBuilds)return {linked:new Map(),items:[],total:0};
 void refreshBuilds();
 const linked=new Map();
 const candidates=[];
 for(const build of builds){
  const origin=build.role==='owner'&&local.find(item=>item.id===build.origin_profile);
  if(origin)linked.set(origin.id,build);
  else candidates.push(build);
 }
 const unmatched=[];
 const unbound=candidates.filter(build=>build.role==='owner'&&!build.origin_profile);
 const matches=(build,item)=>item.id.startsWith('build-')&&item.installed&&item.name.toLowerCase()===build.name.toLowerCase()&&item.game===build.game&&item.loader===build.loader&&item.loaderVersion===build.loader_version;
 for(const build of candidates){
  const matching=unbound.includes(build)?local.filter(item=>!linked.has(item.id)&&matches(build,item)):[];
  if(matching.length===1&&unbound.filter(other=>matches(other,matching[0])).length===1){
   const origin=matching[0];
   linked.set(origin.id,build);
   if(!window.SintetNetwork.offline&&!linking.has(build.id)){
    linking.add(build.id);
    void api().SharedBuildBind(build.id,origin.id).then(()=>{lastRefresh=0;void refreshList?.()}).catch(()=>linking.delete(build.id));
   }
  }else unmatched.push(build);
 }
 const items=unmatched.filter(build=>[build.name,build.game,build.owner_name,loaders[build.loader]].join(' ').toLocaleLowerCase().includes(query.toLocaleLowerCase()));
 return {linked,items,total:unmatched.length};
}

export function sharedBuildCard(build) {
 const row=el('article','ws-build-card shared-build-card');
 row.setAttribute('role','listitem');
 const icon=el('span','ws-build-icon shared-build-icon');
 icon.innerHTML=layersIcon;
 const info=el('div','ws-build-card-info');
 info.append(el('h3','',build.name),el('p','',[description(build),build.owner_name].filter(Boolean).join(' · ')));
 const controls=el('div','ws-build-card-actions');
 controls.append(button(build.role==='invited'?'Приглашение':'Настроить',()=>openBuild(build)));
 const badges=el('div','shared-build-actions');
 badges.append(el('span','ws-build-state',build.role==='editor'&&readOnly(build)?'Только чтение':'Общая'),roleBadge(build.role));
 row.append(icon,info,badges,controls);
 row.onclick=event=>{if(!event.target.closest('button'))void openBuild(build)};
 return row;
}
async function openBuild(build) {
 if(busy||opened())return;
 if(build.role!=='invited'&&openSettings){openSettings(build);return}
 clearStatus();
 current=build;
 view='build';
 modsQuery='';
 setHeader(build.name,description(build));
 body.replaceChildren();
 dialog.showModal();
 if(build.role==='invited') {
  body.append(el('p','shared-build-hint',window.SintetText.format('%s приглашает тебя редактировать сборку.',build.owner_name||'Sintet ID')));
  const buttons=el('div','shared-build-actions');
  buttons.append(button('Отклонить',()=>perform(()=>answerSharedBuild(build.id,'decline'),{close:true})),button('Принять',async()=>{
   const accepted=await perform(async()=>{await answerSharedBuild(build.id,'accept');return true},{reload:false});
   if(accepted){
    dialog.addEventListener('close',()=>openSettings?.({...build,role:'editor'}),{once:true});
    dialog.close();
   }
  },'shared-build-primary'));
  body.append(buttons);
 } else {
  const finish=showWorkshopLoading(body,'compact',{count:4});
  try{await detail(build.id);clearStatus()}
  catch(error){if(dialog.open&&current?.id===build.id)status.textContent=errorMessage(error)}
  finally{finish()}
 }
}

async function detail(id) {
 const generation=++detailRequest,identity=account;
 const build=await api().SharedBuildDetail(id);
 if(generation!==detailRequest||identity!==account||!opened()||current?.id&&current.id!==id)return;
 current=build;
 setHeader(build.name,description(build));
 if(settingsDialog)window.dispatchEvent(new CustomEvent('sintet:shared-profile',{detail:build}));
 view='build';
 renderBuild(build);
}
async function refreshOpenBuild() {
 if(liveReading||busy||!opened()||!current||current.role==='invited'||document.hidden)return;
 const previous=current,generation=detailRequest,identity=account;
 liveReading=true;
 try{
  const build=await api().SharedBuildDetail(previous.id);
  if(busy||generation!==detailRequest||identity!==account||!opened()||current?.id!==previous.id)return;
  if(JSON.stringify(build)===JSON.stringify(previous))return;
  current=build;
  setHeader(build.name,description(build));
  if(settingsDialog)window.dispatchEvent(new CustomEvent('sintet:shared-profile',{detail:build}));
  if(view==='catalog'&&!readOnly(build)){
   renderFooter(build);
   return;
  }
  const scroll=body.scrollTop;
  const input=body.querySelector('[data-shared-field]:focus');
  const fields=[...body.querySelectorAll('[data-shared-field]')].map(field=>({key:field.dataset.sharedField,value:field.value,start:field.selectionStart,end:field.selectionEnd,focused:field===input}));
  renderBuild(build);
  for(const saved of fields){
   const field=body.querySelector('[data-shared-field="'+saved.key+'"]');
   if(field){
    field.value=saved.value;
    if(saved.focused){field.focus({preventScroll:true});field.setSelectionRange(saved.start,saved.end)}
   }
  }
  body.scrollTop=scroll;
 }catch(error){
  if(!busy&&opened()&&current?.id===previous.id&&generation===detailRequest&&identity===account)showDetailFailure(error);
 }finally{liveReading=false}
}
setInterval(()=>void refreshOpenBuild(),2500);
window.addEventListener('focus',()=>{void refreshOpenBuild();void refreshBuilds()});
window.addEventListener('sintet:network',()=>{
 renderInvitation();
 void refreshBuilds();
 if(current&&opened()){renderBuild(current);void refreshOpenBuild()}
});
setInterval(()=>void refreshBuilds(),5500);
function change(action,key='',enabled=false,close=false) {
 const build=current;
 if(!build)return;
 return perform(()=>api().SharedBuildChange(build.id,action,key,build.draft_version||0,enabled),{close});
}
function group(title) {
 const section=el('section','shared-build-section');
 section.append(el('h2','',title));
 return section;
}

function readOnly(build) {
 if(window.SintetNetwork.offline)return 'Нет доступа к интернету';
 if(build.offlineReason)return build.offlineReason;
 if(build.role==='owner')return '';
 if(!build.access_open)return 'Лидер закрыл общий доступ';
 if(!build.owner_online)return 'Лидер сборки офлайн';
 return '';
}

function renderBuild(build) {
 iconObserver?.disconnect();
 clearTimeout(iconTimer);
 view='build';
 const owner=build.role==='owner';
 const mods=build.mods||[];
 const labels=[['mods','Моды',mods.length],['members','Участники',(build.members?.length||0)+1]]
  .filter(([key])=>!settingsDialog||(settingsArea==='mods'?key==='mods':key!=='mods'));
 tabs.hidden=labels.length<2;
 tabs.replaceChildren(...labels.map(([key,label,count])=>{
  const tab=button(label,()=>{activeTab=key;clearStatus();renderBuild(build);body.scrollTop=0});
  tab.append(el('span','shared-build-count',count));
  tab.setAttribute('aria-pressed',String(activeTab===key));
  return tab;
 }));
 const page=activeTab==='members'?renderMembers(build):renderMods(build);
 const reason=readOnly(build);
 const availability=el('div','shared-build-availability');
 const label=reason?'Только чтение':!build.access_open?'Общий доступ закрыт':'Общий доступ открыт';
 const details=reason||(build.access_open?'Участники могут редактировать, пока лидер онлайн.':'Участники сохранят доступ к скачанным файлам.');
 const summary=el('div');
 summary.append(el('strong','',label),el('p','',details));
 availability.dataset.available=String(!reason&&build.access_open);
 availability.append(summary);
 if(owner&&settingsArea!=='mods'){
  const access=button(build.access_open?'Закрыть доступ':'Открыть доступ',()=>change('access','',!build.access_open));
  access.disabled=!!reason;
  availability.append(access);
 }
 body.replaceChildren(...(settingsArea!=='mods'||reason?[availability]:[]),page);
 if(settingsDialog)q(settingsDialog,'#ws-build-mod-tab-count').textContent=mods.length||'';
 renderFooter(build);
}
function renderFooter(build) {
 const owner=build.role==='owner';
 const pending=(build.mods||[]).filter(mod=>mod.enabled&&!mod.approved).length;
 clearFooterActions();
 if(owner&&pending&&settingsArea!=='mods')footerCaption.textContent=window.SintetText.format('На подтверждении: %s',pending);
 if(view==='catalog')return;
 const reason=readOnly(build);
 const local=build.localProfile?.id?.startsWith('build-shared-');
 if(owner) {
  if(settingsArea==='mods'){
   const rollback=button('Откатить последние изменения',()=>{
    confirmAction('Откатить последние изменения?', 'Будет восстановлен предыдущий список модов.',
     ()=>perform(()=>api().SharedBuildRollback(build.id,build.draft_version),{label:'Восстанавливаем список модов…',success:'Список модов восстановлен'}));
   });
   rollback.disabled=!build.can_rollback||!!reason;
   footerButtons.append(rollback);
  }
  const offlineCopy=!!reason&&local;
  const play=button('Играть',()=>offlineCopy?launchLocalBuild(build):runBuild(build,0),'shared-build-primary');
  play.disabled=!offlineCopy&&(pending>0||!!reason);
  footerButtons.append(play);
 }else if(build.current_revision&&!reason&&(!local||build.localProfile.sharedRevision!==build.current_revision)){
  footerButtons.append(button('Играть',()=>void runBuild(build,build.current_revision),'shared-build-primary'));
 }else if(local){
  footerButtons.append(button('Играть',()=>launchLocalBuild(build),'shared-build-primary'));
 }else if(!build.current_revision){
  footerCaption.textContent='Ждём первого запуска лидером';
 }
}

function renderMods(build) {
 const owner=build.role==='owner';
 const reason=readOnly(build);
 const mods=group('Моды сборки');
 const heading=el('div','shared-build-section-heading');
 const toolbar=el('div','shared-build-actions');
 toolbar.append(
  button('Добавить JAR',()=>perform(()=>api().SharedBuildPickMods(build.id,build.draft_version),{label:'Добавление JAR…'})),
  button('Быстрый поиск',()=>catalogForm(build)),
  button('Добавить из каталога',()=>openWorkshopCatalog?.(current),'shared-build-primary')
 );
 for(const control of toolbar.children)control.disabled=!!reason;
 heading.append(mods.firstChild,toolbar);
 mods.append(heading);
 const search=el('input','shared-mod-search');
 search.type='search';
 search.placeholder='Поиск по модам сборки';
 search.setAttribute('aria-label',search.placeholder);
 search.dataset.sharedField='mods-search';
 search.value=modsQuery;
 const list=el('div','ws-build-mods');
 const rows=[];
 for(const mod of build.mods||[]) {
  const row=el('div','ws-build-mod shared-build-mod');
  const info=el('div','ws-build-mod-info');
  const filename=mod.file.path.slice(5);
  const source=mod.file.sources?.[0]?.provider;
  const name=mod.metadata?.name||filename;
  info.append(el('strong','',name));
  const subtitle=mod.metadata?.version||filename;
  if(subtitle!==name)info.append(el('span','',subtitle));
  const by=el('div','shared-mod-by');
  by.append(avatar({name:mod.authorName,avatarUrl:mod.authorAvatar},'small'),el('span','',mod.authorName||'Автор'));
  info.append(by);
  const hash=mod.file.sha256;
  row.append(projectIcon(name,mod.metadata?.icon||previewIcons.get(hash)),info);
  if(!mod.metadata?.icon&&!mod.available)row.dataset.sharedModIcon=hash;
  const actions=el('div','shared-mod-controls');
  const state=el('span','shared-mod-state',mod.approved?'Одобрен':'Ждёт решения лидера');
  state.dataset.approved=String(!!mod.approved);
  const badges=el('div','shared-mod-badges');
  badges.append(state);
  if(mod.available)badges.append(el('span','shared-mod-local','На этом ПК'));
  if(source){const label=el('span','shared-catalog-tag',source==='modrinth'?'Modrinth':'CurseForge');label.dataset.source=source;badges.append(label)}
  info.append(badges);
  if(!mod.approved) {
   if(owner)actions.append(button(mod.available?'Одобрить':'Скачать и одобрить',()=>{
    confirmAction('Одобрить мод?', 'Моды выполняют код внутри Minecraft. Одобряй файл, если доверяешь его автору.',
     ()=>perform(()=>api().SharedBuildApprove(build.id,mod.key,mod.file.sha256,build.draft_version),{label:'Проверка файла…'}));
   },'shared-build-review'));
  }
  const toggle=button(mod.enabled?'Включён':'Отключён',()=>change('toggle',mod.key,!mod.enabled),'ws-build-toggle');
  toggle.setAttribute('role','switch');
  toggle.setAttribute('aria-checked',String(!!mod.enabled));
  const remove=button('Удалить',()=>{
   confirmAction('Убрать мод?', 'Мод будет убран из списка сборки.',()=>change('remove',mod.key));
  });
  actions.append(toggle,remove);
  for(const control of actions.children)control.disabled=!!reason;
  row.append(actions);
  list.append(row);
  rows.push({node:row,text:[name,filename,mod.authorName].join(' ').toLocaleLowerCase()});
 }
 if(!list.children.length)list.append(el('p','ws-build-no-mods','В сборке пока нет модов.'));
 const filter=()=>{modsQuery=search.value;const term=modsQuery.trim().toLocaleLowerCase();for(const row of rows)row.node.hidden=!row.text.includes(term)};
 search.addEventListener('input',filter);
 filter();
 const dropzone=el('div','ws-build-dropzone shared-build-dropzone','Перетащи сюда JAR-файлы');
 dropzone.hidden=!!reason;
 mods.append(search,dropzone);
 const pending=(build.mods||[]).filter(mod=>!mod.approved).length;
 if(owner&&pending){
  const review=el('div','shared-build-availability');
  review.append(el('strong','',window.SintetText.format('На подтверждении: %s',pending)));
  const approve=button('Скачать и одобрить все моды',()=>{
   confirmAction('Одобрить все моды?', 'Моды выполняют код внутри Minecraft. Подтверди загрузку, если доверяешь авторам.',
    ()=>perform(()=>api().SharedBuildApproveAll(build.id,build.draft_version),{label:'Скачиваем и проверяем моды…',success:'Все моды одобрены'}));
  },'shared-build-review');
  approve.disabled=!!reason;
  review.append(approve);
  mods.append(review);
 }
 mods.append(list);
 if(build.access_open&&build.owner_online&&api()?.SharedBuildModIcons)observeModIcons(build,list);
 return mods;
}

function observeModIcons(build,list){
 const pending=new Set();
 iconObserver=new IntersectionObserver(entries=>{
  for(const entry of entries){
   const hash=entry.target.dataset.sharedModIcon;
   if(entry.isIntersecting&&!requestedIcons.has(hash))pending.add(hash);
  }
  if(!pending.size)return;
  clearTimeout(iconTimer);
  iconTimer=setTimeout(async()=>{
   if(current?.id!==build.id||!list.isConnected)return;
   const hashes=[...pending].slice(0,18);
   for(const hash of hashes){pending.delete(hash);requestedIcons.add(hash)}
   try{
    const icons=await api().SharedBuildModIcons(build.id,hashes);
    if(current?.id!==build.id||!opened())return;
    for(const [hash,address] of Object.entries(icons||{})){
     previewIcons.set(hash,address);
     const row=[...body.querySelectorAll('[data-shared-mod-icon]')].find(item=>item.dataset.sharedModIcon===hash);
     if(row)row.firstChild.replaceWith(projectIcon(row.querySelector('strong').textContent,address));
    }
   }catch{}
  },150);
 });
 for(const row of list.querySelectorAll('[data-shared-mod-icon]'))iconObserver.observe(row);
}
function renderMembers(build) {
 const owner=build.role==='owner';
 const reason=readOnly(build);
 const members=group('Участники');
 if(owner&&!reason){
  inviteControl??=inviteForm();
  members.append(inviteControl);
  inviteControl.refresh();
 }
 const leader=el('div','ws-build-mod');
 leader.append(avatar({name:build.owner_name||'Лидер',avatarUrl:build.ownerAvatar}),el('strong','ws-build-mod-info',build.owner_name||'Лидер'),roleBadge('owner'));
 members.append(leader);
 for(const member of build.members||[]) {
  const row=el('div','ws-build-mod');
  const info=el('div','ws-build-mod-info');
  info.append(el('strong','',member.name));
  if(member.handle)info.append(el('span','','@'+member.handle));
  row.append(avatar({...member,avatarUrl:member.avatarUrl}),info,roleBadge(member.accepted?'editor':'invited'));
  if(owner&&!reason)row.append(button('Убрать доступ',()=>change('revoke',member.id)));
  members.append(row);
 }
 if(!owner)members.append(button('Выйти из сборки',()=>{
  confirmAction('Выйти из сборки?', 'Для возврата потребуется новое приглашение.',()=>perform(()=>answerSharedBuild(build.id,'leave'),{close:true}));
 }));
 if(owner&&!reason){
  const stop=button('Удалить сборку',()=>{
   confirmAction('Удалить сборку у всех?', 'Сборка исчезнет у всех участников. Локальные файлы сборки на этом ПК, включая миры, будут удалены.',()=>perform(async()=>{
    await api().DeleteSharedWorkshopBuild(build.id);
    builds=builds.filter(item=>item.id!==build.id);
    window.dispatchEvent(new Event('sintet:builds'));
   },{close:true,label:'Удаляем сборку…'}));
  },'shared-build-delete');
  members.append(stop);
 }
 return members;
}
function runBuild(build,revision) {
 return perform(()=>api().SharedBuildRun(build.id,build.draft_version,revision,true),
  {label:'Сверка файлов сборки…',close:true});
}
function launchLocalBuild(build){
 return perform(()=>api().SharedBuildLaunchLocal(build.id),{label:'Запуск Minecraft…',close:true});
}
function inviteForm() {
 const form=el('form','shared-build-search');
 const field=el('input');
 field.type='search';
 field.autocomplete='off';
 field.maxLength=37;
 field.placeholder='Ник, @юзернейм или Sintet ID';
 field.value=memberQuery;
 field.dataset.sharedField='member-search';
 field.setAttribute('aria-label',field.placeholder);
 const results=el('div','shared-build-results');
 results.setAttribute('aria-live','polite');
 form.append(field,results);
 let search=0,timer,found=[];
 const render=()=>{
  const visible=found.filter(person=>!current?.members?.some(member=>member.id===person.id));
  results.replaceChildren(...visible.map(person=>{
   const row=el('div','ws-build-mod');
   const info=el('div','ws-build-mod-info');
   info.append(el('strong','',person.name||person.nickname||person.username));
   if(person.handle)info.append(el('span','','@'+person.handle));
   row.append(avatar(person),info,button('Пригласить',()=>change('invite',person.id)));
   return row;
  }));
  if(field.value.trim().length>=2&&!visible.length)results.append(el('p','shared-build-hint','Подходящих аккаунтов нет.'));
 };
 form.refresh=()=>{if(found.length)render()};
 async function find(){
  if(busy)return;
  const term=field.value.trim(),request=++search,build=current;
  if(term.replace(/^@/,'').length<2){results.replaceChildren();return}
  const finish=showWorkshopLoading(results,'compact',{count:2});
  try{
   const matches=await (api().SharedBuildSearch?api().SharedBuildSearch(term):api().FriendsSearch(term));
   if(current?.id!==build.id||request!==search||!form.isConnected)return;
   found=matches||[];
   render();
  }catch(error){
   if(request===search&&form.isConnected)results.replaceChildren(el('p','shared-build-hint',errorMessage(error)));
  }finally{finish()}
 }
 field.addEventListener('input',()=>{
  memberQuery=field.value;
  clearTimeout(timer);
  search++;
  found=[];
  results.replaceChildren();
  timer=setTimeout(()=>void find(),230);
 });
 field.addEventListener('keydown',event=>{
  if(event.key!=='ArrowDown')return;
  const first=results.querySelector('button');
  if(first){event.preventDefault();first.focus()}
 });
 form.onsubmit=event=>{event.preventDefault();clearTimeout(timer);void find()};
 return form;
}

const catalogProviders={modrinth:'Modrinth',curseforge:'CurseForge'};
const catalogSources=Object.keys(catalogProviders);

function catalogForm(build) {
 view='catalog';
 clearStatus();
 tabs.hidden=true;
 clearFooterActions();
 const page=el('section','shared-catalog');
 const toolbar=el('div','shared-catalog-toolbar');
 const back=button('← К сборке',()=>{clearStatus();renderBuild(current||build);body.scrollTop=0});
 const heading=el('div','shared-catalog-heading');
 heading.append(el('h2','','Добавить моды'),el('p','shared-build-hint','Minecraft '+description(build)));
 toolbar.append(back,heading);

 const form=el('form','shared-catalog-search');
 const query=el('input');
 query.type='search';
 query.placeholder='Название мода';
 query.setAttribute('aria-label','Поиск модов');
 query.maxLength=90;
 const submit=el('button','ws-action shared-build-primary','Найти');
 submit.type='submit';
 form.append(query,submit);
 const filters=el('div','shared-catalog-sources');
 filters.setAttribute('aria-label','Источники модов');
 const results=el('div','shared-catalog-results');
 const notice=el('p','shared-catalog-notice');
 notice.setAttribute('role','status');
 const selection=el('aside','shared-catalog-selection');
 const list=el('div','shared-catalog-list');
 const more=button('Показать ещё',()=>void search(true));
 more.hidden=true;
 results.append(notice,list,more);
 const columns=el('div','shared-catalog-columns');
 columns.append(results,selection);
 page.append(toolbar,form,filters,columns);
 body.replaceChildren(page);
 body.scrollTop=0;

 let source='all', request=0, chooseRequest=0, searchTimer, pages={}, fallback=false, selected=null;
 const providerButtons=[['all','Все источники'],...Object.entries(catalogProviders)].map(([key,label])=>{
  const control=button(label,()=>{
   source=key;
   providerButtons.forEach(item=>item.setAttribute('aria-pressed',String(item.dataset.source===key)));
   schedule(true);
  });
  control.dataset.source=key;
  control.setAttribute('aria-pressed',String(source===key));
  return control;
 });
 filters.append(...providerButtons);
 selection.replaceChildren(el('p','shared-build-hint','Выбери мод, чтобы увидеть совместимые версии.'));

 function schedule(now=false) {
  clearTimeout(searchTimer);
  request++;
  chooseRequest++;
  more.hidden=true;
  list.inert=true;
  selection.inert=true;
  searchTimer=setTimeout(()=>void search(false),now?0:320);
 }

 async function loadPages(providers,term,compatible) {
  const filters={kind:'mods',sort:term?'relevance':'downloads',...(compatible?{game:build.game,loader:build.loader}:{})};
  const responses=await Promise.allSettled(providers.map(provider=>api().SearchModsFiltered(provider,term,pages[provider]?.offset||0,filters)));
  return responses.map((response,index)=>({provider:providers[index],response}));
 }

 async function search(append) {
  if(!page.isConnected||busy)return;
  const generation=++request;
  const term=query.value.trim();
  if(!append){
   fallback=false;
   pages={};
   selected=null;
   chooseRequest++;
   selection.replaceChildren(el('p','shared-build-hint','Выбери мод, чтобы увидеть совместимые версии.'));
  }
  notice.textContent='';
  const finish=showWorkshopLoading(list,'shared',{append,count:append?2:5});
  clearStatus();
  list.inert=true;
  selection.inert=true;
  more.disabled=true;
  const providers=(source==='all'?catalogSources:[source]).filter(provider=>!append||!pages[provider]||pages[provider].next);
  try{
   let responses=await loadPages(providers,term,!fallback);
   if(generation!==request||!page.isConnected)return;
   if(!append&&term&&responses.some(({response})=>response.status==='fulfilled')&&
      responses.every(({response})=>response.status!=='fulfilled'||!response.value?.items?.length)){
    const available=responses.filter(({response})=>response.status==='fulfilled').map(({provider})=>provider);
    const broader=await loadPages(available,term,false);
    if(generation!==request||!page.isConnected)return;
    if(broader.some(({response})=>response.status==='fulfilled'&&response.value?.items?.length)){
     fallback=true;
     responses=[...broader,...responses.filter(({response})=>response.status==='rejected')];
    }
   }
   const failed=[];
   for(const {provider,response} of responses){
    if(response.status==='rejected'){failed.push(provider);continue}
    const data=response.value;
    const old=pages[provider],offset=data.nextOffset??0;
    pages[provider]={items:[...(old?.items||[]),...(data.items||[])],offset,next:!!data.next&&offset>(old?.offset||0)};
   }
   const projects=mergeWorkshopResults(pages,term?'relevance':'downloads');
   const warnings=failed.length?'Недоступен источник: '+failed.map(provider=>catalogProviders[provider]).join(', '):'';
   const messages=[
    warnings,
    fallback?'Для сборки совпадений пока нет. Ниже – проекты для других версий.':
     !projects.length?(failed.length===providers.length?'Не удалось загрузить каталог.':'По этому запросу модов нет.'):'',
   ].filter(Boolean);
   notice.replaceChildren(...messages.map((message,index)=>document.createTextNode((index?' ':'')+message)));
   list.replaceChildren(...projects.map(project=>projectRow(project)));
   const next=Object.values(pages).some(entry=>entry.next);
   more.hidden=!next&&!failed.length;
   more.textContent=next?'Показать ещё':'Повторить';
  }finally{
   finish();
   if(generation===request){
    list.inert=false;
    selection.inert=false;
    more.disabled=false;
   }
  }
 }

 function projectRow(project) {
  const row=button('',()=>void choose(project),'shared-catalog-project');
  const id=project.provider+'/'+project.id;
  row.dataset.project=id;
  row.setAttribute('aria-pressed',String(selected===id));
  const icon=projectIcon(project.name,project.icon);
  const info=el('span','shared-catalog-project-info');
  info.append(el('strong','',project.name));
  if(project.author)info.append(el('small','',project.author));
  if(project.description)info.append(el('small','shared-catalog-project-summary',project.description));
  const tags=el('span','shared-catalog-tags');
  for(const item of projectSources(project))tags.append(sourceTag(item.provider));
  row.append(icon,info,tags);
  return row;
 }

 function sourceTag(provider) {
  const tag=el('span','shared-catalog-tag',catalogProviders[provider]);
  tag.dataset.source=provider;
  return tag;
 }

 async function choose(project,item=projectSources(project)[0]) {
  selected=project.provider+'/'+project.id;
  for(const row of list.children)row.setAttribute('aria-pressed',String(row.dataset.project===selected));
  const generation=++chooseRequest;
  const sources=el('div','shared-catalog-sources');
  for(const entry of projectSources(project)){
   const control=button(catalogProviders[entry.provider],()=>void choose(project,entry));
   control.dataset.source=entry.provider;
   control.setAttribute('aria-pressed',String(entry.provider===item.provider));
   sources.append(control);
  }
  const title=el('h3','',project.name);
  const summary=el('p','shared-build-hint',item.description||project.description||'');
  const versionState=el('div','shared-build-hint');
  selection.replaceChildren(title,sources,summary,versionState);
  const finish=showWorkshopLoading(versionState,'lines',{count:1});
  try{
   const found=await api().ModVersions(item.provider,item.id,build.game,build.loader);
   if(generation!==chooseRequest||!page.isConnected)return;
   const compatible=(found||[]).filter(version=>version.games?.includes(build.game)&&version.loaders?.includes(build.loader));
   const versions=orderModVersions(compatible.filter(version=>!version.unavailable));
   if(!versions.length){
    versionState.textContent=compatible.length?'Автор не разрешил автоматическое скачивание этих версий.':
     `Для Minecraft ${build.game} и ${loaders[build.loader]} подходящих версий нет.`;
    return;
   }
   const field=el('label','shared-catalog-version','Версия мода');
   const select=el('select');
   for(const version of versions)select.add(new Option(version.name,version.id));
   field.append(select);
   const add=button('Добавить в сборку',()=>void perform(()=>api().SharedBuildAddCatalog(build.id,current.draft_version,item.provider,item.id,select.value),
    {label:'Готовим загрузку…'}),'shared-build-primary');
   versionState.remove();
   selection.append(field,add);
  }catch(error){
   if(generation===chooseRequest&&page.isConnected)versionState.textContent=errorMessage(error);
  }finally{finish()}
 }

 query.addEventListener('input',()=>schedule());
 form.addEventListener('submit',event=>{event.preventDefault();schedule(true)});
 query.focus();
 void search(false);
}
