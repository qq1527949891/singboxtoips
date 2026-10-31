package export_test

import (
	"fmt"
	"strings"
	"testing"

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
				if !strings.Contains(printed, "[Interface]") || !strings.Contains(printed, "Endpoint = "+first.PublicIP+":1081") || !strings.Contains(printed, first.ClientPrivateKey) {
					t.Fatal("missing native WireGuard client configuration")
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
