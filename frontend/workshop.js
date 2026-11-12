import {workshopCategories as categories} from './data/workshop.js';
import {versionRange,preferredTarget,orderModVersions} from './data/mod-versions.js';
import {catalogDescription,showCatalogDescription} from './data/workshop-translation.js';
import {resourceResolution} from './data/resourcepacks.js';
import {mergeWorkshopResults,projectSources,searchWorkshopSources,workshopProviders} from './data/workshop-sources.js';
const q=s=>document.querySelector(s);
const catalogKinds=['mods','resources','shaders','modpacks'];
const isZipPack=kind=>kind==='resources'||kind==='shaders';
const contentFolder=kind=>({resources:'resourcepacks',shaders:'shaderpacks'}[kind]||'mods');
const names={sintedmods:'SintedMods',modrinth:'Modrinth',curseforge:'CurseForge',fabric:'Fabric',forge:'Forge',neoforge:'NeoForge',quilt:'Quilt',vanilla:'Vanilla'};
const state={scope:'catalog',category:'mods',provider:'modrinth',query:'',offset:0,items:[],sourceResults:{},targets:[],target:'',request:0,modal:0,filesRequest:0,planRequest:0,filters:{game:'',loader:'',topic:'',sort:'downloads'},filtersOpen:false,snapshots:false};
let filterMetadata={games:[],snapshots:[],topics:[],resourceTopics:[],packTopics:[]};
const filterSets={};let lastInstallKind='mods';
let api,project,versions=[],plan=null,searchTimer,polling=false,previousBusy=false,modInstalling=false;
const list=q('#workshop-list'),dialog=q('#mod-install-dialog');
const localDetails=q('#ws-local-details'),localDelete=q('#ws-local-delete-dialog'),localDropzone=q('#ws-local-dropzone');
let localBusy=false,detailsFile=null,deleteFile=null;
const localThemes=()=>state.scope==='local'&&state.category==='themes';
const catalogThemes=()=>state.scope==='catalog'&&state.category==='themes';
const localContent=()=>state.scope==='local'&&(state.category==='mods'||isZipPack(state.category));
const localContext=()=>localContent()&&state.target?{target:state.target,kind:state.category,label:targetLabel(selectedTarget()||{name:state.target})}:null;
const sameContext=context=>localContent()&&state.target===context.target&&state.category===context.kind;
const node=(tag,className,text)=>{const el=document.createElement(tag);if(className)el.className=className;if(text!==undefined)el.textContent=text;return el};
const message=text=>{q('#workshop-results').textContent=text||''};
const error=err=>String(err?.message||err).replace(/^Error:\s*/,'');
const downloads=(value,compact=true)=>Number.isFinite(value)?new Intl.NumberFormat(document.documentElement.lang,compact?{notation:'compact',maximumFractionDigits:1}:{}).format(value):'–';
function downloadBadge(item){const badge=node('span','ws-download-count','↓ '+downloads(item.downloads));badge.title=downloads(item.downloads,false)+' скачиваний';return badge}
const size=bytes=>window.SintetLanguage.formatBytes(bytes);

const bulkSelection=new Map(),bulkRows=new WeakMap(),bulkDialog=q('#ws-bulk-delete');
let bulkScope='',bulkBusy=false,pendingBulk=null;
const bulkContext=()=>state.scope==='local'?state.category+'/'+(localContent()?state.target:''):'';
const canDisable=({item})=>localContent()?item.enabled:localThemes()&&themeLibrary().activeSlot===item.index+1;

function paintBulkSelection(){
 const context=bulkContext();
 if(bulkScope!==context){bulkSelection.clear();bulkScope=context}
 const count=bulkSelection.size;
 const toolbar=q('#ws-bulk-toolbar');
 toolbar.hidden=!count;
 q('#ws-bulk-hint').hidden=!context||!!count;
 toolbar.parentElement.toggleAttribute('data-bulk',!!count);
 q('#ws-bulk-count').textContent=window.SintetText.format('Выбрано: %d',count);
 q('#ws-bulk-disable').hidden=![...bulkSelection.values()].some(canDisable);
 for(const button of toolbar.querySelectorAll('button'))button.disabled=bulkBusy;
 for(const row of list.querySelectorAll('[data-bulk-key]'))paintBulkRow(row);
}

function paintBulkRow(row){
 const selected=bulkSelection.has(row.dataset.bulkKey);
 row.toggleAttribute('data-bulk-selected',selected);
 if(selected)row.setAttribute('aria-describedby','ws-bulk-count');else row.removeAttribute('aria-describedby');
}

function selectable(row,key,item){
 row.dataset.bulkKey=key;
 if(row.tabIndex<0)row.tabIndex=0;
 row.setAttribute('aria-keyshortcuts','Shift+Space');
 bulkRows.set(row,{key,item});
 paintBulkRow(row);
}

function toggleBulkRow(row){
 const entry=row&&bulkRows.get(row);
 if(!entry||bulkBusy||localBusy||themeBusy||state.scope!=='local')return;
 if(bulkSelection.has(entry.key))bulkSelection.delete(entry.key);else bulkSelection.set(entry.key,entry);
 paintBulkSelection();
}

list.addEventListener('contextmenu',event=>{
 const row=event.target.closest('[data-bulk-key]');
 if(!row)return;
 event.preventDefault();
 toggleBulkRow(row);
});
list.addEventListener('click',event=>{
 if(!bulkSelection.size)return;
 const row=event.target.closest('[data-bulk-key]');
 if(!row)return;
 event.preventDefault();
 event.stopPropagation();
 toggleBulkRow(row);
},true);
list.addEventListener('keydown',event=>{
 if(event.shiftKey&&event.key===' '&&bulkRows.has(event.target)){
  event.preventDefault();
  event.stopPropagation();
  toggleBulkRow(event.target);
 }
},true);
document.addEventListener('keydown',event=>{
 if(event.key==='Escape'&&!document.querySelector('dialog[open]')&&!q('#workshop').classList.contains('hidden')&&bulkSelection.size&&!bulkBusy){
  bulkSelection.clear();
  paintBulkSelection();
 }
});
q('#ws-bulk-clear').onclick=()=>{bulkSelection.clear();paintBulkSelection()};

function bulkOperation(disable){
 return {kind:state.category,target:state.target,context:bulkContext(),entries:[...bulkSelection.values()].filter(entry=>!disable||canDisable(entry)),disable};
}

q('#ws-bulk-remove').onclick=()=>{
 if(bulkBusy||!bulkSelection.size)return;
 pendingBulk=bulkOperation(false);
 const {kind,entries}=pendingBulk;
 q('#ws-bulk-delete-hint').textContent=kind==='modpacks'?'Файлы выбранных сборок, включая моды, конфиги и миры, будут удалены.':kind==='themes'?'Выбранные оформления будут удалены из лаунчера.':'Выбранные файлы будут удалены из сборки.';
 q('#ws-bulk-delete-list').replaceChildren(...entries.map(({item})=>node('li','',item.name)));
 bulkDialog.showModal();
};
q('#ws-bulk-delete-cancel').onclick=()=>bulkDialog.close();
bulkDialog.addEventListener('cancel',event=>{if(bulkBusy)event.preventDefault()});
bulkDialog.addEventListener('close',()=>{pendingBulk=null});
q('#ws-bulk-delete-confirm').onclick=()=>{if(pendingBulk)void runBulk(pendingBulk)};
q('#ws-bulk-disable').onclick=()=>void runBulk(bulkOperation(true));

async function runBulk({kind,target,context,entries,disable}){
 if(!entries.length||bulkBusy||localBusy||themeBusy)return;
 bulkBusy=true;
 paintBulkSelection();
 const dialogButtons=[...bulkDialog.querySelectorAll('button')];
 dialogButtons.forEach(button=>button.disabled=true);
 const failed=[];
 let changed=0;
 try{
  for(const {key,item} of entries){
   try{
    if(kind==='modpacks')await api.DeleteWorkshopBuild(item.id);
    else if(kind==='themes'){
     if(disable)await api.SetActiveAppearanceSlot(-1);
     else await api.ClearAppearanceSlot(item.index);
    }else if(disable)await api.ToggleLocalWorkshopFile(target,kind,item.filename,false);
    else await api.RemoveLocalWorkshopFile(target,kind,item.filename);
    changed++;
    if(bulkContext()===context)bulkSelection.delete(key);
   }catch(e){failed.push(item.name+' · '+error(e))}
  }
  if(changed&&kind==='themes')await window.SintetAppearance.refreshLibrary(true);
  if(changed&&kind==='modpacks')window.dispatchEvent(new Event('sintet:builds'));
  if(changed&&bulkContext()===context)await load();
 }catch(e){failed.push(error(e))}
 finally{
  bulkBusy=false;
  if(bulkDialog.open)bulkDialog.close();
  dialogButtons.forEach(button=>button.disabled=false);
  paintBulkSelection();
 }
 if(changed)window.toast?.(window.SintetText.format(disable?'Отключено: %d':'Удалено: %d',changed));
 if(failed.length){
  const summary=window.SintetText.format('Ошибок: %d',failed.length)+' · '+failed.slice(0,3).join(' · ');
  if(bulkContext()===context)message(summary);else window.toast?.(summary);
 }
}

