using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.IO.Compression;
using System.Linq;
using System.Net;
using System.Reflection;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Threading;
using System.Xml.Serialization;
using Microsoft.Win32;
using System.Web.Script.Serialization;

namespace SintetSetup {
 public sealed class InstallOptions { public string Directory; public bool Desktop = true; public bool Integrate = true; }
 public sealed class InstallMarker {
  public string Product = Engine.Product;
  public string Directory;
  public string LauncherHash;
  public string UninstallerHash;
  public string Version = Package.Version;
 }
 public sealed class DownloadedRelease { public string Path; public string Version; }
 public static class Engine {
  public const string Product="Sintet.Launcher.8E6E8495-DB20-44FC-963E-567A8AC82181";
  const string RegistryPath=@"Software\Microsoft\Windows\CurrentVersion\Uninstall\Sintet";
  const string UpdateManifestUrl="https://sintet-updates.zzoyn522.workers.dev/stable/latest.json";
  public static string DefaultPath {get{return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"Programs","Sintet");}}
  static void RemoveUninstallShortcuts(string menu,string root) {
   foreach(var name in new[]{"Uninstall Sintet","Удалить Sintet","Видалити Sintet","Desinstalar o Sintet","Desinstalar Sintet"})
    RemoveShortcut(Path.Combine(menu,name+".lnk"),root);
  }
  public static Stream Resource(string name) {var stream=Assembly.GetExecutingAssembly().GetManifestResourceStream(name);if(stream==null)throw new IOException(SetupLanguage.T("Не найден компонент установщика: ")+name);return stream;}
  public static string Hash(string path){using(var file=File.OpenRead(path))using(var sha=SHA256.Create())return Hex(sha.ComputeHash(file));}
  static string Hex(byte[] bytes){return BitConverter.ToString(bytes).Replace("-","").ToLowerInvariant();}
  [StructLayout(LayoutKind.Sequential,CharSet=CharSet.Unicode)] struct OSInfo {public uint Size,Major,Minor,Build,Platform;[MarshalAs(UnmanagedType.ByValTStr,SizeConst=128)]public string ServicePack;}
  [DllImport("ntdll.dll",CharSet=CharSet.Unicode)] static extern int RtlGetVersion(ref OSInfo info);
  public static bool IsWindows11 {get{var info=new OSInfo();info.Size=(uint)Marshal.SizeOf(typeof(OSInfo));return RtlGetVersion(ref info)==0&&info.Major>=10&&info.Build>=22000;}}
  public static bool HasWebView() {
   foreach(var hive in new[]{RegistryHive.CurrentUser,RegistryHive.LocalMachine})foreach(var view in new[]{RegistryView.Registry32,RegistryView.Registry64}){
    using(var root=RegistryKey.OpenBaseKey(hive,view))using(var key=root.OpenSubKey(@"Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}")){
     Version version;if(key!=null&&Version.TryParse(key.GetValue("pv","").ToString(),out version)&&version>new Version(0,0,0,0))return true;
    }
   }
   return false;
  }
  public static string Normalize(string value) {
   if(String.IsNullOrWhiteSpace(value)||value.IndexOf('"')>=0)throw new IOException(SetupLanguage.T("Выбери папку для установки."));
   string path=Path.GetFullPath(Environment.ExpandEnvironmentVariables(value.Trim())).TrimEnd(Path.DirectorySeparatorChar);
   string root=Path.GetPathRoot(path).TrimEnd(Path.DirectorySeparatorChar);
   if(path.Length>200||String.Equals(path,root,StringComparison.OrdinalIgnoreCase))throw new IOException(SetupLanguage.T("Выбери отдельную папку для Sintet."));
   if(path.StartsWith(@"\\",StringComparison.Ordinal))throw new IOException(SetupLanguage.T("Выбери папку на локальном диске."));
   return path;
  }
  public static void Install(InstallOptions options,Action<int,string> progress) {
   if(!Environment.Is64BitOperatingSystem||Environment.OSVersion.Version.Major<10)throw new IOException(SetupLanguage.T("Для этой сборки нужна 64-битная Windows 10 или 11."));
   string root=Normalize(options.Directory);Directory.CreateDirectory(root);
   string existingMarker=Path.Combine(root,"install.xml");
   if(File.Exists(existingMarker))ReadMarker(root);
   else foreach(string name in new[]{"Sintet-launcher.exe","Sintet.ico","Uninstall.exe"})if(File.Exists(Path.Combine(root,name)))throw new IOException(SetupLanguage.T("В этой папке уже есть файлы с такими именами. Выбери другую папку для Sintet."));
   string probe=Path.Combine(root,".sintet-write-"+Guid.NewGuid().ToString("N"));
   try {using(File.Create(probe)){} } catch(UnauthorizedAccessException){throw new IOException(SetupLanguage.T("Нет доступа к папке. Выбери другую, например ")+DefaultPath);}
   finally {if(File.Exists(probe))File.Delete(probe);}
   progress(4,SetupLanguage.T("Проверяем компоненты"));
   if(options.Integrate&&!HasWebView())InstallWebView(progress);
   DownloadedRelease remote=DownloadLatestLauncher(root,progress);
   progress(60,SetupLanguage.T("Подготавливаем лаунчер"));
   string exe=Path.Combine(root,"Sintet-launcher.exe"),temporary=Path.Combine(root,".sintet-"+Guid.NewGuid().ToString("N")+".tmp");
   try {
    string releaseVersion=Package.Version;
    if(remote!=null){File.Move(remote.Path,temporary);releaseVersion=remote.Version;progress(88,SetupLanguage.T("Проверенная версия Sintet готова"));}
    else {
     using(var compressed=Resource("payload"))using(var input=new GZipStream(compressed,CompressionMode.Decompress))using(var output=new FileStream(temporary,FileMode.CreateNew,FileAccess.Write,FileShare.None,65536,FileOptions.WriteThrough)) {
      byte[] buffer=new byte[65536];int count;long copied=0;
      while((count=input.Read(buffer,0,buffer.Length))>0){output.Write(buffer,0,count);copied+=count;progress(60+(int)(copied*28/Package.LauncherSize),SetupLanguage.T("Распаковываем лаунчер"));}
      output.Flush(true);
     }
     if(Hash(temporary)!=Package.LauncherHash)throw new IOException(SetupLanguage.T("Установщик повреждён. Скопируй его заново."));
    }
    Replace(temporary,exe);
    progress(89,SetupLanguage.T("Сохраняем версию ")+releaseVersion);
    var marker=new InstallMarker{Directory=root,LauncherHash=Hash(exe),UninstallerHash="",Version=releaseVersion};
    progress(90,SetupLanguage.T("Добавляем компоненты Sintet"));
    WriteResource("icon",Path.Combine(root,"Sintet.ico"));
    string uninstaller=Path.Combine(root,"Uninstall.exe");CopyAtomic(Assembly.GetExecutingAssembly().Location,uninstaller);
    marker.UninstallerHash=Hash(uninstaller);
    WriteMarker(root,marker);
    if(options.Integrate){
     progress(95,SetupLanguage.T("Создаём ярлыки"));
     string menu=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.Programs),"Sintet");Directory.CreateDirectory(menu);
     Shortcut(Path.Combine(menu,"Sintet.lnk"),exe,root,"");
     RemoveUninstallShortcuts(menu,root);
     Shortcut(Path.Combine(menu,SetupLanguage.T("Удалить Sintet")+".lnk"),uninstaller,root,"--uninstall");
     if(options.Desktop)Shortcut(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.DesktopDirectory),"Sintet.lnk"),exe,root,"");
     using(var key=Registry.CurrentUser.CreateSubKey(RegistryPath)){
      key.SetValue("DisplayName","Sintet");key.SetValue("DisplayVersion",releaseVersion);key.SetValue("Publisher","Sintet");key.SetValue("InstallLocation",root);key.SetValue("DisplayIcon",Path.Combine(root,"Sintet.ico"));
      key.SetValue("UninstallString","\""+uninstaller+"\" --uninstall");key.SetValue("NoModify",1);key.SetValue("NoRepair",1);key.SetValue("EstimatedSize",(int)((new FileInfo(exe).Length+new FileInfo(uninstaller).Length)/1024));
     }
    }
    progress(100,SetupLanguage.T("Готово"));
   } finally {if(File.Exists(temporary))File.Delete(temporary);if(remote!=null&&File.Exists(remote.Path))File.Delete(remote.Path);}
  }
  static DownloadedRelease DownloadLatestLauncher(string installRoot,Action<int,string> progress){
   string manifestPath=null,verifierPath=null,downloadPath=null;
   try{
    progress(15,SetupLanguage.T("Проверяем последнюю версию Sintet"));
    var request=(HttpWebRequest)WebRequest.Create(UpdateManifestUrl);request.Method="GET";request.Timeout=12000;request.ReadWriteTimeout=20000;request.UserAgent="Sintet-Setup/"+Package.Version;
    string manifest;
    using(var response=(HttpWebResponse)request.GetResponse()){
     if(response.ContentLength>65536)throw new IOException(SetupLanguage.T("Манифест обновления слишком большой."));
     using(var input=response.GetResponseStream())using(var reader=new StreamReader(input))manifest=reader.ReadToEnd();
    }
    if(manifest.Length>65536)throw new IOException(SetupLanguage.T("Манифест обновления слишком большой."));
    var serializer=new JavaScriptSerializer();var envelope=serializer.Deserialize<Dictionary<string,object>>(manifest);
    var payloadBytes=Convert.FromBase64String(Convert.ToString(envelope["payload"]));
    var payload=serializer.Deserialize<Dictionary<string,object>>(System.Text.Encoding.UTF8.GetString(payloadBytes));
    string version=Convert.ToString(payload["version"]),address=Convert.ToString(payload["url"]),hash=Convert.ToString(payload["sha256"]);
    long size=Convert.ToInt64(payload["size"]);Uri uri;
    Version parsedVersion,bundledVersion;
    if(!Version.TryParse(version,out parsedVersion)||!Version.TryParse(Package.Version,out bundledVersion)||parsedVersion<=bundledVersion)return null;
    if(!Uri.TryCreate(address,UriKind.Absolute,out uri)||uri.Scheme!="https"||uri.Host!="sintet-updates.zzoyn522.workers.dev"||uri.AbsolutePath!="/stable/Sintet-"+version+".exe"||size<1||size>350L*1024*1024||hash.Length!=64)throw new IOException(SetupLanguage.T("Манифест Sintet содержит недопустимую версию."));
    string tempDirectory=Path.Combine(Path.GetTempPath(),"Sintet-Setup-"+Guid.NewGuid().ToString("N"));Directory.CreateDirectory(tempDirectory);
    manifestPath=Path.Combine(tempDirectory,"latest.json");verifierPath=Path.Combine(tempDirectory,"Sintet-UpdateVerifier.exe");downloadPath=Path.Combine(installRoot,".sintet-latest-"+Guid.NewGuid().ToString("N")+".tmp");
    File.WriteAllText(manifestPath,manifest,new System.Text.UTF8Encoding(false));
    using(var resource=Resource("updateVerifier"))using(var output=File.Create(verifierPath))resource.CopyTo(output);
    progress(17,SetupLanguage.T("Скачиваем Sintet ")+version);
    var downloadRequest=(HttpWebRequest)WebRequest.Create(uri);downloadRequest.Method="GET";downloadRequest.Timeout=20000;downloadRequest.ReadWriteTimeout=120000;downloadRequest.UserAgent="Sintet-Setup/"+Package.Version;
    using(var response=(HttpWebResponse)downloadRequest.GetResponse())using(var input=response.GetResponseStream())using(var output=new FileStream(downloadPath,FileMode.CreateNew,FileAccess.Write,FileShare.None,65536,FileOptions.WriteThrough)){
     byte[] buffer=new byte[65536];long copied=0;int count;
     while((count=input.Read(buffer,0,buffer.Length))>0){copied+=count;if(copied>size||copied>350L*1024*1024)throw new IOException("Скачанный файл Sintet превышает размер из манифеста.");output.Write(buffer,0,count);double mb=copied/1048576.0,total=size/1048576.0;progress(17+(int)(copied*37/size),SetupLanguage.T("Скачиваем Sintet · ")+mb.ToString("0.0",System.Globalization.CultureInfo.InvariantCulture)+" / "+total.ToString("0.0",System.Globalization.CultureInfo.InvariantCulture)+SetupLanguage.T(" МБ"));}
     output.Flush(true);if(copied!=size)throw new IOException(SetupLanguage.T("Размер скачанной версии Sintet не совпадает с манифестом."));
    }
    progress(55,SetupLanguage.T("Проверяем подпись и целостность Sintet"));
    var verify=Process.Start(new ProcessStartInfo(verifierPath,"\""+manifestPath+"\" \""+downloadPath+"\""){UseShellExecute=false,CreateNoWindow=true,WindowStyle=ProcessWindowStyle.Hidden});
    if(verify==null||!verify.WaitForExit(30000)){if(verify!=null)try{verify.Kill();}catch{}throw new IOException(SetupLanguage.T("Не удалось проверить подпись новой версии Sintet."));}
    if(verify.ExitCode!=0)throw new IOException(SetupLanguage.T("Новая версия Sintet не прошла проверку подписи и целостности."));
    var result=new DownloadedRelease{Path=downloadPath,Version=version};downloadPath=null;return result;
   }catch(WebException){progress(16,SetupLanguage.T("Нет соединения, используем версию из установщика"));return null;}
   finally{if(downloadPath!=null&&File.Exists(downloadPath))File.Delete(downloadPath);if(manifestPath!=null){string directory=Path.GetDirectoryName(manifestPath);try{if(Directory.Exists(directory))Directory.Delete(directory,true);}catch{}}}
  }
  static void InstallWebView(Action<int,string> progress){
   progress(12,SetupLanguage.T("Загружаем Microsoft WebView2"));
   string directory=Path.Combine(Path.GetTempPath(),"Sintet-WebView-"+Guid.NewGuid().ToString("N"));Directory.CreateDirectory(directory);
   string file=Path.Combine(directory,"MicrosoftEdgeWebview2Setup.exe");
   try {
    WriteResource("webview",file);
    if(Hash(file)!=Package.WebViewHash)throw new IOException(SetupLanguage.T("Компонент Microsoft WebView2 повреждён."));
    var start=new ProcessStartInfo(file,"/silent /install"){UseShellExecute=false,CreateNoWindow=true,WorkingDirectory=directory};
    using(var process=Process.Start(start)){
     if(!process.WaitForExit(12*60*1000))throw new IOException(SetupLanguage.T("Microsoft WebView2 ещё устанавливается. Подожди и запусти установщик снова."));
     for(int i=0;i<30&&!HasWebView();i++)Thread.Sleep(300);
     if(!HasWebView())throw new IOException(String.Format(SetupLanguage.T("Не удалось установить Microsoft WebView2 (код {0}). Проверь интернет и повтори установку."),process.ExitCode));
    }
   }finally{try{if(File.Exists(file))File.Delete(file);if(Directory.Exists(directory))Directory.Delete(directory,false);}catch{}}
  }
  static void Replace(string source,string destination){
   if(File.Exists(destination)){
    string backup=destination+".previous-"+Guid.NewGuid().ToString("N");
    try{File.Replace(source,destination,backup,true);if(File.Exists(backup))File.Delete(backup);}catch(IOException){throw new IOException(SetupLanguage.T("Не удалось обновить файл. Закрой Sintet и повтори установку."));}
   }else File.Move(source,destination);
  }
  static void CopyAtomic(string source,string destination){string tmp=destination+"."+Guid.NewGuid().ToString("N")+".tmp";try{File.Copy(source,tmp,false);Replace(tmp,destination);}finally{if(File.Exists(tmp))File.Delete(tmp);}}
  static void WriteResource(string name,string path){string temp=path+"."+Guid.NewGuid().ToString("N")+".tmp";try{using(var input=Resource(name))using(var output=new FileStream(temp,FileMode.CreateNew,FileAccess.Write,FileShare.None)){input.CopyTo(output);output.Flush(true);}Replace(temp,path);}finally{if(File.Exists(temp))File.Delete(temp);}}
  static void WriteMarker(string directory,InstallMarker marker){string path=Path.Combine(directory,"install.xml"),temp=path+".tmp";using(var file=new FileStream(temp,FileMode.Create,FileAccess.Write,FileShare.None)){new XmlSerializer(typeof(InstallMarker)).Serialize(file,marker);file.Flush(true);}Replace(temp,path);}
  public static InstallMarker ReadMarker(string directory){
   string root=Normalize(directory),path=Path.Combine(root,"install.xml");
   if(!File.Exists(path))throw new IOException(SetupLanguage.T("Не найден установленный Sintet."));
   InstallMarker marker;using(var reader=System.Xml.XmlReader.Create(path,new System.Xml.XmlReaderSettings{DtdProcessing=System.Xml.DtdProcessing.Prohibit,MaxCharactersInDocument=16384}))marker=(InstallMarker)new XmlSerializer(typeof(InstallMarker)).Deserialize(reader);
   if(marker.Product!=Product||!String.Equals(root,Normalize(marker.Directory),StringComparison.OrdinalIgnoreCase))throw new IOException(SetupLanguage.T("Некорректная папка Sintet."));
   return marker;
  }
  static void Shortcut(string path,string target,string working,string arguments){
   object shell=null,link=null;
   try{var type=Type.GetTypeFromProgID("WScript.Shell");shell=Activator.CreateInstance(type);link=type.InvokeMember("CreateShortcut",BindingFlags.InvokeMethod,null,shell,new object[]{path});var lt=link.GetType();lt.InvokeMember("TargetPath",BindingFlags.SetProperty,null,link,new object[]{target});lt.InvokeMember("WorkingDirectory",BindingFlags.SetProperty,null,link,new object[]{working});lt.InvokeMember("Arguments",BindingFlags.SetProperty,null,link,new object[]{arguments});lt.InvokeMember("IconLocation",BindingFlags.SetProperty,null,link,new object[]{Path.Combine(working,"Sintet.ico")});lt.InvokeMember("Save",BindingFlags.InvokeMethod,null,link,null);}
   finally{if(link!=null)Marshal.FinalReleaseComObject(link);if(shell!=null)Marshal.FinalReleaseComObject(shell);}
  }
  static void RemoveShortcut(string path,string directory){
   if(!File.Exists(path))return;object shell=null,link=null;
   try{var type=Type.GetTypeFromProgID("WScript.Shell");shell=Activator.CreateInstance(type);link=type.InvokeMember("CreateShortcut",BindingFlags.InvokeMethod,null,shell,new object[]{path});string target=(string)link.GetType().InvokeMember("TargetPath",BindingFlags.GetProperty,null,link,null);if(String.Equals(Path.GetDirectoryName(Path.GetFullPath(target)),directory,StringComparison.OrdinalIgnoreCase))File.Delete(path);}
   finally{if(link!=null)Marshal.FinalReleaseComObject(link);if(shell!=null)Marshal.FinalReleaseComObject(shell);}
  }
  internal static void VerifyShortcut(string directory){
   string path=Path.Combine(directory,"shortcut-check.lnk");
   Shortcut(path,Path.Combine(directory,"Sintet-launcher.exe"),directory,"");
   if(!File.Exists(path))throw new IOException("Shortcut was not created");
   RemoveShortcut(path,directory);
   if(File.Exists(path))throw new IOException("Shortcut target verification failed");
  }
  public static List<string> UserDataTargets(){
   string documents=Environment.GetFolderPath(Environment.SpecialFolder.MyDocuments);
   string root=Path.GetFullPath(Path.Combine(documents,"Sintet"));
   var targets=new List<string>{root};
   string settings=Path.Combine(root,"settings.json");
   if(File.Exists(settings)){
    try{
     var values=new JavaScriptSerializer().Deserialize<Dictionary<string,object>>(File.ReadAllText(settings));
     object value;string game=values!=null&&values.TryGetValue("gameDirectory",out value)?value as string:null;
     if(!String.IsNullOrWhiteSpace(game)){
      game=Path.GetFullPath(Environment.ExpandEnvironmentVariables(game));
      if(game.StartsWith(@"\\",StringComparison.Ordinal))throw new IOException(SetupLanguage.T("Сетевая папка игры не может быть удалена этим установщиком."));
      if(!IsWithin(game,root)&&!targets.Exists(p=>String.Equals(Path.GetFullPath(p),game,StringComparison.OrdinalIgnoreCase)))targets.Add(game);
     }
    }catch{throw new IOException(SetupLanguage.T("Не удалось безопасно прочитать папку Minecraft из настроек Sintet."));}
   }
   return targets;
  }
  static bool IsWithin(string path,string parent){return String.Equals(path,parent,StringComparison.OrdinalIgnoreCase)||path.StartsWith(parent.TrimEnd(Path.DirectorySeparatorChar)+Path.DirectorySeparatorChar,StringComparison.OrdinalIgnoreCase);}
  static void ValidateDataTargets(string install,List<string> targets){
   string documents=Path.GetFullPath(Environment.GetFolderPath(Environment.SpecialFolder.MyDocuments)).TrimEnd(Path.DirectorySeparatorChar);
   string user=Path.GetFullPath(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile)).TrimEnd(Path.DirectorySeparatorChar);
   string local=Path.GetFullPath(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData)).TrimEnd(Path.DirectorySeparatorChar);
   string roaming=Path.GetFullPath(Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData)).TrimEnd(Path.DirectorySeparatorChar);
   string temp=Path.GetFullPath(Path.GetTempPath()).TrimEnd(Path.DirectorySeparatorChar);
   string desktop=Path.GetFullPath(Environment.GetFolderPath(Environment.SpecialFolder.DesktopDirectory)).TrimEnd(Path.DirectorySeparatorChar);
   string downloads=Path.Combine(user,"Downloads");
   foreach(string item in targets){
    string target=Path.GetFullPath(item).TrimEnd(Path.DirectorySeparatorChar);string volume=Path.GetPathRoot(target).TrimEnd(Path.DirectorySeparatorChar);
    if(target.Length==volume.Length||new[]{documents,user,local,roaming,temp,desktop,downloads}.Contains(target,StringComparer.OrdinalIgnoreCase))throw new IOException(SetupLanguage.T("Настройки указывают на слишком широкую папку. Данные не удалены. Исправь папку игры в настройках и повтори."));
    if(IsWithin(install,target)||IsWithin(target,install))throw new IOException(SetupLanguage.T("Папка игры пересекается с папкой установленного лаунчера. Удали или перемести лаунчер отдельно перед очисткой данных."));
   }
  }
  static void DeleteTreeWithoutFollowingLinks(string path){
   if(!Directory.Exists(path))return;
   var info=new DirectoryInfo(path);
   if((info.Attributes&FileAttributes.ReparsePoint)!=0){Directory.Delete(path,false);return;}
   foreach(string file in Directory.GetFiles(path)){var item=new FileInfo(file);if((item.Attributes&FileAttributes.ReparsePoint)!=0)File.Delete(file);else File.Delete(file);}
   foreach(string child in Directory.GetDirectories(path))DeleteTreeWithoutFollowingLinks(child);
   Directory.Delete(path,false);
  }
  public static void Uninstall(string directory,bool integrate,bool removeUserData,Action<int,string> progress){
   string root=Normalize(directory);var marker=ReadMarker(root);
   List<string> dataTargets=removeUserData?UserDataTargets():new List<string>();
   if(removeUserData)ValidateDataTargets(root,dataTargets);
   progress(20,SetupLanguage.T("Удаляем лаунчер"));
   string exe=Path.Combine(root,"Sintet-launcher.exe"),uninstaller=Path.Combine(root,"Uninstall.exe");
   // Only the files owned by this installation are touched. Never recurse into
   // the user's chosen directory, Documents, accounts or Minecraft instances.
   if(File.Exists(exe)&&Hash(exe)!=marker.LauncherHash)throw new IOException(SetupLanguage.T("Файл лаунчера изменён. Переустанови Sintet перед удалением."));
   if(File.Exists(uninstaller)&&Hash(uninstaller)!=marker.UninstallerHash)throw new IOException(SetupLanguage.T("Файл удаления изменён. Переустанови Sintet перед удалением."));
   if(File.Exists(exe)){try{using(var check=new FileStream(exe,FileMode.Open,FileAccess.ReadWrite,FileShare.None)){} }catch(IOException){throw new IOException(SetupLanguage.T("Закрой Sintet перед удалением."));}catch(UnauthorizedAccessException){throw new IOException(SetupLanguage.T("Закрой Sintet перед удалением."));}}
   if(integrate){
    using(var run=Registry.CurrentUser.OpenSubKey(@"Software\Microsoft\Windows\CurrentVersion\Run",true)){
     if(run!=null){string command=run.GetValue("Sintet","").ToString();string expected="\""+exe+"\"";if(command.StartsWith(expected,StringComparison.OrdinalIgnoreCase)||command.StartsWith(exe+" ",StringComparison.OrdinalIgnoreCase))run.DeleteValue("Sintet",false);}
    }
    RemoveShortcut(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.DesktopDirectory),"Sintet.lnk"),root);
    string menu=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.Programs),"Sintet");
    RemoveShortcut(Path.Combine(menu,"Sintet.lnk"),root);RemoveUninstallShortcuts(menu,root);if(Directory.Exists(menu)&&Directory.GetFileSystemEntries(menu).Length==0)Directory.Delete(menu,false);
    using(var key=Registry.CurrentUser.OpenSubKey(RegistryPath)){if(key!=null&&String.Equals(key.GetValue("InstallLocation","").ToString(),root,StringComparison.OrdinalIgnoreCase))Registry.CurrentUser.DeleteSubKeyTree(RegistryPath,false);}
   }
   progress(65,removeUserData?SetupLanguage.T("Удаляем Minecraft и личные данные"):SetupLanguage.T("Сохраняем твои миры и профили"));
   foreach(string name in new[]{"Sintet-launcher.exe","Sintet.ico","Uninstall.exe","install.xml"}){string file=Path.Combine(root,name);if(File.Exists(file))File.Delete(file);}
   if(Directory.GetFileSystemEntries(root).Length==0)Directory.Delete(root,false);
   if(removeUserData)foreach(string target in dataTargets)DeleteTreeWithoutFollowingLinks(target);
   progress(100,SetupLanguage.T("Готово"));
  }
  public static void VerifyPayload(){
   using(var input=Resource("payload"))using(var gzip=new GZipStream(input,CompressionMode.Decompress))using(var sha=SHA256.Create())if(Hex(sha.ComputeHash(gzip))!=Package.LauncherHash)throw new IOException("Payload checksum failed");
   using(var input=Resource("webview"))using(var sha=SHA256.Create())if(Hex(sha.ComputeHash(input))!=Package.WebViewHash)throw new IOException("WebView checksum failed");
  }
 }
}
