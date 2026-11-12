/* Shared single-select control. Native select remains the value/event API. */
(() => {
  const instances = new WeakMap();
  const controls = new Set();
  let active = null;
  let nextId = 0;
  const arrow = '<svg viewBox="0 0 16 16" aria-hidden="true"><path d="m4 6 4 4 4-4"/></svg>';
  class SelectControl {
    constructor(source) {
      this.source = source;
      controls.add(this);
      this.index = -1;
      this.search = '';
      this.searchTime = 0;
      this.wrapper = document.createElement('div');
      this.wrapper.className = 's-select';
      source.before(this.wrapper);
      this.wrapper.append(source);
      source.hidden = true;
      this.button = document.createElement('button');
      this.button.type = 'button';
      this.button.className = 's-select-trigger';
      this.button.setAttribute('role', 'combobox');
      this.button.setAttribute('aria-haspopup', 'listbox');
      this.button.setAttribute('aria-expanded', 'false');
      this.text = document.createElement('span');
      this.button.append(this.text);
      this.button.insertAdjacentHTML('beforeend', arrow);
      this.wrapper.append(this.button);
      this.menu = document.createElement('div');
      this.menu.className = 's-select-menu';
      if (source.form?.id) this.menu.dataset.form = source.form.id;
      this.menu.id = `s-select-${++nextId}`;
      this.menu.setAttribute('role', 'listbox');
      this.menu.hidden = true;
      (source.closest('dialog,[popover]') || document.body).append(this.menu);
      this.button.setAttribute('aria-controls', this.menu.id);
      this.button.addEventListener('click', () => active === this ? this.close() : this.open());
      this.button.addEventListener('keydown', event => this.keydown(event));
      this.button.addEventListener('blur', () => this.close());
      this.menu.addEventListener('pointerdown', event => event.preventDefault());
      source.addEventListener('change', () => this.refresh());
      source.form?.addEventListener('reset', () => setTimeout(() => this.refresh()));
      this.observer = new MutationObserver(() => this.refresh());
      this.observer.observe(source, {subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ['disabled', 'selected', 'label', 'value', 'aria-label']});
      this.refresh();
    }
    refresh() {
      const source = this.source;
      this.options = Array.from(source.options);
      this.button.disabled = source.disabled;
      const t = window.SintetText.translate;
      const labels = Array.from(source.labels || []).map(label => {
        const copy = label.cloneNode(true);
        for (const control of copy.querySelectorAll('.s-select,select')) control.remove();
        return t(copy.textContent.trim());
      });
      this.button.setAttribute('aria-label', t(source.getAttribute('aria-label') || labels.join(' ') || 'Выбор'));
      this.text.textContent = t(source.selectedOptions[0]?.label || 'Выбрать');
      this.menu.replaceChildren();
      this.items = this.options.map((option, index) => {
        const item = document.createElement('div');
        item.id = `${this.menu.id}-${index}`;
        item.className = 's-select-option';
        item.setAttribute('role', 'option');
        item.setAttribute('aria-selected', String(index === source.selectedIndex));
        item.setAttribute('aria-disabled', String(this.disabled(index)));
        const label = document.createElement('span');
        label.textContent = option.label;
        const check = document.createElement('span');
        check.className = 's-select-check';
        check.textContent = '✓';
        check.setAttribute('aria-hidden', 'true');
        item.append(label, check);
        item.addEventListener('pointermove', () => { if (!this.disabled(index)) this.highlight(index); });
        item.addEventListener('click', () => this.choose(index));
        this.menu.append(item);
        return item;
      });
      if (active === this) {
        if (source.disabled) this.close();
        else { this.position(); this.highlight(this.validIndex(source.selectedIndex)); }
      }
    }
    disabled(index) { const option = this.options[index]; return !option || option.disabled || (option.parentElement.tagName === 'OPTGROUP' && option.parentElement.disabled); }
    validIndex(preferred) { return !this.disabled(preferred) ? preferred : this.options.findIndex((_, index) => !this.disabled(index)); }
    open() {
      if (this.source.disabled) return;
      active?.close();
      this.refresh();
      active = this;
      this.menu.hidden = false;
      this.button.setAttribute('aria-expanded', 'true');
      this.position();
      this.highlight(this.validIndex(this.source.selectedIndex));
    }
    close() {
      this.menu.hidden = true;
      this.button.setAttribute('aria-expanded', 'false');
      this.button.removeAttribute('aria-activedescendant');
      if (active === this) active = null;
    }
    position() {
      const rect = this.button.getBoundingClientRect();
      if (!this.button.getClientRects().length) { this.close(); return; }
      const margin = 10, gap = 7;
      const below = innerHeight - rect.bottom - margin - gap;
      const above = rect.top - margin - gap;
      const up = below < Math.min(this.menu.scrollHeight, 240) && above > below;
      const width = Math.min(Math.max(rect.width, 200), innerWidth - margin * 2);
      // A transformed/acrylic dialog establishes a fixed-position containing
      // block. Keep the popup in the top layer, but compensate its origin.
      const host = this.menu.parentElement;
      let originX = 0, originY = 0;
      if (host !== document.body) {
        const style = getComputedStyle(host);
        if (style.transform !== 'none' || style.filter !== 'none' || style.backdropFilter && style.backdropFilter !== 'none' || style.perspective !== 'none') {
          const hostRect = host.getBoundingClientRect();
          originX = hostRect.left + host.clientLeft - host.scrollLeft;
          originY = hostRect.top + host.clientTop - host.scrollTop;
        }
      }
      this.menu.style.width = `${width}px`;
      this.menu.style.maxHeight = `${Math.max(0, Math.min(260, up ? above : below))}px`;
      this.menu.style.left = `${Math.max(margin, Math.min(rect.left, innerWidth - width - margin)) - originX}px`;
      this.menu.style.top = `${(up ? rect.top - gap - this.menu.offsetHeight : rect.bottom + gap) - originY}px`;
      this.menu.dataset.direction = up ? 'up' : 'down';
    }
    highlight(index) {
      this.index = index;
      this.items.forEach((item, i) => item.classList.toggle('is-focused', i === index));
      if (this.items[index]) {
        this.button.setAttribute('aria-activedescendant', this.items[index].id);
        this.items[index].scrollIntoView({block: 'nearest'});
      }
    }
    move(direction) {
      for (let step = 1; step <= this.options.length; step++) {
        const index = (this.index + direction * step + this.options.length) % this.options.length;
        if (!this.disabled(index)) { this.highlight(index); return; }
      }
    }
    choose(index) {
      if (this.disabled(index)) return;
      const changed = this.source.selectedIndex !== index;
      this.source.selectedIndex = index;
      this.close();
      this.refresh();
      this.button.focus();
      if (changed) {
        this.source.dispatchEvent(new Event('input', {bubbles: true}));
        this.source.dispatchEvent(new Event('change', {bubbles: true}));
      }
    }
    keydown(event) {
      const open = active === this;
      if (event.key === 'Tab') { this.close(); return; }
      if (event.key === 'Escape') { if (open) { event.preventDefault(); event.stopPropagation(); this.close(); } return; }
      if (['Enter', ' ', 'ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
        event.preventDefault();
        if (!open) { this.open(); if (event.key !== 'Home' && event.key !== 'End') return; }
        if (event.key === 'Enter' || event.key === ' ') this.choose(this.index);
        if (event.key === 'ArrowDown') this.move(1);
        if (event.key === 'ArrowUp') this.move(-1);
        if (event.key === 'Home') this.highlight(this.validIndex(0));
        if (event.key === 'End') this.highlight(this.options.findLastIndex((_, index) => !this.disabled(index)));
      } else if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
        event.preventDefault();
        if (!open) this.open();
        const now = Date.now();
        this.search = (now - this.searchTime > 700 ? '' : this.search) + event.key.toLocaleLowerCase();
        this.searchTime = now;
        const index = this.options.findIndex((option, i) => !this.disabled(i) && option.label.toLocaleLowerCase().startsWith(this.search));
        if (index >= 0) this.highlight(index);
      }
    }
    destroy() { this.close(); this.observer.disconnect(); this.menu.remove(); this.wrapper.remove(); instances.delete(this.source); }
  }
  function mount(root = document) {
    const selects = root.matches?.('select') ? [root] : Array.from(root.querySelectorAll?.('select') || []);
    selects.forEach(source => { if (!source.multiple && !instances.has(source)) instances.set(source, new SelectControl(source)); });
  }
  mount();
  new MutationObserver(records => {
    records.forEach(record => record.addedNodes.forEach(node => { if (node.nodeType === 1) mount(node); }));
    if (active && !active.source.isConnected) active.close();
    controls.forEach(control => { if (!control.source.isConnected) { control.destroy(); controls.delete(control); } });
  }).observe(document.body, {childList: true, subtree: true});
  document.addEventListener('pointerdown', event => {
    if (active && !active.wrapper.contains(event.target) && !active.menu.contains(event.target)) active.close();
  });
  document.addEventListener('click', event => {
    const label = event.target.closest('label');
    const control = label?.control && instances.get(label.control);
    if (control) { event.preventDefault(); control.button.focus(); }
  });
  document.addEventListener('scroll', event => { if (active && !active.menu.contains(event.target)) active.position(); }, true);
  window.addEventListener('resize', () => active?.close());
  // Programmatic select.value writes can be followed by refresh or a change event.
  window.SintetUI = Object.freeze({select: Object.freeze({mount, refresh(source) { instances.get(source)?.refresh(); }})});
})();