const targetLabel=t=>t.name||`${t.game} · ${names[t.loader]||t.loader}`;
function providerBadge(provider){
 const badge=node('span','ws-source',names[provider]||provider);
 badge.dataset.provider=provider;
 return badge;
}
function catalogSourceBadges(item){
 const badges=node('div','ws-source-badges');
 badges.append(...projectSources(item).map(source=>providerBadge(source.provider)));
 return badges;
}
function selectedTarget(){return state.targets.find(t=>t.id===state.target)}
function selectedProfile(){try{const p=JSON.parse(localStorage.getItem('sintet.selectedProfile'));return p.id||(p.loader==='vanilla'?p.base:`${p.loader}-${p.loaderVersion}-${p.base}`)}catch{return ''}}
function image(target,title,url){target.replaceChildren(node('span','',title.slice(0,2).toUpperCase()));if(!url)return;try{if(!['https:','http:'].includes(new URL(url,location.href).protocol))return}catch{return};const img=node('img');img.alt='';img.draggable=false;img.referrerPolicy='no-referrer';img.src=url;img.onerror=()=>img.remove();target.append(img)}
function options(select,items,desired){select.replaceChildren(...items.map(item=>{const option=node('option','',item.label);option.value=item.value;return option}));if(items.some(i=>i.value===desired))select.value=desired;select.disabled=!items.length;if(!items.length){const empty=node('option','','Нет подходящих вариантов');empty.value='';select.append(empty)}window.SintetUI?.select.refresh(select)}
function remember(){if(catalogKinds.includes(state.category))filterSets[state.category]={...state.filters};try{localStorage.setItem('sintet.workshop',JSON.stringify({scope:state.scope,category:state.category,provider:state.provider,target:state.target,filters:state.filters,filterSets,filtersOpen:state.filtersOpen,snapshots:state.snapshots}))}catch{}}
function updateContentFilters(){
 const resource=state.category==='resources',pack=state.category==='modpacks',shader=state.category==='shaders';options(q('#ws-filter-topic'),[{value:'',label:'Все тематики'},...(shader?[]:resource?filterMetadata.resourceTopics:pack?filterMetadata.packTopics:filterMetadata.topics).map(topic=>({value:topic.id,label:topic.name}))],state.filters.topic);state.filters.topic=q('#ws-filter-topic').value;
 const quick=shader?[]:resource?['pvp','vanilla','realistic','simplistic']:pack?['adventure','optimization','quests','technology']:['pvp','survival','optimization','adventure'];const topics=shader?[]:resource?filterMetadata.resourceTopics:pack?filterMetadata.packTopics:filterMetadata.topics;q('#ws-quick-topics').replaceChildren(...quick.map(id=>{const b=node('button','',topics.find(t=>t.id===id)?.name||id);b.type='button';b.dataset.topic=id;return b}));
 for(const key of ['game','loader','resolution','sort']){q('#ws-filter-'+key).value=state.filters[key]||'';window.SintetUI?.select.refresh(q('#ws-filter-'+key))}
}
function switchCategory(id){if(catalogKinds.includes(state.category))filterSets[state.category]={...state.filters};state.category=id;if(catalogKinds.includes(id)){state.filters={game:'',loader:'',topic:'',resolution:'',sort:'downloads',...(filterSets[id]||{})};fillFilterGames();updateContentFilters()};state.query='';q('#workshop-search').value='';void load()}
function configure(){
 const resource=state.category==='resources',shader=state.category==='shaders';const canFilter=state.scope==='catalog'&&catalogKinds.includes(state.category);q('#ws-filter-toggle').hidden=!canFilter;q('#ws-filters').hidden=!canFilter||!state.filtersOpen;q('#ws-filter-toggle').setAttribute('aria-expanded',String(state.filtersOpen));q('#ws-filter-loader').closest('label').hidden=resource||shader;q('#ws-filter-topic').closest('label').hidden=shader;q('#ws-filter-resolution').closest('label').hidden=!resource;list.classList.toggle('ws-resource-grid',(resource||shader||state.category==='modpacks')&&state.scope==='catalog');updateFilterSummary();
 const ownOption=[...q('#ws-provider').options].find(option=>option.value==='sintedmods');ownOption.disabled=!workshopProviders(state.category).includes('sintedmods');if(ownOption.disabled&&state.provider==='sintedmods'){state.provider='all';q('#ws-provider').value=state.provider;window.SintetUI?.select.refresh(q('#ws-provider'))}
 q('.ws-toolbar').dataset.provider=state.provider;
 for(const button of document.querySelectorAll('[data-scope]'))button.setAttribute('aria-selected',String(button.dataset.scope===state.scope));
 for(const button of q('#workshop-categories').children)button.setAttribute('aria-selected',String(button.dataset.category===state.category));
 const titles=state.scope==='local'
  ?{mods:'Мои моды',resources:'Мои ресурс-паки',shaders:'Мои шейдеры',themes:'Мои оформления',modpacks:'Мои сборки'}
  :{mods:'Моды',resources:'Ресурс-паки',shaders:'Шейдеры',themes:'Оформления',modpacks:'Сборки'};
 q('#workshop-catalog-title').textContent=titles[state.category];
 q('#ws-provider').closest('.s-select')?.classList.toggle('hidden',state.scope!=='catalog'||!catalogKinds.includes(state.category));
 q('#ws-open-folder').hidden=state.scope!=='local'||!catalogKinds.includes(state.category);
 paintBulkSelection();
 q('#ws-create-build').hidden=state.scope!=='local'||state.category!=='modpacks';
 q('#ws-theme-toolbar').hidden=!localThemes();
 q('.ws-sidebar-bottom').hidden=state.scope==='local'&&['modpacks','themes'].includes(state.category);
 q('#workshop-search').placeholder=categories.find(c=>c.id===state.category).search;
 q('#workshop-clear').hidden=!state.query;updateLocalControls();remember();
}
for(const category of categories){const button=node('button','ws-category');button.type='button';button.dataset.category=category.id;button.setAttribute('role','tab');const glyph=node('span');glyph.innerHTML='<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">'+category.icon+'</svg>';button.append(glyph,node('span','',category.name));button.onclick=()=>switchCategory(category.id);q('#workshop-categories').append(button)}
document.querySelectorAll('[data-scope]').forEach(button=>button.addEventListener('click',()=>{state.scope=button.dataset.scope;void load()}));
function empty(title,description=''){list.replaceChildren();const box=node('div','ws-empty');box.append(node('h3','',title));if(description)box.append(node('p','',description));list.append(box)}

const themeDialog=q('#ws-theme-dialog'),themeDelete=q('#ws-theme-delete'),themeName=q('#ws-theme-name');
const themePortalDialog=q('#ws-theme-portal-dialog'),themePortalStatus=q('#ws-theme-portal-status');
let editingTheme=null,themeBusy=false;
let themePortalEnabled=false;
const themeLibrary=()=>window.SintetAppearance.getLibrary();
const currentTheme=()=>editingTheme===null?null:themeLibrary().slots[editingTheme];

function themePreview(target,slot){
 target.replaceChildren();
 target.style.setProperty('--theme-preview-bg',slot?.settings?.canvasColor||'#262626');
 target.style.setProperty('--theme-preview-accent',slot?.settings?.accent||'#c7c7c7');
 const glyph=node('span','ws-theme-glyph');
 glyph.innerHTML='<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 4a8 8 0 1 0 0 16h1a2 2 0 0 0 1.5-3.3 1 1 0 0 1 .8-1.7H17a3 3 0 0 0 3-3 8 8 0 0 0-8-8Z"/><path d="M8 11h.01m3-3h.01m4 1h.01m2 3h.01"/></svg>';
 target.append(glyph);
 if(slot)window.SintetAppearance.renderThemeIcon(target,slot);
}

const catalogInstalls=new Set();
const catalogViews=new Set();
const themeScreens={play:'Главная',settings:'Настройки',profile:'Профиль'};
const gallery=node('dialog','ws-dialog ws-theme-gallery');
gallery.setAttribute('aria-labelledby','ws-theme-gallery-title');
document.body.append(gallery);
gallery.addEventListener('click',event=>{if(event.target===gallery)gallery.close()});

function catalogPreview(id,screen='play',small=false){
 const frame=node('div','ws-catalog-frame');
 const empty=node('span','ws-catalog-frame-empty',small?'':'Предпросмотр пока недоступен');
 const shot=node('img');
 shot.alt=themeScreens[screen];
 shot.decoding='async';
 if(small)shot.loading='lazy';
 shot.onerror=()=>{shot.hidden=true;empty.hidden=false};
 shot.src='/catalog-theme-preview/'+id+'/'+screen;
 empty.hidden=true;
 frame.append(shot,empty);
 return frame;
}
function catalogActivity(theme){
 const activity=node('div','ws-catalog-activity');
 activity.dataset.themeActivity=theme.id;
 const values=[
  ['views','Просмотры','<path d="M2 12c2.8-4 6.1-6 10-6s7.2 2 10 6c-2.8 4-6.1 6-10 6s-7.2-2-10-6Z"/><circle cx="12" cy="12" r="2.5"/>'],
  ['downloads','Скачивания','<path d="M12 3v12m-5-5 5 5 5-5M4 17v3h16v-3"/>'],
 ];
 const format=new Intl.NumberFormat(document.documentElement.lang);
 for(const [key,label,path] of values){
  const item=node('span');
  item.title=label;
  item.innerHTML='<svg viewBox="0 0 24 24" aria-hidden="true">'+path+'</svg>';
  const value=node('span','',format.format(theme[key]||0));
  value.dataset.count=key;
  const accessible=node('span','ws-catalog-sr',label);
  item.append(accessible,value);
  activity.append(item);
 }
 return activity;
}
function updateCatalogActivity(theme,stats){
 if(!stats)return;
 const format=new Intl.NumberFormat(document.documentElement.lang);
 for(const key of ['views','downloads']){
  if(!Number.isSafeInteger(stats[key])||stats[key]<0)continue;
  theme[key]=Math.max(theme[key]||0,stats[key]);
  for(const element of document.querySelectorAll('[data-theme-activity="'+theme.id+'"] [data-count="'+key+'"]'))element.textContent=format.format(theme[key]);
 }
}
function catalogAuthor(theme,withAvatar=false){
 const author=node('div','ws-catalog-author');
 if(withAvatar){
  const avatar=node('span','ws-theme-author-avatar');
  window.SintetAccount.renderAvatar(avatar,theme.author,theme.authorAvatar);
  author.append(avatar);
 }
 const name=node('strong','',theme.author);
 name.setAttribute('data-no-translate','');
 author.append(node('span','','Автор'),name);
 return author;
}
function catalogThemeIcon(target,theme){
 themePreview(target);
 target.style.setProperty('--theme-preview-accent',theme.accent||'#c7c7c7');
 const icon=node('img','appearance-custom-icon');
 icon.alt='';
 icon.loading='lazy';
 icon.onerror=()=>icon.remove();
 icon.src='/catalog-theme-preview/'+theme.id+'/icon';
 target.append(icon);
}

function themeIsInstalled(theme){
 return themeLibrary().slots.some(slot=>slot.package===theme.id);
}

async function installCatalogTheme(theme){
 if(catalogInstalls.has(theme.id))return;
 catalogInstalls.add(theme.id);
 try{
  updateCatalogActivity(theme,await api.InstallCatalogTheme(theme.id));
  await window.SintetAppearance.refreshLibrary();
 }finally{
  catalogInstalls.delete(theme.id);
  if(catalogThemes()&&state.items.length)renderCatalogThemes();
 }
}

function galleryButton(label,className='',text=label){
 const button=node('button',className,text);
 button.type='button';
 button.setAttribute('aria-label',label);
 return button;
}

