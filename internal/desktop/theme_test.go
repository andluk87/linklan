package desktop

import (
	"encoding/xml"
	"linklan/internal/model"
	"strings"
	"testing"
	"time"
)

func TestScheduledStartupIsBoundToInteractiveUserAndEscapesPaths(t *testing.T) {
	doc, err := startupTaskXML("S-1-5-21-1234", `C:\Apps\A & B\LinkLAN.exe`, `C:\Apps\A & B`)
	if err != nil {
		t.Fatal(err)
	}
	// The UTF-16 declaration is for schtasks; decode the UTF-8 test string here.
	doc = strings.Replace(doc, `encoding="UTF-16"`, `encoding="UTF-8"`, 1)
	var task struct {
		Principals struct {
			Principal struct {
				UserID              string `xml:"UserId"`
				LogonType, RunLevel string
			}
		}
		Actions struct {
			Exec struct{ Command, Arguments, WorkingDirectory string }
		}
		Triggers struct {
			LogonTrigger struct {
				UserID string `xml:"UserId"`
			}
		}
	}
	if err := xml.Unmarshal([]byte(doc), &task); err != nil {
		t.Fatal(err)
	}
	if task.Principals.Principal.UserID != "S-1-5-21-1234" || task.Triggers.LogonTrigger.UserID != "S-1-5-21-1234" || task.Principals.Principal.LogonType != "InteractiveToken" || task.Principals.Principal.RunLevel != "HighestAvailable" {
		t.Fatal("wrong principal", task)
	}
	if task.Actions.Exec.Command != `C:\Apps\A & B\LinkLAN.exe` || task.Actions.Exec.Arguments != "--start-hidden" {
		t.Fatal("wrong action", task)
	}
	if strings.Contains(doc, "Password") || strings.Contains(doc, "BootTrigger") {
		t.Fatal("startup must not store passwords or run as a machine service")
	}
	if _, err := startupTaskXML("S-1-5</UserId>", "x", "x"); err == nil {
		t.Fatal("invalid SID accepted")
	}
}
func TestCompactLayoutScalesWithoutLosingIPColumn(t *testing.T) {
	for _, dpi := range []int{96, 120, 144, 192} {
		width := scalePixel(compactTheme.MinWidth, dpi)
		right := width - scalePixel(2*compactTheme.Padding+17, dpi)
		split := right - scalePixel(116, dpi)
		if split-scalePixel(38, dpi) < scalePixel(100, dpi) {
			t.Fatalf("name column too narrow at %d", dpi)
		}
		if got := logicalPixel(scalePixel(680, dpi), dpi); got != 680 {
			t.Fatal("geometry drifts", dpi, got)
		}
	}
}
func TestLiveSessionRefreshDoesNotRebuildUnchangedNetworkRows(t *testing.T) {
	a := []model.NetworkView{{Network: model.Network{ID: "n", Name: "Network"}, Peers: []model.Peer{{ID: "p", Name: "PC", Online: true}}}}
	b := []model.NetworkView{{Network: a[0].Network, Peers: append([]model.Peer(nil), a[0].Peers...)}}
	b[0].Peers[0].LastSeen = time.Now()
	b[0].Peers[0].EphemeralKey = "new-session"
	if !sameNetworkLayout(a, b) {
		t.Fatal("session heartbeat must not rebuild rows")
	}
	b[0].Peers[0].IP = "26.0.0.2"
	if sameNetworkLayout(a, b) {
		t.Fatal("IP change must redraw the row")
	}
}
func TestBadgeAndSignalNeverInventAConnection(t *testing.T) {
	if label, _ := connectionBadge(false, false, false); label != "Не в сети" {
		t.Fatal(label)
	}
	if label, _ := connectionBadge(false, true, true); label != "Ошибка" {
		t.Fatal(label)
	}
	if label, _ := connectionBadge(true, true, false); label != "В сети" {
		t.Fatal(label)
	}
	if s := signalDescription(true, false, echoState{Measured: true, Reachable: true}); !strings.Contains(s, "не измерена") {
		t.Fatal(s)
	}
}
