export function resourceResolution(tags=[]){
 const groups=[['16x','sixteen-x'],['32x','thirty-two-x'],['64x','sixty-four-x'],['128x','one-twenty-eight-x'],['256x','two-fifty-six-x'],['512x+','five-twelve-x-and-beyond']];
 return groups.filter(group=>group.some(tag=>tags.includes(tag))).map(group=>group[0]).join(' · ');
}
