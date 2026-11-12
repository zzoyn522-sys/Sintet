param([switch]$SkipLauncherBuild,[string]$Version='',[string]$UpdatePublicKey='')
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Set-Location -LiteralPath $root
$go = Join-Path $root '_tools/go/bin/go.exe'
if (!(Test-Path -LiteralPath $go)) { $go=(Get-Command go.exe -CommandType Application).Source }
if (!$Version) { $Version=(Get-Content -LiteralPath (Join-Path $root 'VERSION') -Raw).Trim() }
$resources = Join-Path $PSScriptRoot 'resources'
$release = Join-Path $root 'Release'
New-Item -ItemType Directory -Path $resources,$release -Force | Out-Null

# The installer uses native WPF so it can install WebView2 before the launcher UI
# starts. Windows 10/11 includes the required .NET Framework desktop runtime.
$bootstrapper = Join-Path $resources 'MicrosoftEdgeWebview2Setup.exe'
if (-not (Test-Path -LiteralPath $bootstrapper)) {
    Invoke-WebRequest 'https://go.microsoft.com/fwlink/p/?LinkId=2124703' -OutFile $bootstrapper
}
$signature = Get-AuthenticodeSignature -LiteralPath $bootstrapper
if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch 'O=Microsoft Corporation') {
    throw 'Microsoft WebView2 bootstrapper signature verification failed.'
}

$launcher = Join-Path $resources 'Sintet-launcher.exe'
if (-not $SkipLauncherBuild) {
& (Join-Path $root 'bridge/build.ps1')
cmd /c native\build.bat
if ($LASTEXITCODE -ne 0) { throw 'Overlay build failed.' }
& (Join-Path $root 'updater/build.ps1') -Version $Version -UpdatePublicKey $UpdatePublicKey
& $go build -trimpath -tags desktop,production -ldflags "-s -w -H windowsgui -X main.launcherVersion=$Version -X main.defaultIdentityURL=https://sintet.xyz" -o $launcher .
if ($LASTEXITCODE -ne 0) { throw 'Launcher build failed.' }
}
$packed = Join-Path $resources 'launcher.gz'
$input = [IO.File]::OpenRead($launcher)
$output = [IO.File]::Create($packed)
$gzip = [IO.Compression.GZipStream]::new($output,[IO.Compression.CompressionMode]::Compress)
try { $input.CopyTo($gzip) } finally { $input.Dispose();$gzip.Dispose();$output.Dispose() }
$launcherHash = (Get-FileHash -LiteralPath $launcher -Algorithm SHA256).Hash.ToLowerInvariant()
$webViewHash = (Get-FileHash -LiteralPath $bootstrapper -Algorithm SHA256).Hash.ToLowerInvariant()
$launcherSize = (Get-Item -LiteralPath $launcher).Length
$keyPath=Join-Path $env:USERPROFILE '.sintet-release\update-signing-key.pem'
if([string]::IsNullOrWhiteSpace($UpdatePublicKey) -and (Test-Path -LiteralPath $keyPath)){
    $UpdatePublicKey=& $go run .\cmd\update-release -key $keyPath -public-key
    if($LASTEXITCODE -ne 0){throw 'Не удалось получить публичный ключ обновлений для установщика.'}
}
if([string]::IsNullOrWhiteSpace($UpdatePublicKey)){
    $source=[IO.File]::ReadAllText((Join-Path $root 'updates.go'))
    $match=[regex]::Match($source,'var updatePublicKeyHex\s*=\s*"([0-9a-f]{64})"')
    if(-not $match.Success){throw 'Публичный ключ обновлений не найден.'}
    $UpdatePublicKey=$match.Groups[1].Value
}
if($UpdatePublicKey -notmatch '^[0-9a-f]{64}$'){throw 'Некорректный публичный ключ обновлений.'}
$verifier=Join-Path $resources 'Sintet-UpdateVerifier.exe'
& $go build -trimpath -ldflags "-s -w -H windowsgui -X main.publicKeyHex=$UpdatePublicKey" -o $verifier .\cmd\update-verify
if($LASTEXITCODE -ne 0){throw 'Не удалось собрать проверку подписи установщика.'}
$package = @"
namespace SintetSetup { public static class Package {
public const string Version="$Version";
public const string LauncherHash="$launcherHash";
public const string WebViewHash="$webViewHash";
public const long LauncherSize=${launcherSize}L;
} }
"@
[IO.File]::WriteAllText((Join-Path $PSScriptRoot 'Package.cs'),$package,[Text.UTF8Encoding]::new($false))
$parsedVersion=[Version]::Parse($Version)
$assemblyVersion="$($parsedVersion.Major).$($parsedVersion.Minor).$($parsedVersion.Build).0"
$assemblyInfo=@"
using System.Reflection;
[assembly: AssemblyTitle("Sintet Setup")]
[assembly: AssemblyProduct("Sintet")]
[assembly: AssemblyCompany("Sintet")]
[assembly: AssemblyVersion("$assemblyVersion")]
[assembly: AssemblyFileVersion("$assemblyVersion")]
"@
[IO.File]::WriteAllText((Join-Path $PSScriptRoot 'AssemblyInfo.cs'),$assemblyInfo,[Text.UTF8Encoding]::new($false))
$setupManifest=Join-Path $PSScriptRoot 'setup.manifest'
$manifestText=[IO.File]::ReadAllText($setupManifest)
$manifestText=[regex]::Replace($manifestText,'(<assemblyIdentity version=")[^"]+(")',('$1'+$assemblyVersion+'$2'))
[IO.File]::WriteAllText($setupManifest,$manifestText,[Text.UTF8Encoding]::new($false))
$framework = Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319'
$compiler = Join-Path $framework 'csc.exe'
$arguments = @('/nologo','/target:winexe','/platform:x64','/optimize+','/utf8output','/codepage:65001',
    "/out:$release\Sintet-Setup.exe", "/win32manifest:$PSScriptRoot\setup.manifest", "/win32icon:$resources\Sintet.ico",
    "/resource:$packed,payload", "/resource:$bootstrapper,webview", "/resource:$resources\Sintet.ico,icon",
    "/resource:$resources\Sintet.png,image", "/resource:$verifier,updateVerifier", "/resource:$PSScriptRoot\Installer.xaml,view",
    '/r:System.dll','/r:System.Core.dll','/r:System.Xml.dll','/r:System.Windows.Forms.dll','/r:System.Web.Extensions.dll',
    "/r:$framework\WPF\PresentationFramework.dll", "/r:$framework\WPF\PresentationCore.dll",
    "/r:$framework\WPF\WindowsBase.dll", "/r:$framework\System.Xaml.dll",
    "$PSScriptRoot\AssemblyInfo.cs", "$PSScriptRoot\Package.cs", "$PSScriptRoot\Engine.cs", "$PSScriptRoot\SetupLanguage.cs", "$PSScriptRoot\Setup.cs")
& $compiler $arguments
if ($LASTEXITCODE -ne 0) { throw 'Installer build failed.' }
$installer = Join-Path $release 'Sintet-Setup.exe'
Get-Item -LiteralPath $installer | Select-Object FullName,Length,LastWriteTime
