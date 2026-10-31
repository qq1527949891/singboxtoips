package export

import (
	"fmt"
	"strings"

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
		files, err := Files(s)
		if err != nil {
			return "", err
		}
		for _, node := range s.Nodes {
			name := node.Name(s.Protocol) + ".conf"
			fmt.Fprintf(&output, "# %s\n%s\n", name, files[name])
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
