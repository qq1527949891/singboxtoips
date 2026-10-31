package network

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"mipproxy.local/mipproxy/internal/testutil"
)

type fakeRunner struct {
	calls       [][]string
	rules       string
	routes      string
	failReplace bool
}

func (f *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{}, args...))
	joined := strings.Join(args, " ")
	if joined == "ip -j -4 rule show" {
		if f.rules == "" {
			return []byte("[]"), nil
		}
		return []byte(f.rules), nil
	}
	if strings.Contains(joined, "route show table") {
		if f.routes != "" {
			return []byte(f.routes), nil
		}
		return []byte("[]"), nil
	}
	if f.failReplace && strings.Contains(joined, "route replace default") {
		return []byte("Operation not permitted"), fmt.Errorf("permission denied")
	}
	return nil, nil
}

func TestCleanupPreservesRulesWithExtraSelectors(t *testing.T) {
	s := testutil.State(t, "wireguard")
	s.Nodes = s.Nodes[:1]
	n := s.Nodes[0]
	for _, extra := range []string{`"dst":"203.0.113.0/24"`, `"tos":16`, `"iif":"eth1"`, `"not":true`} {
		r := &fakeRunner{rules: fmt.Sprintf(`[{"priority":%d,"table":%d,"src":"%s",%s}]`, n.SourcePriority(), n.Table(), n.LocalIP, extra)}
		if err := CheckConflicts(context.Background(), r, s, &s); err == nil {
			t.Fatal("adopted a foreign rule:", extra)
		}
		if err := Down(context.Background(), r, s); err == nil {
			t.Fatal("cleanup did not report foreign rule:", extra)
		}
		for _, args := range r.calls {
			if strings.Contains(strings.Join(args, " "), " del ") {
				t.Fatal("deleted foreign networking:", args)
			}
		}
	}
}

func TestCleanupPreservesModifiedManagedRoute(t *testing.T) {
	s := testutil.State(t, "wireguard")
	s.Nodes = s.Nodes[:1]
	n := s.Nodes[0]
	r := &fakeRunner{routes: fmt.Sprintf(`[{"dst":"default","gateway":"10.0.0.99","dev":"eth0","protocol":"static","prefsrc":"%s"}]`, n.LocalIP)}
	if err := CheckConflicts(context.Background(), r, s, &s); err == nil {
		t.Fatal("would overwrite a modified default route")
	}
	if err := Down(context.Background(), r, s); err == nil {
		t.Fatal("cleanup did not report modified route")
	}
	for _, args := range r.calls {
		if strings.Contains(strings.Join(args, " "), "route del default") {
			t.Fatal("deleted a modified route")
		}
	}
}

func TestDiscoveryDoesNotImportManagedPolicyGateways(t *testing.T) {
	a := []byte(`[{"ifname":"eth0","flags":["UP"],"addr_info":[{"family":"inet","local":"10.0.0.10","prefixlen":24,"scope":"global"}]}]`)
	b := []byte(`[{"dst":"default","gateway":"10.0.0.1","dev":"eth0"},{"dst":"default","gateway":"10.0.0.99","dev":"eth0","table":42001}]`)
	candidates, err := ParseInventory(a, b)
	if err != nil || len(candidates) != 1 || candidates[0].Gateway != "10.0.0.1" || candidates[0].AmbiguousGateway {
		t.Fatal("managed table polluted gateway discovery", candidates, err)
	}
}

func TestDiscoveryUsesInterfaceNotAddressLabelAndFiltersVirtualIPs(t *testing.T) {
	a := []byte(`[{"ifname":"eth0","flags":["UP"],"addr_info":[{"family":"inet","local":"10.0.0.10","prefixlen":24,"scope":"global","label":"eth0:1"},{"family":"inet","local":"10.0.0.11","prefixlen":24,"scope":"global"}]},{"ifname":"docker0","flags":["UP"],"addr_info":[{"family":"inet","local":"172.17.0.1","prefixlen":16,"scope":"global"}]}]`)
	b := []byte(`[{"dst":"default","gateway":"10.0.0.1","dev":"eth0"}]`)
	cs, err := ParseInventory(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Interface != "eth0" {
		t.Fatal(cs)
	}
}

func TestRoutingFailurePropagatesAndNeverFlushesOtherTables(t *testing.T) {
	s := testutil.State(t, "shadowsocks")
	for i := range s.Nodes {
		n := &s.Nodes[i]
		n.PolicyRouting = true
		n.Interface = "eth0"
		n.Prefix = "10.0.0.0/24"
		n.Gateway = "10.0.0.1"
	}
	r := &fakeRunner{failReplace: true}
	if err := Up(context.Background(), r, s); err == nil {
		t.Fatal("route failure reported as success")
	}
	for _, args := range r.calls {
		if strings.Contains(strings.Join(args, " "), "flush") {
			t.Fatal("broad routing cleanup")
		}
	}
}

func TestForeignPriorityIsPreserved(t *testing.T) {
	s := testutil.State(t, "shadowsocks")
	n := &s.Nodes[0]
	n.PolicyRouting = true
	n.Interface = "eth0"
	n.Prefix = "10.0.0.0/24"
	n.Gateway = "10.0.0.1"
	r := &fakeRunner{rules: fmt.Sprintf(`[{"priority":%d,"table":999,"src":"all"}]`, n.SourcePriority())}
	if err := CheckConflicts(context.Background(), r, s, nil); err == nil {
		t.Fatal("foreign rule accepted")
	}
	if len(r.calls) != 1 {
		t.Fatal("modified networking during conflict check")
	}
}

func TestCSVRetainsDuplicateObservedPublicAddresses(t *testing.T) {
	cs, err := ReadCSV(strings.NewReader("local_ip,public_ip\n10.0.0.1,203.0.113.10\n10.0.0.2,203.0.113.10\n"))
	if err != nil || len(cs) != 2 {
		t.Fatal("mapping rows silently overwritten", err)
	}
}
