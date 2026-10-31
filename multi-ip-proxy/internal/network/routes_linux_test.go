//go:build linux

package network

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"mipproxy.local/mipproxy/internal/testutil"
)

// Run only inside: unshare -n <test-binary>. No host routes are changed.
func TestLinuxRoutesIntegration(t *testing.T) {
	if os.Getenv("MIP_ROUTE_INTEGRATION") != "1" {
		t.Skip("requires an isolated Linux network namespace")
	}
	self, selfErr := os.Readlink("/proc/self/ns/net")
	init, initErr := os.Readlink("/proc/1/ns/net")
	if selfErr != nil || initErr != nil || self == init {
		t.Fatal("refusing to configure the host network namespace")
	}
	r := ExecRunner{}
	ctx := context.Background()
	for _, cmd := range [][]string{{"ip", "link", "set", "lo", "up"}, {"ip", "addr", "add", "10.0.0.10/24", "dev", "lo"}, {"ip", "addr", "add", "10.0.0.11/24", "dev", "lo"}, {"ip", "route", "add", "default", "dev", "lo", "table", "10002"}, {"ip", "rule", "add", "priority", "30000", "lookup", "10002"}} {
		if _, err := r.Run(ctx, cmd...); err != nil {
			t.Fatal(err)
		}
	}
	s := testutil.State(t, "shadowsocks")
	for i := range s.Nodes {
		n := &s.Nodes[i]
		n.PolicyRouting = true
		n.Interface = "lo"
		n.Prefix = "10.0.0.0/24"
		n.Gateway = "10.0.0.1"
	}
	if err := CheckConflicts(ctx, r, s, nil); err != nil {
		t.Fatal(err)
	}
	for repeat := 0; repeat < 2; repeat++ {
		if err := Up(ctx, r, s); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range s.Nodes {
		data, err := r.Run(ctx, "ip", "-4", "route", "get", "203.0.113.99", "from", n.LocalIP, "mark", strconv.Itoa(n.Mark()))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "table "+strconv.Itoa(n.Table())) {
			t.Fatal("wrong routing table:", string(data))
		}
	}
	if err := Down(ctx, r, s); err != nil {
		t.Fatal(err)
	}
	if err := Down(ctx, r, s); err != nil {
		t.Fatal("cleanup not idempotent", err)
	}
	b, err := r.Run(ctx, "ip", "-4", "route", "show", "table", "10002")
	if err != nil || !strings.Contains(string(b), "default dev lo") {
		t.Fatal("unrelated routing table damaged", err)
	}
}
