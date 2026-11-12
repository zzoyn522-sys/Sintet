using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Globalization;
using System.IO;
using System.Net;
using System.Reflection;
using System.Security.Cryptography;
using System.Text;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Markup;
using System.Windows.Media;
using System.Windows.Media.Animation;
using System.Windows.Media.Imaging;

namespace SintetUpdater {
 public static class Entry {
  [STAThread] public static int Main(string[] args) {
   try {
    if((args.Length<4||args.Length>7)||args[0]!="--update")return 2;
    SetupLanguage.Preference=args.Length>=7?args[6]:null;
    var application=new Application();
    application.Run(new UpdateWindow(args[1],args[2],args[3],args.Length>=5?args[4]:null,args.Length>=6?args[5]:null).Window);
    return 0;
   } catch(Exception error) {
    MessageBox.Show(error.Message,"Sintet",MessageBoxButton.OK,MessageBoxImage.Information);
    return 1;
   }
  }
 }

 public sealed class UpdateWindow {
  const string ManifestUrl="https://sintet-updates.zzoyn522.workers.dev/stable/latest.json";
  readonly string target,currentVersion,privateManifest;
  readonly int parentPid;
  public readonly Window Window;
  readonly TextBlock status,detail,version,bytes;
  readonly Border track,fill;
  bool finished,busy;

  public UpdateWindow(string executable,string versionText,string pidText,string scopedManifest=null,string displayVersion=null) {
   privateManifest=scopedManifest;
   target=Path.GetFullPath(executable);currentVersion=versionText;int parsedPid;int.TryParse(pidText,out parsedPid);parentPid=parsedPid;
   using(var input=Resource("view"))Window=(Window)XamlReader.Load(input);
   SetupLanguage.Apply(Window);
   using(var input=Resource("image")){var image=new BitmapImage();image.BeginInit();image.CacheOption=BitmapCacheOption.OnLoad;image.StreamSource=input;image.EndInit();image.Freeze();Window.Icon=image;}
   status=Get<TextBlock>("StatusText");detail=Get<TextBlock>("DetailText");version=Get<TextBlock>("VersionText");bytes=Get<TextBlock>("BytesText");track=Get<Border>("ProgressTrack");fill=Get<Border>("ProgressFill");
   version.Text="v"+(String.IsNullOrEmpty(displayVersion)?currentVersion:displayVersion);
    Get<Button>("CloseButton").Click+=delegate{if(!busy)StartCurrent();};
   Window.Closing+=delegate(object sender,System.ComponentModel.CancelEventArgs e){if(!finished)e.Cancel=true;};
   Window.Loaded+=delegate{AnimateIndeterminate();Begin();};
  }

