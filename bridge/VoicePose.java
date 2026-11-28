package sintet.bridge;

import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.Map;

/** Reads the local pose on Minecraft's client thread. */
final class VoicePose {
 private static final String CLIENT="net.minecraft.client.Minecraft";
 private static final String ENTITY="net.minecraft.world.entity.Entity";
 private static final String LEVEL="net.minecraft.world.level.Level";
 private static final String KEY="net.minecraft.resources.ResourceKey";
 private Field player, xField, yField, zField, yawField, legacyDimension;
 private Method x,y,z,yaw,dimension;
 private Class<?> playerType, levelType, keyType;
 private Method keyLocation;

 String read(Object game,Object level,Map<String,String> mappings)throws ReflectiveOperationException{
  if(player==null){
   for(Field field:game.getClass().getDeclaredFields()){
    String type=field.getType().getName();
    if(field.getName().equals(mappings.get(CLIENT+"#player"))||type.equals("net.minecraft.client.player.LocalPlayer")||
       type.equals("net.minecraft.class_746")||type.equals("net.minecraft.client.entity.EntityPlayerSP")){
     field.setAccessible(true);
     player=field;
     break;
    }
   }
   if(player==null)return null;
  }
  Object entity=player.get(game);
  if(entity==null)return null;
  if(playerType!=entity.getClass()){
   playerType=entity.getClass();
   x=method(playerType,mappings.get(ENTITY+"#getX()"),"getX","m_20185_","method_23317");
   y=method(playerType,mappings.get(ENTITY+"#getY()"),"getY","m_20186_","method_23318");
   z=method(playerType,mappings.get(ENTITY+"#getZ()"),"getZ","m_20189_","method_23321");
   yaw=method(playerType,mappings.get(ENTITY+"#getYRot()"),"getYRot","getYaw","m_146908_","method_36454");
   xField=field(playerType,"posX","field_70165_t");
   yField=field(playerType,"posY","field_70163_u");
   zField=field(playerType,"posZ","field_70161_v");
   yawField=field(playerType,"rotationYaw","field_70177_z");
   legacyDimension=field(playerType,"dimension","field_71093_bK");
  }
  if(levelType!=level.getClass()){
   levelType=level.getClass();
   dimension=method(levelType,mappings.get(LEVEL+"#dimension()"),"dimension","getRegistryKey","m_46472_","method_27983");
  }
  String world;
  if(dimension!=null){
   Object key=dimension.invoke(level);
   if(key==null)return null;
   if(keyType!=key.getClass()){
    keyType=key.getClass();
    keyLocation=method(keyType,mappings.get(KEY+"#location()"),mappings.get(KEY+"#identifier()"),
                       "location","identifier","getValue","m_135782_","method_29177");
   }
   if(keyLocation==null)return null;
   world=String.valueOf(keyLocation.invoke(key));
  }else if(legacyDimension!=null){
   world="legacy:"+legacyDimension.getInt(entity);
  }else return null;
  if(!world.matches("[a-z0-9_.-]+:[a-z0-9_/.-]{1,120}"))return null;
  double px=number(entity,x,xField),py=number(entity,y,yField),pz=number(entity,z,zField),rotation=number(entity,yaw,yawField);
  if(!Double.isFinite(px)||!Double.isFinite(py)||!Double.isFinite(pz)||!Double.isFinite(rotation))return null;
  return world+" "+px+" "+py+" "+pz+" "+rotation;
 }

 private static double number(Object target, Method method, Field field)throws ReflectiveOperationException{
  Object value=method!=null?method.invoke(target):field!=null?field.get(target):null;
  return value instanceof Number?((Number)value).doubleValue():Double.NaN;
 }

 private static Method method(Class<?> type,String... names){
  for(String name:names){
   if(name==null)continue;
   try{Method method=type.getMethod(name);method.setAccessible(true);return method;}
   catch(NoSuchMethodException ignored){}
  }
  return null;
 }

 private static Field field(Class<?> type,String... names){
  for(Class<?> owner=type;owner!=null;owner=owner.getSuperclass()){
   for(String name:names){
    try{Field field=owner.getDeclaredField(name);field.setAccessible(true);return field;}
    catch(NoSuchFieldException ignored){}
   }
  }
  return null;
 }
}
