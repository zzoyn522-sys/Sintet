(() => {
 const root = document.documentElement;
 const tabs = [...document.querySelectorAll('.settings-tabs [role=tab]')];
 const form = document.querySelector('#appearance-form');
 const fields = form.elements;
 const editorTabs = [...form.querySelectorAll('.appearance-editor-tabs [role=tab]')];
 const mediaControls = form.querySelector('[data-appearance-media]');
 const waveColorValue = form.querySelector('#appearance-wave-color-value');
 const waveNumbers = ['waveIntensity','waveAmplitude','waveTrail'];
 const status = document.querySelector('#appearance-status');
 const save = document.querySelector('#appearance-save');
 const reset = document.querySelector('#appearance-reset');
 const choose = document.querySelector('#appearance-choose');
 const remove = document.querySelector('#appearance-remove');
 const chooseFont = document.querySelector('#appearance-font-import');
 const removeFont = document.querySelector('#appearance-font-remove');
 const accentPresets = document.querySelector('#appearance-accent-presets');
 const importButton = document.querySelector('#appearance-import');
 const exportButton = document.querySelector('#appearance-export');
 const slotList = document.querySelector('#appearance-slots');
 const fileLabel = document.querySelector('#appearance-file-label');
 const background = document.querySelector('#appearance-background');
 const image = background.querySelector('img');
 const video = background.querySelector('video');
 const mediaEdges = background.querySelector('.appearance-media-edges');
 const edgeBlurControl = fields.videoEdgeBlur.closest('label');
 const videoBackground = /\.(mp4|webm)$/i;
 const motion = matchMedia('(prefers-reduced-motion: reduce)');
 const advancedNumbers=['textScale','textWeight','buttonRadius','backgroundBlur','videoEdgeBlur','backgroundSaturation','backgroundZoom','backgroundBrightness','backgroundContrast','backgroundHue','backgroundGrayscale','backgroundSepia','backgroundOpacity','motionSpeed'];
 const fonts = {
  segoe: '"Segoe UI", sans-serif',
  arial: 'Arial, sans-serif',
  georgia: 'Georgia, serif',
  verdana: 'Verdana, sans-serif',
  calibri: 'Calibri, sans-serif',
  mono: '"Cascadia Mono", Consolas, monospace',
 };
 let api, saved, defaults;
 let draftBackground = '', draftFont = '', busy = false, shownBackground = '';
 let sourceFailed = false;
 let fontFailed = false, fontLoading = false, activeFont = '', fontFace;
 let waveSettings, waveLoad, renderWave;
 const accents = [
  ['#b7ff47','Лайм'], ['#a88bff','Фиолетовый'], ['#ffe45e','Жёлтый'],
  ['#ff5277','Карминовый'], ['#6fd8ff','Ледяной синий'], ['#ededed','Белый'],
 ];
 for (const [color, label] of accents) {
  const button = document.createElement('button');
  button.type = 'button';
  button.setAttribute('aria-label', label);
  button.dataset.color = color;
  button.style.setProperty('--swatch', color);
  button.onclick = () => {
   fields.accent.value = color;
   fields.accent.dispatchEvent(new Event('input', {bubbles: true}));
  };
  accentPresets.append(button);
 }
 let slots = [], activeSlot = 0;
 const palettePreviews = {
  slate: ['#11171a', '#20292f'],
  midnight: ['#0d1120', '#1c2239'],
  warm: ['#181210', '#302522'],
 };
 const sectionColors = {
  nav: {navColor:'--nav-surface',navText:'--nav-text',navActive:'--nav-selected'},
  profile: {profileColor:'--profile-surface',profileText:null,profileControl:'--profile-control'},
 };
 const sectionFields = Object.values(sectionColors).flatMap(Object.keys);
 const navigationSliders = {
  navOpacity:['--nav-opacity','%'], navBlur:['--nav-blur','px'], navHeight:['--nav-height','px'],
  navTabHeight:['--nav-tab-height','px'], navTabWidth:['--nav-tab-width','px'], navTabRadius:['--nav-tab-radius','px'],
  navTabGap:['--nav-tab-gap','px'], navTextSize:['--nav-text-size','px'],
 };
 const navigationFields = Object.keys(navigationSliders);

 function onColor(color) {
  const channels = color.slice(1).match(/../g).map(value => parseInt(value,16)/255);
  const [red,green,blue] = channels.map(value => value <= .04045 ? value / 12.92 : ((value + .055) / 1.055) ** 2.4);
  return .2126*red+.7152*green+.0722*blue > .179 ? '#090909' : '#ffffff';
 }

 function applySections(settings) {
  const baseBar = settings.barColor !== defaults.barColor ? settings.barColor :
   ({slate:'#141d22',midnight:'#12172a',warm:'#201714'})[settings.theme] || defaults.barColor;
  const baseProfile = settings.panelColor !== defaults.panelColor ? settings.panelColor :
   (palettePreviews[settings.theme]?.[1] || '#171717');
  const swatches = {
   navColor:baseBar,navText:onColor(settings.navColor || baseBar),navActive:'#242424',
   profileColor:baseProfile,profileText:onColor(settings.profileColor || baseProfile),profileControl:'#292929',
  };
  for (const [section,values] of Object.entries(sectionColors)) {
   root.toggleAttribute('data-custom-'+section,Object.keys(values).some(key=>settings[key]));
   for (const [key,property] of Object.entries(values)) {
    fields[key].value = settings[key] || swatches[key];
    if (property) root.style.setProperty(property,settings[key] || '');
   }
  }
  const navSurface = settings.navColor || baseBar;
  const profileSurface = settings.profileColor || baseProfile;
  const onNav = settings.navText || onColor(navSurface);
  const onProfile = settings.profileText || onColor(profileSurface);
  const bright = color => onColor(color) !== '#ffffff';
  root.style.setProperty('--nav-auto-ink',onNav);
  root.style.setProperty('--nav-selected-ink',settings.navActive ? onColor(settings.navActive) : onNav);
  root.style.setProperty('--nav-logo',bright(navSurface)?'brightness(.2)':'none');
  root.style.setProperty('--profile-auto-ink',onProfile);
  root.style.setProperty('--profile-control-ink',settings.profileControl?onColor(settings.profileControl):onProfile);
  root.style.setProperty('--profile-logo',bright(profileSurface)?'brightness(.2)':'none');
  root.style.setProperty('--profile-tile-logo',bright(settings.profileControl || profileSurface)?'brightness(.2)':'none');
  const glass = settings.navOpacity !== defaults.navOpacity || settings.navBlur !== defaults.navBlur;
  root.toggleAttribute('data-nav-glass',glass);
  root.toggleAttribute('data-nav-layout',navigationFields.slice(2).some(key=>settings[key] !== defaults[key]));
  for (const [key,[property,unit]] of Object.entries(navigationSliders)) {
   const value = settings[key] ?? defaults[key];
   const cssValue = value+unit;
   const applyValue = key==='navOpacity' || key==='navBlur' ? glass : value !== defaults[key];
   root.style.setProperty(property,applyValue?cssValue:'');
   fields[key].previousElementSibling.value = unit === 'px' ? value+' px' : cssValue;
  }
 }

 function selectTab(tab, focus = false) {
  for (const item of tabs) {
   const selected = item === tab;
   item.setAttribute('aria-selected', String(selected));
   item.tabIndex = selected ? 0 : -1;
   document.getElementById(item.getAttribute('aria-controls')).hidden = !selected;
  }
  if (focus) tab.focus();
 }
 for (const tab of tabs) {
  tab.addEventListener('click', () => selectTab(tab));
  tab.addEventListener('keydown', event => {
   const index = tabs.indexOf(tab);
   const next = {ArrowLeft: 1 - index, ArrowRight: 1 - index, Home: 0, End: tabs.length - 1}[event.key];
   if (next === undefined) return;
   event.preventDefault();
   selectTab(tabs[next], true);
  });
 }

 function selectEditorTab(tab) {
  for (const item of editorTabs) {
   const selected = item === tab;
   item.setAttribute('aria-selected', String(selected));
   item.tabIndex = selected ? 0 : -1;
   document.getElementById(item.getAttribute('aria-controls')).hidden = !selected;
  }
 }
 for (const tab of editorTabs) {
  tab.addEventListener('click', () => selectEditorTab(tab));
  tab.addEventListener('keydown', event => {
   const index = editorTabs.indexOf(tab);
   const previous = (index + editorTabs.length - 1) % editorTabs.length;
   const next = (index + 1) % editorTabs.length;
   const target = {ArrowUp:previous,ArrowLeft:previous,ArrowDown:next,ArrowRight:next,Home:0,End:editorTabs.length-1}[event.key];
   if (target === undefined) return;
   event.preventDefault();
   selectEditorTab(editorTabs[target]);
   editorTabs[target].focus();
  });
 }

 function read() {
  return {
   ...saved,
   theme: fields.theme.value, font: fields.font.value, accent: fields.accent.value,
   opacity: fields.opacity.valueAsNumber, blur: fields.blur.valueAsNumber,
   radius: fields.radius.valueAsNumber, dim: fields.dim.valueAsNumber,
   fit: fields.fit.value, gradient: fields.waveEnabled.checked, animate: fields.animate.checked,
   backgroundEffect: 'wave', waveColor: fields.waveColor.value, waveOffset: fields.waveOffset.valueAsNumber,
   background: draftBackground, fontFile: draftFont,
   barColor: fields.barColor.value, mutedColor: fields.mutedColor.value,
   borderWidth: fields.borderWidth.valueAsNumber, borderOpacity: fields.borderOpacity.valueAsNumber,
   shadow: fields.shadow.value, shadowStrength: fields.shadowStrength.valueAsNumber,
   vignette: fields.vignette.valueAsNumber, grain: fields.grain.valueAsNumber,
   canvasColor: fields.canvasColor.value, panelColor: fields.panelColor.value, textColor: fields.textColor.value,
   position: fields.position.value, compact: fields.compact.checked, borders: fields.borders.checked,
   ...Object.fromEntries(sectionFields.map(key=>[key,fields[key].hasAttribute('data-override')?fields[key].value:''])),
   ...Object.fromEntries(advancedNumbers.map(name=>[name,fields[name].valueAsNumber])),
   ...Object.fromEntries(waveNumbers.map(name=>[name,fields[name].valueAsNumber])),
   ...Object.fromEntries(navigationFields.map(name=>[name,fields[name].valueAsNumber])),
  };
 }

 function fill(settings) {
  draftBackground = settings.background;
  draftFont = settings.fontFile;
  fields.font.querySelector('[value=custom]').disabled = !draftFont;
  for (const [key, value] of Object.entries(settings)) {
   const field = fields.namedItem(key);
   if (!field) continue;
   if (sectionFields.includes(key)) {
    field.toggleAttribute('data-override',Boolean(value));
    continue;
   }
   if (field.type === 'checkbox') field.checked = value;
   else field.value = value;
   if (field instanceof HTMLSelectElement) window.SintetUI?.select.refresh(field);
  }
  for (const key of navigationFields) fields[key].value = settings[key] ?? defaults[key];
  fields.waveEnabled.checked = settings.backgroundEffect === 'wave' && settings.gradient;
  apply(settings);
 }

 function renderBackground(settings) {
  const name = settings.background;
  const animated = settings.animate && !motion.matches;
  const isVideo = videoBackground.test(name);
  const changed = shownBackground !== name;
  if (changed) {
   shownBackground = name;
   sourceFailed = false;
   background.style.removeProperty('--media-ratio');
   video.pause();
   image.removeAttribute('src');
   video.removeAttribute('src');
   video.load();
   if (name) (isVideo ? video : image).src = '/custom-background/' + encodeURIComponent(name);
  }
  background.hidden = !name || sourceFailed;
  image.hidden = !name || isVideo;
  video.hidden = !name || !isVideo;
  edgeBlurControl.hidden = !name;
  mediaControls.hidden = !name;
  mediaEdges.hidden = background.hidden || !settings.videoEdgeBlur;
  background.toggleAttribute('data-media-contain', settings.fit === 'contain');
  background.style.setProperty('--media-edge-blur', (settings.videoEdgeBlur || 0) + 'px');
  background.style.setProperty('--media-offset-x', ({left:-.5,right:.5})[settings.position] || 0);
  background.style.setProperty('--media-offset-y', ({top:-.5,bottom:.5})[settings.position] || 0);
  if (isVideo && !sourceFailed) {
   if (animated) {
    video.muted = true;
    void video.play().catch(error => {
     if (error.name !== 'AbortError' && shownBackground === name && video.paused &&
         fields.animate.checked && !motion.matches && !sourceFailed) {
      fileLabel.textContent = 'Не удалось запустить видео';
     }
    });
   } else {
    video.pause();
    fileLabel.textContent = settings.animate ? 'Видео на паузе · в Windows уменьшено движение' : 'Видео на паузе · включи «Анимация фона»';
   }
  }
  if (name && !isVideo && name.endsWith('.gif')) {
   image.style.visibility = !animated ? 'hidden' : '';
   const canvas = background.querySelector('canvas');
   canvas.hidden = animated || !image.complete || !image.naturalWidth;
   if (!canvas.hidden) {
    canvas.width = image.naturalWidth;
    canvas.height = image.naturalHeight;
    canvas.getContext('2d').drawImage(image, 0, 0);
   }
  } else {
   background.querySelector('canvas').hidden = true;
   image.style.visibility = '';
  }
 }

 function applyWave(settings, enabled) {
  if (root.dataset.backgroundExperiment) return;
  waveSettings = {
   enabled, animate: settings.animate && !window.SintetThemePreview,
   color: settings.waveColor, intensity: settings.waveIntensity, amplitude: settings.waveAmplitude,
   trail: settings.waveTrail, speed: settings.motionSpeed, offset: settings.waveOffset,
  };
  if (renderWave) {
   renderWave(waveSettings);
  } else if (enabled && !waveLoad) {
   waveLoad = import('./background-wave.js').then(module => {
    renderWave = module.configureWave;
    renderWave(waveSettings);
   }).catch(error => {
    waveLoad = undefined;
    console.error(error);
   });
  }
 }

 async function loadFont(name) {
  if (activeFont === name) return;
  activeFont = name;
  fontFailed = false;
  if (fontFace) document.fonts.delete(fontFace);
  fontFace = undefined;
  fontLoading = Boolean(name);
  root.style.removeProperty('--imported-font');
  setBusy(busy);
  if (!name) return;
  try {
   const face = await new FontFace('SintetImported', 'url("/custom-background/' + encodeURIComponent(name) + '")').load();
   if (activeFont !== name) return;
   document.fonts.add(face);
   fontFace = face;
   root.style.setProperty('--imported-font', '"SintetImported", "Segoe UI", sans-serif');
  } catch {
   if (activeFont !== name) return;
   fontFailed = true;
   status.textContent = 'Шрифт не удалось открыть. Выбери другой файл.';
  } finally {
   if (activeFont === name) {
    fontLoading = false;
    setBusy(busy);
   }
  }
 }

 function apply(settings) {
  const data = root.dataset;
  const style = root.style;
  root.toggleAttribute('data-text-scale',settings.textScale!==defaults.textScale);
  root.toggleAttribute('data-text-weight',settings.textWeight!==defaults.textWeight);
  root.toggleAttribute('data-button-radius',settings.buttonRadius!==defaults.buttonRadius);
  style.setProperty('--text-scale',settings.textScale/100);
  style.setProperty('--text-weight',settings.textWeight);
  style.setProperty('--button-radius',settings.buttonRadius+'px');
  style.setProperty('--media-blur',settings.backgroundBlur+'px');
  style.setProperty('--media-saturation',settings.backgroundSaturation/100);
  style.setProperty('--media-brightness',settings.backgroundBrightness/100);
  style.setProperty('--media-contrast',settings.backgroundContrast/100);
  style.setProperty('--media-hue',settings.backgroundHue+'deg');
  style.setProperty('--media-grayscale',settings.backgroundGrayscale/100);
  style.setProperty('--media-sepia',settings.backgroundSepia/100);
  style.setProperty('--media-opacity',settings.backgroundOpacity/100);
  style.setProperty('--media-zoom',settings.backgroundZoom/100);
  const waveEnabled = settings.backgroundEffect === 'wave' && settings.gradient;
  root.toggleAttribute('data-launcher-wave', waveEnabled);
  waveColorValue.value = settings.waveColor.toUpperCase();
  form.querySelector('[data-appearance-shadow]').hidden = settings.shadow === 'off';
  form.querySelector('[data-appearance-borders]').hidden = !settings.borders;
  applyWave(settings, waveEnabled);
  style.setProperty('--vignette-opacity', settings.vignette / 100);
  style.setProperty('--grain-opacity', settings.grain / 100);
  data.launcherTheme = settings.theme;
  applySections(settings);
  root.toggleAttribute('data-custom-canvas', settings.canvasColor !== defaults.canvasColor);
  root.toggleAttribute('data-custom-panel', settings.panelColor !== defaults.panelColor);
  root.toggleAttribute('data-custom-text', settings.textColor !== defaults.textColor);
  root.toggleAttribute('data-custom-bar', settings.barColor !== defaults.barColor);
  root.toggleAttribute('data-custom-muted', settings.mutedColor !== defaults.mutedColor);
  root.toggleAttribute('data-panel-shadow', settings.shadow !== 'off' && settings.shadowStrength > 0);
  style.setProperty('--custom-bar', settings.barColor);
  style.setProperty('--custom-muted', settings.mutedColor);
  style.setProperty('--border-width', settings.borderWidth + 'px');
  style.setProperty('--border-opacity', settings.borderOpacity / 100);
  const shadow = settings.shadow === 'glow'
   ? '0 0 25px color-mix(in srgb,var(--launcher-accent) ' + settings.shadowStrength * .4 + '%,transparent)'
   : '0 12px 32px rgb(0 0 0 / ' + settings.shadowStrength / 125 + ')';
  style.setProperty('--panel-shadow', shadow);
  for (const button of accentPresets.children) button.setAttribute('aria-pressed', String(button.dataset.color === settings.accent));
  root.toggleAttribute('data-compact-layout', settings.compact);
  root.toggleAttribute('data-panel-borders', settings.borders);
  style.setProperty('--custom-canvas', settings.canvasColor);
  style.setProperty('--custom-panel', settings.panelColor);
  style.setProperty('--custom-text', settings.textColor);
  style.setProperty('--background-position', settings.position);
  root.toggleAttribute('data-launcher-font', settings.font !== 'default');
  root.toggleAttribute('data-launcher-accent', settings.accent !== defaults.accent);
  root.toggleAttribute('data-launcher-panels', settings.theme !== defaults.theme || settings.opacity !== defaults.opacity ||
   settings.blur !== defaults.blur || settings.radius !== defaults.radius || settings.panelColor !== defaults.panelColor || settings.barColor !== defaults.barColor || Boolean(settings.background));
  style.setProperty('--launcher-font', settings.font === 'custom' ? 'var(--imported-font, "Segoe UI", sans-serif)' : fonts[settings.font] || '');
  void loadFont(settings.font === 'custom' ? settings.fontFile : '');
  style.setProperty('--launcher-accent', settings.accent);
  style.setProperty('--launcher-accent-text', onColor(settings.accent));
  style.setProperty('--launcher-radius', settings.radius + 'px');
  style.setProperty('--launcher-opacity', settings.opacity + '%');
  style.setProperty('--launcher-blur', settings.blur + 'px');
  style.setProperty('--background-fit', settings.fit);
  style.setProperty('--background-dim', settings.dim / 100);
  for (const [key, unit] of Object.entries({dim:'%', opacity:'%', blur:' px', radius:' px', vignette:'%', grain:'%', borderWidth:' px', borderOpacity:'%', shadowStrength:'%',textScale:'%',textWeight:'',buttonRadius:' px',backgroundBlur:' px',videoEdgeBlur:' px',backgroundSaturation:'%',backgroundZoom:'%',backgroundBrightness:'%',backgroundContrast:'%',backgroundHue:'°',backgroundGrayscale:'%',backgroundSepia:'%',backgroundOpacity:'%',motionSpeed:'%',waveOffset:' px'})) {
   fields[key].previousElementSibling.value = settings[key] + unit;
  }
  for (const key of waveNumbers) fields[key].previousElementSibling.value = settings[key] + '%';
  for (const range of form.querySelectorAll('input[type=range]')) {
   const min = Number(range.min);
   const max = Number(range.max);
   range.style.setProperty('--range-fill', ((range.valueAsNumber - min) / (max - min) * 100) + '%');
  }
  fileLabel.textContent = settings.background ? settings.background.split('.').pop().toUpperCase() : 'Фон не выбран';
  remove.disabled = busy || !settings.background;
  renderBackground(settings);
 }

 function mediaError(event) {
  if (event.target.hidden || !event.target.hasAttribute('src')) return;
  sourceFailed = true;
  background.hidden = true;
  if (event.target === video) fileLabel.textContent = 'Не удалось открыть видео';
  status.textContent = 'Этот файл фона не удалось открыть. Попробуй другой формат.';
  save.disabled = true;
 }
 image.addEventListener('error', mediaError);
 video.addEventListener('error', mediaError);
 function updateMediaRatio(width, height) {
  if (width && height) background.style.setProperty('--media-ratio', width / height);
 }
 video.addEventListener('loadedmetadata', () => {
  updateMediaRatio(video.videoWidth, video.videoHeight);
 });
 image.addEventListener('load', () => {
  updateMediaRatio(image.naturalWidth, image.naturalHeight);
  if (draftBackground.endsWith('.gif')) renderBackground(read());
 });
 motion.addEventListener('change', () => { if (saved) apply(read()); });

 function setBusy(value) {
  busy = value;
  choose.disabled = reset.disabled = value;
  save.disabled = value || sourceFailed || fontFailed || fontLoading;
  chooseFont.disabled = value;
  removeFont.disabled = value || !draftFont;
  importButton.disabled = value;
  exportButton.disabled = value;
  for (const control of slotList.querySelectorAll('button,input')) {
   control.disabled = value || control.hasAttribute('data-unavailable');
  }
  remove.disabled = value || !draftBackground;
 }

 form.addEventListener('input', event => {
  if (!saved) return;
  if (sectionFields.includes(event.target.name)) event.target.setAttribute('data-override','');
  apply(read());
  status.textContent = sourceFailed ? 'Этот файл фона не удалось открыть.' : 'Есть несохранённые изменения';
 });
 for (const button of form.querySelectorAll('[data-reset-colors]')) {
  button.onclick = () => {
   if (!saved || busy) return;
   const section = button.dataset.resetColors;
   for (const name of Object.keys(sectionColors[section])) fields[name].removeAttribute('data-override');
   if (section === 'nav') {
    for (const name of navigationFields) fields[name].value = defaults[name];
   }
   apply(read());
   status.textContent = 'Есть несохранённые изменения';
  };
 }
 choose.addEventListener('click', async () => {
  if (!api || busy) return;
  setBusy(true);
  try {
   const selected = await api.ChooseAppearanceBackground();
   if (selected) {
    draftBackground = selected;
    if (videoBackground.test(selected)) fields.animate.checked = true;
    apply(read());
    status.textContent = 'Фон выбран. Нажми «Сохранить».';
   }
  } catch (error) {
   status.textContent = String(error);
  } finally {
   setBusy(false);
  }
 });
 remove.addEventListener('click', () => {
  draftBackground = '';
  apply(read());
  save.disabled = busy || fontFailed || fontLoading;
  status.textContent = 'Фон убран. Нажми «Сохранить».';
 });
 reset.addEventListener('click', () => void runAction(async () => {
  await api.SetActiveAppearanceSlot(-1);
  await refreshLibrary(true);
  return 'Применено оформление по умолчанию.';
 }));
 form.addEventListener('submit', async event => {
  event.preventDefault();
  if (!saved || busy || sourceFailed || fontFailed || fontLoading) return;
  const settings = read();
  setBusy(true);
  try {
   await api.SaveAppearance(settings);
   await refreshLibrary(true);
   status.textContent = 'Оформление сохранено';
   toast('Оформление сохранено');
  } catch (error) {
   status.textContent = String(error);
  } finally {
   setBusy(false);
  }
 });
 function markCurrentSlot() {
  [...slotList.children].forEach((card, index) => {
   const preview = card.querySelector('.appearance-slot-preview');
   if (slots[index].name) preview.setAttribute('aria-pressed', String(index + 1 === activeSlot));
  });
 }

 function renderThemeIcon(target, slot) {
  if (!slot.icon || !/^[a-f0-9]{64}\.png$/.test(slot.icon)) return;
  const image = document.createElement('img');
  image.className = 'appearance-custom-icon';
  image.alt = '';
  image.draggable = false;
  image.src = '/theme-icons/' + slot.icon;
  image.onerror = () => image.remove();
  target.append(image);
 }

 async function refreshLibrary(forceApply = false) {
  const library = await api.GetAppearanceLibrary();
  const changed = !saved || Object.keys(library.current).some(key => saved[key] !== library.current[key]);
  saved = library.current;
  slots = library.slots;
  activeSlot = library.activeSlot;
  exportButton.textContent='Экспорт .sldcfg';
  if (changed || forceApply) fill(saved);
  renderSlots();
  setBusy(busy);
  window.dispatchEvent(new CustomEvent('sintet:appearance-library', {detail: library}));
  return library;
 }

 function renderSlots() {
  slotList.replaceChildren();
  slots.forEach((slot, index) => {
   const card = document.createElement('article');
   card.className = 'appearance-slot';
   card.innerHTML = `
    <button type="button" class="appearance-slot-preview">
      <svg class="appearance-slot-thumbnail" viewBox="0 0 72 54" aria-hidden="true">
        <rect class="slot-window" x="2" y="2" width="68" height="50" rx="7"/>
        <path class="slot-divider" d="M2 14h68"/>
        <rect class="slot-panel" x="9" y="21" width="30" height="24" rx="3"/>
        <rect class="slot-panel" x="44" y="21" width="19" height="24" rx="3"/>
        <rect class="slot-accent" x="48" y="36" width="11" height="4" rx="2"/>
      </svg>
      <span class="appearance-slot-add" aria-hidden="true"><span>+</span><small>Добавить</small></span>
    </button>
    <div class="appearance-slot-tools"><input type="text" maxlength="24" autocomplete="off" spellcheck="false"><button type="button" data-store><svg viewBox="0 0 20 20" aria-hidden="true"><path d="M4 3h10l3 3v11H3V3h1Z M6 3v5h7V3 M6 17v-5h8v5"/></svg></button><button type="button" data-clear>×</button></div>
   `;
   const preview = card.querySelector('.appearance-slot-preview');
   const name = card.querySelector('input');
   const store = card.querySelector('[data-store]');
   const clear = card.querySelector('[data-clear]');
   const number = index + 1;
   name.value = slot.name;
   name.placeholder = 'Пустой слот';
   name.setAttribute('aria-label', 'Название слота ' + number);
   const previewLabel = slot.name ? (number === activeSlot ? 'Отключить «' : 'Применить «') + slot.name + '»' : 'Сохранить оформление в слот ' + number;
   preview.setAttribute('aria-label', previewLabel);
   store.setAttribute('aria-label', 'Записать текущий вид в слот ' + number);
   clear.setAttribute('aria-label', 'Очистить слот ' + number);
   if (!slot.name) {
    preview.classList.add('appearance-slot-empty');
    clear.dataset.unavailable = '';
   } else {
    const {settings} = slot;
    const [canvas, panel] = palettePreviews[settings.theme] || [defaults.canvasColor, defaults.panelColor];
    card.style.setProperty('--slot-canvas', settings.canvasColor === defaults.canvasColor ? canvas : settings.canvasColor);
    card.style.setProperty('--slot-panel', settings.panelColor === defaults.panelColor ? panel : settings.panelColor);
    card.style.setProperty('--slot-accent', settings.accent);
    renderThemeIcon(preview, slot);
   }
   const applySlot = () => runAction(async () => {
    const disable = index + 1 === activeSlot;
    await api.SetActiveAppearanceSlot(disable ? -1 : index);
    await refreshLibrary(true);
    return disable ? 'Оформление отключено.' : 'Применено оформление «' + slot.name + '».';
   });
   store.onclick = () => {
    if (sourceFailed || fontFailed || fontLoading || !form.reportValidity()) return;
    void runAction(async () => {
     const label = name.value.trim() || 'Оформление ' + number;
     await api.SaveAppearanceSlot(index, label, read());
     await refreshLibrary();
     return 'Текущий вид сохранён в слот ' + number + '.';
    });
   };
   preview.onclick = slot.name ? applySlot : store.onclick;
   clear.onclick = () => runAction(async () => {
    await api.ClearAppearanceSlot(index);
    await refreshLibrary();
    return 'Слот ' + number + ' очищен.';
   });
   name.addEventListener('keydown', event => {
    if (event.key === 'Enter') {event.preventDefault();store.click()}
   });
   slotList.append(card);
  });
  markCurrentSlot();
 }

 async function runAction(action) {
  if (!saved || busy) return;
  setBusy(true);
  try {
   const message = await action();
   if (message) {
    status.textContent = message;
    toast(message);
   }
  } catch (error) {
   status.textContent = String(error);
   toast(String(error));
  } finally {
   setBusy(false);
  }
 }

 chooseFont.onclick = () => runAction(async () => {
  const selected = await api.ChooseAppearanceFont();
  if (!selected) return;
  fill({...read(), font: 'custom', fontFile: selected});
  return 'Шрифт загружен. Сохрани оформление или запиши его в слот.';
 });
 removeFont.onclick = () => {
  fill({...read(), font: 'default', fontFile: ''});
  setBusy(false);
  status.textContent = 'Импортированный шрифт убран.';
 };

 importButton.onclick = () => runAction(async () => {
  if (!await api.ImportAppearanceSlot()) return;
  await refreshLibrary();
  return 'Оформление добавлено в библиотеку.';
 });
 document.querySelector('#appearance-manage').onclick = () => window.SintetWorkshop?.openThemes();
 exportButton.onclick = () => {
  if (sourceFailed || fontFailed || fontLoading || !form.reportValidity()) return;
  void runAction(async () => {
   status.textContent='Подготавливаем файл оформления…';
   const exported=await api.ExportAppearance(read());
   await refreshLibrary();
   return exported?'Оформление экспортировано.':'';
  });
 };

 const ready = (async () => {
  await window.SintetSettings.ready;
  api = window.go?.main?.App;
  if (!api) {
   status.textContent = 'Запусти Sintet через Start.bat.';
   return;
  }
  try {
   defaults = await api.DefaultAppearance();
   await refreshLibrary(true);
   await waveLoad;
   setBusy(false);
   status.textContent = 'Изменения видны сразу';
  } catch (error) {
   status.textContent = String(error);
  }
 })();
 window.runtime?.EventsOn?.('sintet:catalog-themes',()=>void ready.then(()=>refreshLibrary()).catch(()=>{}));
 window.SintetAppearance = {ready, refreshLibrary, renderThemeIcon, getLibrary: () => ({current: saved, slots, activeSlot})};
})();
