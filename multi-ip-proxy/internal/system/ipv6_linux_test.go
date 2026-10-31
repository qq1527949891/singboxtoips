//go:build linux

package system

import (
	"context"
	"os"
	"strings"
	"testing"

	"mipproxy.local/mipproxy/internal/network"
)

func TestLinuxIPv6DisableAndRestore(t *testing.T) {
	if os.Getenv("MIP_IPV6_INTEGRATION") != "1" {
		t.Skip("requires a separate network namespace")
	}
	self, selfErr := os.Readlink("/proc/self/ns/net")
	init, initErr := os.Readlink("/proc/1/ns/net")
	if selfErr != nil || initErr != nil || self == init {
		t.Fatal("refusing to change host IPv6")
	}
	ctx, r, l := context.Background(), network.ExecRunner{}, Paths("/")
	for _, args := range [][]string{{"ip", "link", "set", "lo", "up"}, {"ip", "-6", "address", "add", "2001:db8::99/128", "dev", "lo"}, {"ip", "-6", "route", "add", "2001:db8:1::/64", "dev", "lo", "table", "10002"}} {
		if b, err := r.Run(ctx, args...); err != nil {
			t.Fatal(err, string(b))
		}
	}
	b := Backup{}
	var err error
	b.IPv6, err = ipv6Values(l)
	if err != nil {
		t.Fatal(err)
	}
	b.IPv6Addresses, err = r.Run(ctx, "ip", "-6", "address", "save")
	if err != nil {
		t.Fatal(err)
	}
	b.IPv6Routes, err = r.Run(ctx, "ip", "-6", "route", "save", "table", "all")
	if err != nil {
		t.Fatal(err)
	}
	if err = DisableIPv6(ctx, l, r); err != nil {
		t.Fatal(err)
	}
	values, err := ipv6Values(l)
	if err != nil || ipv6Enabled(values) {
		t.Fatal("IPv6 remained enabled", values, err)
	}
	if err = restoreIPv6(ctx, l, r, b); err != nil {
		t.Fatal(err)
	}
	values, err = ipv6Values(l)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range b.IPv6 {
		if values[name] != value {
			t.Fatalf("%s: want %s, got %s", name, value, values[name])
		}
	}
	addresses, err := r.Run(ctx, "ip", "-6", "address", "show", "dev", "lo")
	if err != nil || !strings.Contains(string(addresses), "2001:db8::99/128") {
		t.Fatal("lost original IPv6 address", err, string(addresses))
	}
	routes, err := r.Run(ctx, "ip", "-6", "route", "show", "table", "10002")
	if err != nil || !strings.Contains(string(routes), "2001:db8:1::/64") {
		t.Fatal("lost original IPv6 route", err, string(routes))
	}
}
