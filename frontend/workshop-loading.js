const pending=new WeakMap();

function element(tag,className) {
 const node=document.createElement(tag);
 node.className=className;
 return node;
}

function block(shape) {
 return element('span','ws-skeleton-block ws-skeleton-'+shape);
}

function stack(...shapes) {
 const node=element('div','ws-skeleton-stack');
 node.append(...shapes.map(block));
 return node;
}

function placeholder(kind) {
 const card=element('div','ws-skeleton ws-skeleton--'+kind);
 if(kind==='lines'){
  card.append(stack('title','short'));
 }else if(kind==='tile'){
  const body=stack('title','short','description','description-tail');
  const footer=element('div','ws-skeleton-footer');
  footer.append(block('short'),block('action'));
  body.append(footer);
  card.append(block('cover'),body);
 }else{
  const description=kind==='mod'?['description','description-tail']:['description'];
  card.append(block('icon'),stack('title',...description));
  if(kind==='mod'||kind==='theme')card.append(stack('meta-title','meta-line','meta-line-short'));
  if(kind==='build'||kind==='shared')card.append(block('badge'));
  if(kind!=='shared')card.append(block('action'));
 }
 return card;
}

export function workshopLoading(container) {
 return pending.has(container);
}

export function clearWorkshopLoading(container) {
 pending.get(container)?.();
}

export function showWorkshopLoading(container,kind,{append=false,count=append?2:kind==='tile'?4:5}={}) {
 clearWorkshopLoading(container);
 const group=element('div','ws-skeleton-group');
 group.setAttribute('aria-hidden','true');
 for(let i=0;i<count;i++)group.append(placeholder(kind));
 if(append)container.append(group);
 else container.replaceChildren(group);
 container.setAttribute('aria-busy','true');
 const finish=()=>{
  if(pending.get(container)!==finish)return;
  pending.delete(container);
  group.remove();
  container.removeAttribute('aria-busy');
 };
 pending.set(container,finish);
 return finish;
}
