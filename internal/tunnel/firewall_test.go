package tunnel

import (
	"linklan/internal/model"
	"strings"
	"testing"
)

func TestFirewallAllTrafficIsLimitedToVirtualInterfaceAndSubnet(t *testing.T) {
	n := model.Network{ID: "c12c70cc-d1b4-4bc6-890d-b6d2eaa05362", Subnet: "10.32.5.0/24", IP: "10.32.5.2"}
	script, err := virtualFirewallScript(n, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"-InterfaceAlias 'LinkLAN-c12c70cc'", "-LocalAddress '10.32.5.2'", "-RemoteAddress '10.32.5.0/24'", "-Protocol Any", "@('Inbound','Outbound')", "-Profile Any"} {
		if !strings.Contains(script, scope) {
			t.Fatal("missing scope", scope)
		}
	}
	denied, err := virtualFirewallScript(n, false)
	if err != nil || strings.Contains(denied, "New-NetFirewallRule") {
		t.Fatal("declined consent adds rules", err)
	}
	n.Subnet = "10.32.5.0/24'; Set-NetFirewallProfile -Enabled False; '"
	if _, err = virtualFirewallScript(n, true); err == nil {
		t.Fatal("invalid subnet accepted")
	}
}
func TestClientFirewallQuotesExecutableAndBindsProgram(t *testing.T) {
	script := clientFirewallScript(`C:\Users\O'Neil\LinkLAN\LinkLAN.exe`)
	if !strings.Contains(script, `$app='C:\Users\O''Neil\LinkLAN\LinkLAN.exe'`) || !strings.Contains(script, "-Program $app") {
		t.Fatal(script)
	}
	if strings.Contains(script, "Set-NetFirewallProfile") {
		t.Fatal("global firewall changed")
	}
}
