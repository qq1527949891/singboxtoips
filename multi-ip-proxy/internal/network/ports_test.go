package network

import (
	"context"
	"errors"
	"testing"
)

type listenerInventory struct {
	text string
	err  error
}

func (l listenerInventory) Run(context.Context, ...string) ([]byte, error) {
	return []byte(l.text), l.err
}

func TestSubscriptionPortSelectionMatchesOnlyConflictingListeners(t *testing.T) {
	for _, test := range []struct {
		name, listeners string
		want            int
	}{
		{"another-ip", "LISTEN 0 4096 10.0.0.11:443 0.0.0.0:*", 443},
		{"same-ip", "LISTEN 0 4096 10.0.0.10:443 0.0.0.0:*", 8443},
		{"ipv4-mapped", "LISTEN 0 4096 [::ffff:10.0.0.10]:443 [::]:*", 8443},
		{"ipv4-wildcard", "LISTEN 0 4096 0.0.0.0:443 0.0.0.0:*", 8443},
		{"dual-stack-wildcard", "LISTEN 0 4096 [::]:443 [::]:*", 8443},
		{"ss-wildcard", "LISTEN 0 4096 *:443 *:*", 8443},
		{"fallback", "LISTEN 0 4096 *:443 *:*\nLISTEN 0 4096 *:8443 *:*", 9443},
		{"all-busy", "LISTEN 0 4096 *:443 *:*\nLISTEN 0 4096 *:8443 *:*\nLISTEN 0 4096 *:9443 *:*\nLISTEN 0 4096 *:18443 *:*", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			port, err := RecommendedTCPPort(context.Background(), listenerInventory{text: test.listeners}, "10.0.0.10", 443)
			if port != test.want || (err != nil) != (test.want == 0) {
				t.Fatalf("port=%d, err=%v; want=%d", port, err, test.want)
			}
		})
	}
	if _, err := RecommendedTCPPort(context.Background(), listenerInventory{err: errors.New("ss failed")}, "10.0.0.10", 443); err == nil {
		t.Fatal("listener inventory failure was ignored")
	}
	port, err := RecommendedTCPPort(context.Background(), nil, "10.0.0.10", 8443, 8443)
	if err != nil || port != 9443 {
		t.Fatal("offline preview did not reserve the future proxy listener", port, err)
	}
	port, err = RecommendedTCPPort(context.Background(), listenerInventory{text: "LISTEN 0 4096 *:9443 *:*"}, "10.0.0.10", 9443, 8443)
	if err != nil || port != 18443 {
		t.Fatal("fallback reused the future proxy listener", port, err)
	}
}
