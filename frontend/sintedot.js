(() => {
  const dialog = document.querySelector('#sintedot-dialog');
  const q = name => document.querySelector('#sintedot-' + name);
  const actions = [...dialog.querySelectorAll('[data-sintedot]')];
  const optionIDs = ['preview', 'stable', 'configs'];
  let saved = {};
  try { saved = JSON.parse(localStorage.getItem('sintet.sintedot.options')) || {}; } catch {}
  for (const id of optionIDs) {
    if (id !== 'preview' && typeof saved[id] === 'boolean') q(id).checked = saved[id];
    q(id).onchange = () => {
      if (id !== 'preview') {
        saved[id] = q(id).checked;
        try { localStorage.setItem('sintet.sintedot.options', JSON.stringify(saved)); } catch {}
      }
      updateControls();
    };
  }
  let current, task, result, mode = 'repair', canRollback = false, polling = false;
  const app = () => window.go?.main?.App;
  const warning = state => !!(state.error || state.issues?.length || state.conflicts?.length);

  function list(id, rows = []) {
    const section = q(id);
    const items = (rows || []).map(row => {
      const item = document.createElement('li');
      if (row instanceof DocumentFragment) item.append(row);
      else item.textContent = row;
      return item;
    });
    section.querySelector('ul').replaceChildren(...items);
    section.hidden = !items.length;
  }

  function updateControls() {
    actions.forEach(action => {
      action.disabled = !!task;
      action.setAttribute('aria-pressed', String(action.dataset.sintedot === mode));
    });
    q('options').disabled = !!task;
    q('config-row').hidden = !!current?.shared;
    q('configs').disabled = mode !== 'repair';
    const readOnly = current?.shared && (current.shared.offlineReason || (current.shared.role !== 'owner' &&
      (!current.shared.owner_online || !current.shared.access_open)));
    q('preview').disabled = !!readOnly;
    if (readOnly) q('preview').checked = true;
    q('run').disabled = !!task;
    q('run').textContent = q('preview').checked ? 'Показать план' : mode === 'update' ? 'Обновить моды' : 'Начать ремонт';
    q('rollback').hidden = !!task || !canRollback;
    if (!current?.shared) {
      const note = q('preview').checked ? '' : mode === 'update' && !q('stable').checked ?
        'Обновление может выбрать альфа- или бета-версию. Чтобы их пропустить, включи только стабильные версии.' :
        mode === 'repair' && q('configs').checked ?
        'Повреждённые JSON и TOML будут переименованы. При следующем запуске мод может создать новый конфиг.' : '';
      q('note').hidden = !note;
      q('note').textContent = note;
    }
  }

  function drawReport(state = {}) {
    q('outcome').hidden = !state.stage;
    q('outcome').textContent = state.stage || '';
    q('changes').querySelector('h3').textContent = state.preview ? 'План изменений' : 'Выполнено';
    list('changes', state.changes);
    list('issues', [...(state.error ? [state.error] : []), ...(state.issues || [])]);
    list('journal', (state.journal || []).map(entry => {
      const separator = entry.indexOf(' — ');
      if (separator < 0) return entry;
      const row = document.createDocumentFragment();
      row.append(entry.slice(0, separator), ' — ', entry.slice(separator + 3));
      return row;
    }));
    list('conflicts', (state.conflicts || []).map(conflict => {
      const row = document.createDocumentFragment();
      row.append(conflict.modId + ' · ');
      if (conflict.installed.length) row.append('Установлено: ' + conflict.installed.join(', '));
      else row.append('Нет установленной версии');
      for (const requirement of conflict.requirements) {
        row.append(document.createElement('br'), `${requirement.mod} (${requirement.file}) требует ${requirement.range || '*'}`);
      }
      return row;
    }));
    q('backup').hidden = !state.backup;
    q('backup').textContent = state.backup ? 'Резервная копия: ' + state.backup : '';
    updateControls();
  }

  function footer(state = {}, busy = false) {
    q('footer').hidden = !state.stage;
    q('footer').classList.toggle('sintedot-has-warning', warning(state));
    const label = q('footer-stage');
    label.replaceChildren();
    if (state.stage) {
      label.append('Sintedot · ', state.stage);
      if (state.error) label.append(' · ', state.error);
    }
    label.title = state.error || state.stage || '';
    q('footer-percent').textContent = busy && state.percent > 0 ? Math.floor(state.percent) + '%' : '';
    const progress = q('footer-progress');
    progress.hidden = !busy;
    if (state.percent > 0) progress.value = state.percent;
    else progress.removeAttribute('value');
    q('footer-cancel').hidden = !busy || !state.busy;
    q('footer-dismiss').hidden = busy || !state.stage;
    q('footer-report').hidden = busy || !state.stage;
    if (busy) document.querySelector('#install-status').hidden = true;
  }

  async function refreshRollback(profile) {
    try {
      const available = await app()?.SintedotCanRollback(profile.id);
      if (current !== profile || task) return;
      canRollback = !!available;
      updateControls();
    } catch {}
  }

  async function run(rollback = false) {
    if (task || !current || !app()) return;
    const profile = current;
    const options = {dryRun:q('preview').checked, stableOnly:q('stable').checked, checkConfigs:q('configs').checked && !profile.shared};
    const api = app();
    task = profile;
    result = null;
    q('footer-cancel').disabled = false;
    dialog.close();
    footer({stage:rollback ? 'Восстановление резервной копии…' : 'Подготовка проверки…'}, true);
    const timer = rollback ? undefined : setInterval(async () => {
      if (polling) return;
      polling = true;
      try {
        const state = await api.SintedotState();
        if (task === profile && state.busy && state.target === profile.id) footer(state, true);
      } catch {} finally { polling = false; }
    }, 300);
    try {
      if (rollback) {
        await api.RollbackLastSintedot(profile.id);
        result = {stage:'Последние изменения отменены'};
      } else {
        result = await api.RunSintedotReport(profile.id, mode, options);
      }
    } catch (error) {
      result = {stage:rollback ? 'Ошибка отката' : 'Ошибка Sintedot', error:error?.message || String(error)};
    } finally {
      clearInterval(timer);
      task = null;
      footer(result);
      if ((!result.error && (rollback || (!result.preview && result.changes?.length))) || (result.error && profile.id.startsWith('shared:'))) {
        window.dispatchEvent(new CustomEvent('sintet:mods-changed', {detail:{id:profile.id}}));
      }
      void refreshRollback(profile);
    }
  }

  q('close').onclick = () => dialog.close();
  q('run').onclick = () => void run();
  q('rollback').onclick = () => void run(true);
  q('footer-cancel').onclick = () => {
    q('footer-cancel').disabled = true;
    void app()?.CancelSintedot();
  };
  q('footer-dismiss').onclick = () => footer();
  q('footer-report').onclick = () => {
    if (!current || dialog.open) return;
    drawReport(result || {});
    dialog.showModal();
  };
  actions.forEach(action => { action.onclick = () => { mode = action.dataset.sintedot; updateControls(); }; });
  window.Sintedot = {
    active: () => !!task,
    open(id, label, selectedMode = 'repair', shared = null) {
      if (!id || task || dialog.open) return;
      current = {id, shared};
      q('preview').checked = false;
      mode = selectedMode;
      result = null;
      canRollback = false;
      footer();
      q('target').textContent = label;
      q('note').hidden = !shared;
      const reason = shared?.offlineReason || (shared?.role !== 'owner' && (shared?.access_open === false ?
        'Лидер закрыл общий доступ' : shared?.owner_online === false ? 'Лидер офлайн' : ''));
      q('note').textContent = reason ? reason + '. Доступна проверка по скачанным файлам.' :
        shared && !shared.sintedot_available ? 'На этом сервере правки сохраняются по очереди. Если черновик изменится между шагами, ремонт остановится.' :
        shared?.role === 'editor' ? 'Проверяется общий черновик. Новые версии должен подтвердить лидер.' :
        'Проверяется общий черновик. Конфиги участников остаются на их компьютерах.';
      drawReport();
      dialog.showModal();
      void refreshRollback(current);
    }
  };
})();
