//go:build linux

package network

import (
	"context"
	"net"
	"os"
	"testing"
)

func TestLinuxSubscriptionPortReservation(t *testing.T) {
	if os.Getenv("MIP_PORT_INTEGRATION") != "1" {
		t.Skip("requires an isolated network namespace")
	}
	self, selfErr := os.Readlink("/proc/self/ns/net")
	init, initErr := os.Readlink("/proc/1/ns/net")
	if selfErr != nil || initErr != nil || self == init {
		t.Fatal("refusing to use the host network")
	}
	ctx := context.Background()
	runner := ExecRunner{}
	if _, err := runner.Run(ctx, "ip", "link", "set", "lo", "up"); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:8443")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port, err := RecommendedTCPPort(ctx, runner, "127.0.0.1", 8443, 9443)
	if err != nil || port != 18443 {
		t.Fatal("failed to exclude the real listener and future proxy port", port, err)
	}
	second, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", "18443"))
	if err != nil {
		t.Fatal("recommended port was not usable", err)
	}
	second.Close()
}