function openCatalogGallery(theme){
 delete gallery.dataset.expanded;
 const heading=node('h1','',theme.name);
 heading.id='ws-theme-gallery-title';
 heading.setAttribute('data-no-translate','');
 const caption=node('div');
 caption.append(heading,catalogAuthor(theme,true));
 const icon=node('div','mod-avatar ws-theme-preview');
 catalogThemeIcon(icon,theme);
 const close=galleryButton('Закрыть','account-close','×');
 close.onclick=()=>gallery.close();
 const header=node('header','ws-mod-header');
 header.append(icon,caption);
 const columns=node('div','ws-mod-columns');
 const main=node('section','ws-mod-info ws-theme-detail-main');
 const stage=node('div','ws-theme-gallery-stage');
 const tabs=node('div','ws-theme-gallery-tabs');
 const screens=Object.keys(themeScreens);
 let current=0;
 const previous=galleryButton('Предыдущий снимок','ws-theme-gallery-arrow','‹');
 const next=galleryButton('Следующий снимок','ws-theme-gallery-arrow','›');
 const expand=galleryButton('Развернуть снимок','ws-theme-gallery-expand','⛶');
 const image=node('div','ws-theme-gallery-image');
 const buttons=screens.map((screen,index)=>{
  const button=galleryButton(themeScreens[screen]);
  button.onclick=()=>select(index);
  return button;
 });
 function select(index){
  current=(index+screens.length)%screens.length;
  image.replaceChildren(catalogPreview(theme.id,screens[current]));
  buttons.forEach((button,i)=>button.setAttribute('aria-pressed',String(i===current)));
 }
 previous.onclick=()=>select(current-1);
 next.onclick=()=>select(current+1);
 expand.onclick=()=>{
  const expanded=gallery.toggleAttribute('data-expanded');
  expand.setAttribute('aria-label',expanded?'Свернуть снимок':'Развернуть снимок');
  expand.title=expanded?'Свернуть снимок':'Развернуть снимок';
 };
 expand.title='Развернуть снимок';
 stage.append(image,previous,next,expand);
 tabs.append(...buttons);
 select(0);
 const description=node('p','',theme.description);
 description.setAttribute('data-no-translate','');
 main.append(stage,tabs,node('h2','ws-info-heading ws-theme-about','Об оформлении'),description);
 const aside=node('aside','ws-mod-controls ws-theme-detail-meta');
 const details=node('section','ws-mod-info');
 const properties=node('dl');
 properties.append(node('dt','','Версия Sintet'),node('dd','',theme.launcherVersion||'Не указана'));
 if(theme.formatVersion)properties.append(node('dt','','Формат оформления'),node('dd','','.sldcfg v'+theme.formatVersion));
 details.append(node('h2','ws-info-heading','Информация'),properties,catalogActivity(theme));
 aside.append(details);
 columns.append(main,aside);
 const status=node('div','ws-mod-status');
 status.setAttribute('role','status');
 const footer=node('footer');
 const back=galleryButton('Закрыть');
 back.onclick=()=>gallery.close();
 const add=galleryButton(themeIsInstalled(theme)?'Добавлено':'Добавить в «Моё»','primary');
 add.disabled=themeIsInstalled(theme)||catalogInstalls.has(theme.id);
 add.onclick=async()=>{
  add.disabled=true;
  add.textContent='Добавляем…';
  status.textContent='';
  try{await installCatalogTheme(theme)}
  catch(e){status.textContent=error(e)}
  finally{add.disabled=themeIsInstalled(theme);add.textContent=add.disabled?'Добавлено':'Добавить в «Моё»'}
 };
 footer.append(back,add);
 gallery.onkeydown=event=>{
  if(event.key==='ArrowLeft'||event.key==='ArrowRight'){
   event.preventDefault();
   select(current+(event.key==='ArrowLeft'?-1:1));
  }
 };
 gallery.replaceChildren(close,header,columns,status,footer);
 if(!gallery.open)gallery.showModal();
 if(!catalogViews.has(theme.id)){
  catalogViews.add(theme.id);
  void api.ViewCatalogTheme(theme.id).then(stats=>updateCatalogActivity(theme,stats)).catch(()=>catalogViews.delete(theme.id));
 }
}
function renderCatalogThemes(library=themeLibrary()){
 const installed=new Set(library.slots.map(slot=>slot.package));
 q('#workshop-count').textContent=state.items.length||'';
 if(!state.items.length){empty(state.query.trim()?'Ничего не найдено':'В каталоге пока нет оформлений');return}
 list.replaceChildren(...state.items.map(theme=>{
  const row=node('article','mod-card ws-catalog-theme');
  row.setAttribute('role','listitem');
  const preview=node('button','ws-catalog-preview-trigger');
  preview.type='button';
  preview.setAttribute('aria-label','Открыть предпросмотр');
  preview.append(catalogPreview(theme.id,'play',true));
  preview.onclick=()=>openCatalogGallery(theme);
  const info=node('div','mod-info');
  const heading=node('h2','',theme.name);
  const description=node('p','',theme.description);
  heading.setAttribute('data-no-translate','');
  description.setAttribute('data-no-translate','');
  const headingRow=node('div','ws-catalog-theme-title');
  const themeIcon=node('span','ws-theme-preview');
  catalogThemeIcon(themeIcon,theme);
  headingRow.append(themeIcon,heading);
  info.append(headingRow,catalogAuthor(theme),description);
  const meta=node('div','ws-mod-meta ws-catalog-theme-meta');
  const source=node('strong','ws-source','Sintet');
  source.dataset.provider='sintedmods';
  meta.append(source);
  const version=theme.launcherVersion?'Sintet '+theme.launcherVersion:theme.formatVersion?'.sldcfg v'+theme.formatVersion:'';
  if(version)meta.append(node('span','ws-theme-badge',version));
  meta.append(catalogActivity(theme));
  const saved=installed.has(theme.id), installing=catalogInstalls.has(theme.id);
  const action=node('button','ws-action ws-catalog-theme-action',saved?'Добавлено':installing?'Добавляем…':'Добавить в «Моё»');
  action.type='button';
  action.disabled=saved||installing;
  action.onclick=async()=>{
   if(catalogInstalls.has(theme.id))return;
   action.disabled=true;
   action.textContent='Добавляем…';
   message('');
   try{await installCatalogTheme(theme)}
   catch(e){if(catalogThemes())message(error(e))}
  };
  openCard(row,theme,()=>openCatalogGallery(theme),window.SintetText.translate('Открыть предпросмотр')+' · '+theme.name);
  row.append(preview,info,meta,action);
  return row;
 }));
}

function renderThemes(library=themeLibrary()){
 const query=state.query.trim().toLocaleLowerCase();
 const entries=library.slots.map((slot,index)=>({slot,index})).filter(({slot})=>slot.name);
 const matching=entries.filter(({slot})=>slot.name.toLocaleLowerCase().includes(query));
 q('#workshop-count').textContent=matching.length?(query?window.SintetText.format('%d из %d',matching.length,entries.length):String(entries.length)):'';
 list.replaceChildren(...matching.map(({slot,index})=>{
  const active=library.activeSlot===index+1;
  const row=node('article','mod-card ws-theme-card');
  const preview=node('div','ws-theme-preview');
  preview.setAttribute('aria-hidden','true');
  themePreview(preview,slot);
  const info=node('div','mod-info');
  info.append(node('h2','',slot.name),node('span',active?'ws-theme-badge active':'ws-theme-badge',active?'Активно':'Неактивно'));
  const controls=node('div','ws-theme-card-controls');
  const edit=node('button','ws-action ws-theme-secondary','Управлять');
  edit.type='button';
  edit.onclick=()=>openTheme(index);
  const toggle=node('button','ws-action ws-theme-toggle',active?'Отключить':'Включить');
  toggle.type='button';
  toggle.dataset.active=String(active);
  toggle.onclick=()=>void themeOperation(async()=>{
   await api.SetActiveAppearanceSlot(active?-1:index);
   await window.SintetAppearance.refreshLibrary(true);
   return active?'Оформление отключено':'Применено оформление «'+slot.name+'»';
  });
  edit.disabled=toggle.disabled=themeBusy;
  controls.append(edit,toggle);
  row.append(preview,info,controls);
  selectable(row,String(index),{index,name:slot.name});
  return row;
 }));
 if(!matching.length){
  empty(query?'Ничего не найдено':'Нет сохранённых оформлений',
   query?'Попробуй другое название.':'Сохрани текущий вид лаунчера или импортируй подписанный .sldcfg.');
 }
}

function updateThemeDialog(){
 const slot=currentTheme();
 themePreview(q('#ws-theme-icon'),slot);
 q('#ws-theme-reset-icon').disabled=themeBusy||!slot?.icon;
 q('#ws-theme-export').hidden=!slot;
 q('#ws-theme-publish').hidden=!slot||!themePortalEnabled;
 q('#ws-theme-edit').textContent=themeLibrary().activeSlot===editingTheme+1?'В редактор':'Включить и редактировать';
}

function openTheme(index=null){
 if(themeBusy)return;
 editingTheme=index;
 const slot=currentTheme();
 q('#ws-theme-title').textContent=slot?'Управление оформлением':'Сохранить текущий вид';
 themeName.value=slot?.name||'';
 q('#ws-theme-tools').hidden=!slot;
 q('#ws-theme-remove').hidden=!slot;
 q('#ws-theme-status').textContent='';
 updateThemeDialog();
 themeDialog.showModal();
 themeName.focus();
}

function setThemeBusy(value){
 themeBusy=value;
 q('#ws-theme-toolbar').querySelectorAll('button').forEach(button=>{button.disabled=value});
 themeDialog.querySelectorAll('button,input').forEach(control=>{control.disabled=value});
 themeDelete.querySelectorAll('button').forEach(button=>{button.disabled=value});
 if(themeDialog.open)updateThemeDialog();
 if(localThemes()&&!q('#workshop').classList.contains('hidden'))renderThemes();
}

async function themeOperation(action){
 if(themeBusy)return;
 setThemeBusy(true);
 message('');
 q('#ws-theme-status').textContent='';
 try{
  const result=await action();
  if(result)window.toast?.(result);
 }catch(e){
  if(themeDialog.open)q('#ws-theme-status').textContent=error(e);
  else message(error(e));
  window.toast?.(error(e));
 }finally{setThemeBusy(false)}
}

