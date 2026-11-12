(() => {
 const languages = [
  ['', '', ['System language','Язык системы','Мова системи','Idioma do sistema','Idioma del sistema']],
  ['en','GB','English'], ['ru','RU','Русский'], ['uk','UA','Українська'],
  ['pt','PT','Português'], ['es','ES','Español'],
 ];
 const label=['Interface language','Язык интерфейса','Мова інтерфейсу','Idioma da interface','Idioma de la interfaz'];
 const formatters=new Map();
 function formatDuration(minutes,{lessThanMinute=false,includeMinutes=false}={}) {
  const locale=document.documentElement.lang||'en';
  if(!formatters.has(locale)){
   const unit=name=>new Intl.NumberFormat(locale,{style:'unit',unit:name,unitDisplay:'short'});
   formatters.set(locale,{hour:unit('hour'),minute:unit('minute')});
  }
  const {hour,minute}=formatters.get(locale);
  if(minutes===0)return (lessThanMinute?'< ':'')+minute.format(lessThanMinute?1:0);
  const hours=Math.floor(minutes/60),remaining=minutes%60;
  if(!hours)return minute.format(remaining);
  return remaining||includeMinutes?hour.format(hours)+' '+minute.format(remaining):hour.format(hours);
 }
 function formatSize(value,unit,digits=0,locale=document.documentElement.lang||'en'){
  return new Intl.NumberFormat(locale,{
   style:'unit',unit,unitDisplay:'short',minimumFractionDigits:digits,maximumFractionDigits:digits,
  }).format(value);
 }
 function formatBytes(value){
  if(value>=1048576)return formatSize(value/1048576,'megabyte',1);
  if(value>=1024)return formatSize(value/1024,'kilobyte',1);
  return formatSize(value,'byte');
 }
 window.SintetLanguage={formatDuration,formatSize,formatBytes};
 const select=document.querySelector('#interface-language');
 const heading=document.querySelector('#interface-language-label');
 function apply(config) {
  const active=config.interfaceLanguage||'en';
  const index=Math.max(0,languages.findIndex(([code])=>code===active)-1);
  document.documentElement.lang=active;
  heading.textContent=label[index];
  select.replaceChildren(...languages.map(([code,flag,name])=>{
   const text=Array.isArray(name)?name[index]:name;
   const symbol=flag?String.fromCodePoint(...[...flag].map(letter=>127397+letter.charCodeAt(0)))+'  ':'';
   return new Option(symbol+text,code);
  }));
  select.value=config.settings.interfaceLanguage||'';
  window.SintetUI?.select.refresh(select);
 }
 window.addEventListener('sintet:settings',event=>apply(event.detail.config));
})();
