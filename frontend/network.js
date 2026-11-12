(() => {
 const message='Без доступа к интернету';
 let reachable=true,online=navigator.onLine,checking=false;
 window.SintetNetwork={get offline(){return !online},message};

 function update(){
  const next=navigator.onLine&&reachable;
  if(next===online)return;
  online=next;
  window.dispatchEvent(new CustomEvent('sintet:network',{detail:{offline:!online}}));
 }
 async function refresh(){
  const api=window.go?.main?.App;
  if(!api?.InternetAvailable||checking)return;
  checking=true;
  try{reachable=await api.InternetAvailable();update()}
  catch{/* Keep the last network state if the local bridge is unavailable. */}
  finally{checking=false}
 }
 window.addEventListener('offline',update);
 window.addEventListener('online',()=>{update();void refresh()});
 setInterval(refresh,2000);
 void refresh();
})();