q('#ws-theme-add').onclick=()=>openTheme();
q('#ws-theme-import').onclick=()=>void themeOperation(async()=>{
 if(!await api.ImportAppearanceSlot())return;
 await window.SintetAppearance.refreshLibrary();
 return 'Оформление добавлено в библиотеку';
});
q('#ws-theme-export').onclick=()=>void themeOperation(async()=>{
 const slot=currentTheme();
 if(!slot)return;
 if(themeName.value.trim()!==slot.name)throw Error('Сначала сохрани новое название.');
 q('#ws-theme-status').textContent='Подготавливаем файл оформления…';
 const exported=await api.ExportAppearanceSlot(editingTheme);
 await window.SintetAppearance.refreshLibrary();
 q('#ws-theme-status').textContent=exported?'Файл готов. Можно отправить другу.':'';
 return exported?'Оформление экспортировано':'';
});
q('#ws-theme-publish').onclick=()=>void themeOperation(async()=>{
 q('#ws-theme-status').textContent='Готовим три снимка оформления…';
 await api.PublishPortalTheme(editingTheme);
 const local=await api.ThemePortalIsLocal();
 const notice=local?'Оформление и три снимка сохранены в локальном кабинете.':'Оформление и три снимка сохранены на sintet.xyz.';
 q('#ws-theme-status').textContent=notice;
 return notice;
});
q('#ws-theme-portal-close').onclick=()=>themePortalDialog.close();
q('#ws-theme-portal').onclick=async()=>{
 const rows=q('#ws-theme-portal-list');
 rows.replaceChildren();
 themePortalStatus.textContent='Загружаем оформления…';
 themePortalDialog.showModal();
 try{
  const themes=await api.PortalThemes()||[];
  if(!themePortalDialog.open)return;
  themePortalStatus.textContent=themes.length?'':'Сохрани оформление в кабинет кнопкой «В кабинет» или загрузи .sldcfg на сайте.';
  rows.replaceChildren(...themes.map(theme=>{
   const row=node('div','ws-theme-portal-row');
   const dot=node('i','');
   if(/^#[a-f0-9]{6}$/i.test(theme.accent))dot.style.backgroundColor=theme.accent;
   const button=node('button','ws-action','Добавить');
   button.type='button';
   const update=()=>{button.disabled=themeLibrary().slots.some(slot=>slot.package===theme.id);button.textContent=button.disabled?'Добавлено':'Добавить'};
   update();
   button.onclick=async()=>{
    button.disabled=true;
    themePortalStatus.textContent='';
    try{
     await api.ImportPortalTheme(theme.id);
     await window.SintetAppearance.refreshLibrary();
     window.toast?.('Оформление добавлено в «Моё»');
    }catch(e){themePortalStatus.textContent=error(e)}
    finally{update()}
   };
   row.append(dot,node('strong','',theme.name),button);
   return row;
  }));
 }catch(e){themePortalStatus.textContent=error(e)}
};
q('#ws-theme-close').onclick=()=>themeDialog.close();
themeDialog.addEventListener('cancel',event=>{if(themeBusy)event.preventDefault()});
themeDelete.addEventListener('cancel',event=>{if(themeBusy)event.preventDefault()});
q('#ws-theme-form').onsubmit=event=>{
 event.preventDefault();
 const name=themeName.value.trim();
 if(!name){themeName.focus();return}
 void themeOperation(async()=>{
  if(editingTheme===null)await api.AddCurrentAppearance(name);
  else await api.RenameAppearanceSlot(editingTheme,name);
  await window.SintetAppearance.refreshLibrary();
  themeDialog.close();
  return 'Оформление сохранено';
 });
};
q('#ws-theme-change-icon').onclick=()=>void themeOperation(async()=>{
 if(!await api.ChooseAppearanceIcon(editingTheme))return;
 await window.SintetAppearance.refreshLibrary();
 return 'Иконка обновлена';
});
q('#ws-theme-reset-icon').onclick=()=>void themeOperation(async()=>{
 await api.ClearAppearanceIcon(editingTheme);
 await window.SintetAppearance.refreshLibrary();
 return 'Иконка удалена';
});
q('#ws-theme-edit').onclick=()=>void themeOperation(async()=>{
 if(themeLibrary().activeSlot!==editingTheme+1){
  await api.SetActiveAppearanceSlot(editingTheme);
  await window.SintetAppearance.refreshLibrary(true);
 }
 themeDialog.close();
 q('#settings-nav').click();
 q('#settings-tab-appearance').click();
});
q('#ws-theme-remove').onclick=()=>{
 const slot=currentTheme();
 if(!slot)return;
 q('#ws-theme-delete-summary').textContent='«'+slot.name+'» исчезнет из мастерской и настроек.';
 themeDelete.showModal();
};
q('#ws-theme-delete-cancel').onclick=()=>themeDelete.close();
q('#ws-theme-delete-confirm').onclick=()=>void themeOperation(async()=>{
 try{
  const wasActive=themeLibrary().activeSlot===editingTheme+1;
  await api.ClearAppearanceSlot(editingTheme);
  await window.SintetAppearance.refreshLibrary(wasActive);
  themeDialog.close();
  return 'Оформление удалено';
 }finally{themeDelete.close()}
});
window.addEventListener('sintet:appearance-library',event=>{
 if(q('#workshop').classList.contains('hidden'))return;
 if(localThemes())renderThemes(event.detail);
 if(catalogThemes()&&state.items.length)renderCatalogThemes(event.detail);
});
function openCard(row,item,open,label='Открыть '+item.name){
 row.classList.add('ws-mod-link');
 row.tabIndex=0;
 row.setAttribute('aria-label',label);
 row.onclick=event=>{if(!event.target.closest('button,a,input,select'))open()};
 row.onkeydown=event=>{
  if(event.target===row&&['Enter',' '].includes(event.key)){event.preventDefault();open()}
 };
}
function updateLocalControls(){
 const context=localContext();
 localDropzone.hidden=!context;
 q('#ws-local-add').disabled=localBusy;
 for(const button of list.querySelectorAll('[data-local-change]'))button.disabled=localBusy;
 if(!context)return;
 const pack=isZipPack(context.kind),shader=context.kind==='shaders';
 q('#ws-local-drop-title').textContent=shader?'Перетащи шейдеры ZIP':pack?'Перетащи ресурс-паки ZIP':'Перетащи моды JAR';
 q('#ws-local-drop-hint').textContent=window.SintetText.format('Добавим в сборку «%s»',context.label);
 q('#ws-local-add').textContent=shader?'Добавить шейдеры':pack?'Добавить ресурс-паки':'Добавить моды';
}
async function changeLocalFiles(context,change){
 if(localBusy)return false;
 localBusy=true;updateLocalControls();
 try{
  const result=await change();
  if(result&&sameContext(context))await load();
  return result;
 }finally{localBusy=false;updateLocalControls()}
}
async function importLocalFiles(context,paths){
 try{
  const count=await changeLocalFiles(context,()=>paths?api.AddLocalWorkshopFiles(context.target,context.kind,paths):api.PickLocalWorkshopFiles(context.target,context.kind));
  if(count)window.toast?.('Добавлено файлов: '+count);
 }catch(e){if(sameContext(context))message(error(e));else window.toast?.(error(e))}
}
function localDescription(item){
 return (item.description||'').replace(/§x(?:§[0-9a-f]){6}|§[0-9a-fk-or]/gi,'').trim();
}
q('#ws-local-add').onclick=()=>{const context=localContext();if(api&&context)void importLocalFiles(context)};
function showLocalDetails(context,item){
 const pack=isZipPack(context.kind),shader=context.kind==='shaders';
 detailsFile={context,item};
 image(q('#ws-local-icon'),item.name,item.icon);
 q('#ws-local-title').textContent=item.name;
 q('#ws-local-subtitle').textContent=(shader?'Шейдер':pack?'Ресурс-пак':'Мод')+' · '+context.label;
 const description=localDescription(item);
 q('#ws-local-about').hidden=!description;
 q('#ws-local-description').textContent=description;
 const stateBadge=q('#ws-local-state');
 stateBadge.textContent=pack?(item.enabled?'Доступен в Minecraft':'Скрыт в Minecraft'):(item.enabled?'Включён':'Выключен');
 stateBadge.dataset.enabled=String(item.enabled);
 const values=[
  ['Файл',item.filename],['Размер',size(item.size)],['Версия',item.version],['Загрузчик',item.loader],
  ['ID мода',item.modID],['Авторы',item.authors],['Формат ресурс-пака',item.packFormat],
  ['Источник',names[item.provider]||item.provider],
 ];
 q('#ws-local-info').replaceChildren(...values.filter(([,value])=>value).map(([label,value])=>{
  const row=node('div','ws-local-property');
  row.append(node('dt','',label),node('dd','',value));
  return row;
 }));
 q('.ws-local-body').scrollTop=0;
 localDetails.showModal();
}
function confirmLocalDelete(context,item){
 if(localBusy)return;
 deleteFile={context,item};
 q('#ws-local-delete-file').textContent=item.filename+' · '+context.label;
 q('#ws-local-delete-error').textContent='';
 localDelete.showModal();
}
q('#ws-local-close').onclick=()=>localDetails.close();
q('#ws-local-detail-delete').onclick=()=>{if(detailsFile)confirmLocalDelete(detailsFile.context,detailsFile.item)};
q('#ws-local-delete-cancel').onclick=()=>localDelete.close();
localDelete.addEventListener('cancel',event=>{if(localBusy)event.preventDefault()});
q('#ws-local-delete-confirm').onclick=async()=>{
 if(!deleteFile||localBusy)return;
 const {context,item}=deleteFile;
 const button=q('#ws-local-delete-confirm'),cancel=q('#ws-local-delete-cancel');
 button.disabled=true;cancel.disabled=true;
 try{
  const changed=await changeLocalFiles(context,async()=>{await api.RemoveLocalWorkshopFile(context.target,context.kind,item.filename);return true});
  if(changed){
   localDelete.close();
   if(localDetails.open&&detailsFile?.item.filename===item.filename)localDetails.close();
   window.toast?.('Файл удалён из сборки');
  }
 }catch(e){q('#ws-local-delete-error').textContent=error(e)}
 finally{button.disabled=false;cancel.disabled=false}
};
function localCard(item){
 const context=localContext(),pack=isZipPack(context.kind);
 const row=node('article','mod-card ws-local-card');
 row.setAttribute('role','listitem');
 openCard(row,item,()=>showLocalDetails(context,item));
 const avatar=node('div','mod-avatar');
 image(avatar,item.name,item.icon);
 const body=node('div','mod-info');
 body.append(node('h2','',item.name),node('p','',localDescription(item)||item.filename));
 const meta=node('div','ws-mod-meta');
 const source=node('strong','ws-source',names[item.provider]||item.provider||'Локальный файл');
 source.dataset.provider=item.provider||'';
 meta.append(source,node('span','',[item.version,item.loader,item.packFormat&&'Формат '+item.packFormat].filter(Boolean).join(' · ')||size(item.size)));
 const actions=node('div','ws-local-actions');
 const toggle=node('button','ws-action',pack?(item.enabled?'Доступен':'Скрыт'):(item.enabled?'Включён':'Выключен'));
 toggle.type='button';
 toggle.dataset.localChange='';
 toggle.setAttribute('role','switch');
 toggle.setAttribute('aria-checked',String(item.enabled));
 toggle.setAttribute('aria-label',(pack?(item.enabled?'Скрыть ':'Вернуть '):(item.enabled?'Отключить ':'Включить '))+item.name);
 if(pack)toggle.title='Управляет доступностью пака в меню игры';
 toggle.onclick=async()=>{
  try{await changeLocalFiles(context,async()=>{await api.ToggleLocalWorkshopFile(context.target,context.kind,item.filename,!item.enabled);return true})}
  catch(e){if(sameContext(context))message(error(e))}
 };
 const remove=node('button','ws-local-remove');
 remove.type='button';
 remove.dataset.localChange='';
 remove.title='Удалить из сборки';
 remove.setAttribute('aria-label','Удалить '+item.name);
 remove.innerHTML='<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M10 11v6m4-6v6M6 7l1 13h10l1-13M9 7V4h6v3"/></svg>';
 remove.onclick=()=>confirmLocalDelete(context,item);
 actions.append(toggle,remove);
 row.append(avatar,body,meta,actions);
 selectable(row,item.filename,item);
 return row;
}
function card(item){
 if((isZipPack(item.kind)||item.kind==='modpacks'))return resourceCard(item);
 const row=node('article','mod-card');row.setAttribute('role','listitem');const avatar=node('div','mod-avatar');image(avatar,item.name,item.icon);
 const body=node('div','mod-info');body.append(node('h2','',item.name),catalogDescription(item.description||'',item.filename||''));
 const meta=node('div','ws-mod-meta');
 const games=item.games||[];meta.append(catalogSourceBadges(item),node('span','',versionRange(games)),node('span','ws-loaders',(item.loaders||[]).map(v=>names[v]||v).join(' · ')),downloadBadge(item));meta.title=games.join(', ');
 const action=node('button','ws-action','Установить');action.type='button';action.onclick=()=>openMod(item);
 openCard(row,item,()=>openMod(item));
 row.append(avatar,body,meta,action);return row;
}
function resourceCard(item){
 const row=node('article','ws-resource-card ws-mod-link');row.setAttribute('role','listitem');row.tabIndex=0;row.setAttribute('aria-label','Открыть '+item.name);
 const cover=node('div','ws-resource-cover');image(cover,item.name,item.preview||item.icon);const img=cover.querySelector('img');if(img)img.loading='lazy';if(!item.preview||item.preview===item.icon)cover.classList.add('ws-cover-logo');
 cover.append(catalogSourceBadges(item));const resolution=item.kind==='modpacks'?(item.loaders||[]).map(l=>names[l]||l).join(' · '):resourceResolution(item.tags);if(resolution)cover.append(node('span','ws-pack-resolution',resolution));
 const body=node('div','ws-resource-body');body.append(node('h2','',item.name),node('span','ws-pack-author',item.author||names[item.provider]),catalogDescription(item.description||''));body.append(downloadBadge(item));const footer=node('div','ws-resource-footer');footer.append(node('span','',versionRange(item.games)));const install=node('button','ws-action','Установить');install.type='button';install.onclick=()=>openMod(item);footer.append(install);body.append(footer);row.append(cover,body);row.onclick=event=>{if(!event.target.closest('button'))void openMod(item)};row.onkeydown=event=>{if(event.target===row&&['Enter',' '].includes(event.key)){event.preventDefault();void openMod(item)}};return row;
}
function render(){
 const local=localContent(),query=state.query.toLocaleLowerCase();
 const items=local?state.items.filter(item=>[item.name,item.description,item.filename,item.authors,item.modID].join(' ').toLocaleLowerCase().includes(query)):state.items;
 list.replaceChildren(...items.map(local?localCard:card));
 if(!items.length)empty(state.query?'Ничего не найдено':'Здесь пока пусто',local?'Перетащи файлы в область выше или нажми «Добавить».':'Попробуй другой запрос.');
 q('#workshop-count').textContent=items.length?(`${items.length}${local||state.provider==='all'?'':' из '+state.total}`):'';
 updateLocalControls();
}
async function refreshTargets(){const targets=await api.WorkshopTargets();state.targets=targets||[];if(!targets.some(t=>t.id===state.target))state.target=targets.find(t=>t.id===selectedProfile())?.id||targets[0]?.id||'';options(q('#ws-target'),targets.map(t=>({value:t.id,label:targetLabel(t)})),state.target);paintBulkSelection();remember()}
async function load(more=false){
 if(!api)return;const request=++state.request;configure();q('#ws-more').hidden=true;q('#ws-more').textContent='Показать ещё';if(!more){state.offset=0;state.items=[];state.sourceResults={};list.replaceChildren()};message('Загрузка…');
 try{
  if(state.scope==='catalog'){
   if(window.SintetNetwork.offline){message('');q('#workshop-count').textContent='';empty(window.SintetNetwork.message,'Каталог загрузится, когда соединение восстановится.');return}
   if(catalogThemes()){
    await window.SintetAppearance.ready;
    const result=await api.CatalogThemes(more?state.offset:0,state.query.slice(0,120));
    if(request!==state.request)return;
    const themes=result.themes||[];
    state.items=more?state.items.concat(themes.filter(theme=>!state.items.some(item=>item.id===theme.id))):themes;
    state.offset=result.nextOffset;
    renderCatalogThemes();
    q('#ws-more').hidden=!result.more;
    message('');
    return;
   }
   const filters={...state.filters,kind:state.category,loader:isZipPack(state.category)?'':state.filters.loader};
   if(state.provider==='all'){
    const result=await searchWorkshopSources((...args)=>api.SearchModsFiltered(...args),state.category,state.query,filters,state.sourceResults);
    if(request!==state.request)return;
    state.sourceResults=result.sources;
    state.items=mergeWorkshopResults(result.sources,filters.sort);
    render();
    q('#ws-more').hidden=!result.more;
    q('#ws-more').textContent=result.failed.length&&!Object.values(result.sources).some(page=>page.next)?'Повторить':'Показать ещё';
    message(result.failed.length?window.SintetText.format('Недоступные источники: %s',result.failed.map(provider=>names[provider]).join(', ')):'');
    return;
   }
   const result=await api.SearchModsFiltered(state.provider,state.query,more?state.offset:0,filters);
   if(request!==state.request)return;
   state.items=more?state.items.concat(result.items||[]):result.items||[];
   state.total=result.total;
   state.offset=result.nextOffset??state.items.length;
   render();
   q('#ws-more').hidden=!result.next;
   message('');
  }else{
   if(localThemes()){
    await window.SintetAppearance.ready;
    themePortalEnabled=await api.ThemePortalAvailable();
    if(request!==state.request)return;
    q('#ws-theme-portal').hidden=!themePortalEnabled;
    renderThemes();
    message('');
    return;
   }
   await refreshTargets();if(request!==state.request)return;updateLocalControls();
   if(state.category==='modpacks'){await refreshBuildList(request);message('');return}
   if(!state.target){empty('Сначала установи Minecraft','После установки здесь появятся файлы выбранной сборки.');message('');return}
   const records=await api.LocalWorkshop(state.target,state.category);if(request!==state.request)return;state.items=records||[];render();message('');
  }
 }catch(e){if(request===state.request){message(error(e));if(more)q('#ws-more').hidden=false;else if(!catalogThemes())empty('Не удалось загрузить содержимое','Можно сменить источник или повторить поиск.')}}
}
function buildCard(build){
 const row=node('article','ws-build-card');row.setAttribute('role','listitem');
 const icon=node('div','ws-build-icon');window.SintetBuildIcon.render(icon,build.icon||'grass');
 const body=node('div','ws-build-card-info');body.append(node('h3','',build.name),node('p','',`${build.game} · ${names[build.loader]||build.loader}${build.loaderVersion?' · '+build.loaderVersion:''}`));
 const badge=node('span','ws-build-state',build.installing?'Устанавливается':build.installed?'Готова':'Не установлена');badge.dataset.state=build.installing?'busy':build.installed?'ready':'pending';
 const actions=node('div','ws-build-card-actions');
 if(build.installing){const progress=node('span','ws-build-progress','Подготовка файлов…');actions.append(progress)}
 else if(build.installed){const manage=node('button','ws-action','Настроить');manage.type='button';manage.onclick=()=>openBuild(build);actions.append(manage)}
 else{const retry=node('button','ws-action','Установить');retry.type='button';retry.onclick=async()=>{retry.disabled=true;try{await api.InstallWorkshopBuild(build.id);window.toast?.('Установка сборки началась');setTimeout(()=>load(),500)}catch(e){message(error(e));retry.disabled=false}};actions.append(retry)}
 row.append(icon,body,badge,actions);if(!build.installing)selectable(row,build.id,build);if(build.installed){row.tabIndex=0;row.setAttribute('aria-label',window.SintetText.format('Управлять сборкой %s',build.name));row.onclick=event=>{if(!event.target.closest('button'))void openBuild(build)};row.onkeydown=event=>{if(event.target===row&&['Enter',' '].includes(event.key)){event.preventDefault();void openBuild(build)}}}return row;
}
async function refreshBuildList(request=state.request){const builds=await api.WorkshopBuilds();if(request!==state.request)return;if(activeBuild&&manageBuildDialog.open){const latest=builds.find(build=>build.id===activeBuild.id);if(latest){activeBuild={...activeBuild,...latest};const busy=!!latest.installing;q('#ws-build-save').disabled=busy;q('#ws-build-duplicate').disabled=busy||!latest.installed;q('#ws-build-delete').disabled=busy;q('#ws-build-apply-variant').disabled=busy;for(const control of q('#ws-build-mods').querySelectorAll('.ws-build-toggle'))control.disabled=busy}}const filtered=(builds||[]).filter(build=>`${build.name} ${build.game} ${names[build.loader]||build.loader} ${build.loaderVersion}`.toLocaleLowerCase().includes(state.query.toLocaleLowerCase()));list.replaceChildren(...filtered.map(build=>buildCard(build)));if(!filtered.length)empty(state.query?'Сборки не найдены':'Создай первую сборку','Отдельная папка для каждой сборки, а моды можно включать и выключать в один клик.');q('#workshop-count').textContent=builds?.length?String(builds.length):''}
q('#workshop-search').oninput=()=>{
 state.query=q('#workshop-search').value.trim();q('#workshop-clear').hidden=!state.query;clearTimeout(searchTimer);
 if(bulkSelection.size&&!bulkBusy){bulkSelection.clear();paintBulkSelection()}
 if(localContent()){
  if(state.target)render();
 }else{state.request++;searchTimer=setTimeout(()=>load(),350)}
};
q('#workshop-clear').onclick=()=>{q('#workshop-search').value='';state.query='';void load()};q('#ws-provider').onchange=()=>{state.provider=q('#ws-provider').value;void load()};q('#ws-target').onchange=()=>{state.target=q('#ws-target').value;remember();if(state.scope==='local')void load()};
q('#ws-more').onclick=()=>load(true);q('#ws-open-folder').onclick=()=>api.OpenWorkshopFolder(state.target,contentFolder(state.category)).catch(e=>message(error(e)));
const createBuildDialog=q('#ws-create-build-dialog'),manageBuildDialog=q('#ws-build-manage-dialog'),deleteBuildDialog=q('#ws-build-delete-dialog');let activeBuild=null,buildSystemMemory=8192;
function updateBuildLoaderVisibility(){const vanilla=q('#ws-build-loader').value==='vanilla';q('#ws-build-loader-version-label').hidden=vanilla;q('#ws-build-loader-version-label').disabled=vanilla}
async function refreshBuildLoaderVersions(){const game=q('#ws-build-game').value,loader=q('#ws-build-loader').value;updateBuildLoaderVisibility();if(loader==='vanilla'){options(q('#ws-build-loader-version'),[]);q('#ws-build-create-submit').disabled=false;q('#ws-build-create-status').textContent='';return}q('#ws-build-create-submit').disabled=true;q('#ws-build-create-status').textContent='Загружаю версии загрузчика…';try{const builds=await api.LoaderVersions(game,loader);options(q('#ws-build-loader-version'),(builds||[]).map(value=>({value,label:value})),builds?.[0]);q('#ws-build-create-submit').disabled=!builds?.length;q('#ws-build-create-status').textContent=builds?.length?'':'Для этой версии Minecraft нет доступных версий загрузчика'}catch(e){options(q('#ws-build-loader-version'),[]);q('#ws-build-create-submit').disabled=true;q('#ws-build-create-status').textContent=error(e)}}
async function showCreateBuild(){q('#ws-build-create-status').textContent='Загружаю версии Minecraft…';q('#ws-build-create-submit').disabled=true;q('#ws-build-name').value='';q('#ws-build-loader').value='fabric';createBuildDialog.showModal();try{const result=await api.ListVersions(),releases=(result.versions||[]).filter(version=>version.type==='release').map(version=>({value:version.id,label:version.id}));options(q('#ws-build-game'),releases,result.latest);if(!releases.length)throw Error('Список версий Minecraft пуст');await refreshBuildLoaderVersions()}catch(e){q('#ws-build-create-status').textContent=error(e)}}
q('#ws-create-build').onclick=()=>void showCreateBuild();for(const button of document.querySelectorAll('[data-close-build-create]'))button.onclick=()=>createBuildDialog.close();q('#ws-build-game').onchange=()=>void refreshBuildLoaderVersions();q('#ws-build-loader').onchange=()=>void refreshBuildLoaderVersions();
q('#ws-create-build-form').onsubmit=async event=>{event.preventDefault();const submit=q('#ws-build-create-submit');submit.disabled=true;q('#ws-build-create-status').textContent='Создаю папки и устанавливаю Minecraft…';try{const build=await api.CreateWorkshopBuild(q('#ws-build-name').value,q('#ws-build-game').value,q('#ws-build-loader').value,q('#ws-build-loader-version').value);createBuildDialog.close();window.toast?.(window.SintetText.format('Сборка «%s» создаётся',build.name));state.scope='local';state.category='modpacks';configure();await load();}catch(e){q('#ws-build-create-status').textContent=error(e)}finally{submit.disabled=false}};
for(const button of document.querySelectorAll('[data-close-build-manage]'))button.onclick=()=>manageBuildDialog.close();for(const button of document.querySelectorAll('[data-close-build-delete]'))button.onclick=()=>deleteBuildDialog.close();
const buildTabList=manageBuildDialog.querySelector('.ws-build-tabs');
const buildTabs=[...buildTabList.querySelectorAll('[data-build-tab]')];
function selectBuildTab(tab){
 for(const button of buildTabs){
  const selected=button.dataset.buildTab===tab;
  button.setAttribute('aria-selected',String(selected));
  button.tabIndex=selected?0:-1;
 }
 for(const panel of manageBuildDialog.querySelectorAll('[data-build-panel]'))panel.hidden=panel.dataset.buildPanel!==tab;
}
buildTabList.onclick=event=>{
 const button=event.target.closest('[data-build-tab]');
 if(button)selectBuildTab(button.dataset.buildTab);
};
buildTabList.onkeydown=event=>{
 const index=buildTabs.indexOf(event.target);
 if(index<0)return;
 const next={
  ArrowUp:(index+buildTabs.length-1)%buildTabs.length,
  ArrowDown:(index+1)%buildTabs.length,
  Home:0,
  End:buildTabs.length-1,
 }[event.key];
 if(next===undefined)return;
 event.preventDefault();
 selectBuildTab(buildTabs[next].dataset.buildTab);
 buildTabs[next].focus();
};
function renderBuildMods(mods){
 const area=q('#ws-build-mods'),query=q('#ws-build-mod-search').value.toLocaleLowerCase().trim();
 const filtered=(mods||[]).filter(mod=>`${mod.name} ${mod.filename} ${mod.version||''}`.toLocaleLowerCase().includes(query));
 area.replaceChildren();
 q('#ws-build-mod-count').textContent=window.SintetText.format('Модов: %s',filtered.length===mods.length?String(filtered.length):window.SintetText.format('%d из %d',filtered.length,mods.length));
 q('#ws-build-mod-tab-count').textContent=mods.length?` ${mods.length}`:'';
 if(!filtered.length){area.append(node('div','ws-build-no-mods',mods.length?'Ничего не найдено':'Пока нет модов. Добавь JAR-файлы или установи моды из каталога.'));return}
 for(const mod of filtered){
  const row=node('article','ws-build-mod');
  const avatar=node('div','ws-build-mod-avatar');
  if(mod.icon){const img=node('img');img.alt='';img.draggable=false;img.src=mod.icon;img.onerror=()=>img.remove();avatar.append(img)}
  else avatar.textContent=(mod.name||'?').slice(0,1).toUpperCase();
  const info=node('div','ws-build-mod-info');
  info.append(node('strong','',mod.name),node('span','',`${mod.version||mod.filename}${mod.loader?' · '+mod.loader:''}`));
  const toggle=node('button','ws-build-toggle',mod.enabled?'Включён':'Выключен');
  toggle.type='button';toggle.setAttribute('role','switch');toggle.setAttribute('aria-checked',String(mod.enabled));toggle.disabled=!!activeBuild?.installing;
  toggle.onclick=async()=>{toggle.disabled=true;q('#ws-build-manage-status').textContent='Сохраняю состояние мода…';try{await api.ToggleMod(activeBuild.id,mod.filename,!mod.enabled);await refreshBuildMods()}catch(e){q('#ws-build-manage-status').textContent=error(e);toggle.disabled=false}};
  row.append(avatar,info,toggle);area.append(row)
 }
}
async function refreshBuildMods(){if(!activeBuild)return;q('#ws-build-manage-status').textContent='Загружаю моды…';try{const mods=await api.LocalWorkshop(activeBuild.id,'mods');if(!manageBuildDialog.open||!activeBuild)return;renderBuildMods(mods);q('#ws-build-manage-status').textContent=''}catch(e){q('#ws-build-manage-status').textContent=error(e)}}
q('#ws-build-mod-search').oninput=()=>void refreshBuildMods();
async function loadManageVersions(){try{const result=await api.ListVersions(),releases=(result.versions||[]).filter(v=>v.type==='release').map(v=>({value:v.id,label:v.id}));options(q('#ws-manage-game'),releases,activeBuild.game);await loadManageLoaderVersions()}catch(e){q('#ws-build-manage-status').textContent=error(e)}}
async function loadManageLoaderVersions(){if(!activeBuild)return;const loader=q('#ws-manage-loader').value,versionLabel=q('#ws-manage-loader-version-label');versionLabel.hidden=loader==='vanilla';if(loader==='vanilla'){options(q('#ws-manage-loader-version'),[]);q('#ws-build-apply-variant').disabled=false;return}try{const versions=await api.LoaderVersions(q('#ws-manage-game').value,loader);options(q('#ws-manage-loader-version'),versions.map(v=>({value:v,label:v})),activeBuild.loader===loader?activeBuild.loaderVersion:versions?.[0]);q('#ws-build-apply-variant').disabled=!versions?.length}catch(e){options(q('#ws-manage-loader-version'),[]);q('#ws-build-apply-variant').disabled=true;q('#ws-build-manage-status').textContent=error(e)}}
q('#ws-manage-game').onchange=()=>void loadManageLoaderVersions();q('#ws-manage-loader').onchange=()=>void loadManageLoaderVersions();q('#ws-build-edit-variant').onclick=async()=>{const editor=q('#ws-build-version-editor');editor.hidden=!editor.hidden;q('#ws-build-edit-variant').setAttribute('aria-expanded',String(!editor.hidden));if(!editor.hidden){q('#ws-manage-loader').value=activeBuild.loader;window.SintetUI?.select.refresh(q('#ws-manage-loader'));await loadManageVersions()}};
q('#ws-build-apply-variant').onclick=async()=>{const button=q('#ws-build-apply-variant');button.disabled=true;q('#ws-build-manage-status').textContent='Переустанавливаю основу сборки…';try{await api.UpdateWorkshopBuildVersion(activeBuild.id,q('#ws-manage-game').value,q('#ws-manage-loader').value,q('#ws-manage-loader-version').value);activeBuild.game=q('#ws-manage-game').value;activeBuild.loader=q('#ws-manage-loader').value;activeBuild.loaderVersion=q('#ws-manage-loader-version').value;q('#ws-build-manage-meta').textContent=`${activeBuild.game} · ${names[activeBuild.loader]}${activeBuild.loaderVersion?' · '+activeBuild.loaderVersion:''}`;window.toast?.('Переустановка сборки началась');await load()}catch(e){q('#ws-build-manage-status').textContent=error(e)}finally{button.disabled=false}};
function updateBuildPerformance(){
 const useGlobal=q('#ws-build-use-global').checked;
 for(const section of manageBuildDialog.querySelectorAll('[data-build-custom-performance]'))section.hidden=useGlobal;
 q('#ws-build-global-note').hidden=!useGlobal;
}
q('#ws-build-use-global').onchange=updateBuildPerformance;
q('#ws-build-ram').oninput=()=>{q('#ws-build-ram-output').textContent=window.SintetLanguage.formatSize(Number(q('#ws-build-ram').value)/1024,'gigabyte',Number(q('#ws-build-ram').value)%1024?1:0);if(Number(q('#ws-build-ram-min').value)>Number(q('#ws-build-ram').value)){q('#ws-build-ram-min').value='512';window.SintetUI?.select.refresh(q('#ws-build-ram-min'))}};
q('#ws-build-jvm-preset').onchange=()=>{const presets={g1:'-XX:+UseG1GC',aikar:'-XX:+UseG1GC -XX:+ParallelRefProcEnabled -XX:MaxGCPauseMillis=200 -XX:+UnlockExperimentalVMOptions -XX:+DisableExplicitGC',zgc:'-XX:+UseZGC'};if(presets[q('#ws-build-jvm-preset').value])q('#ws-build-jvm-args').value=presets[q('#ws-build-jvm-preset').value]};
q('#ws-build-java-browse').onclick=async()=>{try{const path=await api.ChooseJava();if(path)q('#ws-build-java-path').value=path}catch(e){q('#ws-build-manage-status').textContent=error(e)}};
q('#ws-build-java-select').onchange=()=>{q('#ws-build-java-path').value=q('#ws-build-java-select').value};
async function loadJavaOptions(selected=''){try{const runtimes=await api.WorkshopJavaRuntimes();options(q('#ws-build-java-select'),[{value:'',label:'Автоматический выбор'},...(runtimes||[]).map(runtime=>({value:runtime.path,label:runtime.label}))],selected);if(selected)q('#ws-build-java-select').value=selected;window.SintetUI?.select.refresh(q('#ws-build-java-select'))}catch{options(q('#ws-build-java-select'),[{value:'',label:'Автоматический выбор'}])}}
function setBuildIcon(value){
 const select=q('#ws-build-icon'),custom=window.SintetBuildIcon.isCustom(value);
 select.querySelector('[data-custom]')?.remove();
 if(custom){const option=new Option('Своя иконка',value);option.dataset.custom='';select.add(option)}
 select.value=value||'grass';
 if(!select.value)select.value='grass';
 window.SintetUI?.select.refresh(select);
 window.SintetBuildIcon.render(q('#ws-build-manage-icon'),select.value);
}
q('#ws-build-icon').onchange=()=>window.SintetBuildIcon.render(q('#ws-build-manage-icon'),q('#ws-build-icon').value);
q('#ws-build-icon-upload').onclick=async()=>{
 const button=q('#ws-build-icon-upload'),id=activeBuild?.id;
 button.disabled=true;
 try{
  const value=await api.ChooseWorkshopBuildIcon();
  if(value&&manageBuildDialog.open&&activeBuild?.id===id)setBuildIcon(value);
 }catch(e){q('#ws-build-manage-status').textContent=error(e)}
 finally{button.disabled=false}
};
async function openBuild(build){activeBuild=build;state.target=build.id;options(q('#ws-target'),state.targets.map(t=>({value:t.id,label:targetLabel(t)})),state.target);remember();setBuildIcon(build.icon);q('#ws-build-manage-title').textContent=build.name;q('#ws-build-manage-meta').textContent=`${build.game} · ${names[build.loader]||build.loader}${build.loaderVersion?' · '+build.loaderVersion:''}`;q('#ws-build-name-input').value=build.name;q('#ws-build-use-global').checked=build.useGlobalSettings!==false;updateBuildPerformance();q('#ws-build-ram').max=Math.max(2048,Math.floor((buildSystemMemory-2048)/512)*512);q('#ws-build-ram-max-label').textContent=window.SintetLanguage.formatSize(Number(q('#ws-build-ram').max)/1024,'gigabyte',Number(q('#ws-build-ram').max)%1024?1:0);q('#ws-build-ram').value=Math.min(build.memoryMB||4096,Number(q('#ws-build-ram').max));q('#ws-build-ram-min').value=String(build.minMemoryMB||512);window.SintetUI?.select.refresh(q('#ws-build-ram-min'));q('#ws-build-ram').dispatchEvent(new Event('input'));q('#ws-build-java-path').value=build.javaPath||'';void loadJavaOptions(build.javaPath||'');q('#ws-build-jvm-args').value=build.jvmArgs||'';q('#ws-build-jvm-preset').value='';q('#ws-build-fullscreen').checked=!!build.fullscreen;q('#ws-build-width').value=build.width||1280;q('#ws-build-height').value=build.height||720;q('#ws-build-version-editor').hidden=true;q('#ws-build-edit-variant').setAttribute('aria-expanded','false');q('#ws-build-manage-status').textContent='';q('#ws-build-save').disabled=!!build.installing;q('#ws-build-duplicate').disabled=!!build.installing||!build.installed;q('#ws-build-delete').disabled=!!build.installing;selectBuildTab('general');q('#ws-build-folder').onclick=()=>api.OpenWorkshopBuildFolder(build.id).catch(e=>q('#ws-build-manage-status').textContent=error(e));q('#ws-build-mods-folder').onclick=()=>api.OpenWorkshopFolder(build.id,'mods').catch(e=>q('#ws-build-manage-status').textContent=error(e));q('#ws-build-drop-add').onclick=()=>addMods();q('#ws-build-duplicate').onclick=async()=>{try{const clone=await api.DuplicateWorkshopBuild(build.id);window.toast?.(window.SintetText.format('Копия «%s» создаётся',clone.name));manageBuildDialog.close();await load()}catch(e){q('#ws-build-manage-status').textContent=error(e)}};q('#ws-build-delete').onclick=()=>deleteBuildDialog.showModal();manageBuildDialog.showModal();await refreshBuildMods()}
async function addMods(paths){if(!activeBuild)return;q('#ws-build-manage-status').textContent='Добавляю JAR-файлы…';try{if(paths?.length)await api.AddWorkshopBuildFiles(activeBuild.id,paths);else await api.AddWorkshopBuildMods(activeBuild.id);await refreshBuildMods();q('#ws-build-manage-status').textContent='Моды добавлены'}catch(e){q('#ws-build-manage-status').textContent=error(e)}}
q('#ws-build-delete-confirm').onclick=async()=>{if(!activeBuild)return;const button=q('#ws-build-delete-confirm');button.disabled=true;try{await api.DeleteWorkshopBuild(activeBuild.id);deleteBuildDialog.close();manageBuildDialog.close();activeBuild=null;window.dispatchEvent(new Event('sintet:builds'));window.toast?.('Сборка удалена');await refreshTargets();await load()}catch(e){deleteBuildDialog.close();q('#ws-build-manage-status').textContent=error(e)}finally{button.disabled=false}};
q('#ws-build-save').onclick=async()=>{if(!activeBuild)return;const button=q('#ws-build-save');button.disabled=true;q('#ws-build-manage-status').textContent='Сохраняю настройки…';try{await api.SaveWorkshopBuildSettings(activeBuild.id,{name:q('#ws-build-name-input').value,icon:q('#ws-build-icon').value,useGlobalSettings:q('#ws-build-use-global').checked,minMemoryMB:Number(q('#ws-build-ram-min').value),memoryMB:Number(q('#ws-build-ram').value),javaPath:q('#ws-build-java-path').value.trim(),jvmArgs:q('#ws-build-jvm-args').value,fullscreen:q('#ws-build-fullscreen').checked,width:Number(q('#ws-build-width').value),height:Number(q('#ws-build-height').value)});activeBuild.name=q('#ws-build-name-input').value;activeBuild.icon=q('#ws-build-icon').value;q('#ws-build-manage-title').textContent=activeBuild.name;window.dispatchEvent(new Event('sintet:builds'));q('#ws-build-manage-status').textContent='Настройки сохранены';window.toast?.('Настройки сборки сохранены');await load()}catch(e){q('#ws-build-manage-status').textContent=error(e)}finally{button.disabled=false}};
let fileDropBound=false;
function isDropInside(zone,x,y){
 if(!zone.getClientRects().length)return false;
 const rect=zone.getBoundingClientRect();
 return x>=rect.left&&x<=rect.right&&y>=rect.top&&y<=rect.bottom;
}
function bindFileDrop(){
 if(fileDropBound||!window.runtime?.OnFileDrop)return;
 window.runtime.OnFileDrop((x,y,paths)=>{
  if(!paths?.length)return;
  if(manageBuildDialog.open&&activeBuild){
   if(isDropInside(q('#ws-build-dropzone'),x,y))void addMods(paths);
   return;
  }
  const context=localContext();
  if(context&&!q('#workshop').classList.contains('hidden')&&!document.querySelector('dialog[open]')&&isDropInside(localDropzone,x,y))void importLocalFiles(context,paths);
 },true);
 fileDropBound=true;
}
async function loadBuildMachineSettings(){try{const settings=await api.GetSettings();buildSystemMemory=settings.systemMemoryMB||8192}catch{buildSystemMemory=8192}}
function updateFilterSummary(){
 const count=Number(!!state.filters.game)+Number(!!state.filters.loader)+Number(!!state.filters.topic)+Number(!!state.filters.resolution)+Number(state.filters.sort!=='downloads');q('#ws-filter-count').hidden=!count;q('#ws-filter-count').textContent=String(count);
 for(const button of q('#ws-quick-topics').children)button.setAttribute('aria-pressed',String(button.dataset.topic===state.filters.topic));
 const thematic=state.category==='resources'?({medieval:'medieval',modern:'modern',animated:'animated',...(state.provider==='curseforge'?{pvp:'pvp',simplistic:'simple',gui:'gui',audio:'sound',models:'models'}:{})})[state.filters.topic]:{pvp:'PvP',survival:'survival',horror:'horror',skyblock:'skyblock'}[state.filters.topic];q('#ws-topic-hint').hidden=!thematic;q('#ws-topic-hint').textContent=thematic?window.SintetText.format('Тематический поиск по названию и описанию: %s',thematic):'';
}
function fillFilterGames(){const games=[...filterMetadata.games,...(state.snapshots?filterMetadata.snapshots:[])];options(q('#ws-filter-game'),[{value:'',label:'Любая версия'},...games.map(game=>({value:game,label:game}))],state.filters.game);state.filters.game=q('#ws-filter-game').value}
async function initFilters(){
 filterMetadata=await api.WorkshopFilterOptions();if(filterMetadata.snapshots.includes(state.filters.game))state.snapshots=true;fillFilterGames();options(q('#ws-filter-topic'),[{value:'',label:'Все тематики'},...(state.category==='resources'?filterMetadata.resourceTopics:state.category==='modpacks'?filterMetadata.packTopics:filterMetadata.topics).map(topic=>({value:topic.id,label:topic.name}))],state.filters.topic);state.filters.topic=q('#ws-filter-topic').value;for(const key of ['loader','sort']){q('#ws-filter-'+key).value=state.filters[key];state.filters[key]=q('#ws-filter-'+key).value;window.SintetUI?.select.refresh(q('#ws-filter-'+key))};q('#ws-filter-snapshots').checked=state.snapshots;updateContentFilters();updateFilterSummary();
}
q('#ws-filter-toggle').onclick=()=>{state.filtersOpen=!state.filtersOpen;configure();if(state.filtersOpen)q('.ws-main').scrollTo({top:0,behavior:'smooth'})};
for(const key of ['game','loader','resolution','topic','sort'])q('#ws-filter-'+key).onchange=()=>{state.filters[key]=q('#ws-filter-'+key).value;remember();void load()};
q('#ws-quick-topics').onclick=event=>{const button=event.target.closest('button[data-topic]');if(!button)return;state.filters.topic=state.filters.topic===button.dataset.topic?'':button.dataset.topic;q('#ws-filter-topic').value=state.filters.topic;window.SintetUI?.select.refresh(q('#ws-filter-topic'));remember();void load()};
q('#ws-filter-snapshots').onchange=()=>{const old=state.filters.game;state.snapshots=q('#ws-filter-snapshots').checked;fillFilterGames();remember();if(old!==state.filters.game)void load()};
q('#ws-filter-reset').onclick=()=>{state.filters={game:'',loader:'',resolution:'',topic:'',sort:'downloads'};for(const key of ['game','loader','resolution','topic','sort']){q('#ws-filter-'+key).value=state.filters[key];window.SintetUI?.select.refresh(q('#ws-filter-'+key))};remember();void load()};
q('#workshop-nav').addEventListener('click',()=>{if(api)void load()});
function showOfflineInstall(){
 q('#ws-mod-install').disabled=true;
 q('#ws-mod-status').textContent=window.SintetNetwork.message;
}
function closeMod(){if(plan?.kind==='modpacks')void api.DiscardCatalogPack(plan.id).catch(()=>{});state.modal++;state.filesRequest++;state.planRequest++;dialog.close();plan=null}
function renderModInfo(item){
 dialog.classList.toggle('ws-dialog-resource',(isZipPack(item.kind)||item.kind==='modpacks'));dialog.classList.toggle('ws-dialog-pack',item.kind==='modpacks');q('#ws-mod-downloads').textContent=downloads(item.downloads,false);q('#ws-mod-target').closest('label').hidden=false;q('#ws-install-destination').hidden=false;q('#ws-requirements').hidden=false;q('#ws-pack-name-row').hidden=item.kind!=='modpacks';
 const shader=item.kind==='shaders',resource=isZipPack(item.kind),pack=item.kind==='modpacks';q('#ws-info-heading').textContent=pack?'О сборке':shader?'О шейдере':resource?'О ресурс-паке':'О моде';q('#ws-content-type-label').textContent=resource?'Тип':'Загрузчики';q('#ws-content-hint').textContent=pack?'Minecraft, загрузчик и содержимое сборки устанавливаются автоматически.':shader?'Выбери шейдер в меню Iris, Oculus или OptiFine. Для работы нужен один из этих модов.':resource?'После установки включи пак в настройках Minecraft.':'Файлы проверяются по контрольной сумме перед установкой.';
 q('#ws-mod-loader').closest('label').hidden=resource;q('#ws-mod-version').closest('label').firstChild.textContent=pack?'Версия сборки':resource?'Версия пака':'Версия мода';q('#ws-mod-version').setAttribute('aria-label',pack?'Версия сборки':shader?'Версия шейдера':resource?'Версия ресурс-пака':'Версия мода');q('#ws-requirements-title').textContent=pack?'Состав сборки':resource?'Связанные паки и требования':'Зависимости';q('#ws-deps-option').lastChild.textContent=resource?'Установить связанные паки':'Установить недостающие зависимости';
 const preview=item.preview||item.icon||'';let safePreview=false;try{const url=new URL(preview);safePreview=url.protocol==='https:'||(url.protocol==='http:'&&['127.0.0.1','localhost','[::1]'].includes(url.hostname));safePreview=safePreview&&!url.username&&!url.password}catch{}
 const showPreview=safePreview&&(resource||pack||preview!==item.icon);const previewBox=q('#ws-resource-preview'),cover=q('#ws-pack-cover');previewBox.hidden=!showPreview;previewBox.classList.toggle('ws-cover-logo',preview===item.icon);cover.alt=pack?'Баннер сборки':resource?'Баннер ресурс-пака':'Баннер мода';cover.onerror=()=>{previewBox.hidden=true};if(showPreview)cover.src=preview;else cover.removeAttribute('src');
 q('#ws-mod-source').replaceChildren();const source=node('span','ws-source',names[item.provider]||item.provider);source.dataset.provider=item.provider;q('#ws-mod-source').append(source);
 q('#ws-mod-games').textContent=versionRange(item.games||[]);
 q('#ws-mod-loaders').replaceChildren(...(resource?(shader?['Шейдер']:['Ресурс-пак',resourceResolution(item.tags)].filter(Boolean)):(item.loaders||[]).map(loader=>names[loader]||loader)).map(label=>node('span','ws-loader-chip',label)));
 q('#ws-supported-count').textContent=String(item.games?.length||0);
 q('#ws-supported-list').replaceChildren(...(item.games||[]).slice().reverse().map(game=>node('span','ws-game-chip',game)));
}
q('#ws-mod-close').onclick=closeMod;q('#ws-mod-cancel').onclick=closeMod;dialog.addEventListener('cancel',event=>{event.preventDefault();closeMod()});
function openMod(item){
 const sources=projectSources(item);
 const container=q('#ws-mod-sources');
 container.hidden=sources.length<2;
 container.replaceChildren(...sources.map(source=>{
  const button=node('button','ws-source',names[source.provider]||source.provider);
  button.type='button';
  button.dataset.provider=source.provider;
  button.onclick=()=>{if(project?.provider!==source.provider)void selectModSource(source)};
  return button;
 }));
 void selectModSource(sources[0]);
}
async function selectModSource(item){
 if(modInstalling)return;
 const switching=dialog.open;
 const game=switching?q('#ws-mod-game').value:'',loader=switching?q('#ws-mod-loader').value:'';
 if(plan?.kind==='modpacks')void api.DiscardCatalogPack(plan.id).catch(()=>{});
 const request=++state.modal;
 state.filesRequest++;
 state.planRequest++;
 project=item;
 versions=[];
 plan=null;
 for(const button of q('#ws-mod-sources').querySelectorAll('button')){
  button.setAttribute('aria-pressed',String(button.dataset.provider===item.provider));
 }
 if(!switching)q('#ws-pack-name').value=(item.name||'Новая сборка').slice(0,40);
 q('#ws-mod-install').disabled=true;
 q('#ws-mod-install').textContent=item.kind==='modpacks'?'Установить сборку':'Установить';
 q('#ws-with-deps').checked=true;
 q('#ws-deps-option').hidden=true;
 q('#ws-dependencies').textContent='Выбери версию';
 q('#ws-mod-title').textContent=item.name;
 q('#ws-mod-author').textContent=item.author||names[item.provider];
 showCatalogDescription(q('#ws-mod-description'),item.description||'');
 renderModInfo(item);
 q('#ws-supported').open=false;
 image(q('#ws-mod-icon'),item.name,item.icon);
 q('#ws-mod-status').textContent='Загрузка информации…';
 q('#ws-mod-size').textContent='–';
 q('#ws-install-path').textContent='Выбери сборку';
 for(const id of ['game','loader','version','target'])options(q('#ws-mod-'+id),[]);
 if(!switching)dialog.showModal();
 if(window.SintetNetwork.offline){showOfflineInstall();return}
 try{
  const [details]=await Promise.all([api.ModDetails(item.provider,item.id),refreshTargets()]);
  if(request!==state.modal)return;
  project={...item,...details,id:item.id,provider:item.provider,
   games:[...new Set([...(details.games||[]),...(item.games||[])])],
   loaders:[...new Set([...(details.loaders||[]),...(item.loaders||[])])],
  };
  q('#ws-mod-author').textContent=project.author||item.author||names[item.provider];
  showCatalogDescription(q('#ws-mod-description'),project.description||'');
  renderModInfo(project);
  const current=preferredTarget(state.targets,selectedTarget(),project);
  const selectedGame=[game,state.filters.game,current?.game].find(value=>project.games.includes(value));
  const selectedLoader=[loader,state.filters.loader,current?.loader].find(value=>project.loaders.includes(value));
  options(q('#ws-mod-game'),project.games.slice().reverse().map(v=>({value:v,label:v})),selectedGame);
  options(q('#ws-mod-loader'),project.loaders.map(v=>({value:v,label:names[v]||v})),selectedLoader);
  await loadModVersions(request);
 }catch(e){if(request===state.modal)q('#ws-mod-status').textContent=error(e)}
}
async function loadModVersions(modal=state.modal){
 if(window.SintetNetwork.offline){showOfflineInstall();return}
 const request=++state.filesRequest;state.planRequest++;plan=null;q('#ws-mod-install').disabled=true;q('#ws-mod-status').textContent='Поиск совместимых файлов…';options(q('#ws-mod-version'),[]);options(q('#ws-mod-target'),[]);q('#ws-dependencies').textContent='Проверка…';
 try{const shader=project.kind==='shaders',resource=isZipPack(project.kind),pack=project.kind==='modpacks',game=q('#ws-mod-game').value,loader=resource?'':q('#ws-mod-loader').value;if(!game||!resource&&!pack&&!loader)throw Error('Нет поддерживаемых версий или загрузчиков');const result=await (shader?api.ShaderVersions(project.provider,project.id,game):resource?api.ResourceVersions(project.provider,project.id,game):api.ModVersions(project.provider,project.id,game,loader));if(modal!==state.modal||request!==state.filesRequest)return;versions=orderModVersions((result||[]).filter(v=>v.games?.includes(game)&&(resource||pack&&!loader||v.loaders?.includes(loader))));options(q('#ws-mod-version'),versions.map(v=>({value:v.id,label:v.name})));if(pack){if(!versions.length)throw Error('Для этой комбинации пока нет файлов');await preparePlan(modal);return};const eligible=state.targets.filter(t=>t.game===game&&(resource||t.loader===loader));options(q('#ws-mod-target'),eligible.map(t=>({value:t.id,label:targetLabel(t)})),state.target);if(!versions.length)throw Error('Для этой комбинации пока нет файлов');if(!eligible.length)throw Error(resource?window.SintetText.format('Сначала установи Minecraft %s на главном экране',game):window.SintetText.format('Сначала установи Minecraft %s · %s на главном экране',game,names[loader]));await preparePlan(modal)}catch(e){if(modal===state.modal&&request===state.filesRequest){q('#ws-mod-status').textContent=error(e);q('#ws-dependencies').textContent='–'}}
}
async function preparePlan(modal=state.modal){
 if(window.SintetNetwork.offline){showOfflineInstall();return}
 if(project?.kind==='modpacks'){
  const request=++state.planRequest;if(plan?.kind==='modpacks')void api.DiscardCatalogPack(plan.id).catch(()=>{});plan=null;q('#ws-mod-install').disabled=true;q('#ws-mod-install').textContent='Установить сборку';q('#ws-mod-status').textContent='Читаем манифест сборки…';
  try{const version=q('#ws-mod-version').value;if(!version)throw Error('Выбери версию сборки');const result=await api.PrepareCatalogPack(project.provider,project.id,version);if(request!==state.planRequest||modal!==state.modal){void api.DiscardCatalogPack(result.id).catch(()=>{});return}const targets=await api.CatalogPackTargets(result.id);if(request!==state.planRequest||modal!==state.modal){void api.DiscardCatalogPack(result.id).catch(()=>{});return}plan={...result,kind:'modpacks'};options(q('#ws-mod-target'),[{value:'__new__',label:'＋ Создать новую сборку'},...(targets||[]).map(target=>({value:target.id,label:target.name}))],'__new__');q('#ws-mod-size').textContent=size(plan.bytes);q('#ws-dependencies').replaceChildren(node('strong','',plan.game+' · '+(names[plan.loader]||plan.loader)+(plan.loaderVersion?' '+plan.loaderVersion:'')),node('p','',plan.files+' файлов'));for(const warning of plan.warnings||[])q('#ws-dependencies').append(node('p','ws-requirement-warning',warning));updatePackTarget();q('#ws-mod-install').disabled=false;
  }catch(e){if(request===state.planRequest&&modal===state.modal){q('#ws-mod-status').textContent=error(e);q('#ws-dependencies').textContent='–'}}return
 }

 const request=++state.planRequest;plan=null;q('#ws-mod-install').disabled=true;q('#ws-mod-status').textContent='Проверка зависимостей…';q('#ws-deps-option').hidden=true;
 try{const version=q('#ws-mod-version').value,target=q('#ws-mod-target').value;if(!version||!target)throw Error('Выбери файл и установленную сборку');const result=await (project.kind==='shaders'?api.PrepareShader(project.provider,project.id,version,target):project.kind==='resources'?api.PrepareResource(project.provider,project.id,version,target):api.PrepareMod(project.provider,project.id,version,target));if(request!==state.planRequest||modal!==state.modal)return;plan=result;q('#ws-install-path').textContent=[plan.target.directory,contentFolder(plan.kind)].join(String.fromCharCode(92));const dependencies=plan.entries.slice(1);q('#ws-dependencies').replaceChildren(...dependencies.map(entry=>{const row=node('div','ws-dependency');row.append(node('strong','',entry.project.name),node('span','',entry.installed?'Установлен':entry.version.name));return row}));if(!dependencies.length&&!plan.warnings?.length)q('#ws-dependencies').textContent=project.kind==='resources'?'Связанные паки не требуются':'Дополнительные моды не нужны';for(const warning of plan.warnings||[])q('#ws-dependencies').append(node('p','ws-requirement-warning',warning));q('#ws-deps-option').hidden=!dependencies.some(d=>!d.installed);q('#ws-mod-size').textContent=size(plan.entries.filter(e=>!e.installed).reduce((total,e)=>total+e.version.size,0));updateInstallButton();q('#ws-mod-status').textContent=dependencies.some(d=>!d.installed)?(project.kind==='resources'?'Нужны связанные паки. Можно установить их вместе или скачать только выбранный пак.':'Нужны дополнительные моды. Можно установить их вместе или скачать только выбранный мод.'):plan.entries[0].installed?'Этот файл уже установлен в выбранную сборку.':'Готово к установке';
 }catch(e){if(request===state.planRequest&&modal===state.modal){q('#ws-mod-status').textContent=error(e);q('#ws-dependencies').textContent='Не удалось проверить зависимости'}}
}
function updatePackTarget(){if(project?.kind!=='modpacks'||!plan)return;const id=q('#ws-mod-target').value,newBuild=id==='__new__';q('#ws-pack-name-row').hidden=!newBuild;const target=state.targets.find(t=>t.id===id);q('#ws-install-path').textContent=newBuild?'Отдельная папка в instances':target?.directory||'Выбранная сборка';q('#ws-mod-install').textContent=newBuild?'Создать и установить':'Установить в сборку';q('#ws-mod-status').textContent=newBuild?'Готово к установке':'Моды и заменяемые конфиги уйдут в резервную копию. Миры сохранятся.'}
function updateInstallButton(){if(window.SintetNetwork.offline){showOfflineInstall();return}if(!plan)return;const entries=q('#ws-with-deps').checked?plan.entries:plan.entries.slice(0,1);q('#ws-mod-install').disabled=!entries.some(e=>!e.installed);q('#ws-mod-install').textContent=entries.every(e=>e.installed)?'Установлено':'Установить';if(!q('#ws-with-deps').checked&&plan.entries.slice(1).some(e=>!e.installed))q('#ws-mod-status').textContent='Без обязательных зависимостей мод может не запуститься.'}
q('#ws-mod-game').onchange=()=>loadModVersions();q('#ws-mod-loader').onchange=()=>loadModVersions();q('#ws-mod-version').onchange=()=>preparePlan();q('#ws-mod-target').onchange=()=>{if(project?.kind==='modpacks'){updatePackTarget();return}state.target=q('#ws-mod-target').value;remember();void preparePlan()};q('#ws-with-deps').onchange=updateInstallButton;
async function installFromModDialog(){if(window.SintetNetwork.offline){showOfflineInstall();return}if(project?.kind==='modpacks'){if(!plan)return;const newBuild=q('#ws-mod-target').value==='__new__',name=q('#ws-pack-name').value.trim();if(newBuild&&name.length<2){q('#ws-mod-status').textContent='Название должно содержать от 2 до 40 символов';q('#ws-pack-name').focus();return}q('#ws-mod-install').disabled=true;try{const build=await api.InstallCatalogPack(plan.id,newBuild?'':q('#ws-mod-target').value,name);lastInstallKind='modpacks';previousBusy=true;state.target=build.id;remember();closeMod();window.dispatchEvent(new Event('sintet:builds'));await pollInstall()}catch(e){q('#ws-mod-status').textContent=error(e);q('#ws-mod-install').disabled=false}return}if(!plan)return;q('#ws-mod-install').disabled=true;try{lastInstallKind=plan.kind||'mods';await api.InstallMod(plan.id,q('#ws-with-deps').checked);previousBusy=true;closeMod();await pollInstall()}catch(e){q('#ws-mod-status').textContent=error(e);q('#ws-mod-install').disabled=false}};
q('#ws-mod-install').onclick=async()=>{
 if(modInstalling)return;
 modInstalling=true;
 const buttons=q('#ws-mod-sources').querySelectorAll('button');
 buttons.forEach(button=>button.disabled=true);
 try{await installFromModDialog()}
 finally{modInstalling=false;buttons.forEach(button=>button.disabled=false)}
};
q('#ws-download-cancel').onclick=()=>api.CancelModInstall();
async function pollInstall(){if(!api||polling)return;polling=true;try{const status=await api.ModInstallState();q('#ws-download').hidden=!status.busy;q('#ws-download-stage').textContent=status.stage;q('#ws-download-percent').textContent=Math.floor(status.percent||0)+'%';q('#ws-download-progress').value=status.percent||0;if(previousBusy&&!status.busy){if(status.error){message(status.error);window.toast?.(status.error)}else{if(lastInstallKind==='modpacks'){window.dispatchEvent(new Event('sintet:builds'));await refreshTargets()}window.toast?.(lastInstallKind==='modpacks'?'Сборка установлена':lastInstallKind==='shaders'?'Шейдеры установлены':lastInstallKind==='resources'?'Ресурс-паки установлены':'Моды установлены');if(!q('#workshop').classList.contains('hidden'))await load()}}previousBusy=status.busy}catch(e){console.error(e)}finally{polling=false}}
window.addEventListener('sintet:settings',async()=>{if(api){try{await refreshTargets();if(!q('#workshop').classList.contains('hidden'))await load()}catch(e){message(error(e))}}});
try{const saved=JSON.parse(localStorage.getItem('sintet.workshop'));if(['catalog','local'].includes(saved?.scope))state.scope=saved.scope;if(categories.some(c=>c.id===saved?.category))state.category=saved.category;if(['all','modrinth','sintedmods','curseforge'].includes(saved?.provider))state.provider=saved.provider;state.target=saved?.target||'';if(saved?.filterSets&&typeof saved.filterSets==='object')Object.assign(filterSets,saved.filterSets);state.filtersOpen=!!saved?.filtersOpen;state.snapshots=!!saved?.snapshots;if(saved?.filters){for(const key of ['game','loader','resolution','topic'])if(typeof saved.filters[key]==='string'&&saved.filters[key].length<65)state.filters[key]=saved.filters[key];if(['relevance','downloads','updated','newest'].includes(saved.filters.sort))state.filters.sort=saved.filters.sort}}catch{}
q('#ws-provider').value=state.provider;window.SintetUI?.select.refresh(q('#ws-provider'));configure();
async function init(){for(let i=0;i<100&&!window.go?.main?.App;i++)await new Promise(resolve=>setTimeout(resolve,100));api=window.go?.main?.App;if(!api){message('Запусти Sintet через Start.bat');return};try{bindFileDrop();await loadBuildMachineSettings();await refreshTargets();await initFilters();configure();if(!q('#workshop').classList.contains('hidden'))await load();setInterval(pollInstall,450);setInterval(()=>{if(api&&!q('#workshop').classList.contains('hidden')&&state.scope==='local'&&state.category==='modpacks')refreshBuildList().catch(e=>message(error(e)))},1800)}catch(e){message(error(e))}}
window.addEventListener('sintet:network',event=>{
 if(state.scope==='catalog'&&!q('#workshop').classList.contains('hidden'))void load();
 if(!dialog.open||!project)return;
 if(event.detail.offline){
  state.modal++;state.filesRequest++;state.planRequest++;
  showOfflineInstall();
 }else{
  void openMod(project);
 }
});
window.SintetWorkshop={refresh:load,openThemes(){
 state.scope='local';
 state.category='themes';
 state.query='';
 q('#workshop-search').value='';
 q('#workshop-nav').click();
}};void init();
