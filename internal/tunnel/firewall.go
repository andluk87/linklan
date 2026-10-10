package tunnel

import (
	"crypto/sha256"
	"fmt"
	"net"
	"strings"

	"linklan/internal/model"
	"linklan/internal/protocol"
)

func psLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func clientFirewallScript(executable string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(executable)))
	prefix := fmt.Sprintf("LinkLAN-App-%x", sum[:8])
	return fmt.Sprintf(`$ErrorActionPreference='Stop'; $app=%s; foreach($direction in @('Inbound','Outbound')) { $name=%s+'-'+$direction; Remove-NetFirewallRule -Name $name -ErrorAction SilentlyContinue; New-NetFirewallRule -Name $name -DisplayName ('LinkLAN — '+$direction) -Group 'LinkLAN' -Program $app -Direction $direction -Action Allow -Protocol Any -Profile Any | Out-Null }`, psLiteral(executable), psLiteral(prefix))
}

func virtualFirewallScript(n model.Network, allowAll bool) (string, error) {
	_, subnet, err := net.ParseCIDR(n.Subnet)
	ip := net.ParseIP(n.IP)
	if err != nil || ip == nil || ip.To4() == nil || !subnet.Contains(ip) {
		return "", fmt.Errorf("Некорректная виртуальная подсеть")
	}
	if _, err = protocol.ParseID(n.ID); err != nil {
		return "", err
	}
	alias := "LinkLAN-" + n.ID[:8]
	prefix := "LinkLAN-VPN-" + n.ID
	cleanup := fmt.Sprintf(`Remove-NetFirewallRule -Name %s,%s -ErrorAction SilentlyContinue; Remove-NetFirewallRule -DisplayName %s -ErrorAction SilentlyContinue; `, psLiteral(prefix+"-Inbound"), psLiteral(prefix+"-Outbound"), psLiteral(alias+" ICMP"))
	if !allowAll {
		return cleanup, nil
	}
	return cleanup + fmt.Sprintf(`foreach($direction in @('Inbound','Outbound')) { New-NetFirewallRule -Name (%s+'-'+$direction) -DisplayName (%s+' — '+$direction) -Group 'LinkLAN' -Direction $direction -Action Allow -InterfaceAlias %s -LocalAddress %s -RemoteAddress %s -Protocol Any -Profile Any | Out-Null }`, psLiteral(prefix), psLiteral(alias), psLiteral(alias), psLiteral(ip.To4().String()), psLiteral(subnet.String())), nil
}
