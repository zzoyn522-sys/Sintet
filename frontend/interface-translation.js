(() => {
 const catalog=window.SintetLanguageCatalog||{};
 const langs=['en','ru','uk','pt','es'];
 const cyrillic=/[А-Яа-яЁё]/;
 const token=/%(?:\.[0-9]+)?[sdvwgfq]/g;
 const escape=value=>value.replace(/[.*+?^${}()|[\]\\]/g,'\\$&');
 const templates=Object.entries(catalog).flatMap(([key,values])=>{
  const pieces=key.split(token), count=pieces.length-1;
  if(count<1||count>3||!pieces[0]||!cyrillic.test(pieces[0])||
     values.some(value=>value.split(token).length!==pieces.length))return [];
  return [{prefix:pieces[0], pattern:new RegExp('^'+pieces.map(escape).join('(.+?)')+'$','s'), values}];
 });
 function translate(phrase, depth=0){
  const index=langs.indexOf(language);
  if(index<0||language==='ru'||!cyrillic.test(phrase))return phrase;
  if(Object.hasOwn(catalog,phrase))return catalog[phrase][index]||phrase;
  if(depth>=2||phrase.length>2000)return phrase;
  for(const {prefix,pattern,values} of templates){
   if(!phrase.startsWith(prefix))continue;
   const match=phrase.match(pattern);
   if(!match)continue;
   let capture=0;
   return values[index].replace(token,()=>translate(match[++capture],depth+1));
  }
  return phrase;
 }
 let language='ru';
 window.SintetText={translate,format:(key,...args)=>{
  let index=0;
  return translate(key).replace(/%%|%(?:\.[0-9]+)?[sdvwgfq]/g,placeholder=>placeholder==='%%'?'%':String(args[index++]??''));
 }};
 const originals=new WeakMap();
 const attrs=['title','aria-label','aria-description','placeholder','alt'];
 const ignored='script,style,code,pre,[contenteditable],#interface-language,[data-no-translate]';
 function update(node,attribute) {
  const previous=originals.get(node)||{};
  const current=attribute?node.getAttribute(attribute):node.nodeValue;
  if(!current)return;
  const key=attribute||'text';
  const source=previous[key]?.output===current?previous[key].source:current;
  const phrase=source.trim();
  const output=source.replace(phrase,()=>translate(phrase));
  if(output===source&&!previous[key])return;
  previous[key]={source,output};originals.set(node,previous);
  if(current!==output){
   if(attribute)node.setAttribute(attribute,output);
   else node.nodeValue=output;
  }
 }
 function walk(node) {
  if(node.nodeType===Node.TEXT_NODE){
   if(!node.parentElement?.closest(ignored))update(node);
  }else if(node.nodeType===Node.ELEMENT_NODE&&!node.closest(ignored)){
   for(const attr of attrs)update(node,attr);
   for(const child of node.childNodes)walk(child);
  }
 }
 const observer=new MutationObserver(records=>{
  for(const record of records){
   if(record.type==='childList'){for(const node of record.addedNodes)walk(node);}
   else if(record.type==='attributes'){if(!record.target.closest(ignored))update(record.target,record.attributeName);}
   else walk(record.target);
  }
 });
 observer.observe(document.body,{subtree:true,childList:true,characterData:true,attributes:true,attributeFilter:attrs});
 window.addEventListener('sintet:settings',event=>{
  language=event.detail.config.interfaceLanguage||'en';
  walk(document.body);
 });
})();
