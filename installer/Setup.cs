using System;
using System.Diagnostics;
using System.IO;
using System.Reflection;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Markup;
using System.Windows.Media;
using System.Windows.Media.Animation;
using System.Windows.Media.Imaging;

namespace SintetSetup {
 public static class Entry {
  [STAThread] public static int Main(string[] args){
   bool headless=args.Length>0&&(args[0]=="--verify"||args[0]=="--test-install"||args[0].StartsWith("--preview"));
   try{
    if(args.Length>0&&args[0]=="--verify"){Engine.VerifyPayload();File.WriteAllText(args[1],"Payload and WebView2 checksums passed");return 0;}
    if(args.Length>0&&args[0]=="--test-install"){
     var options=new InstallOptions{Directory=args[1],Integrate=false};Engine.Install(options,delegate(int p,string message){});
     if(Engine.Hash(Path.Combine(args[1],"Sintet-launcher.exe"))!=Engine.ReadMarker(args[1]).LauncherHash)throw new Exception("Extraction checksum failed");
     // Exercise replacement and uninstallation while preserving unrelated files.
     Engine.Install(options,delegate(int p,string message){});Engine.VerifyShortcut(args[1]);File.WriteAllText(Path.Combine(args[1],"keep-me.txt"),"User file");
     Engine.Uninstall(args[1],false,false,delegate(int p,string message){});
     if(!File.Exists(Path.Combine(args[1],"keep-me.txt"))||File.Exists(Path.Combine(args[1],"Sintet-launcher.exe")))throw new Exception("Uninstall ownership check failed");
     File.WriteAllText(args[2],"Install, update, hash verification and non-destructive uninstall passed");return 0;
    }
    string remove=null;
    if(args.Length>0&&args[0].StartsWith("--preview-uninstall"))remove=Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData),"Programs","Sintet");
    if(args.Length>0&&args[0]=="--uninstall"){
     string root=Path.GetDirectoryName(Assembly.GetExecutingAssembly().Location);Engine.ReadMarker(root);
     string temp=Path.Combine(Path.GetTempPath(),"Sintet-uninstall-"+Guid.NewGuid().ToString("N"));Directory.CreateDirectory(temp);
     string copy=Path.Combine(temp,"Sintet-Uninstall.exe");File.Copy(Assembly.GetExecutingAssembly().Location,copy);
     Process.Start(new ProcessStartInfo(copy,"--remove \""+root+"\""){UseShellExecute=false});return 0;
    }
    if(args.Length==2&&args[0]=="--remove"){
     remove=Engine.Normalize(args[1]);var marker=Engine.ReadMarker(remove);
     if(marker.UninstallerHash!=Engine.Hash(Assembly.GetExecutingAssembly().Location))throw new IOException(SetupLanguage.T("Версия программы удаления не совпадает с установленной."));
    }
    var application=new Application();var ui=new SetupWindow(remove);
    if(args.Length>0&&args[0].StartsWith("--preview")){ui.RenderPreview(args[1],args[0]);return 0;}
    application.Run(ui.Window);return 0;
   }catch(Exception error){
    if(headless){if(args.Length>1)File.WriteAllText(args[args.Length-1]+".error",error.ToString());}
    else MessageBox.Show(error.Message,"Sintet",MessageBoxButton.OK,MessageBoxImage.Information);
    return 1;
   }
  }
 }
 public sealed class SetupWindow {
  public readonly Window Window;
  readonly string remove;
  bool busy,finished;
  string installed;
  public SetupWindow(string uninstall){
   remove=uninstall;
   using(var input=Engine.Resource("view"))Window=(Window)XamlReader.Load(input);
   SetupLanguage.Apply(Window);
   ApplyWindowChrome(Engine.IsWindows11);
   using(var input=Engine.Resource("image")){var image=new BitmapImage();image.BeginInit();image.CacheOption=BitmapCacheOption.OnLoad;image.StreamSource=input;image.EndInit();image.Freeze();Window.Icon=image;}
   Get<TextBox>("InstallPath").Text=remove??Engine.DefaultPath;
   bool webview=Engine.HasWebView();Get<TextBlock>("WebViewStatus").Text=webview?SetupLanguage.T("Уже установлен"):SetupLanguage.T("Установим сами");
   Get<Button>("CloseButton").Click+=delegate{if(!busy)Window.Close();};
   Get<FrameworkElement>("DragRegion").MouseLeftButtonDown+=delegate(object sender,MouseButtonEventArgs e){if(e.ButtonState==MouseButtonState.Pressed)Window.DragMove();};
   Get<Button>("BrowseButton").Click+=delegate{
    using(var dialog=new System.Windows.Forms.FolderBrowserDialog()){
     dialog.Description=SetupLanguage.T("Папка для Sintet");dialog.SelectedPath=Get<TextBox>("InstallPath").Text;
     if(dialog.ShowDialog()==System.Windows.Forms.DialogResult.OK){string path=dialog.SelectedPath;if(!String.Equals(Path.GetFileName(path),"Sintet",StringComparison.OrdinalIgnoreCase))path=Path.Combine(path,"Sintet");Get<TextBox>("InstallPath").Text=path;}
    }
   };
   Get<Button>("PrimaryButton").Click+=Install;
   Window.Closing+=delegate(object sender,System.ComponentModel.CancelEventArgs e){e.Cancel=busy;};
   Window.Loaded+=delegate{Entrance();AnimateSpinner();};
   if(remove!=null){
    Get<TextBlock>("WelcomeTitle").Text=SetupLanguage.T("Удаление Sintet");Get<TextBlock>("WelcomeSubtitle").Text=SetupLanguage.T("Лаунчер можно установить снова в любой момент.");
    Get<StackPanel>("InstallPathSection").Visibility=Visibility.Collapsed;Get<StackPanel>("RemoveDataSection").Visibility=Visibility.Visible;
    Get<TextBlock>("FooterNote").Text=SetupLanguage.T("Minecraft и личные данные сохранятся");Get<Button>("PrimaryButton").Content=SetupLanguage.T("Удалить");
    Get<TextBlock>("RemoveDataDetail").Text=SetupLanguage.T("Удалятся миры, моды, аккаунты, настройки и файлы игры из папок данных Sintet и выбранной папки Minecraft.");
    Get<CheckBox>("RemoveUserData").Checked+=delegate{Get<TextBlock>("RemoveDataDetail").Visibility=Visibility.Visible;Get<TextBlock>("FooterNote").Text=SetupLanguage.T("Minecraft и личные данные будут удалены");Window.Height=465;};
    Get<CheckBox>("RemoveUserData").Unchecked+=delegate{Get<TextBlock>("RemoveDataDetail").Visibility=Visibility.Collapsed;Get<TextBlock>("FooterNote").Text=SetupLanguage.T("Minecraft и личные данные сохранятся");Window.Height=390;};
    Window.Height=390;
   }
  }
  T Get<T>(string name)where T:class{return (T)Window.FindName(name);}
  void ApplyWindowChrome(bool rounded){
   Get<Border>("Outer").CornerRadius=new CornerRadius(rounded?16:0);
  }
  void Entrance(){
   if(!SystemParameters.ClientAreaAnimation)return;
   Get<Grid>("ContentHost").BeginAnimation(UIElement.OpacityProperty,new DoubleAnimation(0,1,TimeSpan.FromMilliseconds(300)));
   ((TranslateTransform)Get<Grid>("ContentHost").RenderTransform).BeginAnimation(TranslateTransform.YProperty,new DoubleAnimation(12,0,TimeSpan.FromMilliseconds(350)){EasingFunction=new CubicEase{EasingMode=EasingMode.EaseOut}});
  }
  void AnimateSpinner(){
   if(!SystemParameters.ClientAreaAnimation)return;
   ((RotateTransform)Get<System.Windows.Shapes.Path>("Spinner").RenderTransform).BeginAnimation(RotateTransform.AngleProperty,new DoubleAnimation(0,360,TimeSpan.FromSeconds(1.4)){RepeatBehavior=RepeatBehavior.Forever});
  }
  void Progress(int percent,string detail){
   Window.Dispatcher.BeginInvoke(new Action(delegate{
    Get<TextBlock>("ProgressDetail").Text=detail;Get<TextBlock>("ProgressPercent").Text=percent+"%";
    double width=Math.Max(1,Get<Grid>("ProgressTrack").ActualWidth)*Math.Min(100,percent)/100;
    Get<Border>("ProgressFill").BeginAnimation(FrameworkElement.WidthProperty,new DoubleAnimation(width,TimeSpan.FromMilliseconds(240)){EasingFunction=new CubicEase{EasingMode=EasingMode.EaseOut}});
   }));
  }
  async void Install(object sender,RoutedEventArgs args){
   if(busy)return;
   if(finished){
    if(remove==null&&Get<CheckBox>("LaunchAfter").IsChecked==true){try{Process.Start(new ProcessStartInfo(Path.Combine(installed,"Sintet-launcher.exe")){WorkingDirectory=installed,UseShellExecute=true});}catch(Exception error){Get<TextBlock>("FinishDetail").Text=error.Message;return;}}
    Window.Close();return;
   }
   bool removeData=remove!=null&&Get<CheckBox>("RemoveUserData").IsChecked==true;
   if(removeData){try{var targets=Engine.UserDataTargets();string details=String.Join("\n",targets.ToArray());var answer=MessageBox.Show(Window,SetupLanguage.T("Будут безвозвратно удалены Minecraft, миры, моды, аккаунты и настройки из этих папок:")+"\n\n"+details+"\n\n"+SetupLanguage.T("Продолжить?"),SetupLanguage.T("Удалить все данные Sintet"),MessageBoxButton.YesNo,MessageBoxImage.Warning);if(answer!=MessageBoxResult.Yes)return;}catch(Exception error){Get<TextBlock>("ErrorText").Text=error.Message;return;}}
   string path;try{path=remove==null?Engine.Normalize(Get<TextBox>("InstallPath").Text):remove;}catch(Exception error){Get<TextBlock>("ErrorText").Text=error.Message;return;}
   busy=true;Get<Button>("CloseButton").IsEnabled=false;Get<Button>("PrimaryButton").IsEnabled=false;Get<Button>("PrimaryButton").Content=SetupLanguage.T("Подождите");
   Get<StackPanel>("WelcomePanel").Visibility=Visibility.Collapsed;Get<StackPanel>("ProgressPanel").Visibility=Visibility.Visible;
   Get<TextBlock>("ProgressTitle").Text=remove==null?SetupLanguage.T("Устанавливаем Sintet"):SetupLanguage.T("Удаляем Sintet");Entrance();
   bool desktop=Get<CheckBox>("DesktopShortcut").IsChecked==true;
   try{
    await Task.Run(delegate{if(remove==null)Engine.Install(new InstallOptions{Directory=path,Desktop=desktop},Progress);else Engine.Uninstall(remove,true,removeData,Progress);});
    await Task.Delay(280);installed=path;finished=true;
    Get<StackPanel>("ProgressPanel").Visibility=Visibility.Collapsed;Get<StackPanel>("FinishPanel").Visibility=Visibility.Visible;
    Get<Button>("PrimaryButton").Content=remove==null?SetupLanguage.T("Готово"):SetupLanguage.T("Закрыть");Get<TextBlock>("FooterNote").Text=remove==null?SetupLanguage.T("Увидимся в игре"):removeData?SetupLanguage.T("Minecraft и выбранные данные удалены"):SetupLanguage.T("Миры и профили сохранены");
    if(remove!=null){Get<TextBlock>("FinishTitle").Text=removeData?SetupLanguage.T("Данные удалены"):SetupLanguage.T("До встречи");Get<TextBlock>("FinishDetail").Text=removeData?SetupLanguage.T("Sintet, Minecraft и все выбранные данные удалены."):SetupLanguage.T("Sintet удалён. Minecraft и профили сохранены.");Get<CheckBox>("LaunchAfter").Visibility=Visibility.Collapsed;}
    Entrance();
   }catch(Exception error){Get<StackPanel>("ProgressPanel").Visibility=Visibility.Collapsed;Get<StackPanel>("WelcomePanel").Visibility=Visibility.Visible;Get<TextBlock>("ErrorText").Text=error.Message;Get<Button>("PrimaryButton").Content=remove==null?SetupLanguage.T("Установить"):SetupLanguage.T("Удалить");Entrance();}
   finally{busy=false;Get<Button>("CloseButton").IsEnabled=true;Get<Button>("PrimaryButton").IsEnabled=true;}
  }
  public void RenderPreview(string path,string stage){
   if(stage=="--preview-win10")ApplyWindowChrome(false);
   if(stage.StartsWith("--preview-uninstall")){Window.Height=stage=="--preview-uninstall-data"?465:390;if(stage=="--preview-uninstall-data")Get<CheckBox>("RemoveUserData").IsChecked=true;}
   if(stage=="--preview-progress"){Get<StackPanel>("WelcomePanel").Visibility=Visibility.Collapsed;Get<StackPanel>("ProgressPanel").Visibility=Visibility.Visible;Get<TextBlock>("ProgressDetail").Text=SetupLanguage.T("Распаковываем лаунчер");Get<TextBlock>("ProgressPercent").Text="72%";Get<Button>("PrimaryButton").Content=SetupLanguage.T("Подождите");Get<Button>("PrimaryButton").IsEnabled=false;Get<Button>("CloseButton").IsEnabled=false;}
   if(stage=="--preview-finish"){Get<StackPanel>("WelcomePanel").Visibility=Visibility.Collapsed;Get<StackPanel>("FinishPanel").Visibility=Visibility.Visible;Get<Button>("PrimaryButton").Content=SetupLanguage.T("Готово");}
   var content=(FrameworkElement)Window.Content;content.Measure(new Size(Window.Width,Window.Height));content.Arrange(new Rect(0,0,Window.Width,Window.Height));content.UpdateLayout();
   if(stage=="--preview-progress")Get<Border>("ProgressFill").Width=Get<Grid>("ProgressTrack").ActualWidth*.72;
   content.UpdateLayout();
   var bitmap=new RenderTargetBitmap((int)(Window.Width*1.5),(int)(Window.Height*1.5),144,144,PixelFormats.Pbgra32);bitmap.Render(content);var encoder=new PngBitmapEncoder();encoder.Frames.Add(BitmapFrame.Create(bitmap));using(var output=File.Create(path))encoder.Save(output);
  }
 }
}

