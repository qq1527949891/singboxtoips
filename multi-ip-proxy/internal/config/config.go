package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"mipproxy.local/mipproxy/internal/model"
)

type Object = map[string]any

func Certificate(s model.State) ([]string, []string, error) {
	cert, err := os.ReadFile(s.Certificate)
	if err != nil {
		return nil, nil, fmt.Errorf("读取证书：%w", err)
	}
	key, err := os.ReadFile(s.Key)
	if err != nil {
		return nil, nil, fmt.Errorf("读取证书私钥：%w", err)
	}
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, nil, fmt.Errorf("证书与私钥不匹配：%w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, nil, err
	}
	if time.Now().Before(leaf.NotBefore) || time.Now().After(leaf.NotAfter) {
		return nil, nil, fmt.Errorf("证书未生效或已过期")
	}
	if err = leaf.VerifyHostname(s.ServerName); err != nil {
		return nil, nil, fmt.Errorf("证书不匹配域名 %s：%w", s.ServerName, err)
	}
	if s.Protocol == "hysteria2" && leaf.PublicKeyAlgorithm == x509.Ed25519 {
		return nil, nil, fmt.Errorf("Hysteria2 的 Chrome 握手模拟不兼容 Ed25519 证书，请使用 ECDSA 或 RSA 证书")
	}
	return []string{string(cert)}, []string{string(key)}, nil
}

func Server(s model.State) (Object, error) {
	return ServerWithACMEDirectory(s, "/var/lib/mip-proxy/acme")
}

