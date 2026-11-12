export function versionRange(games=[]){
 const releases=games.filter(v=>/^\d+\.\d+(?:\.\d+)?$/.test(v)).sort((a,b)=>a.localeCompare(b,'en',{numeric:true}));
 const versions=releases.length?releases:games;
 return versions.length>1?versions[0]+' – '+versions.at(-1):versions[0]||'Minecraft';
}
export function preferredTarget(targets,selected,project){
 const compatible=t=>t&&project.games.includes(t.game)&&(project.kind==='resources'||project.loaders.includes(t.loader));
 return compatible(selected)?selected:targets.find(compatible)||selected;
}
export function orderModVersions(versions){
 const rank={release:0,beta:1,alpha:2};
 return [...versions].sort((a,b)=>(rank[a.release]??0)-(rank[b.release]??0));
}
