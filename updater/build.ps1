param([string]$Version='0.8.5',[string]$UpdatePublicKey='')
$ErrorActionPreference='Stop'
$root=Split-Path $PSScriptRoot -Parent
Set-Location -LiteralPath $root
$go=Join-Path $root '_tools\go\bin\go.exe'
if (!(Test-Path -LiteralPath $go)) { $go=(Get-Command go.exe -CommandType Application).Source }
if([string]::IsNullOrWhiteSpace($UpdatePublicKey)){
  $keyPath=Join-Path $env:USERPROFILE '.sintet-release\update-signing-key.pem'
  if(Test-Path -LiteralPath $keyPath){$UpdatePublicKey=& $go run .\cmd\update-release -key $keyPath -public-key;if($LASTEXITCODE -ne 0){throw 'Не удалось получить ключ подписи обновлений.'}}
}
if([string]::IsNullOrWhiteSpace($UpdatePublicKey)){
  $source=[IO.File]::ReadAllText((Join-Path $root 'updates.go'))
  $match=[regex]::Match($source,'var updatePublicKeyHex\s*=\s*"([0-9a-f]{64})"')
  if(-not $match.Success){throw 'Публичный ключ обновлений не найден.'}
  $UpdatePublicKey=$match.Groups[1].Value
}
if($UpdatePublicKey -notmatch '^[0-9a-f]{64}$'){throw 'Некорректный публичный ключ обновлений.'}
$verifier=Join-Path $PSScriptRoot 'Sintet-UpdateVerifier.exe'
& $go build -trimpath -ldflags "-s -w -H windowsgui -X main.publicKeyHex=$UpdatePublicKey" -o $verifier .\cmd\update-verify
if($LASTEXITCODE -ne 0){throw 'Не удалось собрать проверку подписи обновлений.'}
$framework=Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319'
$compiler=Join-Path $framework 'csc.exe'
$output=Join-Path $PSScriptRoot 'Sintet-Update.exe'
$arguments=@('/nologo','/target:winexe','/platform:x64','/optimize+','/utf8output','/codepage:65001',"/out:$output",
  "/resource:$PSScriptRoot\Updater.xaml,view","/resource:$verifier,updateVerifier",
  "/resource:$(Join-Path $root 'installer\resources\Sintet.png'),image",
  '/r:System.dll','/r:System.Core.dll','/r:System.Xml.dll','/r:System.Web.Extensions.dll',
  "/r:$framework\WPF\PresentationFramework.dll",
  "/r:$framework\WPF\PresentationCore.dll",
  "/r:$framework\WPF\WindowsBase.dll",
  "/r:$framework\System.Xaml.dll",
  "$root\installer\SetupLanguage.cs","$PSScriptRoot\Updater.cs")
& $compiler $arguments
if($LASTEXITCODE -ne 0){throw 'Не удалось собрать окно обновления Sintet.'}
Get-Item -LiteralPath $output | Select-Object FullName,Length,LastWriteTime
