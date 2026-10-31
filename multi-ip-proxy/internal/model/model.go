package model

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"regexp"
	"strings"
)

const CoreVersion = "1.14.4"

type Protocol struct {
	ID, Name, Resistance, Gaming, Requirement string
}

// This is a contextual engineering recommendation, not a measured universal ranking.
var Protocols = []Protocol{
	{"reality", "VLESS + REALITY", "�����Ƽ���TLS �����δ��֤̽����䣻TCP ����ʱ����", "��ת�� UDP����� TCP ����ʱ�������Ӷ���", "������������֤�飻��Ҫ���õ� TLS ����Ŀ��"},
	{"trojan", "Trojan + TLS", "HTTPS ��ۣ�δ��֤������䵽��ҳ", "��ת�� UDP�����ʹ�� TCP", "��Ҫ������ƥ���֤��/˽Կ"},
	{"hysteria2", "Hysteria2", "HTTP/3 ��ۣ���ѡ Salamander ���������� UDP �ɴ�", "��Ϸ���Ⱥ�ѡ��ʹ�� QUIC ���ݱ�ת�� UDP", "��Ҫ������ƥ���֤��/˽Կ"},
	{"vmess", "VMess + TLS", "TLS ����� AEAD ��֤��û�ж�����ҳ����", "��ת�� UDP�����ʹ�� TCP", "��Ҫ������ƥ���֤��/˽Կ"},
	{"tuic", "TUIC v5", "TLS/QUIC ���ܣ����ṩͬ�ȵ���ҳ̽�����", "��Ϸ��ѡ��native UDP ���ݱ�ģʽ", "��Ҫ������ƥ���֤��/˽Կ"},
	{"shadowsocks", "Shadowsocks AEAD", "���ܵ�û�б�׼ TLS/HTTP ��ۣ���Ϊ����ѡ��", "ֱ��ת�� UDP���� UDP ����Ӱ��", "��������֤��"},
	{"wireguard", "WireGuard", "��׼ WireGuard ���ṩ�������������� UDP �ɴ�", "ԭ��������ѡ��������ʹ���û�̬�˵�", "����֤�飻���ڵ�ʹ�ö��� UDP �˿������·��"},
	{"socks5", "SOCKS5���û���/���룩", "�����ܡ���αװ������Ϊ����ѡ��", "֧�� TCP ��Э�̵Ķ�̬�˿� UDP �м�", "����֤�飻UDP ������ж�̬�м̶˿ڣ�NAT �ж�������"},
}

func Lookup(id string) (Protocol, bool) {
	for _, p := range Protocols {
		if p.ID == id {
			return p, true
		}
	}
	return Protocol{}, false
}

type Node struct {
	ID               int    `json:"id"`
	LocalIP          string `json:"local_ip"`
	PublicIP         string `json:"public_ip"`
	PublicPort       int    `json:"public_port,omitempty"`
	Interface        string `json:"interface,omitempty"`
	Gateway          string `json:"gateway,omitempty"`
	Prefix           string `json:"prefix,omitempty"`
	PolicyRouting    bool   `json:"policy_routing"`
	UUID             string `json:"uuid"`
	Password         string `json:"password"`
	PrivateKey       string `json:"private_key"`
	PublicKey        string `json:"public_key"`
	ShortID          string `json:"short_id"`
	Username         string `json:"username,omitempty"`
	ClientPrivateKey string `json:"client_private_key,omitempty"`
	ClientPublicKey  string `json:"client_public_key,omitempty"`
	ProbePrivateKey  string `json:"probe_private_key,omitempty"`
	ProbePublicKey   string `json:"probe_public_key,omitempty"`
}

func (n Node) WGServerIP() string { v := n.ID*4 + 1; return fmt.Sprintf("10.66.%d.%d", v>>8, v&255) }
func (n Node) WGClientIP() string { v := n.ID*4 + 2; return fmt.Sprintf("10.66.%d.%d", v>>8, v&255) }
func (n Node) WGProbeIP() string  { v := n.ID*4 + 3; return fmt.Sprintf("10.66.%d.%d", v>>8, v&255) }
func (s State) ListenPort(n Node) int {
	if s.Protocol == "wireguard" {
		return s.Port + n.ID - 1
	}
	return s.Port
}
func WGKey(rawURL string) string {
	b, _ := base64.RawURLEncoding.DecodeString(rawURL)
	return base64.StdEncoding.EncodeToString(b)
}

func (n *Node) EnsureClientKey() error {
	for _, pair := range [][2]*string{{&n.ClientPrivateKey, &n.ClientPublicKey}, {&n.ProbePrivateKey, &n.ProbePublicKey}} {
		if *pair[0] != "" {
			continue
		}
		k, e := ecdh.X25519().GenerateKey(rand.Reader)
		if e != nil {
			return e
		}
		*pair[0] = base64.StdEncoding.EncodeToString(k.Bytes())
		*pair[1] = base64.StdEncoding.EncodeToString(k.PublicKey().Bytes())
	}
	return nil
}

