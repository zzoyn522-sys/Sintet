param()
$ErrorActionPreference='Stop'
$root=Split-Path $PSScriptRoot -Parent
$bundled=Join-Path $root '_tools/jdk'
$jdk=$null
if (Test-Path -LiteralPath $bundled) {
    $jdk=Get-ChildItem -LiteralPath $bundled -Directory | Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName 'bin/javac.exe') } | Select-Object -First 1
}
$javaRoot=if ($jdk) { $jdk.FullName } else { $env:JAVA_HOME }
if ($javaRoot) {
    $javac=Join-Path $javaRoot 'bin/javac.exe'
    $jar=Join-Path $javaRoot 'bin/jar.exe'
} else {
    $javac=(Get-Command javac.exe -CommandType Application).Source
    $jar=(Get-Command jar.exe -CommandType Application).Source
}
if (!(Test-Path -LiteralPath $javac) -or !(Test-Path -LiteralPath $jar)) {
    throw 'Install a JDK and set JAVA_HOME or add its bin directory to PATH.'
}
$classes=Join-Path $PSScriptRoot 'classes'
New-Item -ItemType Directory -Force $classes | Out-Null
& $javac --release 8 -encoding UTF-8 -d $classes (Join-Path $PSScriptRoot 'SintetBridge.java')
if($LASTEXITCODE -ne 0){throw 'Java bridge compilation failed'}
[IO.File]::WriteAllText((Join-Path $PSScriptRoot 'MANIFEST.MF'),"Manifest-Version: 1.0`nPremain-Class: sintet.bridge.SintetBridge`n`n",[Text.UTF8Encoding]::new($false))
& $jar cfm (Join-Path $PSScriptRoot 'sintet-bridge.jar') (Join-Path $PSScriptRoot 'MANIFEST.MF') -C $classes .
if($LASTEXITCODE -ne 0){throw 'Java bridge packaging failed'}
