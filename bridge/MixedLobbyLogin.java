package sintet.bridge;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.net.InetSocketAddress;
import java.net.SocketAddress;
import java.util.UUID;

import sintet.bridge.asm.ClassReader;
import sintet.bridge.asm.ClassVisitor;
import sintet.bridge.asm.ClassWriter;
import sintet.bridge.asm.Label;
import sintet.bridge.asm.MethodVisitor;
import sintet.bridge.asm.Opcodes;
import sintet.bridge.asm.Type;

/** Restricts the additional authentication path to launcher-owned sockets in an integrated world. */
public final class MixedLobbyLogin {
 static final String LISTENER = "net.minecraft.server.network.ServerLoginPacketListenerImpl";
 static final String PACKET = "net.minecraft.network.protocol.login.ServerboundHelloPacket";
 private static final String INTEGRATED = "net.minecraft.client.server.IntegratedServer";
 private static final String COMPONENT = "net.minecraft.network.chat.Component";
 private static final Names OFFICIAL = new Names(LISTENER,PACKET,INTEGRATED,COMPONENT,
   "handleHello","server","connection","state","requestedUsername","startClientVerification","gameProfile",
   "getRemoteAddress","name","disconnect","literal");
 private static final Names FORGE_LEGACY = new Names(LISTENER,PACKET,INTEGRATED,COMPONENT,
   "m_5990_","f_10018_","f_10013_","f_10019_",null,null,"f_10021_",
   "m_129523_","f_238040_","m_129507_","m_237113_");
 private static final Names FABRIC = new Names("net.minecraft.class_3248","net.minecraft.class_2915",
   "net.minecraft.class_1132","net.minecraft.class_2561","method_12641","field_14162","field_14158","field_14163",
   "field_45028","method_52417","field_14160","method_10755","comp_765","method_10747","method_43470");
 private static final Names[] variants = {OFFICIAL,FORGE_LEGACY,FABRIC,
   new Names("aja","abm","fyp","sw","a","f","g","h",null,null,"j","c","a","a","b"),
   new Names("ash","ajm","gtg","xp","a","f","g","h","j","b",null,"d","b","a","b"),
   new Names("arw","aiy","guo","wz","a","f","g","h","j","b",null,"d","b","a","b"),
   new Names("ayk","aky","iqa","yh","a","f","g","i","k","b",null,"d","b","a","b")};
 private static volatile Layout layout;
 private static volatile Patched patched;
 private static ClassLoader inspected;

 private MixedLobbyLogin() {}

 static boolean matches(String name) {
  if (name == null) return false;
  for (Names n : variants) if (n.internal.equals(name)) return true;
  return false;
 }

 static byte[] patch(ClassLoader loader, String className, byte[] original) {
  ClassReader reader = new ClassReader(original);
  ClassWriter writer = new ClassWriter(reader, ClassWriter.COMPUTE_MAXS);
  Names[] matched = {null};
  reader.accept(new ClassVisitor(Opcodes.ASM9, writer) {
   @Override public MethodVisitor visitMethod(int access, String name, String descriptor, String signature, String[] exceptions) {
    MethodVisitor originalMethod = super.visitMethod(access, name, descriptor, signature, exceptions);
    if ((access & Opcodes.ACC_STATIC) != 0) return originalMethod;
    Names selected = null;
    for (Names n : variants) {
     if (n.internal.equals(className) && name.equals(n.hello) && descriptor.equals(n.descriptor)) { selected = n; break; }
    }
    if (selected == null) return originalMethod;
    matched[0] = selected;
    return new MethodVisitor(Opcodes.ASM9, originalMethod) {
     @Override public void visitCode() {
      super.visitCode();
      Label bridgeStart = new Label();
      Label bridgeEnd = new Label();
      Label bridgeUnavailable = new Label();
      Label standardLogin = new Label();
      visitTryCatchBlock(bridgeStart,bridgeEnd,bridgeUnavailable,"java/lang/ReflectiveOperationException");
      visitLabel(bridgeStart);
      invokeBridge(this);
      visitLabel(bridgeEnd);
      visitJumpInsn(Opcodes.IFEQ,standardLogin);
      visitInsn(Opcodes.RETURN);
      visitLabel(bridgeUnavailable);
      visitFrame(Opcodes.F_SAME1,0,null,1,new Object[]{"java/lang/ReflectiveOperationException"});
      visitInsn(Opcodes.POP);
      visitLabel(standardLogin);
      visitFrame(Opcodes.F_SAME,0,null,0,null);
     }
    };
   }
  },0);
  if (matched[0] == null) return null;
  byte[] patched = writer.toByteArray();
  MixedLobbyLogin.patched = new Patched(loader,matched[0]);
  return patched;
 }

