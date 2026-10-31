//go:build linux

package system

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"mipproxy.local/mipproxy/internal/export"
	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/network"
	"mipproxy.local/mipproxy/internal/store"
	"mipproxy.local/mipproxy/internal/testutil"
)

func TestLinuxHTTPSSubscriptionLifecycle(t *testing.T) {
	if os.Getenv("MIP_SUBSCRIPTION_INTEGRATION") != "1" {
		t.Skip("requires an isolated network namespace")
	}
	self, selfErr := os.Readlink("/proc/self/ns/net")
	init, initErr := os.Readlink("/proc/1/ns/net")
	if selfErr != nil || initErr != nil || self == init {
		t.Fatal("refusing to change the host network")
	}
	selfMount, selfMountErr := os.Readlink("/proc/self/ns/mnt")
	initMount, initMountErr := os.Readlink("/proc/1/ns/mnt")
	if selfMountErr != nil || initMountErr != nil || selfMount == initMount {
		t.Fatal("requires a separate mount namespace for the read-only service test")
	}
	runner := network.ExecRunner{}
	for _, args := range [][]string{{"ip", "link", "set", "lo", "up"}, {"ip", "address", "add", "10.0.0.10/32", "dev", "lo"}} {
		if output, err := runner.Run(context.Background(), args...); err != nil {
			t.Fatal(err, string(output))
		}
	}
	l := Paths(t.TempDir())
	s := testutil.State(t, "trojan")
	s.SubscriptionToken, _ = model.NewSubscriptionToken()
	s.SubscriptionPort = 43100
	cert, _ := os.ReadFile(s.Certificate)
	key, _ := os.ReadFile(s.Key)
	if err := store.Atomic(l.State, model.JSON(s), 0600); err != nil {
		t.Fatal(err)
	}
	unlock, err := lock(l.Lock)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if err := writeSubscriptionTLS(l, append(append(cert, '\n'), key...)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ServeSubscriptions(ctx, l) }()
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(cert)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp4", s.SubscriptionListenAddress())
	}}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 3 * time.Second}
	address := export.Subscriptions(s)[0].URL
	var first *http.Response
	var lastError error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		first, lastError = client.Get(address)
		if lastError == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if lastError != nil {
		t.Fatal(lastError)
	}
	data, _ := io.ReadAll(first.Body)
	first.Body.Close()
	if first.StatusCode != http.StatusOK || len(data) == 0 {
		t.Fatal("HTTPS subscription did not serve its generated URL")
	}
	if err := CheckSubscriptions(ctx, l); err != nil {
		t.Fatal("HTTPS readiness check failed", err)
	}
	firstCertificate := first.TLS.PeerCertificates[0].Raw
	newCertificate, newKey := testutil.Cert(t, s.ServerName)
	cert, _ = os.ReadFile(newCertificate)
	key, _ = os.ReadFile(newKey)
	roots.AppendCertsFromPEM(cert)
	transport.CloseIdleConnections()
	if err := writeSubscriptionTLS(l, append(append(cert, '\n'), key...)); err != nil {
		t.Fatal(err)
	}
	loadedRenewal := false
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		transport.CloseIdleConnections()
		renewed, err := client.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, renewed.Body)
		renewed.Body.Close()
		if renewed.StatusCode == http.StatusOK && string(firstCertificate) != string(renewed.TLS.PeerCertificates[0].Raw) {
			loadedRenewal = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !loadedRenewal {
		t.Fatal("HTTPS did not load renewed certificate without restarting")
	}
	wrong, err := client.Get(strings.Replace(address, s.SubscriptionToken, "invalid-token", 1))
	if err != nil {
		t.Fatal(err)
	}
	wrong.Body.Close()
	if wrong.StatusCode != http.StatusNotFound {
		t.Fatal("unauthenticated subscription accepted")
	}
	unlock, err = lock(l.Lock)
	if err != nil {
		t.Fatal(err)
	}
	busy, requestErr := client.Get(address)
	unlock()
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	busy.Body.Close()
	if busy.StatusCode != http.StatusServiceUnavailable {
		t.Fatal("subscription ignored an active deployment transaction")
	}
	// Reproduce ProtectSystem=strict: a read-only data directory must still
	// allow authenticated requests and transaction-lock coordination.
	if err = syscall.Mount(l.Data, l.Data, "", syscall.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	defer syscall.Unmount(l.Data, syscall.MNT_DETACH)
	if err = syscall.Mount("", l.Data, "", syscall.MS_BIND|syscall.MS_REMOUNT|syscall.MS_RDONLY, ""); err != nil {
		t.Fatal(err)
	}
	readOnly, err := client.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	readOnly.Body.Close()
	if readOnly.StatusCode != http.StatusOK {
		t.Fatal("subscription failed with a read-only service filesystem", readOnly.StatusCode)
	}
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("subscription server did not stop")
	}
}
