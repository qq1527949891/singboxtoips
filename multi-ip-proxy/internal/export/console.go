package export

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
	"mipproxy.local/mipproxy/internal/model"
)

// Console keeps the requested plain SOCKS5/SS layout separate from standard
// subscription URIs, so existing client import formats remain usable.
func Console(s model.State) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	var output strings.Builder
	if s.Protocol == "wireguard" {
		for _, node := range s.Nodes {
			fields := MihomoNode(s, node)
			mapping := yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
			for _, name := range []string{"name", "server", "port", "type", "ip", "private-key", "public-key", "allowed-ips", "persistent-keepalive", "udp", "remote-dns-resolve", "dns", "mtu"} {
				var value yaml.Node
				if err := value.Encode(fields[name]); err != nil {
					return "", err
				}
				mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, &value)
			}
			line, err := yaml.Marshal(&mapping)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&output, "- %s\n", strings.TrimSpace(string(line)))
		}
		return output.String(), nil
	}
	for _, node := range s.Nodes {
		port := node.PublicPort
		if port == 0 {
			port = s.ListenPort(node)
		}
		switch s.Protocol {
		case "socks5":
			fmt.Fprintf(&output, "%s/%d/%s/%s\n", node.PublicIP, port, node.Username, node.Password)
		case "shadowsocks":
			fmt.Fprintf(&output, "%s/%d/aes-256-gcm/%s\n", node.PublicIP, port, node.Password)
		default:
			fmt.Fprintln(&output, URI(s, node))
		}
	}
	return output.String(), nil
}
