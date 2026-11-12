package desktop

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func startupTaskXML(sid, executable, workingDir string) (string, error) {
	if !strings.HasPrefix(sid, "S-1-") || strings.IndexFunc(sid, func(r rune) bool { return (r < '0' || r > '9') && r != 'S' && r != '-' }) >= 0 {
		return "", fmt.Errorf("invalid user SID")
	}
	if executable == "" || workingDir == "" || strings.ContainsAny(executable+workingDir, "\x00\r\n") {
		return "", fmt.Errorf("invalid application path")
	}
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
<RegistrationInfo><Description>LinkLAN: запуск в трее при входе текущего пользователя</Description></RegistrationInfo>
<Triggers><LogonTrigger><Enabled>true</Enabled><UserId>` + xmlText(sid) + `</UserId><Delay>PT10S</Delay></LogonTrigger></Triggers>
<Principals><Principal id="User"><UserId>` + xmlText(sid) + `</UserId><LogonType>InteractiveToken</LogonType><RunLevel>HighestAvailable</RunLevel></Principal></Principals>
<Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><AllowHardTerminate>true</AllowHardTerminate><StartWhenAvailable>true</StartWhenAvailable><RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable><Enabled>true</Enabled><Hidden>false</Hidden><ExecutionTimeLimit>PT0S</ExecutionTimeLimit><Priority>7</Priority></Settings>
<Actions Context="User"><Exec><Command>` + xmlText(executable) + `</Command><Arguments>--start-hidden</Arguments><WorkingDirectory>` + xmlText(workingDir) + `</WorkingDirectory></Exec></Actions></Task>`, nil
}
