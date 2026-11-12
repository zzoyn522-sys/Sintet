import assert from 'node:assert/strict';
import {test} from 'node:test';
import {mergeWorkshopResults,projectSources,searchWorkshopSources} from '../frontend/data/workshop-sources.js';

const item=(provider,id,name,slug,extra={})=>({provider,id,name,slug,kind:'mods',...extra});
test('catalog cards retain source-specific IDs and leave unrelated namesakes separate',()=>{
 const modrinth=item('modrinth','mr','Fabric API','fabric-api',{downloads:250,games:['1.21.1']});
 const curseforge=item('curseforge','123','Fabric API','fabric-api',{downloads:300,games:['1.20.1']});
 const clone=item('sintedmods','other','Fabric API','',{downloads:15,author:'Unrelated'});
 const pages={modrinth:{items:[modrinth]},curseforge:{items:[curseforge]},sintedmods:{items:[clone]}};
 const cards=mergeWorkshopResults(pages,'downloads');
 assert.equal(cards.length,2);
 assert.deepEqual(projectSources(cards[0]).map(source=>source.id),['mr','123']);
 assert.deepEqual(cards[0].games,['1.21.1','1.20.1']);
 assert.equal(cards[0].downloads,300);
});

test('a failed provider retries its own page and finished catalogs are not refetched',async()=>{
 const calls=[];
 let failed=true;
 const search=async(provider,query,offset,filters)=>{
  calls.push([provider,query,offset,filters.game]);
  if(provider==='curseforge'&&failed)throw Error('Offline');
  const nextOffset=offset+(provider==='sintedmods'?0:20);
  return {items:[],next:provider==='modrinth'&&offset===0,nextOffset};
 };
 const filters={game:'1.21.1'};
 const first=await searchWorkshopSources(search,'mods','fabric',filters);
 assert.deepEqual(first.failed,['curseforge']);
 assert.equal(first.more,true);
 failed=false;
 calls.length=0;
 const second=await searchWorkshopSources(search,'mods','fabric',filters,first.sources);
 assert.deepEqual(calls,[['modrinth','fabric',20,'1.21.1'],['curseforge','fabric',0,'1.21.1']]);
 assert.equal(second.more,false);
 calls.length=0;
 await searchWorkshopSources(search,'shaders','iris',filters);
 assert.deepEqual(calls.map(call=>call[0]),['modrinth','curseforge']);
});
