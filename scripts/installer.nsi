Unicode true
!include "MUI2.nsh"
!include "x64.nsh"
!include "WinVer.nsh"
!ifndef VERSION
!define VERSION "0.4.0"
!endif
!define MUI_ICON "${__FILEDIR__}/../assets/linklan.ico"
!define MUI_UNICON "${__FILEDIR__}/../assets/linklan.ico"
Name "LinkLAN"
OutFile "${OUTPUT}"
InstallDir "$PROGRAMFILES64\LinkLAN"
RequestExecutionLevel admin
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "Russian"
!macro CloseLinkLAN
  StrCpy $1 0
close_client_loop:
  FindWindow $0 "LinkLANClientV2"
  StrCmp $0 0 close_client_done
  SendMessage $0 0x111 106 0 /TIMEOUT=1000
  Sleep 200
  IntOp $1 $1 + 1
  IntCmp $1 50 close_client_failed close_client_loop close_client_failed
close_client_failed:
  MessageBox MB_OK|MB_ICONSTOP "LinkLAN ещё работает. Завершите его через меню трея → Выход и повторите." /SD IDOK
  Abort
close_client_done:
!macroend
Function un.onInit
  IfFileExists "$INSTDIR\LinkLAN.exe" 0 +2
  ExecWait '"$INSTDIR\LinkLAN.exe" --quit' $0
  !insertmacro CloseLinkLAN
  IfFileExists "$INSTDIR\LinkLAN.exe" 0 +2
  ExecWait '"$INSTDIR\LinkLAN.exe" --autostart disable' $0
FunctionEnd
Function .onInit
  IfFileExists "$INSTDIR\LinkLAN.exe" 0 +2
  ExecWait '"$INSTDIR\LinkLAN.exe" --quit' $0
  !insertmacro CloseLinkLAN
  ${IfNot} ${RunningX64}
    MessageBox MB_OK "Требуется Windows x64."
    Abort
  ${EndIf}
  ${IfNot} ${AtLeastWin10}
    MessageBox MB_OK "Требуется Windows 10 или новее."
    Abort
  ${EndIf}
FunctionEnd
Section "LinkLAN"
  SetOutPath "$INSTDIR"
  File "${BUNDLE}\LinkLAN.exe"
  File "${BUNDLE}\wintun.dll"
  File "${BUNDLE}\WINTUN-LICENSE.txt"
  File "${BUNDLE}\README.txt"
  File "${BUNDLE}\THIRD_PARTY_NOTICES.txt"
  CreateShortcut "$DESKTOP\LinkLAN.lnk" "$INSTDIR\LinkLAN.exe"
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\LinkLAN" "DisplayName" "LinkLAN"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\LinkLAN" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\LinkLAN" "UninstallString" '"$INSTDIR\Uninstall.exe"'
SectionEnd
Section "Uninstall"
  Delete "$DESKTOP\LinkLAN.lnk"
  Delete "$INSTDIR\LinkLAN.exe"
  Delete "$INSTDIR\wintun.dll"
  Delete "$INSTDIR\WINTUN-LICENSE.txt"
  Delete "$INSTDIR\README.txt"
  Delete "$INSTDIR\THIRD_PARTY_NOTICES.txt"
  Delete "$INSTDIR\Uninstall.exe"
  Delete "$INSTDIR\LinkLAN.exe.bak"
  Delete "$INSTDIR\wintun.dll.bak"
  RMDir "$INSTDIR"
  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\LinkLAN"
SectionEnd
