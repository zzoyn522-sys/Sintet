package sintet.bridge;
import java.lang.instrument.Instrumentation;
import java.lang.reflect.*;
import java.net.*;
import java.io.*;
import java.util.concurrent.Executor;
import java.util.concurrent.atomic.AtomicBoolean;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.Arrays;
import java.util.Base64;
import java.util.HashMap;
import java.util.Map;

/** Local authenticated command bridge. No transformation or remote code loading. */
public final class SintetBridge {
 private static Instrumentation instrumentation;
 private static String endpoint, secret;
 private static long last;
 private static volatile Activity activity;
 private static Class<?> gameClass;
 private static Field levelField, screenField, integratedServerField, serverNameField, serverAddressField;
 private static Method singleton, serverEntry, legacyScheduler;
 private static final String CLIENT="net.minecraft.client.Minecraft";
 private static final String LEVEL="net.minecraft.client.multiplayer.ClientLevel";
 private static final String SCREEN="net.minecraft.client.gui.screens.Screen";
 private static final String TITLE="net.minecraft.client.gui.screens.TitleScreen";
 private static final String PAUSE="net.minecraft.client.gui.screens.PauseScreen";
 private static final String CONNECT="net.minecraft.client.gui.screens.ConnectScreen";
 private static final String ADDRESS="net.minecraft.client.multiplayer.resolver.ServerAddress";
 private static final String SERVER="net.minecraft.client.multiplayer.ServerData";
 private static final String INTEGRATED="net.minecraft.client.server.IntegratedServer";
 private static final Map<String,String> mappings=new HashMap<>();
 private static final Map<String,String> fields=new HashMap<>();
 private static boolean activityFieldsResolved;
 private static final AtomicBoolean worldProbePending = new AtomicBoolean();
 private static volatile boolean worldProbeDisabled, joinReady;

 private static final class Activity {
  final String mode, serverName, serverAddress;
  Activity(String mode, String serverName, String serverAddress) {
   this.mode=mode;this.serverName=serverName;this.serverAddress=serverAddress;
  }
 }

