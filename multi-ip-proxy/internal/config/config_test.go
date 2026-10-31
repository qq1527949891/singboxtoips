package config_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"mipproxy.local/mipproxy/internal/config"
	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/testutil"
)

func TestAllIPsUseOneProtocolAndTheirOwnSource(t *testing.T) {
	for _, p := range model.Protocols {
		t.Run(p.ID, func(t *testing.T) {
			s := testutil.State(t, p.ID)
			c, err := config.Server(s)
			if err != nil {
				t.Fatal(err)
			}
			ins, outs := c["inbounds"].([]any), c["outbounds"].([]any)
			if p.ID == "wireguard" {
				ins = c["endpoints"].([]any)
			}
			if len(ins) != 2 || len(outs) != 2 {
				t.Fatal("unexpected node count")
			}
			expected := p.ID
			if p.ID == "reality" {
				expected = "vless"
			}
			if p.ID == "socks5" {
				expected = "socks"
			}
			for i, n := range s.Nodes {
				in, out := ins[i].(config.Object), outs[i].(config.Object)
				if in["type"] != expected || (p.ID != "wireguard" && in["listen"] != n.LocalIP) || out["inet4_bind_address"] != n.LocalIP {
					t.Fatal("protocol/source mismatch")
				}
			}
			rules := c["route"].(config.Object)["rules"].([]any)
			if first := rules[0].(config.Object); first["ip_version"] != 6 || first["action"] != "reject" {
				t.Fatal("explicit IPv6 could use the host's primary IPv6 source")
			}
			if rules[len(rules)-1].(config.Object)["action"] != "reject" {
				t.Fatal("unknown inbound could leak into another source")
			}
		})
	}
}

func TestRejectCertificateMismatch(t *testing.T) {
	s := testutil.State(t, "hysteria2")
	s.ServerName = "other.example.com"
	if _, err := config.Server(s); err == nil {
		t.Fatal("accepted mismatched certificate")
	}
}

func TestAutomaticCertificateUsesSharedProvider(t *testing.T) {
	for _, protocol := range []string{"trojan", "vmess", "hysteria2", "tuic"} {
		t.Run(protocol, func(t *testing.T) {
			s := testutil.State(t, protocol)
			s.CertificateMode, s.ServerName = "acme-ip", s.Nodes[0].PublicIP
			s.Certificate, s.Key = "", ""
			c, err := config.ServerWithACMEDirectory(s, "/private/acme")
			if err != nil {
				t.Fatal(err)
			}
			provider := c["certificate_providers"].([]any)[0].(config.Object)
			if provider["key_type"] != "p256" || provider["profile"] != "shortlived" || provider["default_server_name"] != s.ServerName || provider["disable_tls_alpn_challenge"] != true {
				t.Fatal("wrong IP certificate / challenge settings", provider)
			}
			for _, inbound := range c["inbounds"].([]any) {
				tls := inbound.(config.Object)["tls"].(config.Object)
				if tls["certificate_provider"] != provider["tag"] || tls["certificate"] != nil || tls["key"] != nil {
					t.Fatal("automatic certificates were embedded or not shared")
				}
			}
		})
	}
}

func TestWireGuardSubscriptionStartsACMEProvider(t *testing.T) {
	s := testutil.State(t, "wireguard")
	s.SubscriptionToken, _ = model.NewSubscriptionToken()
	s.SubscriptionPort, s.CertificateMode, s.ServerName = 443, "acme-ip", s.Nodes[0].PublicIP
	c, err := config.Server(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(c["certificate_providers"].([]any)) != 1 || len(c["endpoints"].([]any)) != 2 {
		t.Fatal("certificate provider was omitted for WireGuard HTTPS subscriptions")
	}
}

func TestHysteria2RejectsIncompatibleEd25519Certificate(t *testing.T) {
	s := testutil.State(t, "hysteria2")
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{s.ServerName}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, &leaf, &leaf, public, private)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	for path, block := range map[string]*pem.Block{s.Certificate: {Type: "CERTIFICATE", Bytes: der}, s.Key: {Type: "PRIVATE KEY", Bytes: key}} {
		if err = os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = config.Server(s); err == nil || !strings.Contains(err.Error(), "Ed25519") {
		t.Fatal("accepted certificate incompatible with the Hysteria2 handshake", err)
	}
	s.Protocol = "trojan"
	if _, err = config.Server(s); err != nil {
		t.Fatal("incorrectly rejected Ed25519 for other TLS protocols", err)
	}
}

func TestPerNodeDNSAndRoutingMark(t *testing.T) {
	s := testutil.State(t, "shadowsocks")
	for i := range s.Nodes {
		s.Nodes[i].PolicyRouting = true
		s.Nodes[i].Interface = "eth0"
		s.Nodes[i].Gateway = "10.0.0.1"
		s.Nodes[i].Prefix = "10.0.0.0/24"
	}
	c, err := config.Server(s)
	if err != nil {
		t.Fatal(err)
	}
	dns := c["dns"].(config.Object)["servers"].([]any)
	for i, n := range s.Nodes {
		d := dns[i].(config.Object)
		if d["inet4_bind_address"] != n.LocalIP || d["routing_mark"] != n.Mark() {
			t.Fatal("DNS did not inherit source routing")
		}
	}
}