func (n Node) Table() int                  { return 42000 + n.ID }
func (n Node) Mark() int                   { return 0x4d500000 + n.ID }
func (n Node) SourcePriority() int         { return 20000 + n.ID*2 }
func (n Node) MarkPriority() int           { return n.SourcePriority() + 1 }
func (n Node) Name(protocol string) string { return fmt.Sprintf("%s-%s", n.PublicIP, protocol) }

type State struct {
	Schema        int    `json:"schema"`
	Protocol      string `json:"protocol"`
	Port          int    `json:"port"`
	ServerName    string `json:"server_name,omitempty"`
	RealityTarget string `json:"reality_target,omitempty"`
	RealityPort   int    `json:"reality_port,omitempty"`
	Certificate   string `json:"certificate,omitempty"`
	Key           string `json:"key,omitempty"`
	ObfsPassword  string `json:"obfs_password,omitempty"`
	CoreVersion   string `json:"core_version"`
	Nodes         []Node `json:"nodes"`
}

var ifacePattern = regexp.MustCompile(`^[a-zA-Z0-9_.:@-]{1,64}$`)
var domainPattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?$`)

func ValidHost(s string) bool {
	if ip, err := netip.ParseAddr(s); err == nil {
		return ip.IsGlobalUnicast()
	}
	if len(s) > 253 || !domainPattern.MatchString(s) {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}
	return true
}

func (s State) NeedsTLS() bool {
	return s.Protocol == "trojan" || s.Protocol == "hysteria2" || s.Protocol == "tuic" || s.Protocol == "vmess"
}
func (s State) Network() string {
	if s.Protocol == "hysteria2" || s.Protocol == "tuic" || s.Protocol == "wireguard" {
		return "udp"
	}
	if s.Protocol == "shadowsocks" || s.Protocol == "socks5" {
		return "tcp+udp"
	}
	return "tcp"
}
func (s State) Endpoint(n Node) string {
	port := n.PublicPort
	if port == 0 {
		port = s.ListenPort(n)
	}
	return net.JoinHostPort(n.PublicIP, fmt.Sprint(port))
}

func (s State) Validate() error {
	if s.Schema != 1 {
		return fmt.Errorf("��֧�ֵ�״̬��ʽ %d", s.Schema)
	}
	if _, ok := Lookup(s.Protocol); !ok {
		return fmt.Errorf("ֻ��ѡ��һ����ЧЭ�飺%q", s.Protocol)
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("�˿ڱ����� 1�C65535 ֮��")
	}
	if s.CoreVersion != CoreVersion {
		return fmt.Errorf("���汾���߽���֤�� sing-box %s", CoreVersion)
	}
	if len(s.Nodes) == 0 {
		return fmt.Errorf("������Ҫһ�� IP �ڵ�")
	}
	if s.Protocol == "reality" && (!ValidHost(s.ServerName) || !ValidHost(s.RealityTarget) || s.RealityPort < 1 || s.RealityPort > 65535) {
		return fmt.Errorf("REALITY �� SNI������Ŀ���˿���Ч")
	}
	if s.Protocol == "reality" {
		if target, err := netip.ParseAddr(s.RealityTarget); err == nil && !target.Is4() {
			return fmt.Errorf("REALITY ����Ŀ����Ҫ IPv4 ��ַ��ɽ��� IPv4 ������")
		}
	}
	if s.NeedsTLS() && (!ValidHost(s.ServerName) || s.Certificate == "" || s.Key == "") {
		return fmt.Errorf("��Э����Ҫ TLS ������֤�鼰˽Կ")
	}
	if s.ObfsPassword != "" && s.Protocol != "hysteria2" {
		return fmt.Errorf("ֻ�� Hysteria2 �����û�������")
	}
	locals, publics, endpoints, ids := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[int]bool{}
	for _, n := range s.Nodes {
		if n.ID < 1 || n.ID > 4095 || ids[n.ID] {
			return fmt.Errorf("�ڵ�����Ψһ���� 1�C4095 ��")
		}
		ids[n.ID] = true
		if s.ListenPort(n) > 65535 {
			return fmt.Errorf("WireGuard �˿ڷ��䳬�� 65535���뽵����ʼ�˿�")
		}
		for _, v := range []string{n.LocalIP, n.PublicIP} {
			ip, err := netip.ParseAddr(v)
			if err != nil || !ip.Is4() || !ip.IsGlobalUnicast() {
				return fmt.Errorf("��Ч�� IPv4 ��ַ %q", v)
			}
		}
		if netip.MustParseAddr(n.PublicIP).IsPrivate() {
			return fmt.Errorf("������ڲ�����˽����ַ��%s", n.PublicIP)
		}
		if publics[n.PublicIP] {
			return fmt.Errorf("���� IP �ظ���%s��ÿ���ڵ���Ҫ�������� IP", n.PublicIP)
		}
		publics[n.PublicIP] = true
		if locals[n.LocalIP] {
			return fmt.Errorf("���� IP �ظ���%s���޷��ݴ����ֹ�������", n.LocalIP)
		}
		locals[n.LocalIP] = true
		if n.PublicPort < 0 || n.PublicPort > 65535 {
			return fmt.Errorf("�����˿���Ч")
		}
		if endpoints[s.Endpoint(n)] {
			return fmt.Errorf("��������ظ���%s����˶� NAT ӳ��", s.Endpoint(n))
		}
		endpoints[s.Endpoint(n)] = true
		if n.Interface != "" && !ifacePattern.MatchString(n.Interface) {
			return fmt.Errorf("��Ч������")
		}
		if n.PolicyRouting {
			g, err := netip.ParseAddr(n.Gateway)
			if err != nil || !g.Is4() || !g.IsGlobalUnicast() || n.Interface == "" {
				return fmt.Errorf("����·����Ҫ��������Ч���أ�%s", n.LocalIP)
			}
			if n.Gateway == n.LocalIP {
				return fmt.Errorf("���ز����ǽڵ�������ַ��%s", n.LocalIP)
			}
			p, err := netip.ParsePrefix(n.Prefix)
			if err != nil || !p.Addr().Is4() || p != p.Masked() || !p.Contains(netip.MustParseAddr(n.LocalIP)) {
				return fmt.Errorf("����������Ч��%s", n.LocalIP)
			}
		}
		if s.Protocol == "wireguard" && !n.PolicyRouting {
			return fmt.Errorf("WireGuard �ڵ���Ҫ��������·�ɣ��Թ̶����ֻذ���Դ IP")
		}
		if s.Protocol == "reality" || s.Protocol == "tuic" || s.Protocol == "vmess" {
			if !validUUID(n.UUID) {
				return fmt.Errorf("�ڵ� UUID ��Ч")
			}
		}
		if s.Protocol == "reality" || s.Protocol == "wireguard" {
			priv, e := base64.RawURLEncoding.DecodeString(n.PrivateKey)
			if e != nil || len(priv) != 32 {
				return fmt.Errorf("�ڵ� X25519 ˽Կ��Ч")
			}
			pk, e := ecdh.X25519().NewPrivateKey(priv)
			if e != nil || base64.RawURLEncoding.EncodeToString(pk.PublicKey().Bytes()) != n.PublicKey {
				return fmt.Errorf("�ڵ� X25519 ��˽Կ��ƥ��")
			}
			if s.Protocol == "reality" {
				short, e := hex.DecodeString(n.ShortID)
				if e != nil || len(short) != 8 {
					return fmt.Errorf("REALITY short ID ��Ч")
				}
			}
			if s.Protocol == "wireguard" {
				for _, pair := range [][2]string{{n.ClientPrivateKey, n.ClientPublicKey}, {n.ProbePrivateKey, n.ProbePublicKey}} {
					b, e := base64.StdEncoding.DecodeString(pair[0])
					if e != nil || len(b) != 32 {
						return fmt.Errorf("WireGuard �ͻ��˻����˽Կ��Ч")
					}
					k, e := ecdh.X25519().NewPrivateKey(b)
					if e != nil || base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()) != pair[1] {
						return fmt.Errorf("WireGuard �ͻ��˻������Կ��ƥ��")
					}
				}
				if n.ClientPublicKey == n.ProbePublicKey {
					return fmt.Errorf("WireGuard �ͻ��˺������Կ�������")
				}
			}
		} else if len(n.Password) < 16 {
			return fmt.Errorf("�ڵ�����������Ҫ 16 �ֽ�")
		}
		if s.Protocol == "socks5" && (n.Username == "" || len(n.Username) > 255 || len(n.Password) > 255) {
			return fmt.Errorf("SOCKS5 �û��������볤����Ч")
		}
	}
	return nil
}

func validUUID(v string) bool {
	b, err := hex.DecodeString(strings.ReplaceAll(v, "-", ""))
	return err == nil && len(v) == 36 && len(b) == 16 && v[8] == '-' && v[13] == '-' && v[18] == '-' && v[23] == '-'
}

func NewNode(id int, local, public string) (Node, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Node{}, err
	}
	buf := make([]byte, 56)
	if _, err = rand.Read(buf); err != nil {
		return Node{}, err
	}
	u := buf[:16]
	u[6] = (u[6] & 15) | 64
	u[8] = (u[8] & 63) | 128
	n := Node{ID: id, LocalIP: local, PublicIP: public, UUID: fmt.Sprintf("%x-%x-%x-%x-%x", u[:4], u[4:6], u[6:8], u[8:10], u[10:]), Password: base64.RawURLEncoding.EncodeToString(buf[16:48]), PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), ShortID: hex.EncodeToString(buf[48:]), Username: "u-" + hex.EncodeToString(buf[:4])}
	if err = n.EnsureClientKey(); err != nil {
		return Node{}, err
	}
	return n, nil
}

func Load(path string) (State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var s State
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err = d.Decode(&s); err != nil {
		return s, fmt.Errorf("��ȡ״̬��%w", err)
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return s, fmt.Errorf("״̬�ļ�����������𻵵�����")
	}
	return s, s.Validate()
}

func JSON(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(b, '\n')
}
