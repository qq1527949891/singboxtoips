package ui_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
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
	u := ui.New(strings.NewReader("3\n"), io.Discard)
	s, err := u.Wizard(context.Background(), network.ExecRunner{}, csv, nil)
	if err != nil {
		t.Fatal("unexpected extra question after protocol selection", err)
	}
	if len(s.Nodes) != 2 || s.Port != 443 || !s.AutomaticCertificate() || s.ServerName != "203.0.113.10" || s.Nodes[0].PolicyRouting {
		t.Fatal("incorrect recommended defaults")
	}
}
