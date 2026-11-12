const searchProviders=['modrinth','curseforge','sintedmods'];

export function workshopProviders(kind){
 return kind==='mods'||kind==='resources'?searchProviders:searchProviders.slice(0,2);
}

export async function searchWorkshopSources(search,kind,query,filters,previous={}){
 const sources={...previous};
 const pending=workshopProviders(kind).filter(provider=>!previous[provider]||previous[provider].next);
 const pages=await Promise.allSettled(pending.map(provider=>search(provider,query,previous[provider]?.offset||0,filters)));
 const failed=[];
 pages.forEach((result,index)=>{
  const provider=pending[index],old=previous[provider],offset=old?.offset||0;
  if(result.status==='rejected'){failed.push(provider);return}
  const page=result.value,items=page.items||[],nextOffset=page.nextOffset??offset+items.length;
  sources[provider]={items:[...(old?.items||[]),...items],offset:nextOffset,next:page.next&&nextOffset>offset};
 });
 return {sources,failed,more:failed.length>0||Object.values(sources).some(page=>page.next)};
}

const normalize=value=>(value||'').normalize('NFKC').toLowerCase().replace(/[^\p{L}\p{N}]/gu,'');
const slug=item=>normalize(item.slug||item.url?.replace(/\/+$/,'').split('/').at(-1));

function sameProject(a,b){
 if(a.kind!==b.kind||normalize(a.name)!==normalize(b.name))return false;
 if(slug(a)&&slug(a)===slug(b))return true;
 const authors=new Set((a.author||'').split(',').map(normalize).filter(Boolean));
 if((b.author||'').split(',').map(normalize).some(author=>authors.has(author)))return true;
 const description=normalize(a.description);
 return description.length>=24&&description===normalize(b.description);
}

export function projectSources(project){return project.sources||[project]}

export function mergeWorkshopResults(sources,sort){
 const groups=[],byTitle=new Map(),seen=new Set();
 for(const provider of searchProviders){
  for(const [rank,item] of (sources[provider]?.items||[]).entries()){
   const key=provider+'/'+item.id;
   if(seen.has(key))continue;
   seen.add(key);
   const matchKey=item.kind+'/'+normalize(item.name);
   const candidates=byTitle.get(matchKey)||[];
   const group=candidates.find(group=>!group.items.some(other=>other.provider===item.provider)&&group.items.every(other=>sameProject(other,item)));
   if(group){group.items.push(item);group.rank=Math.min(group.rank,rank);continue}
   const created={items:[item],rank,order:groups.length};
   candidates.push(created);
   byTitle.set(matchKey,candidates);
   groups.push(created);
  }
 }
 const cards=groups.map(({items,rank,order})=>{
  const primary=items[0];
  const downloads=Math.max(...items.map(item=>item.downloads||0));
  const games=[...new Set(items.flatMap(item=>item.games||[]))];
  const loaders=[...new Set(items.flatMap(item=>item.loaders||[]))];
  const timestamp=sort==='newest'?'createdAt':'updatedAt';
  const date=Math.max(...items.map(item=>Date.parse(item[timestamp])||0));
  return {card:{...primary,games,loaders,downloads,sources:items},rank,order,date};
 });
 cards.sort((a,b)=>(sort==='downloads'?b.card.downloads-a.card.downloads:sort==='updated'||sort==='newest'?b.date-a.date:0)||a.rank-b.rank||a.order-b.order);
 return cards.map(entry=>entry.card);
}
