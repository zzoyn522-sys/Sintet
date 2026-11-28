(() => {
 const q = selector => document.querySelector(selector);
 const screen = q('#screenshots');
 const grid = q('#shots-grid');
 const detail = q('#shots-detail');
 const profile = q('#shots-profile');
 const search = q('#shots-search');
 const sort = q('#shots-sort');
 const status = q('#shots-status');
 const more = q('#shots-more');
 const rename = q('#shots-rename-form');
 const newName = q('#shots-new-name');
 const preview = q('#shots-preview');
 const actions = q('.shots-actions');
 const menuButton = q('#shots-menu');
 const context = q('#shots-context');
 const menuOpen = () => context.matches(':popover-open');
 let menuItem, menuAnchor;
 let loaded = false, renderedDay = 0;
 let files = [], visible = [], selected = '', limit = 48, request = 0, busy = false;
 const found = () => files.find(item => item.id === selected);
 const api = () => window.go?.main?.App;
 const t = phrase => window.SintetText?.translate(phrase) || phrase;
 const errorText = error => String(error?.message || error).replace(/^Error:\s*/, '');
 function node(tag, className, text) {
  const item = document.createElement(tag);
  if (className) item.className = className;
  if (text !== undefined) item.textContent = text;
  return item;
 }
 const url = (item, thumb = false) => '/game-screenshots/' + item.id + '?v=' + item.revision + (thumb ? '&thumb=1' : '');
 const date = item => new Date(item.modified).toLocaleString(document.documentElement.lang, {dateStyle:'medium', timeStyle:'short'});
 const stem = name => name.replace(/\.png$/i, '');
 const source = item => item.source === 'overlay' ? t('Оверлей Sintet') : item.source === 'minecraft' ? 'Minecraft' : '';
 const dayKey = value => {
  const d = new Date(value);
  return Date.UTC(d.getFullYear(), d.getMonth(), d.getDate());
 };
 const dayTitle = value => {
  const d = new Date(value), now = new Date();
  const daysAgo = (dayKey(now) - dayKey(d)) / 86400000;
  if (daysAgo === 0) return 'Сегодня';
  if (daysAgo === 1) return 'Вчера';
  return d.toLocaleDateString(document.documentElement.lang, {day:'numeric', month:'long', ...(d.getFullYear() !== now.getFullYear() ? {year:'numeric'} : {})});
 };
 const shortTime = item => new Date(item.modified).toLocaleTimeString(document.documentElement.lang, {hour:'2-digit', minute:'2-digit'});
 const autoName = name => /^\d{4}-\d{2}-\d{2}_\d{2}\.\d{2}\.\d{2}(?:_\d+){0,2}$/i.test(stem(name));

 function notice(message, action) {
  if (detail.open) {
   q('#shots-view-feedback').textContent = message;
   return;
  }
  window.toast?.(message, action);
 }

 function closeContext(restoreFocus = false) {
  if (menuOpen()) context.hidePopover();
  menuButton.setAttribute('aria-expanded','false');
  if (restoreFocus) (menuAnchor === preview ? menuButton : menuAnchor)?.focus({preventScroll:true});
  menuItem = menuAnchor = null;
 }
 function showContext(item, anchor, x, y) {
  if (!item || busy || !rename.hidden) return;
  closeContext();
  const box = anchor.getBoundingClientRect();

  (detail.open ? detail : document.body).append(context);
  menuItem = item;
  menuAnchor = anchor;
  context.showPopover();
  const {width,height} = context.getBoundingClientRect();
  const fromButton = anchor === menuButton;
  const left = fromButton ? box.right - width : x ?? box.left;
  const top = fromButton ? box.top - height - 6 : y ?? box.bottom;

  context.style.left = Math.max(8,Math.min(left,innerWidth-width-8))+'px';
  context.style.top = Math.max(8,Math.min(top,innerHeight-height-8))+'px';
  if (fromButton) menuButton.setAttribute('aria-expanded','true');
  context.querySelector('button').focus({preventScroll:true});
 }
 menuButton.onclick = () => {
  if (menuOpen() && menuAnchor === menuButton) closeContext(true);
  else showContext(found(),menuButton);
 };
 preview.oncontextmenu = event => {
  event.preventDefault();
  showContext(found(),preview,event.clientX || undefined,event.clientY || undefined);
 };
 document.addEventListener('pointerdown',event => {
  if (menuOpen() && !context.contains(event.target) && !menuButton.contains(event.target)) closeContext();
 },true);
 document.addEventListener('scroll',event => {if (!context.contains(event.target)) closeContext();},true);
 window.addEventListener('resize',() => closeContext());
 window.addEventListener('blur',() => closeContext());
 context.addEventListener('keydown',event => {
  if (event.key === 'Escape' || event.key === 'Tab') {
   event.preventDefault();
   event.stopPropagation();
   closeContext(true);
   return;
  }
  if (!['ArrowUp','ArrowDown','Home','End'].includes(event.key)) return;
  event.preventDefault();
  event.stopPropagation();
  const rows = [...context.querySelectorAll('button')];
  const index = rows.indexOf(document.activeElement);
  const target = event.key === 'Home' ? 0 : event.key === 'End' ? rows.length-1 :
   (index + (event.key === 'ArrowDown' ? 1 : -1) + rows.length) % rows.length;
  rows[target]?.focus({preventScroll:true});
 });
 function showDetail() {
  if (!detail.open) return;
  const item = found();
  if (!item) { detail.close(); return; }
  rename.hidden = true;
  closeContext();
  q('#shots-view-feedback').textContent = '';

  if (preview.getAttribute('src') !== url(item)) preview.src = url(item);
  preview.alt = item.name;
  q('#shots-name').textContent = autoName(item.name) ? (item.profile || t('Общая папка')) : stem(item.name);
  q('#shots-meta').textContent = [date(item), source(item), item.name].filter(Boolean).join(' · ');
  const index = visible.findIndex(shot => shot.id === selected);
  q('#shots-prev').disabled = index <= 0;
  q('#shots-next').disabled = index < 0 || index >= visible.length - 1;
 }
 function select(item) {
  selected = item.id;
  if (!detail.open) detail.showModal();
  showDetail();
 }
 function step(direction) {
  if (busy || !rename.hidden || menuOpen()) return;
  const index = visible.findIndex(shot => shot.id === selected);
  const next = visible[index + direction];
  if (next) select(next);
 }
 q('#shots-prev').onclick = () => step(-1);
 q('#shots-next').onclick = () => step(1);
 q('#shots-close').onclick = () => {if (!busy) detail.close();};
 detail.addEventListener('cancel', event => {
  if (busy || menuOpen() || !rename.hidden) {
   event.preventDefault();
   if (menuOpen()) closeContext(true);
   else if (!busy && !rename.hidden) q('#shots-rename-cancel').click();
  }
 });
 detail.addEventListener('close', () => {
  if (detail.open) return;
  q('#shots-preview').removeAttribute('src');
  closeContext();
  rename.hidden = true;
  selected = '';
 });
 detail.addEventListener('click', event => {
  if (event.target !== detail || busy) return;
  const box = detail.getBoundingClientRect();
  if (event.clientX < box.left || event.clientX >= box.right || event.clientY < box.top || event.clientY >= box.bottom) detail.close();
 });
 detail.addEventListener('keydown', event => {
  if (!['ArrowLeft','ArrowRight'].includes(event.key) || !rename.hidden || menuOpen()) return;
  event.preventDefault();
  step(event.key === 'ArrowLeft' ? -1 : 1);
 });
 function card(item) {
  const button = node('button', 'shots-card');
  button.type = 'button';
  button.dataset.shotId = item.id;
  button.setAttribute('aria-haspopup', 'dialog');
  const picture = node('span', 'shots-thumb');
  const img = node('img');
  img.alt = '';
  img.loading = 'lazy';
  img.decoding = 'async';
  img.src = url(item, true);
  picture.append(img);
  const caption = node('span','shots-caption');
  const info = node('span','shots-caption-line');
  const build = node('span','shots-build',item.profile || t('Общая папка'));
  build.dataset.noTranslate = '';
  const time = node('time','shots-time',shortTime(item));
  time.dateTime = item.modified;
  info.append(build,time);
  if (!autoName(item.name)) {
   const name = node('strong','shots-custom-name',stem(item.name));
   name.dataset.noTranslate = '';
   caption.append(name);
  }
  caption.append(info);
  button.append(picture,caption);
  button.onclick = () => select(item);
  button.oncontextmenu = event => {
   event.preventDefault();
   showContext(item,button,event.clientX || undefined,event.clientY || undefined);
  };
  return button;
 }
 function render() {
  closeContext();
  renderedDay = dayKey(Date.now());
  const term = search.value.trim().toLocaleLowerCase();
  const items = files.filter(item => (!profile.value || item.directory === profile.value) &&
   (!term || [item.name,item.profile,source(item)].some(value => value.toLocaleLowerCase().includes(term))));
  const order = sort.value === 'old' ? 1 : -1;
  items.sort((a,b) => order * (dayKey(a.modified) - dayKey(b.modified)) ||
   (sort.value === 'name' ? a.name.localeCompare(b.name, undefined, {numeric:true}) : order * (new Date(a.modified) - new Date(b.modified))));
  visible = items;
  const sections = new Map();
  for (const item of items.slice(0,limit)) {
   const key = dayKey(item.modified);
   if (!sections.has(key)) {
    const section = node('section','shots-day');
    const heading = node('h2','shots-day-heading',dayTitle(item.modified));
    const row = node('div','shots-grid');
    section.append(heading,row);
    sections.set(key,{section,row});
   }
   sections.get(key).row.append(card(item));
  }
  grid.replaceChildren(...[...sections.values()].map(({section}) => section));
  status.textContent = items.length ? '' : files.length ? 'По запросу ничего не найдено.' : 'Снимков пока нет. Сделай скриншот в Minecraft клавишей F2 или через оверлей.';
  more.hidden = items.length <= limit;
  showDetail();
 }
 const sameShot = (a,b) => a.id === b.id && a.revision === b.revision && a.modified === b.modified &&
  a.name === b.name && a.profile === b.profile && a.directory === b.directory && a.source === b.source;
 async function refresh(quiet = false) {
  const id = ++request;
  const backend = api();
  if (!loaded && !quiet) status.textContent = 'Загружаем снимки…';
  try {
   if (!backend) throw Error('Запусти Sintet через Start.bat.');
   const data = await backend.GameScreenshots();
   if (id !== request) return;
   const next = data || [];
   if (loaded && renderedDay === dayKey(Date.now()) && next.length === files.length &&
       next.every((item,index)=>sameShot(item,files[index]))) return;
   files = next;
   loaded = true;
   const choice = profile.value;
   const labels = new Map(files.map(item => [item.directory, item.profile || t('Общая папка')]));
   profile.replaceChildren(new Option(t('Все сборки'), ''), ...[...labels].sort((a,b) => a[1].localeCompare(b[1])).map(([key,name]) => new Option(name,key)));
   profile.value = labels.has(choice) ? choice : '';
   window.SintetUI?.select.refresh(profile);
   render();
  } catch (error) {
   if (id === request && !quiet) {
    if (loaded) notice(errorText(error));
    else status.textContent = errorText(error);
   }
  }
 }
 async function run(operation) {
  if (busy) return;
  busy = actions.inert = rename.inert = true;
  try {await operation();}
  catch (error) {notice(errorText(error));}
  finally {busy = actions.inert = rename.inert = false;}
 }
 q('#screenshots-toggle').onclick = () => {
  if (!screen.classList.contains('hidden')) { page('home'); return; }
  page('screenshots');
  screen.closest('main').scrollTop = 0;
  screen.querySelector('.shots-scroll').scrollTop = 0;
  void refresh();
 };
 q('#shots-refresh').onclick = () => void refresh();
 search.oninput = profile.onchange = sort.onchange = () => {limit = 48; render();};
 more.onclick = () => {limit += 48; render();};
 window.addEventListener('focus', () => {if (!busy && !detail.open && !menuOpen() && !screen.classList.contains('hidden')) void refresh(true);});
 q('#shots-open').onclick = () => {
  const item = menuItem;
  closeContext(true);
  if (item) void run(() => api().OpenGameScreenshot(item.id,false));
 };
 q('#shots-folder').onclick = () => {if (found()) void run(() => api().OpenGameScreenshot(selected,true));};
 function copyImage(item) {
  if (!item) return;
  void run(async () => {
   if (!window.ClipboardItem || !navigator.clipboard?.write) throw Error(t('Копирование снимка недоступно.'));
   const png = fetch(url(item)).then(response => {
    if (!response.ok) throw Error(t('Не удалось открыть снимок.'));
    return response.blob();
   });
   await navigator.clipboard.write([new ClipboardItem({'image/png':png})]);
   notice(t('Снимок скопирован'));
  });
 }
 q('#shots-copy').onclick = () => copyImage(found());
 q('#shots-context-copy').onclick = () => {
  const item = menuItem;
  closeContext(true);
  copyImage(item);
 };
 q('#shots-rename').onclick = () => {
  const item = menuItem;
  closeContext();
  if (!item) return;
  if (!detail.open) select(item);
  newName.value = stem(item.name);
  rename.hidden = false;
  newName.select();
  newName.focus();
 };
 q('#shots-rename-cancel').onclick = () => {rename.hidden = true; menuButton.focus();};
 rename.onsubmit = event => {
  event.preventDefault();
  const id = selected;
  const name = newName.value;
  if (!id) return;
  void run(async () => {selected = await api().RenameGameScreenshot(id,name); await refresh(); notice(t('Снимок переименован'));});
 };
 q('#shots-trash').onclick = () => {
  const item = menuItem;
  closeContext(true);
  if (!item) return;
  void run(async () => {
   const removed = await api().TrashGameScreenshot(item.id);
   if (detail.open) detail.close();
   await refresh();
   notice(t('Снимок удалён'),{
    label:t('Вернуть снимок'),
    run:()=>run(async()=>{
     await api().RestoreGameScreenshot(removed);
     notice(t('Снимок восстановлен'));
     await refresh();
    })
   });
  });
 };
})();
