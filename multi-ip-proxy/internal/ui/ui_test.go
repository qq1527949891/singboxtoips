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

func TestWizardRetainsIDsAndCredentialsWhenAddressOrderChanges(t *testing.T) {
	old := testutil.State(t, "reality")
	csv := filepath.Join(t.TempDir(), "ips.csv")
	if e := os.WriteFile(csv, []byte("local_ip,public_ip\n10.0.0.11,203.0.113.11\n10.0.0.10,203.0.113.10\n"), 0600); e != nil {
		t.Fatal(e)
	}
	u := ui.New(strings.NewReader("1\na\n\nn\n\n\n\n\n\n"), io.Discard)
	s, err := u.Wizard(context.Background(), network.ExecRunner{}, csv, &old)
	if err != nil {
		t.Fatal(err)
	}
	if s.Nodes[0].ID != old.Nodes[1].ID || s.Nodes[0].UUID != old.Nodes[1].UUID || s.Nodes[1].PrivateKey != old.Nodes[0].PrivateKey {
		t.Fatal("existing credentials changed when IP order changed")
	}
}
