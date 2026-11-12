"""Exercise the actual premain agent and IPC with Minecraft API-shaped fixtures. No game is started."""
import http.server,threading,tempfile,pathlib,subprocess,os,re
root=pathlib.Path(__file__).resolve().parent
jdk=next((root.parent/"_tools/jdk").iterdir())/"bin"
sources={
"net/minecraft/client/gui/screens/Screen.java":"package net.minecraft.client.gui.screens; public class Screen {}",
"net/minecraft/client/gui/screens/TitleScreen.java":"package net.minecraft.client.gui.screens; public class TitleScreen extends Screen {}",
"net/minecraft/client/multiplayer/ClientLevel.java":"package net.minecraft.client.multiplayer; public class ClientLevel {}",
"net/minecraft/client/Minecraft.java":'package net.minecraft.client; public class Minecraft implements java.util.concurrent.Executor {static final Minecraft INSTANCE=new Minecraft(); public net.minecraft.client.gui.screens.Screen screen; public net.minecraft.client.multiplayer.ClientLevel level; public net.minecraft.client.server.IntegratedServer singleplayer; public net.minecraft.client.multiplayer.ServerData remote; public net.minecraft.client.multiplayer.ServerData getCurrentServer(){return remote;} public static Minecraft renamedSingleton(){return INSTANCE;} public LoaderProbe probe(){return null;} public void execute(Runnable r){new Thread(r,"GAME-THREAD").start();}}',
"net/minecraft/client/server/IntegratedServer.java":'package net.minecraft.client.server; public class IntegratedServer {}',
"net/minecraft/client/LoaderProbe.java":"package net.minecraft.client; public class LoaderProbe {}",
"net/minecraft/client/multiplayer/resolver/ServerAddress.java":'package net.minecraft.client.multiplayer.resolver; public class ServerAddress {public String host; public static ServerAddress renamedParse(String s){ServerAddress a=new ServerAddress();a.host=s;return a;}}',
"net/minecraft/client/multiplayer/ServerData.java":'package net.minecraft.client.multiplayer; public class ServerData {public enum Type {LAN,OTHER} public String name,ip; public ServerData(String name,String ip,Type type){this.name=name;this.ip=ip;}}',
"net/minecraft/client/gui/screens/ConnectScreen.java":'package net.minecraft.client.gui.screens; import net.minecraft.client.*; import net.minecraft.client.multiplayer.*; import net.minecraft.client.multiplayer.resolver.*; public class ConnectScreen {public static void renamedConnect(Screen s,Minecraft mc,ServerAddress a,ServerData d,boolean b,Object transfer){if(!Thread.currentThread().getName().equals("GAME-THREAD")||!a.host.equals("127.0.0.1:25565"))throw new AssertionError();System.out.println("FIXTURE-CONNECT-OK");}}',
"Main.java":'''public class Main {
 static class GameLoader extends java.net.URLClassLoader {
  GameLoader(java.net.URL url) { super(new java.net.URL[]{url},Main.class.getClassLoader()); }
  protected Class<?> loadClass(String name,boolean resolve)throws ClassNotFoundException {
   if(name.equals("net.minecraft.client.LoaderProbe") && Thread.currentThread().getName().equals("Sintet game bridge")) {
    if(Thread.currentThread().getContextClassLoader()!=this)throw new AssertionError("Wrong context class loader");
    if(!Boolean.getBoolean("fixture.ready"))throw new AssertionError("Probed before the game screen was ready");
    System.out.println("FIXTURE-LOADER-OK");
   }
   return super.loadClass(name,resolve);
  }
 }
 public static void main(String[] args)throws Exception {
  try(GameLoader loader=new GameLoader(new java.io.File(args[0]).toURI().toURL())){
   Class<?> client=Class.forName("net.minecraft.client.Minecraft",true,loader);
   Thread.sleep(750);
   System.setProperty("fixture.ready","true");
   Class.forName("net.minecraft.client.gui.screens.TitleScreen",true,loader);
   Thread.sleep(1300);
   Object game=client.getMethod("renamedSingleton").invoke(null);
   Class<?> integrated=Class.forName("net.minecraft.client.server.IntegratedServer",true,loader);
   client.getField("singleplayer").set(game,integrated.getConstructor().newInstance());
   Class<?> level=Class.forName("net.minecraft.client.multiplayer.ClientLevel",true,loader);
   client.getField("level").set(game,level.getConstructor().newInstance());
   Thread.sleep(1500);
   Class<?> server=Class.forName("net.minecraft.client.multiplayer.ServerData",true,loader);
   Class<?> kind=Class.forName("net.minecraft.client.multiplayer.ServerData$Type",true,loader);
   client.getField("remote").set(game,server.getConstructor(String.class,String.class,kind).newInstance("Castle","play.minecraft.net",kind.getEnumConstants()[1]));
   client.getField("singleplayer").set(game,null);
   Thread.sleep(1500);
   client.getField("level").set(game,null);
   Thread.sleep(1100);
  }
 }
}'''
}
def verify_fabric(server, modern, obfuscated=False):
 kind="class_8678" if modern else "boolean"
 server_type="public enum class_8678 { field_45609, field_45610, field_45611 }" if modern else ""
 expected_type="class_8678.field_45611" if modern else "false"
 cookie=",class_9112 cookie" if modern else ""
 check_cookie="||cookie!=null" if modern else ""
 fixtures={
  "class_437":"public class class_437 {}",
  "class_442":"public class class_442 extends class_437 {}",
  "class_638":"public class class_638 {}",
  "class_9112":"public class class_9112 {}",
  "class_310":'''public class class_310 implements java.util.concurrent.Executor {
   private static final class_310 INSTANCE=new class_310();
   public class_437 screen;
   public volatile class_638 level;
   public static class_310 method_1551(){return INSTANCE;}
   private final java.util.concurrent.Executor executor=java.util.concurrent.Executors.newSingleThreadExecutor(r->{
    Thread thread=new Thread(r,"GAME-THREAD");thread.setDaemon(true);return thread;
   });
   public void execute(Runnable task){executor.execute(task);}
  }''',
  "class_639":'''public class class_639 {
   public final String host;
   private class_639(String host){this.host=host;}
   public static class_639 method_2950(String host){return new class_639(host);}
  }''',
  "class_642":f'''public class class_642 {{
   {server_type}
   public class_642(String name,String host,{kind} kind){{
    if(!name.equals("Sintet P2P")||!host.equals("127.0.0.1:25565")||kind!={expected_type})throw new AssertionError();
   }}
  }}''',
  "class_412":f'''public class class_412 {{
   public static void method_36877(class_437 screen,class_310 game,class_639 address,class_642 data,boolean quickPlay{cookie}){{
    if(!Thread.currentThread().getName().equals("GAME-THREAD")||
       !(screen instanceof class_442)||!address.host.equals("127.0.0.1:25565")||quickPlay{check_cookie})throw new AssertionError();
    game.level=new class_638();
    System.out.println("FIXTURE-FABRIC-CONNECT-OK");
   }}
  }}''',
 }
 def runtime_name(name):
  return re.sub(r"class_(\d+)",r"Obfuscated\1",name) if obfuscated else name
 with tempfile.TemporaryDirectory() as temp:
  folder=pathlib.Path(temp)
  for name,source in fixtures.items():
   (folder/(runtime_name(name)+".java")).write_text("package net.minecraft;\n"+runtime_name(source),encoding="utf-8")
  main=folder/"FabricMain.java"
  main.write_text(runtime_name('''public class FabricMain {
   public static void main(String[] args)throws Exception {
    net.minecraft.class_310.method_1551();
    Class.forName("net.minecraft.class_442");
    Thread.sleep(3500);
   }
  }'''),encoding="utf-8")
  subprocess.run([str(jdk/"javac.exe"),"-d",temp,*[str(f) for f in folder.glob("*.java")]],check=True)
  env=dict(os.environ,SINTET_OVERLAY_IPC="127.0.0.1:"+str(server.server_port),SINTET_OVERLAY_IPC_SECRET="a"*64)
  if obfuscated:
   classes={
    "net.minecraft.client.Minecraft":"class_310",
    "net.minecraft.client.gui.screens.Screen":"class_437",
    "net.minecraft.client.gui.screens.TitleScreen":"class_442",
    "net.minecraft.client.gui.screens.ConnectScreen":"class_412",
    "net.minecraft.client.multiplayer.ClientLevel":"class_638",
    "net.minecraft.client.multiplayer.resolver.ServerAddress":"class_639",
    "net.minecraft.client.multiplayer.ServerData":"class_642",
    "net.minecraft.client.multiplayer.ServerData$Type":"class_642$class_8678",
   }
   mapping=folder/"mappings.txt"
   mapping.write_text("".join(f"{official} -> net.minecraft.{runtime_name(name)}:\n" for official,name in classes.items())
    +"    net.minecraft.client.multiplayer.ServerData$Type OTHER -> field_45611\n",encoding="utf-8")
   env["SINTET_BRIDGE_MAPPINGS"]=str(mapping)
  acks.clear()
  activity.clear()
  result=subprocess.run([str(jdk/"java.exe"),"-javaagent:"+str(root/"sintet-bridge.jar"),"-cp",temp,"FabricMain"],
                        env=env,capture_output=True,text=True,timeout=15)
  assert result.returncode==0,(result.stdout,result.stderr)
  assert result.stdout.count("FIXTURE-FABRIC-CONNECT-OK")==1,(result.stdout,result.stderr)
  assert acks==[b"1 ok",b"2 failed"],(acks,result.stderr)
  assert "Leave the current world before joining" in result.stderr,result.stderr
  label="obfuscated Vanilla mappings" if obfuscated else ("Fabric 1.21.1" if modern else "Fabric 1.20.1")
  print("PASS:",label,"IPC command, game thread and running-world guard")

