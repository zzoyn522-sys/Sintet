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

function appendInlineMarkdown(parent, value, depth = 0) {
  if (depth > 5) {
    parent.append(document.createTextNode(value));
    return;
  }
  const tokens = /(!?)\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)|\*\*([\s\S]+?)\*\*|__([\s\S]+?)__|~~([\s\S]+?)~~|`([^`\n]+)`|\*([^*\n]+)\*/g;
  let offset = 0;
  for (const match of value.matchAll(tokens)) {
    const start = match.index ?? 0;
    if (start > offset) parent.append(document.createTextNode(value.slice(offset, start)));
    const [, imageMarker, label, rawURL, bold, boldAlt, strike, code, italic] = match;
    if (rawURL) {
      let url;
      try { url = new URL(rawURL); } catch {}
      if (!url || !['http:', 'https:'].includes(url.protocol) || url.username || url.password) {
        parent.append(document.createTextNode(match[0]));
      } else if (imageMarker) {
        const image = document.createElement('img');
        image.src = url.href;
        image.alt = label;
        image.loading = 'lazy';
        image.referrerPolicy = 'no-referrer';
        parent.append(image);
      } else {
        const anchor = document.createElement('a');
        anchor.href = url.href;
        anchor.target = '_blank';
        anchor.rel = 'noopener noreferrer';
        appendInlineMarkdown(anchor, label, depth + 1);
        parent.append(anchor);
      }
    } else if (bold || boldAlt || strike || italic) {
      const element = document.createElement(strike ? 'del' : italic ? 'em' : 'strong');
      appendInlineMarkdown(element, bold || boldAlt || strike || italic, depth + 1);
      parent.append(element);
    } else if (code) {
      const element = document.createElement('code');
      element.textContent = code;
      parent.append(element);
    }
    offset = start + match[0].length;
  }
  if (offset < value.length) parent.append(document.createTextNode(value.slice(offset)));
}

function markdownCells(line) {
  return line.trim().replace(/^\|/, '').replace(/\|$/, '').split('|').map(cell => cell.trim());
}

function renderMarkdown(container, value) {
  const lines = String(value || '').replace(/\r\n?/g, '\n').split('\n');
  container.replaceChildren();
  let index = 0;
  while (index < lines.length) {
    const line = lines[index];
    if (!line.trim()) { index++; continue; }

    const fence = line.match(/^\s*(```+|~~~+)/);
    if (fence) {
      const codeLines = [];
      index++;
      while (index < lines.length && !new RegExp(`^\\s*${fence[1][0]}{${fence[1].length},}\\s*$`).test(lines[index])) codeLines.push(lines[index++]);
      if (index < lines.length) index++;
      const pre = document.createElement('pre');
      const code = document.createElement('code');
      code.textContent = codeLines.join('\n');
      pre.append(code);
      container.append(pre);
      continue;
    }

    const heading = line.match(/^\s*(#{1,6})\s+(.+?)\s*#*\s*$/);
    if (heading) {
      const element = document.createElement('h3');
      element.dataset.level = String(Math.min(heading[1].length, 3));
      appendInlineMarkdown(element, heading[2]);
      container.append(element);
      index++;
      continue;
    }
    if (/^\s*(?:(?:\*\s*){3,}|(?:-\s*){3,}|(?:_\s*){3,})$/.test(line)) {
      container.append(document.createElement('hr'));
      index++;
      continue;
    }

    const tableRule = index + 1 < lines.length && /^\s*\|?\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(lines[index + 1]);
    if (line.includes('|') && tableRule) {
      const table = document.createElement('table');
      const header = document.createElement('thead');
      const headerRow = document.createElement('tr');
      for (const cellText of markdownCells(line)) {
        const cell = document.createElement('th');
        appendInlineMarkdown(cell, cellText);
        headerRow.append(cell);
      }
      header.append(headerRow);
      table.append(header);
      index += 2;
      const body = document.createElement('tbody');
      while (index < lines.length && lines[index].includes('|') && lines[index].trim()) {
        const row = document.createElement('tr');
        for (const cellText of markdownCells(lines[index])) {
          const cell = document.createElement('td');
          appendInlineMarkdown(cell, cellText);
          row.append(cell);
        }
        body.append(row);
        index++;
      }
      table.append(body);
      container.append(table);
      continue;
    }

    if (/^\s*>/.test(line)) {
      const quote = document.createElement('blockquote');
      const paragraph = document.createElement('p');
      while (index < lines.length && /^\s*>/.test(lines[index])) {
        if (paragraph.childNodes.length) paragraph.append(document.createElement('br'));
        appendInlineMarkdown(paragraph, lines[index++].replace(/^\s*>\s?/, ''));
      }
      quote.append(paragraph);
      container.append(quote);
      continue;
    }

    const listItem = line.match(/^\s*([-+*]|\d+[.)])\s+(.*)$/);
    if (listItem) {
      const ordered = /^\d/.test(listItem[1]);
      const list = document.createElement(ordered ? 'ol' : 'ul');
      while (index < lines.length) {
        const item = lines[index].match(/^\s*([-+*]|\d+[.)])\s+(.*)$/);
        if (!item || /^\d/.test(item[1]) !== ordered) break;
        const li = document.createElement('li');
        appendInlineMarkdown(li, item[2]);
        list.append(li);
        index++;
      }
      container.append(list);
      continue;
    }

    const paragraph = document.createElement('p');
    const parts = [];
    while (index < lines.length && lines[index].trim() && !/^\s*(?:#{1,6}\s|```+|~~~+|>|(?:[-+*]|\d+[.)])\s+)/.test(lines[index])) {
      if (index + 1 < lines.length && /^\s*\|?\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(lines[index + 1]) && lines[index].includes('|')) break;
      if (/^\s*(?:(?:\*\s*){3,}|(?:-\s*){3,}|(?:_\s*){3,})$/.test(lines[index])) break;
      parts.push(lines[index++].trim());
    }
    if (!parts.length) { index++; continue; }
    appendInlineMarkdown(paragraph, parts.join(' '));
    container.append(paragraph);
  }
}

