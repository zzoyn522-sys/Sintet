(() => {
 const topic='realtime:sintet-profile-badges';
 const publicKey='sb_publishable_jkZkTDveIPFO7IL5eqKeOA_v2KBbv8O';
 const url='wss://jcaznszmjqoxrvuizvbz.supabase.co/realtime/v1/websocket?vsn=1.0.0&apikey='+publicKey;
 let enabled=false,socket,retry,reconnectDelay=2000,refreshTimer;
 function invalidate(kind){
  window.dispatchEvent(new Event('sintet:badge-'+kind));
 }
 function scheduleRefresh(){
  if(refreshTimer)return;
  refreshTimer=setTimeout(()=>{
   refreshTimer=undefined;
   invalidate('assignments');
  },300);
 }
 function connect(){
  if(!enabled||socket||retry)return;
  const connection=new WebSocket(url);
  socket=connection;
  let heartbeat,lastBeat,ref=1,joined=false;
  const handshake=setTimeout(()=>connection.close(),12000);
  const send=(ref,topic,event,payload)=>connection.send(JSON.stringify({ref,topic,event,payload}));
  connection.onopen=()=>{
   send('1',topic,'phx_join',{config:{private:false,broadcast:{self:false,ack:false}}});
   heartbeat=setInterval(()=>{
    if(lastBeat){connection.close();return}
    lastBeat=String(++ref);
    send(lastBeat,'phoenix','heartbeat',{});
   },20000);
  };
  connection.onmessage=message=>{
   if(typeof message.data!=='string')return;
   let frame;
   try{frame=JSON.parse(message.data)}catch{return}
   if(!frame||typeof frame!=='object')return;
   const {ref:reference,topic:channel,event,payload:body}=frame;
   if(channel==='phoenix'&&event==='phx_reply'&&reference===lastBeat){lastBeat=undefined;return}
   if(channel!==topic)return;
   if(event==='phx_close'||event==='phx_error'){connection.close();return}
   if(event==='phx_reply'&&reference==='1'){
    if(body?.status!=='ok'){connection.close();return}
    joined=true;
    clearTimeout(handshake);
    reconnectDelay=2000;
    scheduleRefresh();
   }
   if(joined&&event==='broadcast'){
    if(body?.event==='assignments')scheduleRefresh();
    if(body?.event==='catalog')invalidate('catalog');
   }
  };
  connection.onclose=()=>{
   clearInterval(heartbeat);
   clearTimeout(handshake);
   if(socket!==connection)return;
   socket=undefined;
   if(!enabled)return;
   retry=setTimeout(()=>{retry=undefined;connect()},reconnectDelay);
   reconnectDelay=Math.min(60000,reconnectDelay*2);
  };
 }
 window.SintetBadgeLive={setEnabled(value){
  if(enabled===value)return;
  enabled=value;
  if(enabled){connect();return}
  clearTimeout(retry);retry=undefined;
  clearTimeout(refreshTimer);refreshTimer=undefined;
  reconnectDelay=2000;
  const connection=socket;
  socket=undefined;
  connection?.close();
 }};
})();
