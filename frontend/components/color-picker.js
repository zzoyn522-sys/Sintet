(() => {
 const picker = document.createElement('div');
 picker.id = 'sintet-color-picker';
 picker.className = 'color-picker';
 picker.popover = 'auto';
 picker.setAttribute('role', 'dialog');
 picker.setAttribute('aria-label', window.SintetText?.translate('Цвет') || 'Цвет');
 picker.innerHTML = `
  <div class="color-picker-plane" aria-hidden="true"><i></i></div>
  <div class="color-picker-toolbar">
   <button type="button" class="color-picker-dropper" aria-label="Пипетка">
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="m8 6 6 6M9 7l5-5a2 2 0 0 1 3 3l-5 5m-2-2-6 6-1 3 3-1 6-6"/></svg>
   </button>
   <span class="color-picker-preview" aria-hidden="true"></span>
   <input class="color-picker-hue" type="range" min="0" max="360" step="any" aria-label="Оттенок">
  </div>
  <div class="color-picker-values">
   <div class="color-picker-channels"></div>
   <select data-native-select aria-label="Формат цвета"><option>RGB</option><option>HEX</option></select>
  </div>`;
 document.body.append(picker);

 const plane = picker.querySelector('.color-picker-plane');
 const hue = picker.querySelector('.color-picker-hue');
 const dropper = picker.querySelector('button');
 const mode = picker.querySelector('select');
 const channels = picker.querySelector('.color-picker-channels');
 const clamp = value => Math.min(1, Math.max(0, value));
 let source;
 let saturation = 0;
 let brightness = 1;
 let current = '#ffffff';
 let pointer = null;

 function rgb(value) {
  return value.slice(1).match(/../g).map(pair => parseInt(pair, 16));
 }

 function loadColor(value) {
  const [r, g, b] = rgb(value).map(channel => channel / 255);
  brightness = Math.max(r, g, b);
  const delta = brightness - Math.min(r, g, b);
  saturation = brightness ? delta / brightness : 0;
  if (delta) {
   const sector = brightness === r ? (g - b) / delta : brightness === g ? (b - r) / delta + 2 : (r - g) / delta + 4;
   hue.value = (sector * 60 + 360) % 360;
  }
 }

 function toHex(values) {
  return '#' + values.map(value => value.toString(16).padStart(2, '0')).join('');
 }

 function updateFields() {
  const values = mode.value === 'HEX' ? [current.toUpperCase()] : rgb(current);
  [...channels.children].forEach((input, index) => { input.value = values[index]; });
 }

 function draw() {
  picker.style.setProperty('--picked', current);
  picker.style.setProperty('--hue', hue.value);
  plane.style.setProperty('--x', saturation * 100 + '%');
  plane.style.setProperty('--y', (1 - brightness) * 100 + '%');
  updateFields();
 }

 function apply(value) {
  current = value;
  if (source.value !== value) {
   source.value = value;
   source.dispatchEvent(new Event('input', {bubbles: true}));
   source.dispatchEvent(new Event('change', {bubbles: true}));
  }
  draw();
 }

 function applyPlane() {
  const values = [5, 3, 1].map(offset => {
   const k = (offset + hue.valueAsNumber / 60) % 6;
   return Math.round(255 * brightness * (1 - saturation * Math.max(0, Math.min(k, 4 - k, 1))));
  });
  apply(toHex(values));
 }

 function move(event) {
  if (event.pointerId !== pointer) return;
  const box = plane.getBoundingClientRect();
  saturation = clamp((event.clientX - box.left) / box.width);
  brightness = 1 - clamp((event.clientY - box.top) / box.height);
  applyPlane();
 }
 plane.addEventListener('pointerdown', event => {
  if (event.button !== 0 || pointer !== null) return;
  pointer = event.pointerId;
  plane.setPointerCapture(pointer);
  move(event);
 });
 plane.addEventListener('pointermove', move);
 plane.addEventListener('lostpointercapture', () => { pointer = null; });
 hue.addEventListener('input', applyPlane);

 function editChannels() {
  const inputs = [...channels.children];
  if (mode.value === 'HEX') {
   const value = inputs[0].value.trim().replace(/^#/, '').toLowerCase();
   if (/^[0-9a-f]{6}$/.test(value)) {
    loadColor('#' + value);
    apply('#' + value);
   }
   return;
  }
  if (inputs.every(input => input.value !== '' && input.validity.valid)) {
   const value = toHex(inputs.map(input => input.valueAsNumber));
   loadColor(value);
   apply(value);
  }
 }

 function renderFields() {
  channels.replaceChildren();
  for (const name of mode.value === 'HEX' ? ['HEX'] : ['R', 'G', 'B']) {
   const input = document.createElement('input');
   input.setAttribute('aria-label', name);
   input.spellcheck = false;
   if (name === 'HEX') input.maxLength = 7;
   else {
    input.type = 'number';
    input.min = 0;
    input.max = 255;
   }
   input.addEventListener('change', () => { editChannels(); updateFields(); });
   input.addEventListener('keydown', event => {
    if (event.key === 'Enter') {
     event.preventDefault();
     editChannels();
    }
   });
   channels.append(input);
  }
  updateFields();
 }
 mode.addEventListener('change', renderFields);
 renderFields();

 dropper.hidden = !window.EyeDropper;
 dropper.addEventListener('click', async () => {
  const field = source;
  try {
   const {sRGBHex} = await new EyeDropper().open();
   if (field !== source || !picker.matches(':popover-open')) return;
   loadColor(sRGBHex);
   apply(sRGBHex);
  } catch {
   // Escape cancels sampling.
  }
 });

 picker.addEventListener('beforetoggle', event => {
  if (event.newState === 'closed') source?.setAttribute('aria-expanded', 'false');
 });
 document.addEventListener('keydown', event => {
  if (!event.target.matches('input[type=color]') || !['Enter', ' '].includes(event.key)) return;
  event.preventDefault();
  if (!event.repeat) event.target.click();
 });
 document.addEventListener('click', event => {
  const field = event.target.closest('input[type=color]');
  if (!field || field.disabled) return;
  event.preventDefault();
  if (picker.matches(':popover-open')) {
   picker.hidePopover();
   if (field === source) return;
  }
  source = field;
  current = field.value;
  hue.value = 0;
  loadColor(current);
  draw();
  picker.showPopover({source: field});
  field.setAttribute('aria-expanded', 'true');
  const anchor = field.getBoundingClientRect();
  const {width, height} = picker.getBoundingClientRect();
  picker.style.left = Math.max(8, Math.min(anchor.left, innerWidth - width - 8)) + 'px';
  const preferred = anchor.bottom + height + 16 <= innerHeight ? anchor.bottom + 8 : anchor.top - height - 8;
  picker.style.top = Math.max(8, Math.min(preferred, innerHeight - height - 8)) + 'px';
 });
 for (const field of document.querySelectorAll('input[type=color]')) {
  field.setAttribute('aria-haspopup', 'dialog');
  field.setAttribute('aria-controls', picker.id);
  field.setAttribute('aria-expanded', 'false');
 }
 window.addEventListener('resize', () => {
  if (picker.matches(':popover-open')) picker.hidePopover();
 });
})();