  T Get<T>(string name) where T:class { return (T)Window.FindName(name); }
  static Stream Resource(string name){var stream=Assembly.GetExecutingAssembly().GetManifestResourceStream(name);if(stream==null)throw new IOException(SetupLanguage.T("Не найден компонент обновления Sintet."));return stream;}
  void AnimateIndeterminate(){
   if(!SystemParameters.ClientAreaAnimation)return;
   var animation=new DoubleAnimation(32,94,TimeSpan.FromMilliseconds(850)){AutoReverse=true,RepeatBehavior=RepeatBehavior.Forever,EasingFunction=new CubicEase{EasingMode=EasingMode.EaseInOut}};
   fill.BeginAnimation(FrameworkElement.WidthProperty,animation);
  }
  void SetProgress(int percent,string text){
   DispatcherUpdate(delegate{
    fill.BeginAnimation(FrameworkElement.WidthProperty,null);fill.Width=Math.Max(2,track.ActualWidth*Math.Max(0,Math.Min(100,percent))/100.0);
    detail.Text=SetupLanguage.T("Скачиваем Sintet");bytes.Text=text;
   });
  }
  void DispatcherUpdate(Action action){Window.Dispatcher.BeginInvoke(action);}
  async void Begin(){
   busy=true;
   try{
    await Task.Run(delegate{WaitForParent();});
    var release=await Task.Run(delegate{return FetchUpdate();});
    if(release==null){status.Text=SetupLanguage.T("Sintet актуален");detail.Text=SetupLanguage.T("Запускаем лаунчер");bytes.Text="";await Task.Delay(420);StartCurrent();return;}
    status.Text=SetupLanguage.T("Проверяем обновление");detail.Text=SetupLanguage.T("Проверяем подпись и целостность");version.Text="v"+release.DisplayVersion;fill.BeginAnimation(FrameworkElement.WidthProperty,null);fill.Width=track.ActualWidth*.96;
    await Task.Run(delegate{Apply(release);});
    status.Text=SetupLanguage.T("Готово");detail.Text=SetupLanguage.T("Запускаем Sintet ")+release.DisplayVersion;bytes.Text="";await Task.Delay(180);StartCurrent();
   }catch(Exception){
    status.Text=SetupLanguage.T("Продолжаем без обновления");detail.Text=SetupLanguage.T("Запускаем установленную версию");bytes.Text="";Task.Delay(650).Wait();StartCurrent();
   }
  }
  void WaitForParent(){if(parentPid<=0)return;try{using(var parent=Process.GetProcessById(parentPid))parent.WaitForExit(15000);}catch{} }
  sealed class Release { public string Version,DisplayVersion,Url,Sha256,Manifest;public long Size;public string DownloadPath; }
  static Version ManifestVersion(string manifest){
   var serializer=new System.Web.Script.Serialization.JavaScriptSerializer();var envelope=serializer.Deserialize<Dictionary<string,object>>(manifest);
   var payload=serializer.Deserialize<Dictionary<string,object>>(Encoding.UTF8.GetString(Convert.FromBase64String(Convert.ToString(envelope["payload"]))));
   return new Version(Convert.ToString(payload["version"]));
  }
  string AccountManifest(){
   string path=Path.Combine(Path.GetTempPath(),"Sintet-scoped-offer-"+Guid.NewGuid().ToString("N")+".json");
   try{
    using(var process=Process.Start(new ProcessStartInfo(target,"--sintet-scoped-offer \""+path+"\""){UseShellExecute=false,CreateNoWindow=true,WindowStyle=ProcessWindowStyle.Hidden})){
     if(process==null)return null;
     if(!process.WaitForExit(15000)){try{process.Kill();}catch{}return null;}
     if(process.ExitCode!=0||!File.Exists(path)||new FileInfo(path).Length>65536)return null;
     return File.ReadAllText(path,Encoding.UTF8);
    }
   }catch{return null;}finally{try{if(File.Exists(path))File.Delete(path);}catch{}}
  }
  Release FetchUpdate(){
   var request=(HttpWebRequest)WebRequest.Create(ManifestUrl);request.Method="GET";request.Timeout=12000;request.ReadWriteTimeout=20000;request.UserAgent="Sintet-Updater/"+currentVersion;
   string manifest;if(!String.IsNullOrEmpty(privateManifest)){
    var file=new FileInfo(privateManifest);if(!file.Exists||file.Length>65536)throw new IOException(SetupLanguage.T("Персональный манифест недоступен."));
    manifest=File.ReadAllText(privateManifest,Encoding.UTF8);
   }else using(var response=(HttpWebResponse)request.GetResponse()){
    if(response.ContentLength>65536)throw new IOException(SetupLanguage.T("Манифест обновлений слишком большой."));
    using(var input=response.GetResponseStream())using(var reader=new StreamReader(input))manifest=reader.ReadToEnd();
   }
   if(String.IsNullOrEmpty(privateManifest)){
    string scoped=AccountManifest();
    if(!String.IsNullOrEmpty(scoped)){try{if(ManifestVersion(scoped)>ManifestVersion(manifest))manifest=scoped;}catch{}}
   }
   var serializer=new System.Web.Script.Serialization.JavaScriptSerializer();var envelope=serializer.Deserialize<Dictionary<string,object>>(manifest);
   byte[] raw=Convert.FromBase64String(Convert.ToString(envelope["payload"]));var payload=serializer.Deserialize<Dictionary<string,object>>(Encoding.UTF8.GetString(raw));
   string candidate=Convert.ToString(payload["version"]),url=Convert.ToString(payload["url"]),sha=Convert.ToString(payload["sha256"]);long size=Convert.ToInt64(payload["size"]);Version candidateVersion,current;
   if(!Version.TryParse(candidate,out candidateVersion)||!Version.TryParse(currentVersion,out current))throw new IOException(SetupLanguage.T("Версия Sintet некорректна."));
   if(candidateVersion<=current)return null;
   Uri uri;if(!Uri.TryCreate(url,UriKind.Absolute,out uri)||uri.Scheme!="https"||uri.Host!="sintet-updates.zzoyn522.workers.dev"||uri.AbsolutePath!="/stable/Sintet-"+candidate+".exe"||size<1||size>350L*1024*1024||sha.Length!=64)throw new IOException(SetupLanguage.T("Манифест содержит недопустимые параметры."));
   string directory=Path.GetDirectoryName(target);string manifestPath=Path.Combine(Path.GetTempPath(),"Sintet-update-"+Guid.NewGuid().ToString("N")+".json");
   string download=Path.Combine(directory,".sintet-update-"+Guid.NewGuid().ToString("N")+".tmp");
   File.WriteAllText(manifestPath,manifest,new UTF8Encoding(false));
   try{
    var downloadRequest=(HttpWebRequest)WebRequest.Create(uri);downloadRequest.Method="GET";downloadRequest.Timeout=20000;downloadRequest.ReadWriteTimeout=120000;downloadRequest.UserAgent="Sintet-Updater/"+currentVersion;
    using(var response=(HttpWebResponse)downloadRequest.GetResponse())using(var input=response.GetResponseStream())using(var output=new FileStream(download,FileMode.CreateNew,FileAccess.Write,FileShare.None,65536,FileOptions.WriteThrough)){
     byte[] buffer=new byte[65536];long copied=0;int count;
     while((count=input.Read(buffer,0,buffer.Length))>0){copied+=count;if(copied>size||copied>350L*1024*1024)throw new IOException(SetupLanguage.T("Сборка Sintet превышает размер из манифеста."));output.Write(buffer,0,count);int percent=(int)(copied*100/size);string amount=(copied/1048576.0).ToString("0.0",CultureInfo.InvariantCulture)+" / "+(size/1048576.0).ToString("0.0",CultureInfo.InvariantCulture)+SetupLanguage.T(" МБ");SetProgress(percent,amount);}
     output.Flush(true);if(copied!=size)throw new IOException(SetupLanguage.T("Размер обновления не совпадает с манифестом."));
    }
    string label=candidate;if(payload.ContainsKey("channel")&&Convert.ToString(payload["channel"])=="test"){var parts=candidate.Split('.');label=parts[0]+"."+parts[1]+"."+parts[2]+" TEST";}
    return new Release{Version=candidate,DisplayVersion=label,Url=url,Sha256=sha,Size=size,Manifest=manifestPath,DownloadPath=download};
   }catch{try{if(File.Exists(download))File.Delete(download);}catch{}try{if(File.Exists(manifestPath))File.Delete(manifestPath);}catch{}throw;}
  }
  void Apply(Release release){
   string verifier=Path.Combine(Path.GetTempPath(),"Sintet-verify-"+Guid.NewGuid().ToString("N")+".exe");
   try{
    using(var input=Resource("updateVerifier"))using(var output=new FileStream(verifier,FileMode.CreateNew,FileAccess.Write,FileShare.None)){input.CopyTo(output);output.Flush(true);}
    var process=Process.Start(new ProcessStartInfo(verifier,"\""+release.Manifest+"\" \""+release.DownloadPath+"\""){UseShellExecute=false,CreateNoWindow=true,WindowStyle=ProcessWindowStyle.Hidden});
    if(process==null||!process.WaitForExit(30000)){if(process!=null)try{process.Kill();}catch{}throw new IOException(SetupLanguage.T("Проверка подписи Sintet не завершилась."));}
    if(process.ExitCode!=0)throw new IOException(SetupLanguage.T("Обновление Sintet не прошло проверку подписи."));
    string backup=target+".sintet-previous";if(File.Exists(backup))File.Delete(backup);
    if(File.Exists(target))File.Replace(release.DownloadPath,target,backup,true);else File.Move(release.DownloadPath,target);
    if(File.Exists(backup))File.Delete(backup);
   }finally{try{if(File.Exists(verifier))File.Delete(verifier);}catch{}try{if(File.Exists(release.Manifest))File.Delete(release.Manifest);}catch{}try{if(File.Exists(release.DownloadPath))File.Delete(release.DownloadPath);}catch{}}
  }
  void StartCurrent(){
   if(finished)return;finished=true;
   try{var start=new ProcessStartInfo(target,"--sintet-skip-update"){UseShellExecute=true,WorkingDirectory=Path.GetDirectoryName(target)};Process.Start(start);}catch{}
   Window.Close();
  }
 }
}
