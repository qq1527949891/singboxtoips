package store

import (
	"os"
	"path/filepath"
	"testing"

	"mipproxy.local/mipproxy/internal/testutil"
)

func TestChangingProtocolRemovesOldWireGuardConfigsOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "export")
	wg := testutil.State(t, "wireguard")
	if err := Export(wg, dir); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(dir, "my-notes.txt")
	if err := os.WriteFile(foreign, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Export(testutil.State(t, "reality"), dir); err != nil {
		t.Fatal(err)
	}
	for _, n := range wg.Nodes {
		if _, err := os.Stat(filepath.Join(dir, n.Name("wireguard")+".conf")); !os.IsNotExist(err) {
			t.Fatal("stale WireGuard config remained")
		}
	}
	if b, err := os.ReadFile(foreign); err != nil || string(b) != "keep" {
		t.Fatal("unrelated file changed", err)
	}
}

func TestManifestCannotDeleteForeignFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "export")
	if err := OwnDirectory(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".export-manifest.json"), []byte(`["../outside"]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Export(testutil.State(t, "reality"), dir); err == nil {
		t.Fatal("accepted unsafe export manifest")
	}
}