 private static void invokeBridge(MethodVisitor code) {
  // The game can use a module or a mod loader that hides the agent's classes.
  code.visitMethodInsn(Opcodes.INVOKESTATIC,"java/lang/ClassLoader","getSystemClassLoader","()Ljava/lang/ClassLoader;",false);
  code.visitLdcInsn(MixedLobbyLogin.class.getName());
  code.visitMethodInsn(Opcodes.INVOKEVIRTUAL,"java/lang/ClassLoader","loadClass","(Ljava/lang/String;)Ljava/lang/Class;",false);
  code.visitLdcInsn("claim");
  code.visitInsn(Opcodes.ICONST_2);
  code.visitTypeInsn(Opcodes.ANEWARRAY,"java/lang/Class");
  for(int index=0;index<2;index++) {
   code.visitInsn(Opcodes.DUP);
   code.visitInsn(Opcodes.ICONST_0+index);
   code.visitLdcInsn(Type.getType(Object.class));
   code.visitInsn(Opcodes.AASTORE);
  }
  code.visitMethodInsn(Opcodes.INVOKEVIRTUAL,"java/lang/Class","getMethod",
    "(Ljava/lang/String;[Ljava/lang/Class;)Ljava/lang/reflect/Method;",false);
  code.visitInsn(Opcodes.ACONST_NULL);
  code.visitInsn(Opcodes.ICONST_2);
  code.visitTypeInsn(Opcodes.ANEWARRAY,"java/lang/Object");
  for(int index=0;index<2;index++) {
   code.visitInsn(Opcodes.DUP);
   code.visitInsn(Opcodes.ICONST_0+index);
   code.visitVarInsn(Opcodes.ALOAD,index);
   code.visitInsn(Opcodes.AASTORE);
  }
  code.visitMethodInsn(Opcodes.INVOKEVIRTUAL,"java/lang/reflect/Method","invoke",
    "(Ljava/lang/Object;[Ljava/lang/Object;)Ljava/lang/Object;",false);
  code.visitTypeInsn(Opcodes.CHECKCAST,"java/lang/Boolean");
  code.visitMethodInsn(Opcodes.INVOKEVIRTUAL,"java/lang/Boolean","booleanValue","()Z",false);
 }

 static boolean available(ClassLoader loader, String listener) {
  Layout current = layout;
  if (current != null && current.listenerClass.getClassLoader() == loader) return true;
  if (loader == inspected || listener == null || !matches(listener.replace('.', '/'))) return false;
  inspected = loader;
  try {
   Class<?> runtimeClass = Class.forName(listener,false,loader);
   Patched candidate = patched;
   if (candidate == null || candidate.loader != loader || !candidate.names.listener.equals(listener)) return false;
   layout = new Layout(runtimeClass,candidate.names);
   return true;
  } catch(ReflectiveOperationException | LinkageError | RuntimeException error) {
   return false;
  }
 }

 // Called at the beginning of Minecraft's Login Start handler.
 public static boolean claim(Object listener, Object packet) {
  Layout l = layout;
  if (l == null || listener.getClass() != l.listenerClass) return false;
  Object connection = null;
  boolean managed = false;
  try {
   Object server = l.server.get(listener);
   if (!l.integrated.isInstance(server)) return false;
   connection = l.connection.get(listener);
   SocketAddress address = (SocketAddress)l.address.invoke(connection);
   if (!(address instanceof InetSocketAddress)) return false;
   InetSocketAddress remote = (InetSocketAddress)address;
   if (remote.getAddress() == null || !"127.0.0.2".equals(remote.getAddress().getHostAddress())) return false;
   managed = true;
   if (!"HELLO".equals(((Enum<?>)l.state.get(listener)).name())) { l.disconnect(connection); return true; }
   String name = (String)l.name.invoke(packet);
   if (name == null || !name.matches("[A-Za-z0-9_]{1,16}")) { l.disconnect(connection); return true; }
   String admission = SintetBridge.lobbyAdmission(remote.getPort(), name);
   if (admission.equals("denied")) {
    l.disconnect(connection);
    return true;
   }
   Object profile = l.profile.newInstance(UUID.fromString(admission), name);
   l.accept(listener, profile, name);
   return true;
  } catch (Exception | LinkageError error) {
   if (managed && connection != null) {
    try { l.disconnect(connection); } catch (ReflectiveOperationException ignored) {}
   }
   return managed;
  }
 }

