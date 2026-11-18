(() => {
 const presets={grass:'🌿',sword:'⚔️',crystal:'💎',copper:'🟠',chest:'📦',end:'🌌'};
 const isCustom=value=>/^custom:[a-f0-9]{64}$/.test(value||'');
 function render(target,value){
  if(target.dataset.icon===value)return;
  target.dataset.icon=value;
  target.replaceChildren();
  if(!value)return;
  if(value!=='minecraft'&&!isCustom(value)){target.textContent=presets[value]||presets.grass;return}
  const img=document.createElement('img');
  img.alt='';
  img.draggable=false;
  img.className=value==='minecraft'?'minecraft-game-icon':'custom-build-icon';
  img.onerror=()=>{if(target.contains(img))target.textContent=presets.grass};
  img.src=value==='minecraft'?'assets/launcher-import/minecraft.svg':'/build-icons/'+value.slice(7)+'.png';
  target.append(img);
 }
 window.SintetBuildIcon={render,isCustom};
})();
