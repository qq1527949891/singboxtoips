package ui_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mipproxy.local/mipproxy/internal/network"
	"mipproxy.local/mipproxy/internal/testutil"
	"mipproxy.local/mipproxy/internal/ui"
)

func TestSingleSelectionRejectsMultipleProtocols(t *testing.T) {
	u := ui.New(strings.NewReader("1 3\n3\n"), io.Discard)
	p, e := u.ChooseProtocol("")
	if e != nil || p != "hysteria2" {
		t.Fatal(p, e)
	}
}

func TestLastAnswerDoesNotNeedTrailingNewline(t *testing.T) {
	u := ui.New(strings.NewReader("3"), io.Discard)
	p, err := u.ChooseProtocol("")
	if err != nil || p != "hysteria2" {
		t.Fatal("discarded final answer at EOF", p, err)
	}
}

func TestAutomaticCertificateDoesNotAskForDomainOrPEMFiles(t *testing.T) {
	s := testutil.State(t, "hysteria2")
	u := ui.New(strings.NewReader("1\n2\n"), io.Discard)
	u.Advanced = true
	if err := u.Certificate(&s); err != nil {
		t.Fatal(err)
	}
	if !s.AutomaticCertificate() || s.ServerName != s.Nodes[1].PublicIP || s.Certificate != "" || s.Key != "" {
		t.Fatal("did not select automatic certificate for the requested IP")
	}
}

func TestWizardRetainsIDsAndCredentialsWhenAddressOrderChanges(t *testing.T) {
	old := testutil.State(t, "reality")
	csv := filepath.Join(t.TempDir(), "ips.csv")
	if e := os.WriteFile(csv, []byte("local_ip,public_ip\n10.0.0.11,203.0.113.11\n10.0.0.10,203.0.113.10\n"), 0600); e != nil {
		t.Fatal(e)
	}
	u := ui.New(strings.NewReader("1\n"), io.Discard)
	s, err := u.Wizard(context.Background(), network.ExecRunner{}, csv, &old)
	if err != nil {
		t.Fatal(err)
	}
	if s.Nodes[0].ID != old.Nodes[1].ID || s.Nodes[0].UUID != old.Nodes[1].UUID || s.Nodes[1].PrivateKey != old.Nodes[0].PrivateKey {
		t.Fatal("existing credentials changed when IP order changed")
	}
}

func TestDefaultWizardOnlyAsksForProtocol(t *testing.T) {
	csv := filepath.Join(t.TempDir(), "ips.csv")
	if err := os.WriteFile(csv, []byte("local_ip,public_ip,interface,gateway,prefix\n203.0.113.10,203.0.113.10,eth0,203.0.113.1,203.0.113.0/24\n203.0.113.11,203.0.113.11,eth0,203.0.113.1,203.0.113.0/24\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		protocol, answer string
		subscriptionPort int
	}{
		{"trojan", "2\n", 8443},
		{"hysteria2", "3\n", 443},
		{"tuic", "5\n", 443},
		{"wireguard", "7\n", 443},
	} {
		t.Run(test.protocol, func(t *testing.T) {
			u := ui.New(strings.NewReader(test.answer), io.Discard)
			s, err := u.Wizard(context.Background(), network.ExecRunner{}, csv, nil)
			if err != nil {
				t.Fatal("unexpected extra question after protocol selection", err)
			}
			if len(s.Nodes) != 2 || s.Port != 443 || !s.AutomaticCertificate() || s.ServerName != "203.0.113.10" || s.Nodes[0].PolicyRouting != (test.protocol == "wireguard") || !s.SubscriptionEnabled() || s.SubscriptionPort != test.subscriptionPort {
				t.Fatal("incorrect recommended defaults")
			}
			u = ui.New(strings.NewReader(test.answer), io.Discard)
			updated, err := u.Wizard(context.Background(), network.ExecRunner{}, csv, &s)
			if err != nil || updated.SubscriptionToken != s.SubscriptionToken || updated.SubscriptionPort != s.SubscriptionPort || updated.Nodes[0].ClientPrivateKey != s.Nodes[0].ClientPrivateKey {
				t.Fatal("ordinary updates must preserve existing subscription URLs and credentials", err)
			}
		})
	}
}

func TestTrojanSubscriptionAvoidsThePlannedProxyPort(t *testing.T) {
	old := testutil.State(t, "trojan")
	old.Port = 8443
	csv := filepath.Join(t.TempDir(), "ips.csv")
	if err := os.WriteFile(csv, []byte("local_ip,public_ip\n10.0.0.10,203.0.113.10\n10.0.0.11,203.0.113.11\n"), 0600); err != nil {
		t.Fatal(err)
	}
	u := ui.New(strings.NewReader("2\n"), io.Discard)
	s, err := u.Wizard(context.Background(), network.ExecRunner{}, csv, &old)
	if err != nil || s.SubscriptionPort == s.Port || s.SubscriptionPort != 9443 {
		t.Fatal("subscription must avoid a proxy port which is not listening yet", s.SubscriptionPort, err)
	}
}

type occupiedPortsRunner struct{}

func (occupiedPortsRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	switch strings.Join(args, " ") {
	case "ip -j -4 address show":
		return []byte(`[{"ifname":"eth0","flags":["UP"],"addr_info":[{"family":"inet","local":"203.0.113.10","prefixlen":24,"scope":"global"},{"family":"inet","local":"203.0.113.11","prefixlen":24,"scope":"global"}]}]`), nil
	case "ip -j -4 route show table all":
		return []byte(`[{"dst":"default","gateway":"203.0.113.1","dev":"eth0"}]`), nil
	case "ss -H -l -n -t":
		return []byte("LISTEN 0 4096 *:443 *:*\nLISTEN 0 4096 *:8443 *:*\nLISTEN 0 4096 *:9443 *:*\nLISTEN 0 4096 *:18443 *:*"), nil
	}
	return nil, nil
}

func TestAdvancedWizardCanChooseAnUnlistedSubscriptionPort(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("automatic address discovery requires Linux")
	}
	// Protocol, addresses, proxy port, policy routing, two public IPs, ACME
	// mode, certificate IP, and a custom subscription port. No real network I/O.
	u := ui.New(strings.NewReader("2\n\n\n\n\n\n1\n1\n80\n443\n25443\n"), io.Discard)
	u.Advanced = true
	s, err := u.Wizard(context.Background(), occupiedPortsRunner{}, "", nil)
	if err != nil || s.SubscriptionPort != 25443 {
		t.Fatal("advanced mode exited before accepting a non-conflicting custom port", s.SubscriptionPort, err)
	}
}