 private static final class Patched {
  final ClassLoader loader;
  final Names names;
  Patched(ClassLoader loader, Names names) { this.loader = loader; this.names = names; }
 }

 private static final class Names {
  final String listener,internal,packet,descriptor,integrated,component,hello,server,connection,state;
  final String requestedName,verify,legacyProfile,address,packetName,close,reason;
  Names(String listener,String packet,String integrated,String component,String hello,String server,String connection,
        String state,String requestedName,String verify,String legacyProfile,String address,String packetName,String close,String reason) {
   this.listener=listener;this.internal=listener.replace('.','/');this.packet=packet;
   this.descriptor="(L"+packet.replace('.','/')+";)V";this.integrated=integrated;this.component=component;
   this.hello=hello;this.server=server;this.connection=connection;this.state=state;this.requestedName=requestedName;
   this.verify=verify;this.legacyProfile=legacyProfile;this.address=address;this.packetName=packetName;this.close=close;this.reason=reason;
  }
 }

 private static final class Layout {
  final Class<?> listenerClass,integrated;
  final Field server,connection,state,requestedName,legacyProfile;
  final Method address,name,verify,close,reason;
  final Constructor<?> profile;
  final Object readyToAccept;

  Layout(Class<?> listener, Names names) throws ReflectiveOperationException {
   listenerClass = listener;
   SintetBridge.openLoginPackage(listener);
   ClassLoader loader = listener.getClassLoader();
   Class<?> profileClass = Class.forName("com.mojang.authlib.GameProfile",false,loader);
   Class<?> component = Class.forName(names.component,false,loader);
   Class<?> packet = Class.forName(names.packet,false,loader);
   integrated = Class.forName(names.integrated,false,loader);
   server = field(listener,names.server);
   connection = field(listener,names.connection);
   state = field(listener,names.state);
   address = connection.getType().getMethod(names.address);
   name = packet.getMethod(names.packetName);
   Method verifyMethod = null;
   Field usernameField = null, profileField = null;
   Object ready = null;
   if (names.verify != null) {
    try { verifyMethod = listener.getDeclaredMethod(names.verify,profileClass); }
    catch(NoSuchMethodException missing) { if(names.legacyProfile==null)throw missing; }
   }
   if (verifyMethod != null) {
    verifyMethod.setAccessible(true);
    usernameField = field(listener,names.requestedName);
   } else {
    profileField = field(listener,names.legacyProfile);
    for(Object candidate : state.getType().getEnumConstants()) {
     String stateName=((Enum<?>)candidate).name();
     // Older Forge negotiates mod channels before it enters READY_TO_ACCEPT.
     if (names == FORGE_LEGACY && "NEGOTIATING".equals(stateName)) { ready=candidate; break; }
     if ("READY_TO_ACCEPT".equals(stateName)) ready=candidate;
    }
    if (ready==null || profileField.getType()!=profileClass) throw new NoSuchFieldException("Legacy login state");
   }
   requestedName=usernameField; legacyProfile=profileField; verify=verifyMethod; readyToAccept=ready;
   close = connection.getType().getMethod(names.close,component);
   reason = component.getMethod(names.reason,String.class);
   profile = profileClass.getConstructor(UUID.class,String.class);
  }

  void accept(Object listener,Object profile,String name) throws ReflectiveOperationException {
   if (verify != null) {
    requestedName.set(listener,name);
    verify.invoke(listener,profile);
   } else {
    legacyProfile.set(listener,profile);
    state.set(listener,readyToAccept);
   }
  }

  void disconnect(Object connection) throws ReflectiveOperationException {
   close.invoke(connection,reason.invoke(null,"Sintet lobby admission rejected"));
  }

  private static Field field(Class<?> owner,String name) throws NoSuchFieldException {
   Field field = owner.getDeclaredField(name);
   field.setAccessible(true);
   return field;
  }
 }
}
