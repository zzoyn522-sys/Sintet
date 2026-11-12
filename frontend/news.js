(() => {
 const workspace = document.querySelector('#play-workspace');
 const toggle = document.querySelector('#news-toggle');
 const panel = document.querySelector('#minecraft-news');
 const presenceToggle = document.querySelector('#rich-presence-toggle');
 const presencePanel = document.querySelector('#rich-presence');
 const q = selector => panel.querySelector(selector);
 const photo = q('#news-photo');
 const card = q('#news-card');
 const title = q('#news-title');
 const date = q('#news-date');
 const status = q('#news-status');
 const counter = q('#news-counter');
 const controls = q('#news-controls');
 const more = q('#news-more');
 const retry = q('#news-retry');
 const reducedMotion = matchMedia('(prefers-reduced-motion: reduce)');
 const dateFormat = () => new Intl.DateTimeFormat(document.documentElement.lang,{day:'numeric',month:'long',timeZone:'UTC'});
 let api, loading = false, animation;
 let entries = [];
 let index = 0;
 let collapsed = storage.get('newsCollapsed','false') === 'true';
 let presenceOpen = false;
 window.addEventListener('sintet:settings',()=>{if(entries.length)render()});

 function applyLayout() {
  const newsHidden = collapsed || presenceOpen;
  workspace.classList.toggle('news-collapsed',newsHidden);
  workspace.classList.toggle('presence-open',presenceOpen);
  panel.inert = newsHidden;
  toggle.inert = presenceOpen;
  presencePanel.inert = !presenceOpen;
  toggle.setAttribute('aria-expanded',String(!newsHidden));
  presenceToggle.setAttribute('aria-expanded',String(presenceOpen));
  const action = newsHidden ? 'Развернуть новости' : 'Свернуть новости';
  toggle.title = action;
  toggle.setAttribute('aria-label',action);
  const presenceAction = presenceOpen ? 'Закрыть Sintet Rich Presence' : 'Открыть Sintet Rich Presence';
  presenceToggle.title = presenceAction;
  presenceToggle.setAttribute('aria-label',presenceAction);
 }

 function setPresence(open) {
  if (!open && presencePanel.contains(document.activeElement)) presenceToggle.focus();
  presenceOpen = open;
  applyLayout();
  window.dispatchEvent(new Event('sintet:news-layout'));
  window.dispatchEvent(new Event('sintet:presence-layout'));
  if (!open && !collapsed && !entries.length) void load();
 }

 function showOffline(){
  panel.classList.add('news-offline');
  status.hidden=false;
  status.textContent=window.SintetNetwork.message;
  controls.hidden=true;
  counter.hidden=true;
  more.hidden=true;
  retry.hidden=true;
 }
 function render(direction = 0) {
  if(window.SintetNetwork.offline){showOffline();return}
  const entry = entries[index];
  if (!entry) return;
  title.textContent = entry.title.replaceAll('\u2014','–');
  photo.style.backgroundImage = entry.image ? 'url('+JSON.stringify(entry.image)+')' : 'none';
  const published = new Date(entry.date+'T00:00:00Z');
  date.hidden = !Number.isFinite(published.getTime());
  if (!date.hidden) {
   date.dateTime = entry.date;
   date.textContent = dateFormat().format(published);
  }
  counter.textContent = (index+1)+' / '+entries.length;
  counter.hidden = entries.length < 2;
  controls.hidden = entries.length < 2;
  more.hidden = false;
  status.hidden = true;
  if (direction && !reducedMotion.matches) {
   animation?.cancel();
   animation = card.animate(
    [{opacity:.15,transform:'translateX('+direction*13+'px)'},{opacity:1,transform:'translateX(0)'}],
    {duration:210,easing:'ease-out'},
   );
  }
 }

 async function load() {
  if(window.SintetNetwork.offline){showOffline();return}
  if (!api || loading) return;
  panel.classList.remove('news-offline');
  loading = true;
  status.hidden = false;
  status.textContent = 'Загружаем новости…';
  retry.hidden = true;
  try {
   entries = await api.MinecraftNews() || [];
   if (!entries.length) throw Error('Empty news feed');
   index = 0;
   render();
  } catch {
   if(window.SintetNetwork.offline){showOffline();return}
   status.textContent = 'Не удалось загрузить новости';
   retry.hidden = false;
  } finally { loading = false; }
 }

 function move(direction) {
  if (entries.length < 2) return;
  index = (index+direction+entries.length)%entries.length;
  render(direction);
 }
 q('#news-prev').onclick = () => move(-1);
 q('#news-next').onclick = () => move(1);
 more.onclick = async () => {
  try { await api.OpenMinecraftNews(entries[index].link); }
  catch(error) { toast(String(error)); }
 };
 retry.onclick = load;
 panel.addEventListener('keydown',event=>{
  if (event.key!=='ArrowLeft'&&event.key!=='ArrowRight') return;
  if (entries.length<2) return;
  event.preventDefault();
  move(event.key==='ArrowLeft'?-1:1);
 });
 toggle.onclick = () => {
  collapsed = !collapsed;
  applyLayout();
  storage.set('newsCollapsed',String(collapsed));
  window.dispatchEvent(new Event('sintet:news-layout'));
  if (!collapsed && !entries.length) void load();
 };
 presenceToggle.onclick = () => setPresence(!presenceOpen);
 workspace.addEventListener('keydown',event=>{
  if (event.key!=='Escape'||!presenceOpen) return;
  if (event.target!==presenceToggle&&!presencePanel.contains(event.target)) return;
  event.preventDefault();
  setPresence(false);
 });
 window.addEventListener('sintet:network',event=>{
  if(event.detail.offline)showOffline();
  else{panel.classList.remove('news-offline');if(!collapsed)void load()}
 });
 if(window.SintetNetwork.offline)showOffline();
 applyLayout();
 (async()=>{
  for (let i=0;i<100&&!window.go?.main?.App;i++) await new Promise(resolve=>setTimeout(resolve,100));
  api=window.go?.main?.App;
  if(api&&!collapsed) await load();
  else if(!api) status.textContent='Открой Sintet через Start.bat';
 })();
})();
