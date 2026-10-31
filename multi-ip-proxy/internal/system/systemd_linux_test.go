//go:build linux

package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mipproxy.local/mipproxy/internal/model"
)

// Use the real service manager and generated dependencies. Stubs avoid changing
// network routes, IPv6, real proxy services, or listening on production ports.
func TestLinuxSystemdRecoversDependenciesAndRepeatedExits(t *testing.T) {
	if os.Getenv("MIP_SYSTEMD_INTEGRATION") != "1" {
		t.Skip("set MIP_SYSTEMD_INTEGRATION=1 on a disposable systemd CI host")
	}
	pid1, err := os.ReadFile("/proc/1/comm")
	if err != nil || strings.TrimSpace(string(pid1)) != "systemd" {
		t.Skip("the local environment has no running systemd; GitHub CI runs this test")
	}
	if os.Geteuid() != 0 {
		t.Fatal("real systemd integration needs root")
	}
	// PrivateTmp would hide a fixture under /tmp from service processes.
	root, err := os.MkdirTemp("/run", "mip-stability-test-")
	if err != nil {
		t.Fatal(err)
	}
	l := Paths(root)
	l.Units = "/run/systemd/system"
	if err = os.MkdirAll(l.Data, 0700); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Base(root)
	names := map[string]string{}
	var replacements []string
	for _, original := range Services {
		name := prefix + strings.TrimPrefix(original, "mip-proxy")
		names[original] = name
		replacements = append(replacements, original, name)
	}
	replacer := strings.NewReplacer(replacements...)
	run := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	}
	var installed []string
	t.Cleanup(func() {
		if t.Failed() {
			for _, name := range names {
				output, _ := run("status", name, "--no-pager", "--full")
				t.Logf("%s\n%s", name, output)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				output, _ = exec.CommandContext(ctx, "journalctl", "-u", name, "-n", "30", "--no-pager").CombinedOutput()
				cancel()
				t.Logf("journal %s\n%s", name, output)
			}
		}
		for _, original := range Services {
			_, _ = run("stop", names[original])
			_, _ = run("reset-failed", names[original])
		}
		for _, path := range installed {
			_ = os.Remove(path)
		}
		_, _ = run("daemon-reload")
		_ = os.RemoveAll(root)
	})
	write := func(path, data string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), mode); err != nil {
			t.Fatal(err)
		}
	}
	// Routes initially fail, then become available. The proxy exits successfully
	// eight times, exceeding systemd's usual startup burst limit, before staying up.
	app := fmt.Sprintf(`#!/bin/sh
set -eu
data='%s'
increment() {
    count=0
    [ ! -f "$data/$1" ] || count=$(cat "$data/$1")
    count=$((count + 1))
    printf '%%s\n' "$count" > "$data/$1"
}
case "$1" in
    ipv6) exit 0 ;;
    routes)
        [ "$2" = up ] || exit 0
        increment route-attempts
        [ -f "$data/ready" ]
        ;;
    cover) increment cover-starts; exec /bin/sleep infinity ;;
    subscriptions) increment subscription-starts; exec /bin/sleep infinity ;;
    *) exit 2 ;;
esac
`, l.Data)
	core := fmt.Sprintf(`#!/bin/sh
set -eu
[ "$1" = run ] || exit 0
counter='%s/core-starts'
count=0
[ ! -f "$counter" ] || count=$(cat "$counter")
count=$((count + 1))
printf '%%s\n' "$count" > "$counter"
[ "$count" -gt 8 ] || exit 0
exec /bin/sleep infinity
`, l.Data)
	write(l.App, app, 0755)
	write(l.Core, core, 0755)
	for original, data := range Units(l, model.State{Protocol: "trojan"}) {
		content := replacer.Replace(string(data))
		if original != RoutesService {
			content += "\n[Service]\nRestartSec=100ms\nReadWritePaths=" + quote(l.Data) + "\n"
		}
		path := filepath.Join(l.Units, names[original])
		write(path, content, 0644)
		installed = append(installed, path)
	}
	if output, err := run("daemon-reload"); err != nil {
		t.Fatal(err, string(output))
	}
	active := func(original string) bool {
		output, err := run("is-active", names[original])
		return err == nil && strings.TrimSpace(string(output)) == "active"
	}
	count := func(filename string) int {
		data, _ := os.ReadFile(filepath.Join(l.Data, filename))
		n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		return n
	}
	wait := func(description string, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if condition() {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("systemd did not " + description)
	}
	if output, err := run("start", "--no-block", names[SubscriptionService]); err != nil {
		t.Fatal(err, string(output))
	}
	wait("attempt the initially failing route dependency", func() bool { return count("route-attempts") >= 2 })
	if active(ProxyService) {
		t.Fatal("proxy started before the required routes were available")
	}
	write(filepath.Join(l.Data, "ready"), "1", 0600)
	wait("recover routes and eight clean proxy exits", func() bool {
		return count("core-starts") >= 9 && active(ProxyService) && active(SubscriptionService) && active(CoverService) && active(RoutesService)
	})
	before := count("core-starts")
	if output, err := run("kill", "--signal=SIGKILL", "--kill-whom=main", names[ProxyService]); err != nil {
		t.Fatal(err, string(output))
	}
	wait("restart a killed proxy and its subscription", func() bool {
		return count("core-starts") > before && active(ProxyService) && active(SubscriptionService)
	})
	for _, auxiliary := range []struct{ service, counter string }{{CoverService, "cover-starts"}, {SubscriptionService, "subscription-starts"}} {
		beforeAuxiliary := count(auxiliary.counter)
		if output, err := run("kill", "--signal=SIGKILL", "--kill-whom=main", names[auxiliary.service]); err != nil {
			t.Fatal(err, string(output))
		}
		wait("recover the killed auxiliary service "+auxiliary.service, func() bool {
			return count(auxiliary.counter) > beforeAuxiliary && active(auxiliary.service) && active(ProxyService) && active(SubscriptionService)
		})
	}
	// Explicit dependency restarts must propagate to the proxy and subscription.
	before = count("core-starts")
	if output, err := run("restart", names[RoutesService]); err != nil {
		t.Fatal(err, string(output))
	}
	wait("propagate a route restart", func() bool {
		return count("core-starts") > before && active(ProxyService) && active(SubscriptionService)
	})
	if output, err := run("stop", names[RoutesService]); err != nil {
		t.Fatal(err, string(output))
	}
	wait("stop dependents after a manual route stop", func() bool { return !active(ProxyService) && !active(SubscriptionService) })
	before = count("core-starts")
	time.Sleep(400 * time.Millisecond)
	if count("core-starts") != before || active(ProxyService) {
		t.Fatal("Restart=always reversed a manual service stop")
	}
	t.Logf("actual systemd recovered %d proxy starts, route failures, SIGKILL and explicit dependency restart/stop", before)
}
