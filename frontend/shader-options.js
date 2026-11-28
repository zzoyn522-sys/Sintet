const dialog = document.querySelector('#ws-shader-options');
const q = selector => dialog.querySelector(selector);
const groups = q('.ws-shader-groups');
const tools = q('.ws-shader-tools');
const layout = q('.ws-shader-layout');
const navigation = q('#ws-shader-sections');
let activeGroup = 0;
const search = q('#ws-shader-search');
const preset = q('#ws-shader-preset');
const status = q('#ws-shader-status');
const save = q('#ws-shader-save');
const reset = q('#ws-shader-reset');
const close = q('#ws-shader-close');
const entries = new Map();
let editor, request = 0, busy = false, language = 'en';

function element(tag, text) {
 const node = document.createElement(tag);
 if (text !== undefined) node.textContent = text;
 return node;
}

const codes = /([§&])(?:x((?:[§&][0-9a-f]){6})|#([0-9a-f]{6})|([0-9a-fgk-or]))/gi;
const colors = ['#6a6a6a','#7373ff','#55cf70','#56c8cc','#ff6969','#dc7ee9','#ffc164','#c4c4c4','#919191','#8998ff','#88ef8f','#83ecef','#ff8585','#ff99e5','#ffef8a','#fff'];
const cacheKey = 'sintet.shader-translations.v2';
const translated = new Map();
try {
 const stored = JSON.parse(localStorage.getItem(cacheKey) || '[]');
 for (const [key, value] of stored.slice(-5000)) {
  if (typeof key === 'string' && typeof value === 'string') translated.set(key, value);
 }
} catch { /* The editor can run without persistent storage. */ }

const translationKey = (language, text) => language + '\n' + text;
const isTranslatable = (language, text) => text.length >= 2 && text.length <= 1500 &&
 /\p{L}/u.test(text) && !((language === 'ru' || language === 'uk') && /[а-яёіїєґ]/i.test(text));

function shaderSegments(raw, unformatted) {
 const value = String(raw ?? '');
 if (!value.match(codes)) return [unformatted(value)];
 return value.split(/(?:[§&])(?:x(?:[§&][0-9a-f]){6}|#[0-9a-f]{6}|[0-9a-fgk-or])/gi);
}

async function prepareTranslations(session, schema, progress) {
 const {language, api} = session;
 if (language === 'original' || language === 'en' || !api.TranslateShaderLabel) return false;
 const titles = [], descriptions = [];
 for (const group of schema.groups || []) {
  for (const segment of String(group.name || '').split(' / ')) titles.push([segment, shaderLabel]);
  for (const option of group.options) {
   titles.push([option.name || option.id, shaderLabel]);
   if (!option.toggle) {
    for (const value of option.choices) titles.push([option.labels?.[value] || value, shaderLabel]);
   }
   if (option.description) descriptions.push([option.description, shaderText]);
  }
 }
 for (const profile of schema.profiles || []) titles.push([profile.name, shaderLabel]);
 const missing = new Set();
 for (const [raw, transform] of [...titles, ...descriptions]) {
  for (const part of shaderSegments(raw, transform)) {
   const text = part.trim();
   if (isTranslatable(language, text) && !translated.has(translationKey(language, text))) missing.add(text);
  }
 }
 const pending = [...missing];
 if (!pending.length) return false;
 let cursor = 0, finished = 0, serviceFailed = false;
 const store = (text, result) => {
  const clean = shaderText(result);
  if (clean && clean !== text) translated.set(translationKey(language, text), clean);
 };
 const persist = () => {
  while (translated.size > 5000) translated.delete(translated.keys().next().value);
  try { localStorage.setItem(cacheKey, JSON.stringify([...translated])); } catch { /* Storage can be full. */ }
 };
 const encoder = new TextEncoder();
 function nextBatch() {
  const batch = [pending[cursor++]];
  let size = encoder.encode(batch[0]).length + 8;
  while (cursor < pending.length && batch.length < 36) {
   const next = encoder.encode(pending[cursor]).length + 8;
   if (size + next > 480) break;
   batch.push(pending[cursor++]);
   size += next;
  }
  return batch;
 }
 async function worker() {
  while (cursor < pending.length && !serviceFailed && !session.skip && editor === session && dialog.open) {
   const batch = nextBatch();
   if (batch.length === 1) {
    try { store(batch[0], await api.TranslateShaderLabel(batch[0], language)); }
    catch { serviceFailed = true; }
   } else {
    try {
     const payload = batch.map((text, index) => '⟦' + index + '⟧ ' + text).join('\n');
     const answer = await api.TranslateShaderLabel(payload, language);
     const matches = [...answer.matchAll(/⟦\s*(\d+)\s*⟧/g)];
     if (matches.length === batch.length && matches.every((match, index) => Number(match[1]) === index)) {
      matches.forEach((match, index) => store(batch[index], answer.slice(match.index + match[0].length, matches[index + 1]?.index).trim()));
     } else {
      for (const text of batch) {
       if (session.skip || editor !== session || !dialog.open) break;
       store(text, await api.TranslateShaderLabel(text, language));
      }
     }
    } catch { serviceFailed = true; }
   }
   finished += batch.length;
   if (editor === session && dialog.open) progress(finished, pending.length);
   persist();
  }
 }
 progress(0, pending.length);
 await Promise.all([worker(), worker()]);
 return serviceFailed;
}

function paint(target, raw, anchor = target, transform = shaderLabel) {
 target.replaceChildren();
 let style = {};
 let end = 0;
 const put = part => {
  if (!part) return;
  const span = element('span');
  Object.assign(span.style, style);
  span.textContent = part;
  target.append(span);
  const result = translated.get(translationKey(editor?.language, part.trim()));
  if (result) span.textContent = part.replace(part.trim(), () => result);
 };
 const value = String(raw ?? '');
 if (!value.match(codes)) {
  put(transform(value));
  return;
 }
 for (const match of value.matchAll(codes)) {
  put(value.slice(end, match.index));
  const [, , hexRun, hex, marker] = match;
  const code = marker?.toLowerCase();
  if (hex || hexRun) style = {color:'#' + (hex || hexRun.replace(/[§&]/g, ''))};
  else if (code === 'r') style = {};
  else if (/^[0-9a-f]$/.test(code)) style = {color:colors[parseInt(code,16)]};
  else if (code === 'g') style = {color:'#e9cf58'};
  else if (code === 'l') style.fontWeight = '700';
  else if (code === 'o') style.fontStyle = 'italic';
  else if (code === 'n') style.textDecoration = 'underline';
  else if (code === 'm') style.textDecoration = 'line-through';
  end = match.index + match[0].length;
 }
 put(value.slice(end));
}

function shaderText(value) {
 return String(value ?? '')
  .replace(codes, '')
  .replace(/[ \t]+/g, ' ')
  .trim();
}

const russianShaderLabels = {
 'Sky coefficients':'Коэффициенты неба',
 'Fog coefficients':'Коэффициенты тумана',
 'Rayleigh scattering':'Рассеяние Рэлея',
 'Mie scattering':'Рассеяние Ми',
 'Mie phase anisotropy':'Анизотропия рассеяния Ми',
 'Shadows':'Тени', 'Shadow distance':'Дальность теней',
 'Lighting':'Освещение', 'Direct light':'Прямое освещение',
 'Ambient light':'Рассеянный свет', 'Post processing':'Постобработка',
 'Reflections':'Отражения', 'Clouds':'Облака', 'Fog':'Туман',
 'Water':'Вода', 'Performance':'Производительность', 'Quality':'Качество',
 'Low':'Низкое', 'Medium':'Среднее', 'High':'Высокое',
 'Very high':'Очень высокое', 'Ultra':'Ультра', 'Off':'Выкл.', 'On':'Вкл.'
};
const ruLabels = new Map(Object.entries(russianShaderLabels).map(([key,value]) => [key.toLowerCase(),value]));

function shaderLabel(value) {
 const clean = shaderText(value);
 if (!clean) return clean;
 const readable = clean.includes('_') && !clean.includes(' ')
  ? clean.replace(/_+/g, ' ').replace(/([a-z])([A-Z])/g, '$1 $2')
  : clean;
 return editor?.language === 'ru' ? ruLabels.get(readable.toLowerCase()) || readable : readable;
}

function updateActions() {
 const values = [...entries.values()];
 save.disabled = busy || !values.some(entry => entry.value !== entry.saved);
 reset.disabled = busy || !values.some(entry => entry.value !== entry.option.default);
}

function change(entry, value) {
 entry.value = value;
 for (const refresh of entry.controls) refresh(value);
 updateActions();
}

function field(option) {
 const row = element('label');
 row.className = 'ws-shader-option';
 const name = shaderLabel(option.name) || option.id;
 const description = shaderText(option.description);
 row.dataset.search = [option.id, name, description].join(' ').toLocaleLowerCase();
 const text = element('span');
 text.dataset.noTranslate = '';
 const title = element('strong');
 paint(title, option.name || option.id, row);
 text.append(title);
 if (description) {
  const detail = element('small');
  paint(detail, option.description, row, shaderText);
  text.append(detail);
 }
 row.append(text);
 let entry = entries.get(option.id);
 if (!entry) {
  entry = {option, value:option.value, saved:option.value, controls:[]};
  entries.set(option.id, entry);
 }
 const control = element(option.toggle || option.slider ? 'input' : 'select');
 control.setAttribute('aria-label', name);
 const label = value => shaderLabel(option.labels?.[value] || value);
 if (option.toggle) {
  row.classList.add('check-label');
  control.type = 'checkbox';
  control.onchange = () => change(entry, String(control.checked));
  entry.controls.push(value => {control.checked = value === 'true';});
  row.append(control);
 } else if (option.slider) {
  const slider = element('span');
  slider.className = 'ws-shader-slider';
  const output = element('output');
  output.dataset.noTranslate = '';
  control.type = 'range';
  control.min = '0';
  control.max = String(option.choices.length - 1);
  control.oninput = () => change(entry, option.choices[control.valueAsNumber]);
  entry.controls.push(value => {
   control.value = String(option.choices.indexOf(value));
   control.setAttribute('aria-valuetext', label(value));
   paint(output, option.labels?.[value] || value, row);
  });
  slider.append(control, output);
  row.append(slider);
 } else {
  control.dataset.compactSelect = '';
  for (const value of option.choices) {
   const choice = element('option', label(value));
   choice.value = value;
   choice.dataset.noTranslate = '';
   const raw = option.labels?.[value] || value;
   choice.sintetRenderLabel = target => paint(target, raw, target);
   control.append(choice);
  }
  control.onchange = () => change(entry, control.value);
  entry.controls.push(value => {
   control.value = value;
   window.SintetUI?.select.refresh(control);
  });
  row.append(control);
 }
 entry.controls.at(-1)(entry.value);
 return row;
}

function filter() {
 const term = search.value.trim().toLocaleLowerCase();
 let found = false;
 [...groups.children].forEach((section, index) => {
  let visible = false;
  for (const row of section.querySelectorAll('.ws-shader-option')) {
   row.hidden = !row.dataset.search.includes(term) && !row.textContent.toLocaleLowerCase().includes(term);
   visible ||= !row.hidden;
  }
  section.hidden = !visible || (!term && index !== activeGroup);
  found ||= !section.hidden;
  navigation.children[index].toggleAttribute('data-selected', index === activeGroup && !term);
  if (index === activeGroup && !term) navigation.children[index].setAttribute('aria-current', 'true');
  else navigation.children[index].removeAttribute('aria-current');
 });
 status.textContent = found ? '' : 'Параметры не найдены.';
}

search.oninput = filter;
preset.onchange = () => {
 const profile = preset.value !== '' && editor?.schema?.profiles?.[Number(preset.value)];
 if (profile) {
  for (const [id, value] of Object.entries(profile.values)) {
   const entry = entries.get(id);
   if (entry) change(entry, value);
  }
 }
 preset.value = '';
 window.SintetUI?.select.refresh(preset);
};
reset.onclick = () => {
 for (const entry of entries.values()) change(entry, entry.option.default);
};
close.onclick = () => dialog.close();
dialog.addEventListener('cancel', event => {
 if (busy) event.preventDefault();
});
dialog.addEventListener('close', () => {
 if (!dialog.open) ++request;
});
const errorText = error => String(error?.message || error).replace(/^Error:\s*/, '');

save.onclick = async () => {
 if (busy || !editor?.schema) return;
 const {api, target, filename, schema} = editor;
 const changes = Object.fromEntries([...entries].filter(([, entry]) => entry.value !== entry.saved).map(([id, entry]) => [id, entry.value]));
 if (!Object.keys(changes).length) return;
 busy = close.disabled = tools.inert = layout.inert = true;
 updateActions();
 status.textContent = 'Применяем настройки шейдера…';
 try {
  const result = await api.SaveLocalShaderOptions(target, filename, schema.revision, changes);
  dialog.close();
  const messages = {
   applied: 'Настройки шейдера применены в Minecraft',
   inactive: 'Настройки сохранены. Включи этот шейдер в Iris.',
   unsupported: 'Настройки сохранены. Горячее применение доступно в Iris.',
   unavailable: 'Настройки сохранены. Игровой мост недоступен.',
   failed: 'Настройки сохранены, но Iris не смог перезагрузить шейдер.',
   unconfirmed: 'Настройки сохранены. Minecraft пока не подтвердил применение.',
  };
  window.toast?.(messages[result] || 'Настройки шейдера сохранены');
 } catch (error) {
  status.textContent = errorText(error);
 } finally {
  busy = close.disabled = tools.inert = layout.inert = false;
  updateActions();
 }
};

window.addEventListener('sintet:settings', event => {
 language = event.detail.config.interfaceLanguage;
 if (!['en','ru','uk','pt','es'].includes(language)) language = document.documentElement.lang;
});

export async function openShaderOptions(api, context, item) {
 if (dialog.open || busy) return;
 const id = ++request;
 editor = {api, target:context.target, filename:item.filename, language:window.SintetSettings?.config?.settings?.shaderLanguage || language};

 entries.clear();
 groups.replaceChildren();
 navigation.replaceChildren(); layout.hidden = true; activeGroup = 0;
 search.value = '';
 tools.hidden = true;
 updateActions();
 q('#ws-shader-pack').textContent = item.name + ' · ' + context.label;
 status.textContent = 'Читаем параметры шейдера…';
 q('#ws-shader-preparation').hidden = true;
 q('#ws-shader-translation-note').hidden = true;
 dialog.showModal();
 try {
  const schema = await api.LocalShaderOptions(context.target, item.filename, editor.language);
  if (id !== request || !dialog.open) return;
  editor.schema = schema;
  if (!schema.groups?.length) {
   status.textContent = 'В этом шейдере не найдены доступные для изменения параметры.';
   return;
  }
  const session = editor;
  const preparation = q('#ws-shader-preparation');
  const progress = q('#ws-shader-progress');
  let skip;
  const skipped = new Promise(resolve => {skip = resolve;});
  q('#ws-shader-skip-translation').onclick = () => {session.skip = true; skip(false);};
  const limited = await Promise.race([skipped, prepareTranslations(session, schema, (done, total) => {
   if (session.skip) return;
   preparation.hidden = false;
   status.textContent = 'Готовим перевод настроек шейдера…';
   progress.max = total;
   progress.value = done;
  })]);
  if (id !== request || !dialog.open) return;
  preparation.hidden = true;
  const notice = q('#ws-shader-translation-note');
  notice.hidden = !limited && !session.skip;
  notice.textContent = 'Часть строк показана без перевода. Готовые переводы сохранены.';
  schema.groups.forEach((group, index) => {
   const trail = String(group.name || '').split(' / ').filter(Boolean);
   const title = trail.pop() || 'Основное';
   const parent = trail.join(' / ');
   const section = element('section');
   const heading = element('h3');
   paint(heading, title, heading);
   const count = element('span', String(group.options.length));
   const button = element('button');
   const navLabel = element('span');
   paint(navLabel, title, button);
   navLabel.className = 'ws-shader-nav-label';
   button.append(navLabel);
   if (parent) button.setAttribute('aria-label', shaderText(parent + ' / ' + title));
   button.type = 'button';
   if (group.name) heading.dataset.noTranslate = button.dataset.noTranslate = '';
   count.className = 'ws-shader-count';
   button.append(count);
   button.onclick = () => {
    activeGroup = index;
    search.value = '';
    groups.scrollTop = 0;
    filter();
   };
   navigation.append(button);
   if (parent) {
    const breadcrumb = element('p');
    paint(breadcrumb, parent, breadcrumb);
    breadcrumb.className = 'ws-shader-breadcrumb';
    breadcrumb.dataset.noTranslate = '';
    section.append(breadcrumb);
   }
   section.append(heading, ...group.options.map(field));
   groups.append(section);
  });
  groups.scrollTop = 0;
  const profiles = schema.profiles || [];
  q('#ws-shader-preset-label').hidden = !profiles.length;
  const placeholder = element('option', 'Выбрать');
  placeholder.value = '';
  preset.replaceChildren(placeholder, ...profiles.map((profile, index) => {
   const option = element('option', shaderLabel(profile.name));
   option.value = String(index);
   option.dataset.noTranslate = '';
   option.sintetRenderLabel = target => paint(target, profile.name, target);
   return option;
  }));
  window.SintetUI?.select.refresh(preset);
  tools.hidden = layout.hidden = false;
  filter();
 } catch (error) {
  if (id === request && dialog.open) status.textContent = errorText(error);
 }
}
