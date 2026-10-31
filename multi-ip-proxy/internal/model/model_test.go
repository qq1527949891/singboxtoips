package model_test

import (
	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/testutil"
	"testing"
)

func TestRejectMixedProtocolAndDuplicateEndpoints(t *testing.T) {
	for _, protocol := range []string{"reality,hysteria2", "1 3", "all", ""} {
		s := testutil.State(t, "reality")
		s.Protocol = protocol
		if s.Validate() == nil {
			t.Fatalf("accepted mixed protocol %q", protocol)
		}
	}
	s := testutil.State(t, "reality")
	s.Nodes[1].LocalIP = s.Nodes[0].LocalIP
	if s.Validate() == nil {
		t.Fatal("accepted duplicate local source")
	}
	s = testutil.State(t, "reality")
	s.Nodes[1].PublicIP = s.Nodes[0].PublicIP
	if s.Validate() == nil {
		t.Fatal("accepted duplicate public endpoint")
	}
}

func TestAutomaticIPCertificateConstraints(t *testing.T) {
	s := testutil.State(t, "hysteria2")
	s.CertificateMode, s.ServerName = "acme-ip", s.Nodes[0].PublicIP
	s.Certificate, s.Key = "", ""
	if err := s.Validate(); err != nil {
		t.Fatal("automatic issuance must not need existing PEM files", err)
	}
	s.ServerName = "example.com"
	if s.Validate() == nil {
		t.Fatal("accepted a domain in automatic IP mode")
	}
	s.ServerName = s.Nodes[0].PublicIP
	s.Protocol, s.Port = "trojan", 80
	if s.Validate() == nil {
		t.Fatal("proxy would occupy the ACME validation port")
	}
	s.Protocol = "hysteria2"
	if err := s.Validate(); err != nil {
		t.Fatal("UDP 80 does not conflict with ACME TCP 80", err)
	}
	s.Protocol = "shadowsocks"
	if s.Validate() == nil {
		t.Fatal("enabled ACME for a protocol without TLS")
	}
}

func TestWireGuardCanObtainCertificateForHTTPSSubscription(t *testing.T) {
	s := testutil.State(t, "wireguard")
	s.SubscriptionToken, _ = model.NewSubscriptionToken()
	s.SubscriptionPort, s.CertificateMode, s.ServerName = 443, "acme-ip", s.Nodes[0].PublicIP
	if err := s.Validate(); err != nil || !s.AutomaticCertificate() || s.NeedsTLS() {
		t.Fatal("WireGuard subscription must obtain its own HTTPS certificate", err)
	}
	s.SubscriptionToken = "invalid"
	if s.Validate() == nil {
		t.Fatal("accepted an insecure subscription token")
	}
}

func TestSecretsIndependentAndKeyIntegrity(t *testing.T) {
	s := testutil.State(t, "reality")
	if s.Nodes[0].UUID == s.Nodes[1].UUID || s.Nodes[0].Password == s.Nodes[1].Password || s.Nodes[0].PrivateKey == s.Nodes[1].PrivateKey {
		t.Fatal("shared secrets")
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s.Nodes[0].PublicKey = s.Nodes[1].PublicKey
	if s.Validate() == nil {
		t.Fatal("accepted mismatching key")
	}
}

func TestRejectUnsafeValues(t *testing.T) {
	for _, host := range []string{"example.com\nExecStart=bad", "../a", "foo;cmd", "-bad.example", "a..b", ""} {
		if model.ValidHost(host) {
			t.Fatalf("accepted host %q", host)
		}
	}
	s := testutil.State(t, "reality")
	s.Nodes[0].PolicyRouting = true
	s.Nodes[0].Interface = "eth0\ncommand"
	s.Nodes[0].Gateway = "10.0.0.1"
	s.Nodes[0].Prefix = "10.0.0.0/24"
	if s.Validate() == nil {
		t.Fatal("accepted unsafe interface")
	}
}

func TestWireGuardRequiresReplyRoutingAndIndependentPeers(t *testing.T) {
	s := testutil.State(t, "wireguard")
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s.Nodes[0].PolicyRouting = false
	if s.Validate() == nil {
		t.Fatal("allowed wildcard WireGuard socket without source selection")
	}
	s.Nodes[0].PolicyRouting = true
	s.Nodes[0].ProbePrivateKey = s.Nodes[0].ClientPrivateKey
	s.Nodes[0].ProbePublicKey = s.Nodes[0].ClientPublicKey
	if s.Validate() == nil {
		t.Fatal("allowed diagnostic peer to take over user's endpoint")
	}
}

func TestRejectSharedPublicIPWithDifferentPorts(t *testing.T) {
	s := testutil.State(t, "shadowsocks")
	s.Nodes[1].PublicIP = s.Nodes[0].PublicIP
	s.Nodes[1].PublicPort = 444
	if s.Validate() == nil {
		t.Fatal("accepted duplicate client tags for one public IP")
	}
	s = testutil.State(t, "shadowsocks")
	s.Nodes[0].PublicIP = "10.0.0.1"
	if s.Validate() == nil {
		t.Fatal("accepted private address as public endpoint")
	}
}
