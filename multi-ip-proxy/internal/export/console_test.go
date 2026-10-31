package export_test

import (
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"mipproxy.local/mipproxy/internal/export"
	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/testutil"
)

func TestConsoleNodeFormatsAndMappedPorts(t *testing.T) {
	for _, protocol := range model.Protocols {
		t.Run(protocol.ID, func(t *testing.T) {
			s := testutil.State(t, protocol.ID)
			s.Nodes[0].PublicPort = 1081
			printed, err := export.Console(s)
			if err != nil {
				t.Fatal(err)
			}
			first := s.Nodes[0]
			switch s.Protocol {
			case "socks5":
				if !strings.HasPrefix(printed, fmt.Sprintf("%s/1081/%s/%s\n", first.PublicIP, first.Username, first.Password)) {
					t.Fatal("wrong requested SOCKS5 console format")
				}
			case "shadowsocks":
				if !strings.HasPrefix(printed, fmt.Sprintf("%s/1081/aes-256-gcm/%s\n", first.PublicIP, first.Password)) {
					t.Fatal("wrong requested SS console format")
				}
			case "wireguard":
				var proxies []map[string]any
				if err := yaml.Unmarshal([]byte(printed), &proxies); err != nil {
					t.Fatal("invalid Mihomo YAML", err)
				}
				if len(strings.Split(strings.TrimSpace(printed), "\n")) != len(s.Nodes) || len(proxies) != len(s.Nodes) {
					t.Fatal("expected one WireGuard flow mapping per IP")
				}
				for i, proxy := range proxies {
					node := s.Nodes[i]
					if proxy["type"] != "wireguard" || proxy["server"] != node.PublicIP || proxy["private-key"] != node.ClientPrivateKey || proxy["public-key"] != model.WGKey(node.PublicKey) || proxy["ip"] != node.WGClientIP() || proxy["remote-dns-resolve"] != true {
						t.Fatal("console fields differ from the actual client configuration")
					}
					if _, exists := proxy["pre-shared-key"]; exists {
						t.Fatal("invented a preshared key not configured on the server")
					}
				}
				if proxies[0]["port"] != 1081 || proxies[1]["port"] != s.ListenPort(s.Nodes[1]) {
					t.Fatal("WireGuard mapped or allocated port was lost")
				}
			default:
				prefix := s.Protocol + "://"
				if s.Protocol == "reality" {
					prefix = "vless://"
				}
				lines := strings.Fields(printed)
				if len(lines) != len(s.Nodes) || !strings.HasPrefix(lines[0], prefix) {
					t.Fatal("expected one standard sharing URI per node")
				}
			}
			for _, node := range s.Nodes {
				if strings.Contains(printed, node.PrivateKey) || strings.Contains(printed, node.ProbePrivateKey) {
					t.Fatal("server/diagnostic private key leaked to console")
				}
			}
		})
	}
}
