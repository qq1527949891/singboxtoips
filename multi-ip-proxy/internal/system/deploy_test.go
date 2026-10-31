package system

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/store"
	"mipproxy.local/mipproxy/internal/testutil"
)

type serviceRunner struct {
	calls                 []string
	active, enabled       map[string]bool
	failCheck, failStart  bool
	failStop              string
	port80Busy            bool
	failSubscriptionStart bool
}

func (r *serviceRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	joined := strings.Join(args, " ")
	r.calls = append(r.calls, joined)
	if args[0] == "ss" && r.port80Busy {
		return []byte("LISTEN 0 128 0.0.0.0:80 0.0.0.0:*\n"), nil
	}
	if len(args) > 1 && args[1] == "version" {
		return []byte("sing-box version " + model.CoreVersion + "\n"), nil
	}
	if len(args) > 1 && args[1] == "check" {
		if r.failCheck {
			return []byte("invalid config"), fmt.Errorf("invalid config")
		}
		return nil, nil
	}
	if joined == "ip -j -4 address show" {
		return []byte(`[{"ifname":"eth0","addr_info":[{"local":"10.0.0.10"},{"local":"10.0.0.11"}]}]`), nil
	}
	if strings.Contains(joined, "ip -j") {
		return []byte("[]"), nil
	}
	if args[0] == "systemctl" && len(args) >= 3 {
		name := args[len(args)-1]
		switch args[1] {
		case "is-active":
			if !r.active[name] {
				return nil, fmt.Errorf("inactive")
			}
		case "is-enabled":
			if !r.enabled[name] {
				return nil, fmt.Errorf("disabled")
			}
		case "start":
			if r.failSubscriptionStart && name == SubscriptionService {
				return nil, fmt.Errorf("injected subscription failure")
			}
			if r.failStart && name == ProxyService {
				r.failStart = false
				return nil, fmt.Errorf("injected service failure")
			}
			r.active[name] = true
		case "stop":
			if name == r.failStop {
				return nil, fmt.Errorf("injected stop failure")
			}
			r.active[name] = false
		case "enable":
			r.enabled[name] = true
		case "disable":
			r.enabled[name] = false
		}
	}
	return nil, nil
}

