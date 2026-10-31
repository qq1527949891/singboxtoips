package export_test

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"mipproxy.local/mipproxy/internal/export"
	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/testutil"
)

func TestExportsMatchProtocolAndNeverExposeServerPrivateKeys(t *testing.T) {
	for _, p := range model.Protocols {
		t.Run(p.ID, func(t *testing.T) {
			s := testutil.State(t, p.ID)
			files, err := export.Files(s)
			if err != nil {
				t.Fatal(err)
			}
			plain, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(files["subscription.txt"])))
			if err != nil || string(plain) != string(files["links.txt"]) {
				t.Fatal("subscription mismatch")
			}
			links := strings.Fields(string(plain))
			if len(links) != 2 && p.ID != "wireguard" {
				t.Fatal("expected one link per IP")
			}
			for i, link := range links {
				if p.ID == "vmess" {
					b, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(link, "vmess://"))
					if e != nil {
						t.Fatal(e)
					}
					var v map[string]any
					if e = json.Unmarshal(b, &v); e != nil || v["add"] != s.Nodes[i].PublicIP {
						t.Fatal("wrong VMess endpoint")
					}
					continue
				}
				u, e := url.Parse(link)
				if e != nil || u.Host != s.Endpoint(s.Nodes[i]) {
					t.Fatal("wrong public endpoint")
				}
				if u.Query().Get("insecure") == "1" {
					t.Fatal("TLS validation disabled")
				}
			}
			if p.ID == "wireguard" {
				for _, n := range s.Nodes {
					b, ok := files[n.Name(s.Protocol)+".conf"]
					if !ok || !strings.Contains(string(b), n.ClientPrivateKey) || strings.Contains(string(b), model.WGKey(n.PrivateKey)) {
						t.Fatal("bad WireGuard native config")
					}
				}
			}
			var client map[string]any
			if err = json.Unmarshal(files["sing-box.json"], &client); err != nil {
				t.Fatal(err)
			}
			var mihomo map[string]any
			if err = yaml.Unmarshal(files["mihomo.yaml"], &mihomo); err != nil {
				t.Fatal(err)
			}
			if len(mihomo["proxies"].([]any)) != 2 {
				t.Fatal("wrong Mihomo node count")
			}
			if mihomo["ipv6"] != false || client["dns"].(map[string]any)["strategy"] != "ipv4_only" {
				t.Fatal("client export did not disable IPv6")
			}
			for _, data := range files {
				for _, n := range s.Nodes {
					if strings.Contains(string(data), n.PrivateKey) {
						t.Fatal("server private key leaked into export")
					}
					if strings.Contains(string(data), n.ProbePrivateKey) {
						t.Fatal("diagnostic private key leaked into export")
					}
				}
			}
		})
	}
}

func TestWireGuardProbeHasIndependentPeer(t *testing.T) {
	s := testutil.State(t, "wireguard")
	c := export.ProbeClient(s, 28000)
	for i, item := range c["endpoints"].([]any) {
		ep := item.(map[string]any)
		n := s.Nodes[i]
		if ep["private_key"] != n.ProbePrivateKey || ep["private_key"] == n.ClientPrivateKey || ep["address"].([]string)[0] != n.WGProbeIP()+"/32" {
			t.Fatal("diagnostic client could take over user peer")
		}
	}
}

func TestCredentialEscaping(t *testing.T) {
	for _, protocol := range []string{"hysteria2", "trojan", "tuic", "shadowsocks"} {
		s := testutil.State(t, protocol)
		s.Nodes[0].Password = "a:/@?#%&+中文-password"
		u, err := url.Parse(export.URI(s, s.Nodes[0]))
		if err != nil {
			t.Fatal(err)
		}
		got := u.User.Username()
		if protocol == "tuic" {
			got, _ = u.User.Password()
		}
		if protocol == "shadowsocks" {
			b, e := base64.RawURLEncoding.DecodeString(got)
			if e != nil {
				t.Fatal(e)
			}
			got = strings.TrimPrefix(string(b), "aes-256-gcm:")
		}
		if got != s.Nodes[0].Password {
			t.Fatalf("credential escaping failed: %s", protocol)
		}
	}
}
