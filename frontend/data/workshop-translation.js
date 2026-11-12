const storageKey = 'sintet.workshop.translations.v1';
const language = () => window.SintetSettings?.config?.settings.workshopLanguage || '';
const currentRequests = new Map();
let cache = new Map();
try { cache = new Map(JSON.parse(localStorage.getItem(storageKey) || '[]')); } catch {}

let active = 0;
const queue = [];

function drain() {
  while (active < 2 && queue.length) {
    const job = queue.shift();
    if (!job.consumers.some(isCurrent => isCurrent())) {
      job.resolve(job.original);
      continue;
    }
    active++;
    window.go.main.App.TranslateWorkshopDescription(job.original, job.target)
      .then(result => {
        cache.set(job.key, result);
        while (cache.size > 256) cache.delete(cache.keys().next().value);
        try { localStorage.setItem(storageKey, JSON.stringify([...cache])); } catch {}
        job.resolve(result);
      }, job.reject)
      .finally(() => { active--; drain(); });
  }
}

function translate(original, target, isCurrent) {
  const key = `${target}\n${original}`;
  if (cache.has(key)) return Promise.resolve(cache.get(key));
  const existing = currentRequests.get(key);
  if (existing) {
    existing.consumers.push(isCurrent);
    return existing.promise;
  }
  const consumers = [isCurrent];
  const promise = new Promise((resolve, reject) => {
    queue.push({original, target, consumers, key, resolve, reject});
    drain();
  }).finally(() => currentRequests.delete(key));
  currentRequests.set(key, {promise, consumers});
  return promise;
}

const pending = new WeakMap();
const visible = new IntersectionObserver(entries => {
  for (const entry of entries) {
    if (!entry.target.isConnected) {
      visible.unobserve(entry.target);
      continue;
    }
    if (!entry.isIntersecting) continue;
    visible.unobserve(entry.target);
    const original = pending.get(entry.target);
    const target = language();
    if (!original || !target || window.SintetNetwork?.offline) continue;
    const isCurrent = () => entry.target.isConnected && target === language();
    void translate(original, target, isCurrent).then(result => {
      if (isCurrent() && result !== original) {
        entry.target.textContent = result;
        entry.target.title = original;
      }
    }).catch(() => {});
  }
});

export function catalogDescription(original, fallback = '') {
  const paragraph = document.createElement('p');
  paragraph.textContent = original || fallback;
  if (original && language()) {
    pending.set(paragraph, original);
    visible.observe(paragraph);
  }
  return paragraph;
}

let detailRevision = 0;
export function showCatalogDescription(paragraph, original) {
  const revision = ++detailRevision;
  paragraph.textContent = original;
  const controls = paragraph.nextElementSibling?.classList.contains('ws-translation-controls')
    ? paragraph.nextElementSibling : document.createElement('div');
  controls.className = 'ws-translation-controls';
  controls.replaceChildren();
  paragraph.after(controls);
  const target = language();
  if (!target || !original || window.SintetNetwork?.offline) return;
  const status = document.createElement('small');
  status.textContent = 'Переводим описание…';
  controls.append(status);
  const isCurrent = () => revision === detailRevision && target === language() && paragraph.closest('dialog')?.open;
  // The caller opens the dialog in the same frame as setting its first description.
  queueMicrotask(async () => {
    try {
      const result = await translate(original, target, isCurrent);
      if (!isCurrent()) return;
      controls.replaceChildren();
      if (result === original) return;
      paragraph.textContent = result;
      const button = document.createElement('button');
      button.type = 'button';
      let translated = true;
      button.textContent = 'Показать оригинал';
      button.onclick = () => {
        translated = !translated;
        paragraph.textContent = translated ? result : original;
        button.textContent = translated ? 'Показать оригинал' : 'Показать перевод';
      };
      controls.append(button);
    } catch (error) {
      if (isCurrent()) status.textContent = String(error?.message || error);
    }
  });
}