function restoreCollapsedMarkdown(value) {
  const withBlocks = String(value || '')
    .replace(/[ \t]+(#{1,6})[ \t]+(?=\S)/g, '\n\n$1 ')
    .replace(/[ \t]+(---+|___+|\*\*\*+)[ \t]*/g, '\n\n$1\n\n');
  const splitList = line => {
    const marker = /\s+([-+*])\s+(?=[A-Za-zА-Яа-яЁё0-9])/g;
    const matches = [...line.matchAll(marker)];
    if (matches.length < 2) return [line];
    const parts = [];
    const first = matches[0];
    const prefix = line.slice(0, first.index).trim();
    if (prefix) parts.push(prefix);
    for (let i = 0; i < matches.length; i++) {
      const current = matches[i];
      const start = (current.index ?? 0) + current[0].length;
      const end = i + 1 < matches.length ? (matches[i + 1].index ?? line.length) : line.length;
      const content = line.slice(start, end).trim();
      if (content) parts.push(`${current[1]} ${content}`);
    }
    return parts;
  };
  return withBlocks.split('\n').flatMap(splitList).join('\n');
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
  renderMarkdown(paragraph, original);
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
      const formattedResult = restoreCollapsedMarkdown(result);
      renderMarkdown(paragraph, formattedResult);
      const button = document.createElement('button');
      button.type = 'button';
      let translated = true;
      button.textContent = 'Показать оригинал';
      button.onclick = () => {
        translated = !translated;
        renderMarkdown(paragraph, translated ? formattedResult : original);
        button.textContent = translated ? 'Показать оригинал' : 'Показать перевод';
      };
      controls.append(button);
    } catch (error) {
      if (isCurrent()) status.textContent = String(error?.message || error);
    }
  });
}
