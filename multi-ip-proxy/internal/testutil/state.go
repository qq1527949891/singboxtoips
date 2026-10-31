package testutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mipproxy.local/mipproxy/internal/model"
)

func State(t *testing.T, protocol string) model.State {
	t.Helper()
	s := model.State{Schema: 1, Protocol: protocol, Port: 10443, ServerName: "example.com", RealityTarget: "example.com", RealityPort: 443, CoreVersion: model.CoreVersion}
	for i, pair := range [][2]string{{"10.0.0.10", "203.0.113.10"}, {"10.0.0.11", "203.0.113.11"}} {
		n, e := model.NewNode(i+1, pair[0], pair[1])
		if e != nil {
			t.Fatal(e)
		}
		s.Nodes = append(s.Nodes, n)
	}
	if protocol == "wireguard" {
		for i := range s.Nodes {
			s.Nodes[i].PolicyRouting = true
			s.Nodes[i].Interface = "eth0"
			s.Nodes[i].Gateway = "10.0.0.1"
			s.Nodes[i].Prefix = "10.0.0.0/24"
		}
	}
	if s.NeedsTLS() {
		s.Certificate, s.Key = Cert(t, "example.com")
	}
	return s
}

func Cert(t *testing.T, host string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, &cert, &cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	c, k := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err = os.WriteFile(c, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(k, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}), 0600); err != nil {
		t.Fatal(err)
	}
	return c, k
}
