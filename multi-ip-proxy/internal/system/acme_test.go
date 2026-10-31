package system

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mipproxy.local/mipproxy/internal/testutil"
)

func TestIssuedCertificateRequiresMatchingTrustedChain(t *testing.T) {
	directory := t.TempDir()
	path, _ := testutil.Cert(t, "203.0.113.10")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(directory, "certificate.crt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	trusted := x509.NewCertPool()
	trusted.AppendCertsFromPEM(data)
	for _, test := range []struct {
		identity string
		roots    *x509.CertPool
		want     bool
	}{{"203.0.113.10", trusted, true}, {"203.0.113.11", trusted, false}, {"203.0.113.10", x509.NewCertPool(), false}} {
		ready, err := issuedCertificate(directory, test.identity, test.roots)
		if err != nil || ready != test.want {
			t.Fatalf("identity %s, ready %t, want %t: %v", test.identity, ready, test.want, err)
		}
	}
}

func TestAutomaticCertificateCacheWritableInService(t *testing.T) {
	l := Paths(t.TempDir())
	s := testutil.State(t, "hysteria2")
	s.CertificateMode, s.ServerName = "acme-ip", s.Nodes[0].PublicIP
	unit := string(Units(l, s)[ProxyService])
	if !strings.Contains(unit, "ReadWritePaths="+quote(l.ACME)) {
		t.Fatal("automatic renewal cannot write its cache under ProtectSystem=strict")
	}
}
