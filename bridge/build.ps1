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
$asm=Join-Path $classes 'asm-9.9.1.jar'
$asmHash='6F3828A215C920059A5EFA2FB55C233D6C54EC5CADCA99CE1B1BDD10077C7DDD'
if (!(Test-Path $asm) -or (Get-FileHash $asm).Hash -ne $asmHash) {
    Invoke-WebRequest 'https://repo.maven.apache.org/maven2/org/ow2/asm/asm/9.9.1/asm-9.9.1.jar' -OutFile $asm -TimeoutSec 40
}
if ((Get-FileHash $asm).Hash -ne $asmHash) { throw 'ASM digest mismatch' }
[IO.File]::WriteAllText((Join-Path $PSScriptRoot 'MANIFEST.MF'),"Manifest-Version: 1.0`nPremain-Class: sintet.bridge.SintetBridge`n`n",[Text.UTF8Encoding]::new($false))
Push-Location $classes
try {
    & $jar xf $asm org/objectweb/asm
    if($LASTEXITCODE -ne 0){throw 'ASM extraction failed'}
} finally { Pop-Location }
# The JVM loads agents before Forge and Fabric. Keep this ASM out of their namespace.
# Both substitutions preserve the byte lengths in class constant pools.
$binary=[Text.Encoding]::GetEncoding(28591)
$asmClasses=Join-Path $classes 'sintet/bridge/asm'
New-Item -ItemType Directory -Force $asmClasses | Out-Null
Get-ChildItem -LiteralPath (Join-Path $classes 'org/objectweb/asm') -File -Filter '*.class' | ForEach-Object {
    $data=$binary.GetString([IO.File]::ReadAllBytes($_.FullName))
    $data=$data.Replace('org/objectweb/asm','sintet/bridge/asm').Replace('org.objectweb.asm','sintet.bridge.asm')
    [IO.File]::WriteAllBytes((Join-Path $asmClasses $_.Name),$binary.GetBytes($data))
}
& $javac --release 8 -encoding UTF-8 -cp $classes -d $classes (Join-Path $PSScriptRoot 'SintetBridge.java') (Join-Path $PSScriptRoot 'GameLobbyLimit.java') (Join-Path $PSScriptRoot 'MixedLobbyLogin.java') (Join-Path $PSScriptRoot 'VoicePose.java')
if($LASTEXITCODE -ne 0){throw 'Java bridge compilation failed'}
& $jar cfm (Join-Path $PSScriptRoot 'sintet-bridge.jar') (Join-Path $PSScriptRoot 'MANIFEST.MF') -C $classes sintet -C $PSScriptRoot ASM-LICENSE.txt
if($LASTEXITCODE -ne 0){throw 'Java bridge packaging failed'}
