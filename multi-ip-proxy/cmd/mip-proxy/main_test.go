package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmptyStateFileReportsCLIError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestEmptyStateCLIHelper$")
	cmd.Env = append(os.Environ(), "MIP_EMPTY_STATE_HELPER="+path)
	b, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(b), "��ȡ״̬") {
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
