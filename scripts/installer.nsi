Unicode true
!include "MUI2.nsh"
!include "x64.nsh"
!include "WinVer.nsh"
!ifndef VERSION
!define VERSION "0.2.5"
!endif
Name "LinkLAN"
OutFile "${OUTPUT}"
InstallDir "$PROGRAMFILES64\LinkLAN"
RequestExecutionLevel admin
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "Russian"
Function .onInit
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