 private static void loadMappings(){
  String path=System.getenv("SINTET_BRIDGE_MAPPINGS");
  if(path==null||path.isEmpty())return;
  java.util.List<String> names=Arrays.asList(CLIENT,LEVEL,SCREEN,TITLE,PAUSE,CONNECT,ADDRESS,SERVER,SERVER+"$Type",INTEGRATED);
  try(BufferedReader reader=Files.newBufferedReader(Paths.get(path),StandardCharsets.UTF_8)){
   String owner="";
   for(String line;(line=reader.readLine())!=null;){
    int separator=line.lastIndexOf(" -> ");
    if(separator<0||line.isEmpty())continue;
    if(!Character.isWhitespace(line.charAt(0))&&line.endsWith(":")){
     owner=line.substring(0,separator);
     if(names.contains(owner))mappings.put(owner,line.substring(separator+4,line.length()-1));
    }else if(owner.equals(SERVER)||owner.equals(SERVER+"$Type")){
     String member=line.substring(0,separator).trim();
     int split=member.lastIndexOf(' ');
     if(split<0)continue;
     String name=member.substring(split+1);
     if(name.equals("name")||name.equals("ip")||name.equals("OTHER"))fields.put(owner+"#"+name,line.substring(separator+4));
    }
   }
  }catch(IOException|RuntimeException error){
   System.err.println("[Sintet Bridge] Could not read vanilla mappings: "+error.getClass().getSimpleName());
  }
 }
 private static boolean matches(String actual,String official,String intermediary){
  return actual.equals(official)||actual.equals(intermediary)||actual.equals(mappings.get(official));
 }
 private static boolean obfuscated(Class<?> mc){
  return !mc.getName().equals(CLIENT)&&mc.getName().equals(mappings.get(CLIENT));
 }
 private static void sampleWorld(Object game) {
  try {
   if (!activityFieldsResolved) {
    for (Field candidate : gameClass.getDeclaredFields()) {
     String type=candidate.getType().getName();
     if(matches(type,LEVEL,"net.minecraft.class_638")||type.equals("net.minecraft.client.multiplayer.WorldClient")) {
      candidate.setAccessible(true);
      levelField = candidate;
     } else if(matches(type,SCREEN,"net.minecraft.class_437")||type.equals("net.minecraft.client.gui.GuiScreen")) {
      candidate.setAccessible(true);
      screenField=candidate;
     } else if(matches(type,INTEGRATED,"net.minecraft.class_1132")||type.equals("net.minecraft.server.integrated.IntegratedServer")) {
      candidate.setAccessible(true);
      integratedServerField=candidate;
     }
    }
    activityFieldsResolved=true;
   }
   joinReady=false;
   if(levelField==null)return;
   if(levelField.get(game)==null){
    activity=new Activity("menu",null,null);
    Object screen=screenField==null?null:screenField.get(game);
    for(Class<?> type=screen==null?null:screen.getClass();type!=null;type=type.getSuperclass()){
     String name=type.getName();
     if(matches(name,TITLE,"net.minecraft.class_442")||name.equals("net.minecraft.client.gui.GuiMainMenu")){
      joinReady=true;
      break;
     }
    }
    return;
   }
   if(integratedServerField!=null&&integratedServerField.get(game)!=null){activity=new Activity("singleplayer",null,null);return;}
   try {
    Object server=currentServer(game);
    if(server!=null){
     activity=new Activity("multiplayer",readString(serverNameField,server),readString(serverAddressField,server));
     return;
    }
   } catch(ReflectiveOperationException | RuntimeException | LinkageError ignored) {
    // World detection still works if a loader changes the server metadata API.
   }
   activity=new Activity(integratedServerField==null?"world":"multiplayer",null,null);
  } catch (ReflectiveOperationException | RuntimeException ignored) {
   activity = null;
  } catch (LinkageError error) {
   activity = null;
   worldProbeDisabled = true;
   System.err.println("[Sintet Bridge] World probe unavailable: "+error);
  }
 }
 private static Object currentServer(Object game)throws ReflectiveOperationException {
  if(serverEntry==null){
   for(Method candidate:gameClass.getDeclaredMethods()){
    String type=candidate.getReturnType().getName();
    if(candidate.getParameterCount()!=0||Modifier.isStatic(candidate.getModifiers())||
       !matches(type,SERVER,"net.minecraft.class_642"))continue;
    for(Field field:candidate.getReturnType().getDeclaredFields()){
     if(field.getType()!=String.class||Modifier.isStatic(field.getModifiers()))continue;
     String name=field.getName();
     if(name.equals("name")||name.equals("field_3752")||name.equals(fields.get(SERVER+"#name"))){field.setAccessible(true);serverNameField=field;}
     if(name.equals("ip")||name.equals("field_3761")||name.equals(fields.get(SERVER+"#ip"))){field.setAccessible(true);serverAddressField=field;}
    }
    candidate.setAccessible(true);serverEntry=candidate;break;
   }
  }
  return serverEntry==null?null:serverEntry.invoke(game);
 }
 private static String readString(Field field,Object value)throws IllegalAccessException {
  return field==null?null:(String)field.get(value);
 }
 private static void activityHeader(HttpURLConnection connection,String key,String value){
  if(value==null||value.isEmpty())return;
  String clipped=value.substring(0,Math.min(value.length(),255));
  connection.setRequestProperty(key,Base64.getUrlEncoder().withoutPadding().encodeToString(clipped.getBytes(StandardCharsets.UTF_8)));
 }
 private static final java.util.concurrent.atomic.AtomicReference<String> acknowledgement=new java.util.concurrent.atomic.AtomicReference<>();
 public static void premain(String options, Instrumentation inst) {
  instrumentation=inst;
  endpoint=System.getenv("SINTET_OVERLAY_IPC"); secret=System.getenv("SINTET_OVERLAY_IPC_SECRET");
  if(endpoint==null||!endpoint.matches("127\\.0\\.0\\.1:[0-9]{1,5}")||secret==null||!secret.matches("[a-f0-9]{64}"))return;
  Thread thread=new Thread(SintetBridge::poll,"Sintet game bridge"); thread.setDaemon(true);thread.start();
 }
 private static String request(String path,String body) throws Exception {
  HttpURLConnection c=(HttpURLConnection)new URL("http://"+endpoint+path).openConnection(java.net.Proxy.NO_PROXY);
  c.setConnectTimeout(1500);c.setReadTimeout(2000);c.setInstanceFollowRedirects(false);c.setRequestProperty("Authorization","Bearer "+secret);
  if(path.equals("/game/command")&&joinReady)c.setRequestProperty("X-Sintet-Join-Ready","1");
  Activity current=activity;
  if((path.equals("/game/command")||path.equals("/game/presence"))&&current!=null){
   c.setRequestProperty("X-Sintet-World",current.mode.equals("menu")?"0":"1");
   c.setRequestProperty("X-Sintet-Activity",current.mode);
   activityHeader(c,"X-Sintet-Server-Name",current.serverName);
   activityHeader(c,"X-Sintet-Server-Address",current.serverAddress);
  }
  try {
   if(body!=null){c.setRequestMethod("POST");c.setDoOutput(true);try(OutputStream out=c.getOutputStream()){out.write(body.getBytes(StandardCharsets.UTF_8));}}
   if(c.getResponseCode()!=200)throw new IOException("IPC status "+c.getResponseCode());
   ByteArrayOutputStream out=new ByteArrayOutputStream();try(InputStream in=c.getInputStream()){byte[] b=new byte[256];int n;while((n=in.read(b))!=-1){out.write(b,0,n);if(out.size()>4096)throw new IOException("IPC response too large");}}
   return new String(out.toByteArray(),StandardCharsets.UTF_8);
  }finally{c.disconnect();}
 }
 private static Class<?> minecraft(){
  if(gameClass!=null)return gameClass;
  Class<?> client=null;
  Class<?>[] loaded=instrumentation.getAllLoadedClasses();
  for(Class<?> c:loaded){
   if(matches(c.getName(),CLIENT,"net.minecraft.class_310")){client=c;break;}
  }
  if(client==null)return null;
  // Wait until the client has reached its screen classes. Reflection can cause class
  // loading, so probing Minecraft as soon as its class appears races mod initialization.
  for(Class<?> c:loaded){
   String name=c.getName();
   if((matches(name,TITLE,"net.minecraft.class_442")
    ||matches(name,PAUSE,"net.minecraft.class_433")
    ||matches(name,CONNECT,"net.minecraft.class_412")
    ||name.equals("net.minecraft.client.gui.GuiMainMenu")
    ||name.equals("net.minecraft.client.gui.GuiIngameMenu")
    ||name.equals("net.minecraft.client.multiplayer.GuiConnecting"))
    &&c.getClassLoader()==client.getClassLoader()){
    gameClass=client;
    return client;
   }
  }
  return null;
 }
 private static void queueWorldProbe(Executor executor,Object game){
  if(worldProbeDisabled||!worldProbePending.compareAndSet(false,true))return;
  try{
   executor.execute(()->{
    try{sampleWorld(game);}finally{worldProbePending.set(false);}
   });
  }catch(RuntimeException|LinkageError error){
   worldProbePending.set(false);
   throw error;
  }
 }
 private static Object instance(Class<?> c)throws Exception{
  if(singleton==null) for(Method m:c.getDeclaredMethods()){
   if(Modifier.isStatic(m.getModifiers())&&m.getParameterCount()==0&&m.getReturnType()==c){
    m.setAccessible(true);singleton=m;break;
   }
  }
  if(singleton==null)throw new NoSuchMethodException("Minecraft singleton");
  return singleton.invoke(null);
 }
 private static Executor gameExecutor(Object game,Class<?> mc){
  if(game instanceof Executor)return (Executor)game;
  if(legacyScheduler==null){
   for(String name:new String[]{"addScheduledTask","func_152344_a"}){
    try{legacyScheduler=mc.getMethod(name,Runnable.class);break;}
    catch(NoSuchMethodException ignored){}
   }
  }
  if(legacyScheduler==null)return null;
  return task->{
   try{legacyScheduler.invoke(game,task);}
   catch(ReflectiveOperationException error){throw new IllegalStateException(error);}
  };
 }
 private static void pollGame()throws Exception{
  Class<?> mc=minecraft();
  if(mc==null)return;
  Thread current=Thread.currentThread();
  ClassLoader previousLoader=current.getContextClassLoader();
  // Forge's event transformer resolves parent classes through the current thread's loader.
  current.setContextClassLoader(mc.getClassLoader());
  try{
   Object game=instance(mc);
   Executor executor=gameExecutor(game,mc);
   if(executor!=null){
    queueWorldProbe(executor,game);
    String line=request("/game/command",null).trim();String[] parts=line.split(" ");
    if(parts.length==2){long id=Long.parseLong(parts[0]);int port=Integer.parseInt(parts[1]);
     if(id>last&&port>0&&port<=65535){last=id;executor.execute(()->{try{connect(game,mc,port);acknowledgement.set(id+" ok");}catch(Throwable e){System.err.println("[Sintet Bridge] Connection command failed: "+e);acknowledgement.set(id+" failed");}});}
    }
   }
  }finally{
   current.setContextClassLoader(previousLoader);
  }
 }
 private static void poll(){
  loadMappings();
  while(true){try{
   String ack=acknowledgement.get();
   if(ack!=null){request("/game/result",ack);acknowledgement.compareAndSet(ack,null);}
   pollGame();
  }catch(LinkageError error){
   System.err.println("[Sintet Bridge] Integration disabled after a class loading error: "+error);
   return;
  }catch(Exception ignored){}try{Thread.sleep(500);}catch(InterruptedException e){return;}}
 }
 private static Class<?> gameType(Class<?> mc,String mojang,String fabric)throws ClassNotFoundException{
  String name=mc.getName().equals("net.minecraft.class_310")?fabric:obfuscated(mc)?mappings.get(mojang):mojang;
  if(name==null)throw new ClassNotFoundException(mojang);
  return Class.forName(name,false,mc.getClassLoader());
 }
 private static void connect(Object game,Class<?> mc,int port)throws Exception{
  ClassLoader loader=mc.getClassLoader();
  boolean intermediary=mc.getName().equals("net.minecraft.class_310");
  Class<?> screen;
  try{screen=gameType(mc,SCREEN,"net.minecraft.class_437");}
  catch(ClassNotFoundException error){
   if(!mc.getName().equals(CLIENT))throw error;
   connectLegacy(game,mc,port);
   return;
  }
  Class<?> level=gameType(mc,LEVEL,"net.minecraft.class_638");
  Class<?> data=gameType(mc,SERVER,"net.minecraft.class_642");
  Class<?> connectScreen=gameType(mc,CONNECT,"net.minecraft.class_412");

  // Only dispatch from a menu. An unsupported mapping must not unload a world.
  boolean checkedWorld=false;
  Object parent=null;
  for(Field field:mc.getDeclaredFields()){
   if(Modifier.isStatic(field.getModifiers()))continue;
   if(field.getType()==level){
    field.setAccessible(true);
    if(field.get(game)!=null)throw new IllegalStateException("Leave the current world before joining");
    checkedWorld=true;
   }else if(field.getType()==screen){
    field.setAccessible(true);
    parent=field.get(game);
   }
  }
  if(!checkedWorld)throw new NoSuchFieldException("Minecraft world field");
  if(parent==null){
   Class<?> title=gameType(mc,TITLE,"net.minecraft.class_442");
   parent=title.getConstructor().newInstance();
  }

  String host="127.0.0.1:"+port;
  Class<?> address=null;
  try{address=gameType(mc,ADDRESS,"net.minecraft.class_639");}catch(ClassNotFoundException ignored){}
  Object parsed=null;
  for(Method method:address==null?new Method[0]:address.getDeclaredMethods()){
   if(intermediary&&!method.getName().equals("method_2950"))continue;
   if(Modifier.isStatic(method.getModifiers())&&method.getReturnType()==address&&
      java.util.Arrays.equals(method.getParameterTypes(),new Class<?>[]{String.class})){
    method.setAccessible(true);
    parsed=method.invoke(null,host);
    break;
   }
  }
  Object server=null;
  for(Constructor<?> constructor:data.getDeclaredConstructors()){
   Class<?>[] types=constructor.getParameterTypes();
   if(types.length!=3||types[0]!=String.class||types[1]!=String.class)continue;
   Object kind;
   if(types[2]==boolean.class){
    kind=false;
   }else if(types[2].isEnum()){
    if(intermediary){
     kind=types[2].getField("field_45611").get(null); // ServerInfo.ServerType.OTHER
    }else if(obfuscated(mc)&&fields.containsKey(SERVER+"$Type#OTHER")){
     Field constant=types[2].getDeclaredField(fields.get(SERVER+"$Type#OTHER"));
     constant.setAccessible(true);
     kind=constant.get(null);
    }else{
     Object[] choices=types[2].getEnumConstants();
     if(choices.length==0)continue;
     kind=choices[0];
     for(Object choice:choices)if(((Enum<?>)choice).name().equals("OTHER"))kind=choice;
    }
   }else{
    continue;
   }
   constructor.setAccessible(true);
   server=constructor.newInstance("Sintet P2P",host,kind);
   break;
  }
  if(server==null)throw new NoSuchMethodException("Server data API");

  for(Method method:connectScreen.getDeclaredMethods()){
   if(intermediary&&!method.getName().equals("method_36877"))continue;
   Class<?>[] types=method.getParameterTypes();
   if(parsed==null||!Modifier.isStatic(method.getModifiers())||method.getReturnType()!=void.class||
      types.length<4||types.length>6||types[0]!=screen||types[1]!=mc||
      types[2]!=address||types[3]!=data||(types.length>=5&&types[4]!=boolean.class)||
      (types.length==6&&types[5].isPrimitive()))continue;
   Object[] args=new Object[types.length];
   args[0]=parent;
   args[1]=game;
   args[2]=parsed;
   args[3]=server;
   if(types.length>=5)args[4]=false;
   method.setAccessible(true);
   method.invoke(null,args);
   System.out.println("[Sintet Bridge] Connection dispatched on game thread");
   return;
  }
  for(Constructor<?> constructor:connectScreen.getDeclaredConstructors()){
   Class<?>[] types=constructor.getParameterTypes();
   Object[] args;
   if(Arrays.equals(types,new Class<?>[]{screen,mc,data}))args=new Object[]{parent,game,server};
   else if(Arrays.equals(types,new Class<?>[]{screen,mc,String.class,int.class}))args=new Object[]{parent,game,"127.0.0.1",port};
   else continue;
   Method setter=screenSetter(mc,screen);
   constructor.setAccessible(true);
   setter.invoke(game,constructor.newInstance(args));
   return;
  }
  throw new NoSuchMethodException("ConnectScreen API unsupported");
 }
 private static Method screenSetter(Class<?> mc,Class<?> screen)throws NoSuchMethodException{
  for(String name:new String[]{"setScreen","displayGuiScreen","func_147108_a","method_1507"}){
   try{return mc.getMethod(name,screen);}catch(NoSuchMethodException ignored){}
  }
  Method setter=null;
  for(Method method:mc.getDeclaredMethods()){
   if(Modifier.isStatic(method.getModifiers())||method.getReturnType()!=void.class||
      !Arrays.equals(method.getParameterTypes(),new Class<?>[]{screen}))continue;
   if(setter!=null)throw new NoSuchMethodException("Ambiguous screen setter");
   setter=method;
  }
  if(setter==null)throw new NoSuchMethodException("Screen setter");
  setter.setAccessible(true);
  return setter;
 }
 private static void connectLegacy(Object game,Class<?> mc,int port)throws Exception{
  ClassLoader loader=mc.getClassLoader();
  Class<?> screen=Class.forName("net.minecraft.client.gui.GuiScreen",false,loader);
  Class<?> world=Class.forName("net.minecraft.client.multiplayer.WorldClient",false,loader);
  boolean checked=false;
  Object parent=null;
  for(Field field:mc.getDeclaredFields()){
   if(Modifier.isStatic(field.getModifiers()))continue;
   if(field.getType()==world){
    field.setAccessible(true);
    if(field.get(game)!=null)throw new IllegalStateException("Leave the current world before joining");
    checked=true;
   }else if(field.getType()==screen){field.setAccessible(true);parent=field.get(game);}
  }
  if(!checked)throw new NoSuchFieldException("Minecraft world field");
  if(parent==null)parent=Class.forName("net.minecraft.client.gui.GuiMainMenu",false,loader).getConstructor().newInstance();
  Class<?> connecting=Class.forName("net.minecraft.client.multiplayer.GuiConnecting",false,loader);
  Method setter=screenSetter(mc,screen);
  Object next=connecting.getConstructor(screen,mc,String.class,int.class).newInstance(parent,game,"127.0.0.1",port);
  setter.invoke(game,next);
 }
}
