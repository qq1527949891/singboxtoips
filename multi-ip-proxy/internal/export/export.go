package export

import (
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	"mipproxy.local/mipproxy/internal/model"
)

type object = map[string]any

func URI(s model.State, n model.Node) string {
	q := url.Values{}
	u := url.URL{Host: s.Endpoint(n), Fragment: n.Name(s.Protocol)}
	switch s.Protocol {
	case "reality":
		u.Scheme = "vless"
		u.User = url.User(n.UUID)
		q.Set("encryption", "none")
		q.Set("security", "reality")
		q.Set("type", "tcp")
		q.Set("sni", s.ServerName)
		q.Set("fp", "chrome")
		q.Set("pbk", n.PublicKey)
		q.Set("sid", n.ShortID)
		q.Set("flow", "xtls-rprx-vision")
	case "trojan":
		u.Scheme = "trojan"
		u.User = url.User(n.Password)
		q.Set("sni", s.ServerName)
		q.Set("security", "tls")
		q.Set("type", "tcp")
	case "hysteria2":
		u.Scheme = "hysteria2"
		u.User = url.User(n.Password)
		q.Set("sni", s.ServerName)
		if s.ObfsPassword != "" {
			q.Set("obfs", "salamander")
			q.Set("obfs-password", s.ObfsPassword)
		}
	case "tuic":
		u.Scheme = "tuic"
		u.User = url.UserPassword(n.UUID, n.Password)
		q.Set("sni", s.ServerName)
		q.Set("congestion_control", "bbr")
		q.Set("udp_relay_mode", "native")
		q.Set("alpn", "h3")
	case "shadowsocks":
		u.Scheme = "ss"
		u.User = url.User(base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:" + n.Password)))
	case "socks5":
		u.Scheme = "socks5"
		u.User = url.UserPassword(n.Username, n.Password)
	case "vmess":
		port := n.PublicPort
		if port == 0 {
			port = s.ListenPort(n)
		}
		data, _ := json.Marshal(object{"v": "2", "ps": n.Name(s.Protocol), "add": n.PublicIP, "port": strconv.Itoa(port), "id": n.UUID, "aid": "0", "scy": "auto", "net": "tcp", "type": "none", "host": "", "path": "", "tls": "tls", "sni": s.ServerName, "alpn": "http/1.1"})
		return "vmess://" + base64.StdEncoding.EncodeToString(data)
	case "wireguard":
		return ""
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func SingBoxNode(s model.State, n model.Node) object {
	port := n.PublicPort
	if port == 0 {
		port = s.ListenPort(n)
	}
	o := object{"tag": n.Name(s.Protocol), "server": n.PublicIP, "server_port": port}
	tls := object{"enabled": true, "server_name": s.ServerName}
	switch s.Protocol {
	case "reality":
		o["type"] = "vless"
		o["uuid"] = n.UUID
		o["flow"] = "xtls-rprx-vision"
		o["packet_encoding"] = "xudp"
		tls["utls"] = object{"enabled": true, "fingerprint": "chrome"}
		tls["reality"] = object{"enabled": true, "public_key": n.PublicKey, "short_id": n.ShortID}
		o["tls"] = tls
	case "trojan":
		o["type"] = "trojan"
		o["password"] = n.Password
		tls["alpn"] = []string{"http/1.1"}
		o["tls"] = tls
	case "hysteria2":
		o["type"] = "hysteria2"
		o["password"] = n.Password
		o["tls"] = tls
		if s.ObfsPassword != "" {
			o["obfs"] = object{"type": "salamander", "password": s.ObfsPassword}
		}
	case "tuic":
		o["type"] = "tuic"
		o["uuid"] = n.UUID
		o["password"] = n.Password
		o["congestion_control"] = "bbr"
		o["udp_relay_mode"] = "native"
		o["zero_rtt_handshake"] = false
		tls["alpn"] = []string{"h3"}
		o["tls"] = tls
	case "shadowsocks":
		o["type"] = "shadowsocks"
		o["method"] = "aes-256-gcm"
		o["password"] = n.Password
	case "vmess":
		o["type"] = "vmess"
		o["uuid"] = n.UUID
		o["security"] = "auto"
		o["alter_id"] = 0
		tls["alpn"] = []string{"http/1.1"}
		o["tls"] = tls
	case "socks5":
		o["type"] = "socks"
		o["version"] = "5"
		o["username"] = n.Username
		o["password"] = n.Password
	case "wireguard":
		o = object{"type": "wireguard", "tag": n.Name(s.Protocol), "system": false, "mtu": 1280, "address": []string{n.WGClientIP() + "/32"}, "private_key": n.ClientPrivateKey, "peers": []any{object{"address": n.PublicIP, "port": port, "public_key": model.WGKey(n.PublicKey), "allowed_ips": []string{"0.0.0.0/0"}, "persistent_keepalive_interval": 25}}}
	}
	return o
}

func MihomoNode(s model.State, n model.Node) object {
	port := n.PublicPort
	if port == 0 {
		port = s.ListenPort(n)
	}
	o := object{"name": n.Name(s.Protocol), "server": n.PublicIP, "port": port, "udp": true}
	switch s.Protocol {
	case "reality":
		o["type"] = "vless"
		o["uuid"] = n.UUID
		o["flow"] = "xtls-rprx-vision"
		o["tls"] = true
		o["servername"] = s.ServerName
		o["client-fingerprint"] = "chrome"
		o["network"] = "tcp"
		o["packet-encoding"] = "xudp"
		o["reality-opts"] = object{"public-key": n.PublicKey, "short-id": n.ShortID}
	case "trojan":
		o["type"] = "trojan"
		o["password"] = n.Password
		o["sni"] = s.ServerName
		o["alpn"] = []string{"http/1.1"}
	case "hysteria2":
		o["type"] = "hysteria2"
		o["password"] = n.Password
		o["sni"] = s.ServerName
		if s.ObfsPassword != "" {
			o["obfs"] = "salamander"
			o["obfs-password"] = s.ObfsPassword
		}
	case "tuic":
		o["type"] = "tuic"
		o["uuid"] = n.UUID
		o["password"] = n.Password
		o["sni"] = s.ServerName
		o["alpn"] = []string{"h3"}
		o["congestion-controller"] = "bbr"
		o["udp-relay-mode"] = "native"
		o["reduce-rtt"] = false
	case "shadowsocks":
		o["type"] = "ss"
		o["cipher"] = "aes-256-gcm"
		o["password"] = n.Password
	case "vmess":
		o["type"] = "vmess"
		o["uuid"] = n.UUID
		o["alterId"] = 0
		o["cipher"] = "auto"
		o["tls"] = true
		o["servername"] = s.ServerName
		o["network"] = "tcp"
		o["alpn"] = []string{"http/1.1"}
	case "socks5":
		o["type"] = "socks5"
		o["username"] = n.Username
		o["password"] = n.Password
	case "wireguard":
		o["type"] = "wireguard"
		o["private-key"] = n.ClientPrivateKey
		o["public-key"] = model.WGKey(n.PublicKey)
		o["ip"] = n.WGClientIP()
		o["allowed-ips"] = []string{"0.0.0.0/0"}
		o["persistent-keepalive"] = 25
		o["remote-dns-resolve"] = true
		o["dns"] = []string{"1.1.1.1", "8.8.8.8"}
		o["mtu"] = 1280
	}
	return o
}

func Files(s model.State) (map[string][]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	links := []string{}
	proxies := []any{}
	outbounds := []any{}
	names := []string{}
	var csvText strings.Builder
	w := csv.NewWriter(&csvText)
	_ = w.Write([]string{"name", "protocol", "public_ip", "port", "local_ip", "share_uri"})
	for _, n := range s.Nodes {
		uri := URI(s, n)
		links = append(links, uri)
		names = append(names, n.Name(s.Protocol))
		proxies = append(proxies, MihomoNode(s, n))
		outbounds = append(outbounds, SingBoxNode(s, n))
		port := n.PublicPort
		if port == 0 {
			port = s.ListenPort(n)
		}
		_ = w.Write([]string{n.Name(s.Protocol), s.Protocol, n.PublicIP, strconv.Itoa(port), n.LocalIP, uri})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	plain := strings.Join(links, "\n") + "\n"
	provider, err := yaml.Marshal(object{"proxies": proxies})
	if err != nil {
		return nil, err
	}
	full, err := yaml.Marshal(object{"mixed-port": 7890, "allow-lan": false, "ipv6": false, "bind-address": "127.0.0.1", "mode": "rule", "log-level": "warning", "proxies": proxies, "proxy-groups": []any{object{"name": "节点选择", "type": "select", "proxies": names}}, "rules": []string{"MATCH,节点选择"}})
	if err != nil {
		return nil, err
	}
	clientOut := []any{object{"type": "selector", "tag": "select", "outbounds": names, "default": names[0]}}
	endpoints := []any{}
	if s.Protocol == "wireguard" {
		endpoints = outbounds
	} else {
		clientOut = append(clientOut, outbounds...)
	}
	client := object{"log": object{"level": "warn"}, "dns": object{"strategy": "ipv4_only", "servers": []any{object{"type": "local", "tag": "local-dns"}}}, "inbounds": []any{object{"type": "mixed", "tag": "local", "listen": "127.0.0.1", "listen_port": 1080}}, "outbounds": clientOut, "endpoints": endpoints, "route": object{"rules": []any{object{"ip_version": 6, "action": "reject"}}, "final": "select", "default_domain_resolver": "local-dns"}}
	readme := fmt.Sprintf("所选协议：%s\n节点数：%d（每个 IP 一个节点，全部使用同一种协议）\n\nlinks.txt：逐行分享链接\nsubscription.txt：Base64 分享链接集合；客户端须支持对应协议。\nmihomo.yaml：可导入的 Mihomo 配置，手动选择节点。\nmihomo-provider.yaml：Mihomo proxy-provider 内容。\nsing-box.json：可导入的 sing-box 客户端配置。\nnodes.csv：节点清单。\n\nTLS 校验保持开启；证书须受客户端信任。\n这些文件和订阅地址包含节点凭据，请妥善保存。\n", s.Protocol, len(s.Nodes))
	for _, link := range Subscriptions(s) {
		readme += fmt.Sprintf("%s：%s\n", link.Name, link.URL)
	}
	files := map[string][]byte{"links.txt": []byte(plain), "subscription.txt": []byte(base64.StdEncoding.EncodeToString([]byte(plain)) + "\n"), "mihomo.yaml": full, "mihomo-provider.yaml": provider, "sing-box.json": model.JSON(client), "nodes.csv": []byte(csvText.String()), "使用说明.txt": []byte(readme)}
	if s.Protocol == "wireguard" {
		delete(files, "links.txt")
		delete(files, "subscription.txt")
		files["使用说明.txt"] = []byte(strings.ReplaceAll(readme, "links.txt：逐行分享链接\nsubscription.txt：Base64 分享链接集合；客户端须支持对应协议。\n", "") + "\nWireGuard 没有统一的分享 URI；请导入每个 IP 对应的 .conf 或上述客户端配置。\n每个 IP 导出一个客户端 peer。同一配置请勿在多台设备同时启用，否则 WireGuard 的端点漫游会互相抢占连接。\n本工具使用用户态端点转发 IPv4 TCP/UDP，不承诺完整的内核 VPN/ICMP 行为。\n")
		for _, n := range s.Nodes {
			files[n.Name(s.Protocol)+".conf"] = []byte(fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = %s/32\nDNS = 1.1.1.1\nMTU = 1280\n\n[Peer]\nPublicKey = %s\nAllowedIPs = 0.0.0.0/0\nEndpoint = %s\nPersistentKeepalive = 25\n", n.ClientPrivateKey, n.WGClientIP(), model.WGKey(n.PublicKey), s.Endpoint(n)))
		}
	}
	if s.Protocol == "socks5" {
		files["使用说明.txt"] = []byte(readme + "\nSOCKS5 的账号及流量不加密。UDP ASSOCIATE 使用动态中继端口，放行监听端口的 UDP 并不足够。直接绑定公网 IP 时还需按客户端来源放行动态 UDP 端口；NAT 需保留这些端口，客户端须能处理返回的私网中继地址。仅固定端口映射不能保证 SOCKS5 UDP 可用。\nSOCKS5 分享链接的导入格式因客户端而异，可优先使用 Mihomo/sing-box 配置或手动填写账号。\n")
	}
	return files, nil
}

// ProbeClient creates one temporary local mixed listener per node for a real proxy test.
func ProbeClient(s model.State, port int) object {
	ins, outs, rules := []any{}, []any{}, []any{}
	for i, n := range s.Nodes {
		tag := fmt.Sprintf("probe-%d", i)
		ins = append(ins, object{"type": "mixed", "tag": tag, "listen": "127.0.0.1", "listen_port": port + i})
		node := SingBoxNode(s, n)
		if s.Protocol == "wireguard" {
			// A dedicated diagnostic peer avoids taking over a user's roaming peer.
			node["private_key"] = n.ProbePrivateKey
			node["address"] = []string{n.WGProbeIP() + "/32"}
		}
		outs = append(outs, node)
		rules = append(rules, object{"inbound": []string{tag}, "action": "route", "outbound": n.Name(s.Protocol)})
	}
	rules = append(rules, object{"action": "reject"})
	endpoints := []any{}
	if s.Protocol == "wireguard" {
		endpoints = outs
		outs = []any{}
	}
	return object{"log": object{"level": "warn"}, "dns": object{"servers": []any{object{"type": "local", "tag": "local-dns"}}}, "inbounds": ins, "outbounds": outs, "endpoints": endpoints, "route": object{"rules": rules, "default_domain_resolver": "local-dns"}}
}

func ProxyURL(port int) *url.URL {
	return &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
}