func ServerWithACMEDirectory(s model.State, acmeDirectory string) (Object, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	var cert, key []string
	if s.NeedsTLS() && !s.AutomaticCertificate() {
		var err error
		cert, key, err = Certificate(s)
		if err != nil {
			return nil, err
		}
	}
	inbounds, outbounds, rules, dnsServers := []any{}, []any{}, []any{}, []any{}
	// IPv4 source binding does not constrain an explicit IPv6 destination.
	// Reject it before node routing so it cannot use the host's primary IPv6.
	rules = append(rules, Object{"ip_version": 6, "action": "reject"})
	endpoints := []any{}
	for _, n := range s.Nodes {
		inTag, outTag, dnsTag := fmt.Sprintf("in-%d", n.ID), fmt.Sprintf("out-%d", n.ID), fmt.Sprintf("dns-%d", n.ID)
		out := Object{"type": "direct", "tag": outTag, "inet4_bind_address": n.LocalIP, "domain_resolver": Object{"server": dnsTag, "strategy": "ipv4_only"}}
		dns := Object{"type": "udp", "tag": dnsTag, "server": "1.1.1.1", "server_port": 53, "inet4_bind_address": n.LocalIP}
		if n.Interface != "" {
			out["bind_interface"] = n.Interface
			dns["bind_interface"] = n.Interface
		}
		if n.PolicyRouting {
			out["routing_mark"] = n.Mark()
			dns["routing_mark"] = n.Mark()
		}
		in := Object{"tag": inTag, "listen": n.LocalIP, "listen_port": s.ListenPort(n)}
		switch s.Protocol {
		case "reality":
			in["type"] = "vless"
			in["users"] = []any{Object{"uuid": n.UUID, "flow": "xtls-rprx-vision"}}
			in["tls"] = Object{"enabled": true, "server_name": s.ServerName, "reality": Object{"enabled": true, "handshake": Object{"server": s.RealityTarget, "server_port": s.RealityPort, "inet4_bind_address": n.LocalIP, "domain_resolver": Object{"server": dnsTag, "strategy": "ipv4_only"}}, "private_key": n.PrivateKey, "short_id": []string{n.ShortID}}}
		case "trojan":
			in["type"] = "trojan"
			in["users"] = []any{Object{"password": n.Password}}
			in["tls"] = Object{"enabled": true, "server_name": s.ServerName, "certificate": cert, "key": key, "alpn": []string{"http/1.1"}}
			in["fallback"] = Object{"server": "127.0.0.1", "server_port": 18080}
		case "hysteria2":
			in["type"] = "hysteria2"
			in["users"] = []any{Object{"password": n.Password}}
			in["tls"] = Object{"enabled": true, "server_name": s.ServerName, "certificate": cert, "key": key}
			in["masquerade"] = Object{"type": "string", "status_code": 200, "headers": map[string]string{"Content-Type": "text/html; charset=utf-8"}, "content": CoverHTML}
			if s.ObfsPassword != "" {
				in["obfs"] = Object{"type": "salamander", "password": s.ObfsPassword}
			}
		case "tuic":
			in["type"] = "tuic"
			in["users"] = []any{Object{"uuid": n.UUID, "password": n.Password}}
			in["congestion_control"] = "bbr"
			in["zero_rtt_handshake"] = false
			in["tls"] = Object{"enabled": true, "server_name": s.ServerName, "certificate": cert, "key": key, "alpn": []string{"h3"}}
		case "shadowsocks":
			in["type"] = "shadowsocks"
			in["method"] = "aes-256-gcm"
			in["password"] = n.Password
		case "vmess":
			in["type"] = "vmess"
			in["users"] = []any{Object{"uuid": n.UUID, "alterId": 0}}
			in["tls"] = Object{"enabled": true, "server_name": s.ServerName, "certificate": cert, "key": key, "alpn": []string{"http/1.1"}}
		case "socks5":
			in["type"] = "socks"
			in["users"] = []any{Object{"username": n.Username, "password": n.Password}}
		case "wireguard":
			in = Object{"type": "wireguard", "tag": inTag, "system": false, "mtu": 1280, "address": []string{n.WGServerIP() + "/32"}, "private_key": model.WGKey(n.PrivateKey), "listen_port": s.ListenPort(n), "inet4_bind_address": n.LocalIP, "domain_resolver": Object{"server": dnsTag, "strategy": "ipv4_only"}, "peers": []any{Object{"public_key": n.ClientPublicKey, "allowed_ips": []string{n.WGClientIP() + "/32"}}, Object{"public_key": n.ProbePublicKey, "allowed_ips": []string{n.WGProbeIP() + "/32"}}}}
			if n.PolicyRouting {
				in["routing_mark"] = n.Mark()
			}
			if n.Interface != "" {
				in["bind_interface"] = n.Interface
			}
		}
		if s.AutomaticCertificate() {
			tls := in["tls"].(Object)
			delete(tls, "certificate")
			delete(tls, "key")
			tls["certificate_provider"] = "mip-certificate"
		}
		if s.Protocol == "wireguard" {
			endpoints = append(endpoints, in)
		} else {
			inbounds = append(inbounds, in)
		}
		outbounds = append(outbounds, out)
		dnsServers = append(dnsServers, dns)
		rules = append(rules, Object{"inbound": []string{inTag}, "action": "route", "outbound": outTag})
	}
	// Unknown inbounds must never silently use another node's source address.
	rules = append(rules, Object{"action": "reject"})
	result := Object{"log": Object{"level": "warn", "timestamp": true}, "dns": Object{"servers": dnsServers, "strategy": "ipv4_only"}, "inbounds": inbounds, "endpoints": endpoints, "outbounds": outbounds, "route": Object{"rules": rules, "default_domain_resolver": fmt.Sprintf("dns-%d", s.Nodes[0].ID)}}
	if s.AutomaticCertificate() {
		result["certificate_providers"] = []any{Object{"type": "acme", "tag": "mip-certificate", "domain": []string{s.ServerName}, "default_server_name": s.ServerName, "data_directory": acmeDirectory, "provider": "letsencrypt", "key_type": "p256", "profile": "shortlived", "disable_tls_alpn_challenge": true}}
	}
	return result, nil
}

const CoverHTML = "<!doctype html><html lang=\"en\"><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>Welcome</title><body><h1>Welcome</h1><p>This service is available.</p></body></html>"
