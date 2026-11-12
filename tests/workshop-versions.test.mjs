import {test} from 'node:test';
import assert from 'node:assert/strict';
import {versionRange,preferredTarget,orderModVersions} from '../frontend/data/mod-versions.js';
import {resourceResolution} from '../frontend/data/resourcepacks.js';
test('version range compares numeric releases and excludes snapshots or beta Minecraft IDs from its endpoints',()=>{
 assert.equal(versionRange(['1.21.11','1.21.2','b1.7.3','26w14a','26.3','1.7.10']),'1.7.10 – 26.3');
 assert.equal(versionRange(['26.4-snapshot-7']),'26.4-snapshot-7');
});
test('resource packs accept a Vanilla target and display resolution categories from either catalog',()=>{
 const vanilla={game:'1.21.1',loader:'vanilla'};
 assert.equal(preferredTarget([vanilla],null,{kind:'resources',games:['1.21.1'],loaders:[]}),vanilla);
 assert.equal(resourceResolution(['traditional','thirty-two-x']),'32x');
 assert.equal(resourceResolution(['vanilla-like','16x']),'16x');
 assert.equal(resourceResolution(['512x+']),'512x+');
});
test('mod dialog prefers a compatible installed modloader and stable files while keeping all versions selectable',()=>{
 const vanilla={game:'1.21.1',loader:'vanilla'},fabric={game:'1.21.1',loader:'fabric'};
 assert.equal(preferredTarget([vanilla,fabric],vanilla,{games:['1.21.1'],loaders:['fabric']}),fabric);
 const ordered=orderModVersions([{id:'beta',release:'beta'},{id:'release',release:'release'},{id:'alpha',release:'alpha'}]);
 assert.deepEqual(ordered.map(v=>v.id),['release','beta','alpha']);
});
