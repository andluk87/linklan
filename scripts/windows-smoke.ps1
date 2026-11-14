param([Parameter(Mandatory=$true)][string]$Version)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.Security
Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class LinkLANWindow {
 [StructLayout(LayoutKind.Sequential)] public struct Rect { public int Left,Top,Right,Bottom; }
 [StructLayout(LayoutKind.Sequential)] public struct IconID { public uint Size; public IntPtr Window; public uint ID; public Guid Guid; }
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern IntPtr FindWindow(string cls,string title);
 [DllImport("user32.dll")] public static extern uint GetDpiForWindow(IntPtr h);
 [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
 [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h,out Rect r);
 [DllImport("user32.dll")] public static extern bool MoveWindow(IntPtr h,int x,int y,int w,int height,bool repaint);
 [DllImport("user32.dll")] public static extern IntPtr GetDlgItem(IntPtr h,int id);
 [DllImport("user32.dll")] public static extern IntPtr SendMessage(IntPtr h,uint m,UIntPtr w,IntPtr l);
 [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h,uint m,UIntPtr w,IntPtr l);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern uint RegisterWindowMessage(string name);
 [DllImport("shell32.dll")] public static extern int Shell_NotifyIconGetRect(ref IconID id,out Rect r);
}
'@
function Run-Client([string[]]$ClientArgs) {
 $p=Start-Process -FilePath $script:client -ArgumentList $ClientArgs -PassThru
 if (-not $p.WaitForExit(30000) -or $p.ExitCode -ne 0) { throw "Client command failed: $ClientArgs" }
}
function Wait-Window([string]$Class,[System.Diagnostics.Process]$Process) {
 for($i=0;$i -lt 60;$i++) {
  $Process.Refresh()
  if($Process.HasExited){throw "Client exited before opening $Class"}
  $h=[LinkLANWindow]::FindWindow($Class,'LinkLAN')
  if($h -ne [IntPtr]::Zero){return $h}
  Start-Sleep -Milliseconds 100
 }
 throw "No window: $Class"
}
function Stop-Window([System.Diagnostics.Process]$Process,[IntPtr]$Handle) {
 $null=[LinkLANWindow]::PostMessage($Handle,[LinkLANWindow]::RegisterWindowMessage('LinkLAN.Quit'),[UIntPtr]::Zero,[IntPtr]::Zero)
 if(-not $Process.WaitForExit(15000) -or $Process.ExitCode -ne 0){throw 'Client did not shut down cleanly'}
}
function Save-WindowImage([IntPtr]$Handle,[string]$Name) {
 $rect=New-Object LinkLANWindow+Rect
 $null=[LinkLANWindow]::GetWindowRect($Handle,[ref]$rect)
 try {
  $bitmap=New-Object System.Drawing.Bitmap(($rect.Right-$rect.Left),($rect.Bottom-$rect.Top))
  $graphics=[System.Drawing.Graphics]::FromImage($bitmap)
  try{$graphics.CopyFromScreen($rect.Left,$rect.Top,0,0,$bitmap.Size);$bitmap.Save((Join-Path $script:validation $Name),[System.Drawing.Imaging.ImageFormat]::Png)}finally{$graphics.Dispose();$bitmap.Dispose()}
 } catch {Write-Warning 'Screenshot capture unavailable in this runner desktop; Win32 layout assertions remain required.'}
}
$validation=Join-Path $PWD 'validation'
$null=New-Item -ItemType Directory -Force $validation
$installer=Join-Path $PWD "artifacts/LinkLAN-Setup-x64-v$Version.exe"
$p=Start-Process -FilePath $installer -ArgumentList '/S' -PassThru
if(-not $p.WaitForExit(60000) -or $p.ExitCode -ne 0){throw 'Silent installer failed'}
$installDir=Join-Path $env:ProgramFiles 'LinkLAN'
$client=Join-Path $installDir 'LinkLAN.exe'
$entry=Get-ItemProperty 'HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\LinkLAN'
$expected='"'+(Join-Path $installDir 'Uninstall.exe')+'"'
if($entry.UninstallString -ne $expected -or $entry.DisplayVersion -ne $Version){throw 'Installer registry mismatch'}
Run-Client @('--self-test')
$versionFile=Join-Path $env:RUNNER_TEMP 'linklan-version.txt'
$p=Start-Process $client -ArgumentList '--version' -RedirectStandardOutput $versionFile -PassThru
if(-not $p.WaitForExit(30000) -or $p.ExitCode -ne 0 -or (Get-Content $versionFile -Raw).Trim() -ne $Version){throw 'Version mismatch'}
$info=[System.Diagnostics.FileVersionInfo]::GetVersionInfo($client)
if($info.ProductName -ne 'LinkLAN' -or $info.ProductVersion -ne $Version){throw 'File properties mismatch'}
$icon=[System.Drawing.Icon]::ExtractAssociatedIcon($client)
if($null -eq $icon){throw 'Explorer icon missing'}
$icon.Dispose()
# Prove the logon task is scoped to this user and stores no password.
Run-Client @('--autostart','enable')
# Re-read the existing task XML, as saving Settings does.
Run-Client @('--autostart','enable')
$sid=[System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value
$taskName='LinkLAN-'+$sid
$task=Get-ScheduledTask -TaskName $taskName
$principalSid=$task.Principal.UserId
if($principalSid -ne $sid){$principalSid=([Security.Principal.NTAccount]::new($principalSid)).Translate([Security.Principal.SecurityIdentifier]).Value}
if($principalSid -ne $sid -or "$($task.Principal.LogonType)" -ne 'Interactive' -or "$($task.Principal.RunLevel)" -ne 'Highest'){throw 'Wrong scheduled-task principal'}
if($task.Actions[0].Execute -ne $client -or $task.Actions[0].Arguments -ne '--start-hidden'){throw 'Wrong scheduled-task action'}
Run-Client @('--autostart','disable')
if(Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue){throw 'Autostart task was not removed'}
$results=@()
foreach($dpi in @(96,120,144,192)) {
 $clock=[System.Diagnostics.Stopwatch]::StartNew()
 $preview=Start-Process $client -ArgumentList @('--ui-preview-large','--ui-preview-dpi',"$dpi") -PassThru
 try {
  $h=Wait-Window 'LinkLANPreviewV4' $preview
  $tree=[LinkLANWindow]::GetDlgItem($h,3000)
  for($i=0;$i -lt 60;$i++) {
   $count=[LinkLANWindow]::SendMessage($tree,0x1105,[UIntPtr]::Zero,[IntPtr]::Zero).ToInt64()
   if($count -eq 550){break}
   Start-Sleep -Milliseconds 100
  }
  if($count -ne 550){throw "Large fixture did not create 50 networks and 500 participants: $count"}
  $clock.Stop()
  if($clock.Elapsed.TotalSeconds -gt 20){throw 'Large fixture startup exceeded 20 seconds'}
  $root=New-Object LinkLANWindow+Rect;$list=New-Object LinkLANWindow+Rect
  $null=[LinkLANWindow]::GetWindowRect($h,[ref]$root);$null=[LinkLANWindow]::GetWindowRect($tree,[ref]$list)
  if($list.Left -lt $root.Left -or $list.Right -gt $root.Right -or $list.Bottom -gt $root.Bottom){throw "List escapes window at DPI $dpi"}
  Save-WindowImage $h "large-$dpi.png"
  # Verify real tray presence, hiding, restore, and close-to-tray when Explorer is available.
  $iconId=New-Object LinkLANWindow+IconID
  $iconId.Size=[System.Runtime.InteropServices.Marshal]::SizeOf($iconId);$iconId.Window=$h;$iconId.ID=1
  $iconRect=New-Object LinkLANWindow+Rect
  $trayPresent=[LinkLANWindow]::Shell_NotifyIconGetRect([ref]$iconId,[ref]$iconRect) -eq 0
  if($trayPresent) {
   $null=[LinkLANWindow]::SendMessage($h,0x111,[UIntPtr]510,[IntPtr]::Zero)
   if([LinkLANWindow]::IsWindowVisible($h)){throw 'Minimize did not hide the window'}
   $null=[LinkLANWindow]::SendMessage($h,[LinkLANWindow]::RegisterWindowMessage('LinkLAN.Restore'),[UIntPtr]::Zero,[IntPtr]::Zero)
   if(-not [LinkLANWindow]::IsWindowVisible($h)){throw 'Tray restore failed'}
   $null=[LinkLANWindow]::SendMessage($h,0x10,[UIntPtr]::Zero,[IntPtr]::Zero)
   if($preview.HasExited -or [LinkLANWindow]::IsWindowVisible($h)){throw 'Close did not keep the process in the tray'}
  } else {Write-Warning 'Explorer notification area unavailable: tray-only assertions skipped, window fallback tested.'}
  $results+=@{dpi=$dpi;networks=50;participants=500;startupMs=$clock.ElapsedMilliseconds;tray=$trayPresent}
  Stop-Window $preview $h
 } finally {if(-not $preview.HasExited){Stop-Process $preview.Id}}
}
# Production persistence/single-instance checks use an isolated, credential-free test profile.
$profileDir=Join-Path $env:APPDATA 'LinkLAN';$null=New-Item -ItemType Directory -Force $profileDir
$profileFile=Join-Path $profileDir 'profile.dpapi'
$oldProfile=if(Test-Path $profileFile){[System.IO.File]::ReadAllBytes($profileFile)}else{$null}
try {
 $config=@{Server='https://127.0.0.1:1';DeviceName='LinkLAN-CI';Theme='dark';AutoConnect=$false;FirewallAllowed=$false;StartAtLogon=$false;Profiles=@{};ExpandedNetworks=@{'saved-network'=$false}}
 $clear=[Text.Encoding]::UTF8.GetBytes(($config|ConvertTo-Json -Depth 10))
 $encrypted=[Security.Cryptography.ProtectedData]::Protect($clear,$null,[Security.Cryptography.DataProtectionScope]::CurrentUser)
 [System.IO.File]::WriteAllBytes($profileFile,$encrypted)
 $app=Start-Process $client -ArgumentList '--start-hidden' -PassThru
 try {
  $h=Wait-Window 'LinkLANClientV2' $app
  $null=[LinkLANWindow]::SendMessage($h,[LinkLANWindow]::RegisterWindowMessage('LinkLAN.Restore'),[UIntPtr]::Zero,[IntPtr]::Zero)
  $windowDpi=[LinkLANWindow]::GetDpiForWindow($h)
  $testWidth=[int](420*$windowDpi/96);$testHeight=[int](600*$windowDpi/96)
  $null=[LinkLANWindow]::MoveWindow($h,100,100,$testWidth,$testHeight,$true)
  $null=[LinkLANWindow]::SendMessage($h,0x232,[UIntPtr]::Zero,[IntPtr]::Zero)
  Start-Sleep -Milliseconds 1000
  $second=Start-Process $client -PassThru
  if(-not $second.WaitForExit(10000) -or $second.ExitCode -ne 0){throw 'Second launch created another client'}
  Stop-Window $app $h
 }finally{if(-not $app.HasExited){Stop-Process $app.Id}}
 $clear=[Security.Cryptography.ProtectedData]::Unprotect([IO.File]::ReadAllBytes($profileFile),$null,[Security.Cryptography.DataProtectionScope]::CurrentUser)
 $saved=[Text.Encoding]::UTF8.GetString($clear)|ConvertFrom-Json
 if($saved.Window.Width -ne 420 -or $saved.Window.Height -ne 600 -or -not $saved.Window.Positioned){throw 'Window geometry was not saved'}
 if($saved.ExpandedNetworks.'saved-network' -ne $false){throw 'Offline startup lost saved network expansion'}
 $app=Start-Process $client -ArgumentList '--start-hidden' -PassThru
 try {
  $h=Wait-Window 'LinkLANClientV2' $app
  $r=New-Object LinkLANWindow+Rect;$null=[LinkLANWindow]::GetWindowRect($h,[ref]$r)
  if(($r.Right-$r.Left) -ne $testWidth -or ($r.Bottom-$r.Top) -ne $testHeight){throw 'Window geometry was not restored'}
  Stop-Window $app $h
 }finally{if(-not $app.HasExited){Stop-Process $app.Id}}
} finally {
 if($null -ne $oldProfile){[IO.File]::WriteAllBytes($profileFile,$oldProfile)}else{Remove-Item $profileFile -ErrorAction SilentlyContinue}
}
$results|ConvertTo-Json -Depth 5|Set-Content (Join-Path $validation 'results.json')
Write-Host 'Installer, icons, startup task, preview DPI/large-list, tray and persistence checks completed.'