acks=[]
fabric_fixture=False
activity=[]
class Handler(http.server.BaseHTTPRequestHandler):
 def log_message(self,*a):pass
 def do_GET(self):
  assert self.headers.get("Authorization")=="Bearer "+"a"*64
  mode=self.headers.get("X-Sintet-Activity")
  if mode:
   activity.append((mode,self.headers.get("X-Sintet-Server-Name")))
  port_command=b"2 25565" if fabric_fixture and acks else b"1 25565"
  self.send_response(200);self.end_headers();self.wfile.write(port_command)
 def do_POST(self):
  acks.append(self.rfile.read(int(self.headers["Content-Length"])))
  self.send_response(200);self.end_headers();self.wfile.write(b"ok")
server=http.server.HTTPServer(("127.0.0.1",0),Handler)
threading.Thread(target=server.serve_forever,daemon=True).start()
try:
 with tempfile.TemporaryDirectory() as temp:
  temp=pathlib.Path(temp)
  for name,source in sources.items():
   f=temp/name;f.parent.mkdir(parents=True,exist_ok=True);f.write_text(source,encoding="utf-8")
  game=temp/"game"
  subprocess.run([str(jdk/"javac.exe"),"-d",str(game),*[str(temp/n) for n in sources if n!="Main.java"]],check=True)
  subprocess.run([str(jdk/"javac.exe"),"-d",str(temp),str(temp/"Main.java")],check=True)
  env=dict(os.environ,SINTET_OVERLAY_IPC="127.0.0.1:"+str(server.server_port),SINTET_OVERLAY_IPC_SECRET="a"*64)
  result=subprocess.run([str(jdk/"java.exe"),"-javaagent:"+str(root/"sintet-bridge.jar"),"-cp",str(temp),"Main",str(game)],env=env,capture_output=True,text=True,timeout=15)
  assert result.returncode==0,(result.stdout,result.stderr)
  assert "FIXTURE-CONNECT-OK" in result.stdout,(result.stdout,result.stderr)
  assert "FIXTURE-LOADER-OK" in result.stdout,(result.stdout,result.stderr)
  assert acks==[b"1 ok"],acks
  assert ("singleplayer",None) in activity,activity
  assert ("multiplayer","Q2FzdGxl") in activity,activity
  assert activity[-1]==("menu",None),activity
  print("PASS: premain, authenticated IPC, game activity, exactly-once dispatch and acknowledgement")
 fabric_fixture=True
 verify_fabric(server, modern=False)
 verify_fabric(server, modern=True)
 verify_fabric(server, modern=True, obfuscated=True)
finally:server.shutdown()
