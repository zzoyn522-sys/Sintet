package sintet.bridge;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.IOException;

/** Replaces the integrated server's constant eight-slot getter where present. */
final class GameLobbyLimit {
 static final String PROPERTY = "sintet.lan.maxPlayers";

 static byte[] patch(byte[] original, String mappedMethod) throws IOException {
  ByteArrayInputStream raw = new ByteArrayInputStream(original);
  DataInputStream in = new DataInputStream(raw);
  if (in.readInt()!=0xcafebabe) return null;
  in.readInt();
  int count=in.readUnsignedShort();
  if(count>65523)return null;
  String[] utf=new String[count];
  for(int index=1;index<count;index++){
   switch(in.readUnsignedByte()){
    case 1:utf[index]=in.readUTF();break;
    case 3:case 4:case 9:case 10:case 11:case 12:case 17:case 18:skip(in,4);break;
    case 5:case 6:skip(in,8);index++;break;
    case 7:case 8:case 16:case 19:case 20:skip(in,2);break;
    case 15:skip(in,3);break;
    default:return null;
   }
  }
  int poolEnd=original.length-raw.available();
  skip(in,6);
  skip(in,2*in.readUnsignedShort());
  int fields=in.readUnsignedShort();
  for(int field=0;field<fields;field++){
   skip(in,6);
   int attributes=in.readUnsignedShort();
   for(int attribute=0;attribute<attributes;attribute++){
    skip(in,2);
    skip(in,in.readInt());
   }
  }
  int methods=in.readUnsignedShort();
  for(int method=0;method<methods;method++){
   int flags=in.readUnsignedShort();
   String name=utf[in.readUnsignedShort()], descriptor=utf[in.readUnsignedShort()];
   boolean target=(flags&8)==0&&"()I".equals(descriptor)&&
    ("getMaxPlayers".equals(name)||"getMaxPlayerCount".equals(name)||name.equals(mappedMethod));
   int attributes=in.readUnsignedShort();
   for(int attribute=0;attribute<attributes;attribute++){
    int attributeStart=original.length-raw.available();
    int key=in.readUnsignedShort(),length=in.readInt();
    int start=original.length-raw.available();
    skip(in,length);
    if(target&&"Code".equals(utf[key])&&length>=15&&
     original[start+4]==0&&original[start+5]==0&&original[start+6]==0&&original[start+7]==3&&
     original[start+8]==0x10&&original[start+9]==8&&original[start+10]==(byte)0xac&&
     original[start+11]==0&&original[start+12]==0){
     return replace(original,count,poolEnd,attributeStart,start+length,key);
    }
   }
  }
  return null;
 }

 private static void skip(DataInputStream in,int count)throws IOException {
  if(count<0||in.skipBytes(count)!=count)throw new IOException("Incomplete class file");
 }

 private static byte[] replace(byte[] original,int count,int poolEnd,int start,int end,int codeKey)
  throws IOException {
  ByteArrayOutputStream bytes=new ByteArrayOutputStream(original.length+200);
  DataOutputStream out=new DataOutputStream(bytes);
  out.write(original,0,8);
  out.writeShort(count+12);
  out.write(original,10,poolEnd-10);
  utf(out,PROPERTY);
  ref(out,8,count);
  utf(out,"java/lang/Integer");
  ref(out,7,count+2);
  utf(out,"getInteger");
  utf(out,"(Ljava/lang/String;I)Ljava/lang/Integer;");
  pair(out,12,count+4,count+5);
  pair(out,10,count+3,count+6);
  utf(out,"intValue");
  utf(out,"()I");
  pair(out,12,count+8,count+9);
  pair(out,10,count+3,count+10);
  out.write(original,poolEnd,start-poolEnd);
  out.writeShort(codeKey);
  out.writeInt(24);
  out.writeShort(2);
  out.writeShort(1);
  out.writeInt(12);
  ref(out,0x13,count+1);
  out.writeByte(0x10);out.writeByte(8);
  ref(out,0xb8,count+7);
  ref(out,0xb6,count+11);
  out.writeByte(0xac);
  out.writeShort(0);out.writeShort(0);
  out.write(original,end,original.length-end);
  return bytes.toByteArray();
 }

 private static void utf(DataOutputStream out,String value)throws IOException{
  out.writeByte(1);out.writeUTF(value);
 }
 private static void ref(DataOutputStream out,int tag,int reference)throws IOException{
  out.writeByte(tag);out.writeShort(reference);
 }
 private static void pair(DataOutputStream out,int tag,int first,int second)throws IOException{
  ref(out,tag,first);out.writeShort(second);
 }
}