func TestRecoveryWorksWithCorruptCurrentStateAndManifest(t *testing.T) {
	l := Paths(t.TempDir())
	old := testutil.State(t, "wireguard")
	candidate := testutil.State(t, "wireguard")
	candidate.Nodes = candidate.Nodes[1:]
	b := Backup{Previous: &old, Attempted: &candidate, Files: []SavedFile{{Path: l.State, Exists: true, Data: model.JSON(old), Mode: 0600}}}
	path, err := writeBackup(l, b)
	if err != nil {
		t.Fatal(err)
	}
	for file, data := range map[string][]byte{l.State: []byte("{"), l.Pending: model.JSON(path), filepath.Join(l.Exports, ".export-manifest.json"): []byte("{")} {
		if err = store.Atomic(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	r := runner()
	if err = Recover(context.Background(), l, r); err != nil {
		t.Fatal(err)
	}
	if _, err = model.Load(l.State); err != nil {
		t.Fatal("did not restore state", err)
	}
	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "route del default via 10.0.0.1 dev eth0 src 10.0.0.11 table 42002") {
		t.Fatal("lost attempted route inventory", joined)
	}
	if strings.Contains(joined, "route replace") {
		t.Fatal("restored routes even though prior routes service was inactive")
	}
}

func TestRestoreReportsStopFailureBeforeReplacingFiles(t *testing.T) {
	l := Paths(t.TempDir())
	if err := store.Atomic(l.Config, []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}
	r := runner()
	r.active[ProxyService] = true
	r.failStop = ProxyService
	b := Backup{Files: []SavedFile{{Path: l.Config, Exists: true, Data: []byte("old"), Mode: 0600}}}
	if err := restore(context.Background(), l, r, b, nil); err == nil {
		t.Fatal("ignored failure to stop core")
	}
	data, err := os.ReadFile(l.Config)
	if err != nil || string(data) != "current" {
		t.Fatal("replaced config while core was still running", err)
	}
}

func TestApplyFileRecoversBeforeLoadingBrokenState(t *testing.T) {
	l := Paths(t.TempDir())
	previous := testutil.State(t, "shadowsocks")
	b := Backup{Previous: &previous, Files: []SavedFile{{Path: l.State, Exists: true, Data: model.JSON(previous), Mode: 0600}}}
	path, err := writeBackup(l, b)
	if err != nil {
		t.Fatal(err)
	}
	for file, data := range map[string][]byte{l.State: []byte("{"), l.Pending: model.JSON(path)} {
		if err = store.Atomic(file, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	r := runner()
	r.failCheck = true // Stop after recovery, before attempting a new deployment.
	if err = ApplyFile(context.Background(), l, r, l.State); err == nil || !strings.Contains(err.Error(), "核心配置检查失败") {
		t.Fatal("failed to reach configuration validation after recovery", err)
	}
	if _, err = model.Load(l.State); err != nil {
		t.Fatal("state was not recovered", err)
	}
	if _, err = os.Stat(l.Pending); !os.IsNotExist(err) {
		t.Fatal("recovery did not finish", err)
	}
}

func runner() *serviceRunner {
	return &serviceRunner{active: map[string]bool{}, enabled: map[string]bool{}}
}

func TestInvalidCoreConfigDoesNotStopExistingService(t *testing.T) {
	l := Paths(t.TempDir())
	s := testutil.State(t, "shadowsocks")
	if err := store.Atomic(l.State, model.JSON(s), 0600); err != nil {
		t.Fatal(err)
	}
	r := runner()
	r.failCheck = true
	r.active[ProxyService] = true
	if err := Apply(context.Background(), l, r, s); err == nil {
		t.Fatal("expected validation error")
	}
	for _, call := range r.calls {
		if strings.Contains(call, "systemctl stop") {
			t.Fatal("stopped service before configuration validated")
		}
	}
	if !r.active[ProxyService] {
		t.Fatal("old service stopped")
	}
}

func TestAutomaticCertificatePortConflictRestoresExistingDeployment(t *testing.T) {
	l := Paths(t.TempDir())
	old := testutil.State(t, "reality")
	if err := store.Atomic(l.State, model.JSON(old), 0600); err != nil {
		t.Fatal(err)
	}
	candidate := testutil.State(t, "hysteria2")
	candidate.CertificateMode, candidate.ServerName = "acme-ip", candidate.Nodes[0].PublicIP
	candidate.Certificate, candidate.Key = "", ""
	r := runner()
	r.port80Busy, r.active[ProxyService], r.enabled[ProxyService] = true, true, true
	if err := Apply(context.Background(), l, r, candidate); err == nil || !strings.Contains(err.Error(), "TCP 80") {
		t.Fatal("did not reject occupied validation port", err)
	}
	restored, err := model.Load(l.State)
	if err != nil || restored.Protocol != old.Protocol || !r.active[ProxyService] || !r.enabled[ProxyService] {
		t.Fatal("previous deployment was not restored after ACME port conflict", err)
	}
}

func TestSubscriptionFailureRestoresPreviousStateAndServices(t *testing.T) {
	l := Paths(t.TempDir())
	old := testutil.State(t, "reality")
	if err := store.Atomic(l.State, model.JSON(old), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Atomic(l.SubscriptionTLS, []byte("previous private PEM"), 0600); err != nil {
		t.Fatal(err)
	}
	candidate := testutil.State(t, "trojan")
	candidate.SubscriptionToken, _ = model.NewSubscriptionToken()
	candidate.SubscriptionPort = 8443
	r := runner()
	r.failSubscriptionStart, r.active[ProxyService], r.enabled[ProxyService] = true, true, true
	if err := Apply(context.Background(), l, r, candidate); err == nil {
		t.Fatal("ignored subscription failure")
	}
	restored, err := model.Load(l.State)
	if err != nil || restored.Protocol != old.Protocol || !r.active[ProxyService] || r.active[SubscriptionService] {
		t.Fatal("subscription failure did not restore prior services", err)
	}
	data, err := os.ReadFile(l.SubscriptionTLS)
	if err != nil || string(data) != "previous private PEM" {
		t.Fatal("private subscription certificate was not restored", err)
	}
}

func TestFailedDeploymentRestoresFilesAndService(t *testing.T) {
	l := Paths(t.TempDir())
	old := testutil.State(t, "shadowsocks")
	candidate := testutil.State(t, "reality")
	if err := store.Atomic(l.State, model.JSON(old), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Atomic(l.Config, []byte("previous config"), 0600); err != nil {
		t.Fatal(err)
	}
	r := runner()
	r.active[ProxyService] = true
	r.enabled[ProxyService] = true
	r.failStart = true
	if err := Apply(context.Background(), l, r, candidate); err == nil {
		t.Fatal("expected injected startup failure")
	}
	data, err := os.ReadFile(l.Config)
	if err != nil || string(data) != "previous config" {
		t.Fatal("configuration was not restored", err)
	}
	restored, err := model.Load(l.State)
	if err != nil || restored.Protocol != "shadowsocks" || restored.Nodes[0].Password != old.Nodes[0].Password {
		t.Fatal("state/credentials not restored", err)
	}
	if !r.active[ProxyService] || !r.enabled[ProxyService] {
		t.Fatal("previous service state not restored")
	}
	if _, err = os.Stat(l.Pending); !os.IsNotExist(err) {
		t.Fatal("pending transaction left after successful recovery")
	}
}

func TestBackupCannotRestoreArbitraryFiles(t *testing.T) {
	l := Paths(t.TempDir())
	bad := Backup{Files: []SavedFile{{Path: filepath.Join(l.Root, "etc/other-service.conf"), Exists: true, Data: []byte("bad")}}}
	path, err := writeBackup(l, bad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = readBackup(l, path); err == nil {
		t.Fatal("accepted foreign restore path")
	}
}
