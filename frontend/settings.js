(() => {
 const q=s=>document.querySelector(s);let api,config,saving=false,hotkey="Shift + Tab";
 const message=text=>q('#settings-status').textContent=text;

 const settingsScroll=q('main');
 const settingsNav=q('.launcher-settings-nav');
 const sections=[...settingsNav.querySelectorAll('a')].map(link=>({link,section:q(link.hash)}));
 const selectSection=selected=>{
  for(const {link,section} of sections){
   if(section===selected)link.setAttribute('aria-current','location');
   else link.removeAttribute('aria-current');
  }
 };
 let sectionFrame=0;
 function updateSection(){
  if(sectionFrame)return;
  sectionFrame=requestAnimationFrame(()=>{
   sectionFrame=0;
   if(!settingsNav.getClientRects().length)return;
   const edge=settingsNav.getBoundingClientRect().top+6;
   let current=sections[0].section;
   for(const {section} of sections){
    if(section.getBoundingClientRect().top>edge)break;
    current=section;
   }
   if(settingsScroll.scrollHeight-settingsScroll.clientHeight-settingsScroll.scrollTop<2)current=sections.at(-1).section;
   selectSection(current);
  });
 }
 for(const {link,section} of sections)link.addEventListener('click',event=>{
  event.preventDefault();
  section.focus({preventScroll:true});
  section.scrollIntoView({block:'start',behavior:matchMedia('(prefers-reduced-motion: reduce)').matches?'auto':'smooth'});
  selectSection(section);
 });
 settingsScroll.addEventListener('scroll',updateSection,{passive:true});
 window.addEventListener('resize',updateSection);
 q('#settings-nav').addEventListener('click',updateSection);
 q('#settings-tab-launcher').addEventListener('click',updateSection);
 updateSection();

 const fields=['interface-language','game-directory','instance-mode','game-width','game-height','fullscreen','java-mode','java-path','jvm-profile','ram','memory-mb','auto-memory','game-start-action','close-action','minimize-to-tray','start-with-windows','start-in-tray','workshop-language','shader-language','hide-overlay','world-backup','world-backup-minutes','discord-presence','show-snapshots','show-pre-releases','show-release-candidates','show-experimental','show-old-versions'];
 const versionOptions={snapshots:'show-snapshots',preReleases:'show-pre-releases',releaseCandidates:'show-release-candidates',experimentalVersions:'show-experimental',oldVersions:'show-old-versions'};
 const languageCodes=['ru','uk','en','de','fr','es','it','pt','pt-BR','pl','cs','sk','ro','hu','nl','da','sv','no','fi','et','lv','lt','el','bg','sr','hr','tr','ar','he','fa','hi','bn','id','ms','th','vi','ja','ko','zh-CN','zh-TW'];
 const languageSelect=q('#workshop-language');
 const minecraftLanguage=q('#minecraft-language'), minecraftScale=q('#minecraft-gui-scale');
 const minecraftVolume=q('#minecraft-master-volume'), minecraftFOV=q('#minecraft-fov');
 const minecraftMusic=q('#minecraft-disable-music'), minecraftNarrator=q('#minecraft-disable-narrator');
 const minecraftBobbing=q('#minecraft-disable-bobbing');
 const minecraftLocales=`af_za ar_sa ast_es az_az ba_ru bar be_by be_latn bg_bg br_fr brb bs_ba ca_es cbk_zam
  cs_cz cv_cu cy_gb da_dk de_at de_ch de_de el_gr en_au en_ca en_gb en_nz en_pt en_ud en_us enp enws eo_uy
  es_ar es_cl es_ec es_es es_mx es_uy es_ve esan et_ee eu_es fa_ir fi_fi fil_ph fo_fo fr_ca fr_ch fr_fr fra_de fur_it
  fy_nl ga_ie gd_gb gl_es go_fr got_de hal_ua haw_us he_il hi_in hn_no hr_hr hu_hu hy_am id_id ig_ng io_en is_is
  isv it_it ja_jp jbo_en jv_id ka_ge kk_kz kn_in ko_kr ksh kw_gb ky_kg la_la lb_lu li_li lmo lo_la lol_us lt_lt lv_lv
  lzh mk_mk mn_mn ms_my mt_mt nah nds_de nl_be nl_nl nn_no no_no oc_fr ovd pl_pl pls pt_br pt_pt qcb_es qid
  qya_aa ro_ro rpr ru_ru ry_ua sah_sah se_no sk_sk sl_si so_so sq_al sr_cs sr_sp sv_se sxu szl ta_in th_th tl_ph
  tlh_aa tok tr_tr tt_ru tzo_mx uk_ua uz_uz val_es vec_it vi_vn vp_vl vro yi_de yo_ng zh_cn zh_hk zh_tw zlm_arab`.split(/\s+/);
 const minecraftLocaleNames={en_pt:'Pirate English',en_ud:'Upside Down English',enp:'Anglish',enws:'Shakespearean English',lol_us:'LOLCAT',qya_aa:'Quenya',tlh_aa:'tlhIngan Hol'};

 function fillGamePreferences(preferences={}){
  if(preferences.language && !Array.from(minecraftLanguage.options).some(option=>option.value===preferences.language)){
   minecraftLanguage.add(new Option(preferences.language,preferences.language));
  }
  minecraftLanguage.value=preferences.language||'';
  minecraftScale.value=preferences.guiScale||'';
  minecraftVolume.value=preferences.masterVolume||'';
  minecraftFOV.value=preferences.fov||'';
  minecraftMusic.checked=!!preferences.disableMusic;
  minecraftNarrator.checked=!!preferences.disableNarrator;
  minecraftBobbing.checked=!!preferences.disableBobbing;
  for(const select of [minecraftLanguage,minecraftScale,minecraftVolume,minecraftFOV])window.SintetUI?.select.refresh(select);
 }

 function readGamePreferences(){
  return {
   language:minecraftLanguage.value,guiScale:minecraftScale.value,masterVolume:minecraftVolume.value,fov:minecraftFOV.value,
   disableMusic:minecraftMusic.checked,disableNarrator:minecraftNarrator.checked,disableBobbing:minecraftBobbing.checked,
  };
 }
 function localizeLanguageOptions(locale){
  const current=languageSelect.value, names=new Intl.DisplayNames(locale,{type:'language'});
  const languages=languageCodes.map(code=>({code,label:names.of(code)})).sort((a,b)=>a.label.localeCompare(b.label,locale));
  languageSelect.options.length=1;
  for(const {code,label} of languages)languageSelect.add(new Option(label,code));
  languageSelect.value=current;
  window.SintetUI?.select.refresh(languageSelect);
  const selected=minecraftLanguage.value;
  const locales=new Set(minecraftLocales);
  if(selected) locales.add(selected);
  const gameLanguages=Array.from(locales,code=>{
   let label=minecraftLocaleNames[code];
   if(!label){
    try{label=names.of(code.replaceAll('_','-'))}catch{label=code}
   }
   return {code,label};
  }).sort((a,b)=>a.label.localeCompare(b.label,locale));
  minecraftLanguage.options.length=1;
  for(const {code,label} of gameLanguages)minecraftLanguage.add(new Option(label,code));
  minecraftLanguage.value=selected;
  window.SintetUI?.select.refresh(minecraftLanguage);
 }
 localizeLanguageOptions(document.documentElement.lang||'en');
 window.addEventListener('sintet:settings',event=>localizeLanguageOptions(event.detail.config.interfaceLanguage||'en'));
 const maxMemory=()=>Math.max(1024,Math.min(131072,Math.floor((config?.systemMemoryMB||8192)/256)*256));
 function memory(){const auto=q('#auto-memory').checked;let value=Number(q('#memory-mb').value)||4096;if(auto)value=Math.max(1024,Math.min(4096,Math.floor((config?.systemMemoryMB||8192)/3/256)*256));q('#ram').disabled=auto;q('#memory-mb').disabled=auto;if(auto)q('#memory-mb').value=value;q('#ram').value=value;q('#ram-value').textContent=window.SintetLanguage.formatSize(value/1024,'gigabyte',value%1024?1:0,config?.interfaceLanguage)+(auto?' · '+window.SintetText.translate('Автоматически'):'')}
 function javaMode(){const auto=q('#java-mode').value==='auto';q('#java-path').disabled=auto;q('#browse-java').disabled=auto;q('#check-java').disabled=auto;q('#java-info').textContent=auto?'Java загружается и выбирается по требованиям версии Minecraft':'Укажите java.exe подходящей версии. Проверка совместимости выполняется перед запуском.'}
 const backupInterval=q('#world-backup-minutes');
 const updateBackupControls=()=>{
  backupInterval.disabled=!q('#world-backup').checked;
  window.SintetUI?.select.refresh(backupInterval);
 };
 q('#world-backup').addEventListener('input',updateBackupControls);
 q('#open-world-backups').onclick=async()=>{
  try{await api.OpenWorldBackupFolder()}catch(error){message(String(error))}
 };
 function fill(settings){q('#world-backup').checked=!!settings.worldBackup;backupInterval.value=String(settings.worldBackupMinutes||60);updateBackupControls();fillGamePreferences(settings.gamePreferences);q('#hide-overlay').checked=settings.overlayEnabled===false;q('#discord-presence').checked=settings.discordPresence!==false;for(const [field,id] of Object.entries(versionOptions))q('#'+id).checked=!!settings[field];q('#interface-language').value=settings.interfaceLanguage||'';window.SintetUI?.select.refresh(q('#interface-language'));q('#game-directory').value=settings.gameDirectory;q('#instance-mode').value=settings.instanceMode;q('#game-width').value=settings.width;q('#game-height').value=settings.height;q('#fullscreen').checked=settings.fullscreen;q('#java-mode').value=settings.javaPath?'custom':'auto';q('#java-path').value=settings.javaPath;q('#jvm-profile').value=settings.jvmProfile||'';window.SintetUI?.select.refresh(q('#jvm-profile'));q('#memory-mb').value=settings.memoryMB;q('#auto-memory').checked=settings.autoMemory;q('#game-start-action').value=settings.gameStartAction||'tray';q('#close-action').value=settings.closeAction||'quit';q('#minimize-to-tray').checked=!!settings.minimizeToTray;q('#start-with-windows').checked=!!settings.startWithWindows;q('#start-in-tray').checked=!!settings.startInTray;hotkey=settings.overlayHotkey||'Shift + Tab';q('#overlay-hotkey').value=hotkey;q('#apply-overlay-font').checked=settings.applyOverlayFont!==false;languageSelect.value=settings.workshopLanguage||'';q('#shader-language').value=settings.shaderLanguage||'';window.SintetUI?.select.refresh(q('#shader-language'));window.SintetUI?.select.refresh(languageSelect);behavior();q('#ram').max=maxMemory();q('#memory-mb').max=maxMemory();javaMode();memory();window.SintetUI?.select.refresh(q('#instance-mode'));window.SintetUI?.select.refresh(q('#java-mode'));window.SintetUI?.select.refresh(q('#game-start-action'));window.SintetUI?.select.refresh(q('#close-action'))}
 function behavior(){const enabled=q('#start-with-windows').checked;q('#start-in-tray').disabled=!enabled;q('#start-in-tray-label').classList.toggle('disabled',!enabled);q('#window-behavior-note').textContent=q('#game-start-action').value==='quit'?'Minecraft продолжит работать. Друзья и P2P отключатся после закрытия Sintet.':'Из трея можно открыть Sintet или завершить работу.'}
 function read(){return {worldBackup:q('#world-backup').checked,worldBackupMinutes:Number(backupInterval.value),gamePreferences:readGamePreferences(),interfaceLanguage:q('#interface-language').value,overlayEnabled:!q('#hide-overlay').checked,discordPresence:q('#discord-presence').checked,gameDirectory:q('#game-directory').value.trim(),instanceMode:q('#instance-mode').value,width:Number(q('#game-width').value),height:Number(q('#game-height').value),fullscreen:q('#fullscreen').checked,javaPath:q('#java-mode').value==='auto'?'':q('#java-path').value.trim(),jvmProfile:q('#jvm-profile').value,memoryMB:Number(q('#memory-mb').value),autoMemory:q('#auto-memory').checked,autoClose:false,gameStartAction:q('#game-start-action').value,closeAction:q('#close-action').value,minimizeToTray:q('#minimize-to-tray').checked,startWithWindows:q('#start-with-windows').checked,startInTray:q('#start-in-tray').checked,overlayHotkey:hotkey,applyOverlayFont:q('#apply-overlay-font').checked,workshopLanguage:languageSelect.value,shaderLanguage:q('#shader-language').value,...Object.fromEntries(Object.entries(versionOptions).map(([field,id])=>[field,q('#'+id).checked])),onlyInstalled:!!config.settings.onlyInstalled}}
 function publish(previous){window.SintetSettings.config=config;q('#home-ram').textContent=window.SintetLanguage.formatSize(config.effectiveMemoryMB/1024,'gigabyte',config.effectiveMemoryMB%1024?1:0,config.interfaceLanguage);window.dispatchEvent(new CustomEvent('sintet:settings',{detail:{config,rootChanged:previous!==undefined&&previous!==config.settings.gameDirectory}}));memory();q('#memory-system').textContent=window.SintetText.format('В системе %s',window.SintetLanguage.formatSize(config.systemMemoryMB/1024,'gigabyte',1))}
 window.SintetSettings={config:null,ready:null};
 window.SintetSettings.ready=(async()=>{for(let i=0;i<100&&!window.go?.main?.App;i++)await new Promise(r=>setTimeout(r,100));api=window.go?.main?.App;if(!api){message('Запустите Sintet через Start.bat');return}try{config=await api.GetSettings();fill(config.settings);q('#save-settings').disabled=false;message(config.warning||'Настройки сохранены');publish();refreshCache()}catch(error){message(String(error))}})();
 fields.forEach(id=>q('#'+id).addEventListener('input',()=>{if(!saving)message('Есть несохранённые изменения')}));
 for(const control of [minecraftLanguage,minecraftScale,minecraftVolume,minecraftFOV,minecraftMusic,minecraftNarrator,minecraftBobbing]){
  control.addEventListener('input',()=>{if(!saving)message('Есть несохранённые изменения')});
 }
 q('#ram').addEventListener('input',()=>{q('#memory-mb').value=q('#ram').value;memory()});q('#memory-mb').addEventListener('input',memory);q('#auto-memory').addEventListener('change',memory);q('#java-mode').addEventListener('change',javaMode);
 q('#game-start-action').addEventListener('change',behavior);q('#start-with-windows').addEventListener('change',behavior);
 q('#overlay-hotkey').addEventListener('focus',()=>{q('#overlay-hotkey').classList.add('recording');q('#overlay-hotkey').value=window.SintetText.translate('Нажми сочетание…')});
 q('#overlay-hotkey').addEventListener('blur',()=>{q('#overlay-hotkey').classList.remove('recording');q('#overlay-hotkey').value=hotkey});
 q('#overlay-hotkey').addEventListener('keydown',event=>{event.preventDefault();event.stopPropagation();if(event.key==='Escape'){event.target.blur();return}if(['Shift','Control','Alt','Meta'].includes(event.key))return;const parts=[];if(event.ctrlKey)parts.push('Ctrl');if(event.altKey)parts.push('Alt');if(event.shiftKey)parts.push('Shift');const code=event.code;let key=code==='Tab'?'Tab':/^Key[A-Z]$/.test(code)?code.slice(3):/^Digit[0-9]$/.test(code)?code.slice(5):/^F([1-9]|1[0-2])$/.test(code)?code:'';if(event.metaKey||!parts.length||!key||(event.altKey&&['Tab','F4'].includes(key))||(event.ctrlKey&&parts.length===1&&key==='W')){q('#hotkey-hint').textContent='Используй Ctrl, Alt или Shift и Tab, F1–F12, букву или цифру.';return}hotkey=parts.concat(key).join(' + ');event.target.value=hotkey;event.target.blur();q('#hotkey-hint').textContent='Применится при следующем запуске игры.';message('Есть несохранённые изменения')});
 q('#reset-hotkey').onclick=()=>{hotkey='Shift + Tab';q('#overlay-hotkey').value=hotkey;message('Есть несохранённые изменения')};
 const size=value=>window.SintetLanguage.formatBytes(value);
 async function refreshCache(){try{const report=await api.CacheUsage();q('#cache-size').textContent=size(report.bytes);q('#cache-groups').replaceChildren(...report.groups.map(group=>{const row=document.createElement('div'),title=document.createElement('span'),value=document.createElement('span');title.textContent=group.name;value.textContent=size(group.bytes);row.append(title,value);return row}));q('#clear-cache').disabled=report.files===0}catch(error){q('#cache-status').textContent=String(error)}}
 q('#clear-cache').onclick=async()=>{q('#clear-cache').disabled=true;q('#cache-status').textContent='Очистка…';try{const report=await api.ClearCache();q('#cache-status').textContent='Освобождено '+size(report.freed)+(report.skipped?' · пропущено '+report.skipped:'');await refreshCache()}catch(error){q('#cache-status').textContent=String(error);q('#clear-cache').disabled=false}};
 window.addEventListener('sintet:settings',()=>{if(api)refreshCache()});
 q('#settings-nav').addEventListener('click',()=>{if(api)refreshCache()});
 q('#browse-directory').onclick=async()=>{try{const path=await api.ChooseGameDirectory();if(path){q('#game-directory').value=path;message('Новая папка будет применена после сохранения')}}catch(e){message(String(e))}};
 q('#browse-java').onclick=async()=>{try{const path=await api.ChooseJava();if(path){q('#java-path').value=path;message('Есть несохранённые изменения')}}catch(e){message(String(e))}};
 q('#check-java').onclick=async()=>{q('#check-java').disabled=true;q('#java-info').textContent='Проверяем Java…';try{const info=await api.CheckJava(q('#java-path').value.trim());q('#java-info').textContent=window.SintetText.format('Java %s · файл запускается',info.version)}catch(e){q('#java-info').textContent=String(e)}finally{q('#check-java').disabled=false}};
 q('#open-game-folder').onclick=()=>api?.OpenGameFolder();
 q('#settings-form').onsubmit=async event=>{event.preventDefault();if(!api||saving)return;if(q('#java-mode').value==='custom'&&!q('#java-path').value.trim()){message('Укажите путь к Java или выберите автоматический режим');q('#java-path').focus();return}const previous=config.settings.gameDirectory;saving=true;q('#save-settings').disabled=true;message(previous!==q('#game-directory').value.trim()?'Применение папки и перенос файлов…':'Сохранение…');try{config=await api.SaveSettings(read(),q('#move-game-files').checked);fill(config.settings);message(config.warning||'Настройки сохранены');publish(previous);toast('Настройки применены')}catch(e){message(String(e))}finally{saving=false;q('#save-settings').disabled=false}};
 q('#reset-settings').onclick=()=>{if(config){fill(config.defaults);message('Значения по умолчанию. Нажмите «Сохранить», чтобы применить.')}};
})();
