package system

import (
	"crypto/x509"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/store"
	"mipproxy.local/mipproxy/internal/testutil"
)

func TestHTTPSSubscriptionsRequireTokenAndNeverServeServerFiles(t *testing.T) {
	l := Paths(t.TempDir())
	s := testutil.State(t, "trojan")
	s.SubscriptionToken, _ = model.NewSubscriptionToken()
	s.SubscriptionPort = 8443
	if err := store.Atomic(l.State, model.JSON(s), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Atomic(l.Lock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(SubscriptionHandler(l))
	defer server.Close()
	client := server.Client()
	get := func(path string, want int) string {
		t.Helper()
		response, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		if response.StatusCode != want {
			t.Fatalf("status %d, want %d for subscription request", response.StatusCode, want)
		}
		return string(data)
	}
	valid := "/" + s.SubscriptionToken + "/subscription.txt"
	encoded := get(valid, http.StatusOK)
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(strings.Fields(string(decoded))) != 2 || !strings.Contains(string(decoded), "trojan://") {
		t.Fatal("invalid universal subscription")
	}
	for _, path := range []string{"/subscription.txt", "/wrong-token/subscription.txt", "/" + s.SubscriptionToken + "/state.json", "/" + s.SubscriptionToken + "/subscription.pem", "/" + s.SubscriptionToken + "/../state.json", "/" + s.SubscriptionToken + "/%2e%2e%2fstate.json"} {
		get(path, http.StatusNotFound)
	}
	for _, name := range []string{"mihomo.yaml", "mihomo-provider.yaml", "sing-box.json"} {
		data := get("/"+s.SubscriptionToken+"/"+name, http.StatusOK)
		for _, node := range s.Nodes {
			if strings.Contains(data, node.PrivateKey) || strings.Contains(data, node.ProbePrivateKey) {
				t.Fatal("server or diagnostic key exposed")
			}
		}
	}
	old := valid
	s.SubscriptionToken, _ = model.NewSubscriptionToken()
	if err = store.Atomic(l.State, model.JSON(s), 0600); err != nil {
		t.Fatal(err)
	}
	get(old, http.StatusNotFound)
	valid = "/" + s.SubscriptionToken + "/subscription.txt"
	get(valid, http.StatusOK)
	if err = store.Atomic(l.Pending, []byte(`"unfinished"`), 0600); err != nil {
		t.Fatal(err)
	}
	get(valid, http.StatusServiceUnavailable)
}

func TestWireGuardSubscriptionProvidesNativeClientOnly(t *testing.T) {
	l := Paths(t.TempDir())
	s := testutil.State(t, "wireguard")
	s.SubscriptionToken, _ = model.NewSubscriptionToken()
	s.SubscriptionPort, s.CertificateMode, s.ServerName = 443, "acme-ip", s.Nodes[0].PublicIP
	if err := store.Atomic(l.State, model.JSON(s), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Atomic(l.Lock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/"+s.SubscriptionToken+"/"+s.Nodes[0].Name(s.Protocol)+".conf", nil)
	response := httptest.NewRecorder()
	SubscriptionHandler(l).ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), s.Nodes[0].ClientPrivateKey) || strings.Contains(response.Body.String(), model.WGKey(s.Nodes[0].PrivateKey)) {
		t.Fatal("incorrect WireGuard configuration access")
	}
}

func TestSubscriptionTLSLoadsRenewedCertificates(t *testing.T) {
	l := Paths(t.TempDir())
	s := testutil.State(t, "hysteria2")
	s.CertificateMode, s.ServerName = "acme-ip", s.Nodes[0].PublicIP
	certificate, key := testutil.Cert(t, s.ServerName)
	certData, _ := os.ReadFile(certificate)
	keyData, _ := os.ReadFile(key)
	for name, data := range map[string][]byte{"certificate.crt": certData, "certificate.key": keyData} {
		if err := store.Atomic(filepath.Join(l.ACME, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certData)
	first, err := loadSubscriptionCertificate(l, s, roots)
	if err != nil {
		t.Fatal(err)
	}
	certificate, key = testutil.Cert(t, s.ServerName)
	certData, _ = os.ReadFile(certificate)
	keyData, _ = os.ReadFile(key)
	roots.AppendCertsFromPEM(certData)
	for name, data := range map[string][]byte{"certificate.crt": certData, "certificate.key": keyData} {
		if err := store.Atomic(filepath.Join(l.ACME, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	renewed, err := loadSubscriptionCertificate(l, s, roots)
	if err != nil || string(first.Certificate[0]) == string(renewed.Certificate[0]) {
		t.Fatal("renewed certificate was not loaded", err)
	}
}
