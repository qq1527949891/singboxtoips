package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mipproxy.local/mipproxy/internal/system"
	"mipproxy.local/mipproxy/internal/testutil"
	"mipproxy.local/mipproxy/internal/ui"
)

func TestSuccessfulInstallEndsWithCopyableNodes(t *testing.T) {
	s := testutil.State(t, "socks5")
	var output strings.Builder
	terminal := ui.New(strings.NewReader(""), &output)
	installed(terminal, system.Paths(t.TempDir()), s)
	last := s.Nodes[len(s.Nodes)-1]
	expected := fmt.Sprintf("%s/%d/%s/%s\n", last.PublicIP, s.Port, last.Username, last.Password)
	if !strings.HasSuffix(output.String(), expected) {
		t.Fatal("deployment summary did not finish by printing node credentials")
	}
}

func TestEmptyStateFileReportsCLIError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestEmptyStateCLIHelper$")
	cmd.Env = append(os.Environ(), "MIP_EMPTY_STATE_HELPER="+path)
	b, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(b), "读取状态") {
		t.Fatalf("invalid state was silently accepted: %v; %s", err, b)
	}
}

func TestEmptyStateCLIHelper(t *testing.T) {
	path := os.Getenv("MIP_EMPTY_STATE_HELPER")
	if path == "" {
		return
	}
	os.Args = []string{"mip-proxy", "generate", "--state", path, "--out", filepath.Join(filepath.Dir(path), "output")}
	main()
}
