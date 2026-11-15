param([Parameter(Mandatory=$true)][string]$Version)
$ErrorActionPreference='Stop'
$validation=Join-Path $PWD 'validation'
$null=New-Item -ItemType Directory -Force $validation
# Isolate the runner profile; do not modify an existing developer identity.
$env:APPDATA=Join-Path $env:RUNNER_TEMP 'linklan-wpf-profile'
$null=New-Item -ItemType Directory -Force $env:APPDATA
Add-Type -AssemblyName System.Drawing
Add-Type @'
using System;using System.Runtime.InteropServices;
public static class WindowCheck {
 [DllImport("user32.dll",EntryPoint="FindWindowW",CharSet=CharSet.Unicode)]public static extern IntPtr FindWindow(IntPtr cls,string title);
 [DllImport("user32.dll")]public static extern bool IsWindowVisible(IntPtr hwnd);
 [DllImport("user32.dll")]public static extern bool PostMessage(IntPtr hwnd,uint msg,IntPtr w,IntPtr l);
}
'@
function Run-Client([string[]]$ClientArgs,[int]$Timeout=60000){$p=Start-Process $script:client -ArgumentList $ClientArgs -PassThru;if(-not $p.WaitForExit($Timeout)-or $p.ExitCode -ne 0){if(Test-Path (Join-Path $validation 'wpf-check.json')){Write-Host (Get-Content (Join-Path $validation 'wpf-check.json') -Raw)};throw "Client command failed: $ClientArgs / $($p.ExitCode)"}}
$installer=Join-Path $PWD "artifacts/LinkLAN-Setup-x64-v$Version.exe"
$p=Start-Process $installer -ArgumentList '/S' -PassThru
if(-not $p.WaitForExit(90000)-or $p.ExitCode -ne 0){throw 'Installer failed'}
$installDir=Join-Path $env:ProgramFiles 'LinkLAN'
$client=Join-Path $installDir 'LinkLAN.exe'
foreach($name in @('LinkLAN.exe','LinkLAN-engine.exe','LinkLAN.dll','LinkLAN.runtimeconfig.json','PresentationFramework.dll','coreclr.dll','wintun.dll')){if(-not(Test-Path (Join-Path $installDir $name))){throw "Missing self-contained runtime: $name"}}
$entry=Get-ItemProperty 'HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\LinkLAN'
if($entry.DisplayVersion -ne $Version -or $entry.UninstallString -ne ('"'+(Join-Path $installDir 'Uninstall.exe')+'"')){throw 'Installer registry mismatch'}
$versionFile=Join-Path $validation 'version.txt'
$p=Start-Process $client -ArgumentList '--version' -RedirectStandardOutput $versionFile -PassThru
if(-not $p.WaitForExit(30000)-or $p.ExitCode -ne 0 -or (Get-Content $versionFile -Raw).Trim() -ne $Version){throw 'Version mismatch'}
$info=[Diagnostics.FileVersionInfo]::GetVersionInfo($client)
if($info.ProductName -ne 'LinkLAN' -or -not $info.ProductVersion.StartsWith($Version)){throw 'Windows version resource mismatch'}
$icon=[Drawing.Icon]::ExtractAssociatedIcon($client);if($null -eq $icon){throw 'Application icon missing'};$icon.Dispose()
Run-Client @('--self-test')
Run-Client @('--autostart','enable');Run-Client @('--autostart','enable')
$sid=[Security.Principal.WindowsIdentity]::GetCurrent().User.Value;$taskName='LinkLAN-'+$sid
$task=Get-ScheduledTask -TaskName $taskName
$principalSid=$task.Principal.UserId
if($principalSid -ne $sid){$principalSid=([Security.Principal.NTAccount]::new($principalSid)).Translate([Security.Principal.SecurityIdentifier]).Value}
if($principalSid -ne $sid){throw 'Autostart uses another user identity'}
if($task.Actions[0].Execute -ne $client -or $task.Actions[0].Arguments -ne '--start-hidden' -or "$($task.Principal.LogonType)" -ne 'Interactive' -or "$($task.Principal.RunLevel)" -ne 'Highest'){throw 'Autostart does not target WPF frontend'}
Run-Client @('--autostart','disable')
if(Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue){throw 'Autostart not removed'}
# Actual WPF templates, bindings, retained rendering and dialogs execute on Windows.
Run-Client @('--ui-check','--validation-dir',('"'+$validation+'"')) 180000
$report=Get-Content (Join-Path $validation 'wpf-check.json') -Raw|ConvertFrom-Json
if(-not $report.passed -or $report.cases.Count -ne 16){throw 'WPF layout matrix failed'}
# Seed a prior-version DPAPI profile and verify identity/settings survive UI migration.
Add-Type -AssemblyName System.Security
$profile=Join-Path $env:APPDATA 'LinkLAN/profile.dpapi'
$seed=@{DeviceName='Migrated PC';Theme='dark';Language='en';StartAtLogon=$false;StartInTray=$false;AutoConnect=$false;NetworkID='preserved-network';Profiles=@{'device|https://example.test/api'=@{device_id='preserved-device';private_key='fixture-private-key';refresh_token='fixture-refresh';peer_keys=@{peer='fixture-pin'}}};ExpandedNetworks=@{'preserved-network'=$false}}|ConvertTo-Json -Depth 8 -Compress
$encrypted=[Security.Cryptography.ProtectedData]::Protect([Text.Encoding]::UTF8.GetBytes($seed),$null,[Security.Cryptography.DataProtectionScope]::CurrentUser)
[IO.File]::WriteAllBytes($profile,$encrypted)
function Read-Profile {[Text.Encoding]::UTF8.GetString([Security.Cryptography.ProtectedData]::Unprotect([IO.File]::ReadAllBytes($profile),$null,[Security.Cryptography.DataProtectionScope]::CurrentUser))|ConvertFrom-Json}
function Assert-Identity {param($Config)
 $identity=$Config.Profiles.'device|https://example.test/api'
 if($Config.DeviceName -ne 'Migrated PC' -or $Config.NetworkID -ne 'preserved-network' -or $identity.device_id -ne 'preserved-device' -or $identity.private_key -ne 'fixture-private-key' -or $identity.refresh_token -ne 'fixture-refresh' -or $identity.peer_keys.peer -ne 'fixture-pin' -or $Config.Theme -ne 'dark' -or $Config.Language -ne 'en'){throw 'UI migration altered identity or preferences'}
}
# Real DPAPI lifecycle and single-instance forwarding. Autostart is disabled in this fixture.
$p=Start-Process $client -PassThru
try {
 for($i=0;$i -lt 100;$i++){Start-Sleep -Milliseconds 100;$p.Refresh();if($p.HasExited){throw 'Normal frontend exited'};$h=[WindowCheck]::FindWindow([IntPtr]::Zero,'LinkLAN');if($h -ne [IntPtr]::Zero){break}}
 if($h -eq [IntPtr]::Zero){throw 'No WPF main window'}
 Run-Client @('--show')
 $workers=@(Get-CimInstance Win32_Process -Filter "Name='LinkLAN-engine.exe'");if($workers.Count -ne 1){throw 'Multiple engines for one user'}
 $workerId=$workers[0].ProcessId
 $null=[WindowCheck]::PostMessage($h,0x10,[IntPtr]::Zero,[IntPtr]::Zero);Start-Sleep -Milliseconds 500
 $p.Refresh();if($p.HasExited){throw 'Closing the window stopped VPN worker'}
 Run-Client @('--show');Start-Sleep -Milliseconds 300
 if(-not [WindowCheck]::IsWindowVisible($h)){throw 'Second launch failed to restore hidden window'}
 Run-Client @('--quit');if(-not $p.WaitForExit(30000)-or $p.ExitCode -ne 0){throw 'Frontend did not shut down'}
 if(Get-Process -Id $workerId -ErrorAction SilentlyContinue){throw 'Go engine survived frontend exit'}
}finally{if(-not $p.HasExited){$p.Kill($true)}}
# Reinstall over an active WPF process: --quit must wait until both processes release files.
$upgradeClient=Start-Process $client -ArgumentList '--show' -PassThru
try {
 Start-Sleep -Seconds 2
 $upgrade=Start-Process $installer -ArgumentList '/S' -PassThru
 if(-not $upgrade.WaitForExit(90000)-or $upgrade.ExitCode -ne 0){throw 'Upgrade over a running WPF client failed'}
 if(-not $upgradeClient.WaitForExit(10000)){throw 'Upgrade did not await frontend shutdown'}
 if(Get-Process -Name 'LinkLAN-engine' -ErrorAction SilentlyContinue){throw 'Upgrade left Go worker running'}
}finally{if(-not $upgradeClient.HasExited){$upgradeClient.Kill($true)}}
$profile=Join-Path $env:APPDATA 'LinkLAN/profile.dpapi';if(-not(Test-Path $profile)){throw 'Existing DPAPI profile format not preserved'}
Assert-Identity (Read-Profile)
# Uninstall removes only package-owned files, preserving unrelated files and DPAPI profile.
Set-Content (Join-Path $installDir 'user-note.txt') 'keep'
$p=Start-Process (Join-Path $installDir 'Uninstall.exe') -ArgumentList '/S' -PassThru
if(-not $p.WaitForExit(90000)-or $p.ExitCode -ne 0){throw 'Uninstaller failed'}
Start-Sleep -Seconds 2
if(Test-Path $client){throw 'Uninstall left frontend installed'}
if(-not(Test-Path (Join-Path $installDir 'user-note.txt'))){throw 'Uninstall deleted unrelated user file'}
Assert-Identity (Read-Profile)
@{passed=$true;version=$Version;selfContained=$true;wpfCases=$report.cases.Count;autostart=$true;singleInstance=$true;dpapiPreserved=$true;workerShutdown=$true}|ConvertTo-Json|Set-Content (Join-Path $validation 'windows-check.json')
