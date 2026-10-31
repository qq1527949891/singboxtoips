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
	{"reality", "VLESS + REALITY", "优先推荐：TLS 外观与未认证探测回落；TCP 可用时适用", "可转发 UDP；外层 TCP 丢包时可能增加抖动", "无需自有域名证书；需要可用的 TLS 握手目标"},
	{"trojan", "Trojan + TLS", "HTTPS 外观，未认证请求回落到网页", "可转发 UDP；外层使用 TCP", "需要域名及匹配的证书/私钥"},
	{"hysteria2", "Hysteria2", "HTTP/3 外观，可选 Salamander 混淆；依赖 UDP 可达", "游戏优先候选：使用 QUIC 数据报转发 UDP", "需要域名及匹配的证书/私钥"},
	{"vmess", "VMess + TLS", "TLS 外观与 AEAD 认证；没有独立网页回落", "可转发 UDP；外层使用 TCP", "需要域名及匹配的证书/私钥"},
	{"tuic", "TUIC v5", "TLS/QUIC 加密；不提供同等的网页探测回落", "游戏候选：native UDP 数据报模式", "需要域名及匹配的证书/私钥"},
	{"shadowsocks", "Shadowsocks AEAD", "加密但没有标准 TLS/HTTP 外观，作为兼容选项", "直接转发 UDP；受 UDP 限制影响", "无需域名证书"},
	{"wireguard", "WireGuard", "标准 WireGuard 不提供流量混淆，依赖 UDP 可达", "原生隧道候选；本工具使用用户态端点", "无需证书；各节点使用独立 UDP 端口与策略路由"},
	{"socks5", "SOCKS5（用户名/密码）", "不加密、不伪装，仅作为兼容选项", "支持 TCP 与协商的动态端口 UDP 中继", "无需证书；UDP 还需放行动态中继端口，NAT 有额外限制"},
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
		return fmt.Errorf("不支持的状态格式 %d", s.Schema)
	}
	if _, ok := Lookup(s.Protocol); !ok {
		return fmt.Errorf("只能选择一种有效协议：%q", s.Protocol)
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("端口必须在 1–65535 之间")
	}
	if s.CoreVersion != CoreVersion {
		return fmt.Errorf("本版本工具仅验证过 sing-box %s", CoreVersion)
	}
	if len(s.Nodes) == 0 {
		return fmt.Errorf("至少需要一个 IP 节点")
	}
	if s.Protocol == "reality" && (!ValidHost(s.ServerName) || !ValidHost(s.RealityTarget) || s.RealityPort < 1 || s.RealityPort > 65535) {
		return fmt.Errorf("REALITY 的 SNI、握手目标或端口无效")
	}
	if s.Protocol == "reality" {
		if target, err := netip.ParseAddr(s.RealityTarget); err == nil && !target.Is4() {
			return fmt.Errorf("REALITY 握手目标需要 IPv4 地址或可解析 IPv4 的域名")
		}
	}
	if s.NeedsTLS() && (!ValidHost(s.ServerName) || s.Certificate == "" || s.Key == "") {
		return fmt.Errorf("此协议需要 TLS 域名、证书及私钥")
	}
	if s.ObfsPassword != "" && s.Protocol != "hysteria2" {
		return fmt.Errorf("只有 Hysteria2 可设置混淆密码")
	}
	locals, publics, endpoints, ids := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[int]bool{}
	for _, n := range s.Nodes {
		if n.ID < 1 || n.ID > 4095 || ids[n.ID] {
			return fmt.Errorf("节点编号须唯一且在 1–4095 内")
		}
		ids[n.ID] = true
		if s.ListenPort(n) > 65535 {
			return fmt.Errorf("WireGuard 端口分配超过 65535，请降低起始端口")
		}
		for _, v := range []string{n.LocalIP, n.PublicIP} {
			ip, err := netip.ParseAddr(v)
			if err != nil || !ip.Is4() || !ip.IsGlobalUnicast() {
				return fmt.Errorf("无效的 IPv4 地址 %q", v)
			}
		}
		if netip.MustParseAddr(n.PublicIP).IsPrivate() {
			return fmt.Errorf("公网入口不能是私网地址：%s", n.PublicIP)
		}
		if publics[n.PublicIP] {
			return fmt.Errorf("公网 IP 重复：%s；每个节点需要独立公网 IP", n.PublicIP)
		}
		publics[n.PublicIP] = true
		if locals[n.LocalIP] {
			return fmt.Errorf("本机 IP 重复：%s；无法据此区分公网出口", n.LocalIP)
		}
		locals[n.LocalIP] = true
		if n.PublicPort < 0 || n.PublicPort > 65535 {
			return fmt.Errorf("公网端口无效")
		}
		if endpoints[s.Endpoint(n)] {
			return fmt.Errorf("公网入口重复：%s；请核对 NAT 映射", s.Endpoint(n))
		}
		endpoints[s.Endpoint(n)] = true
		if n.Interface != "" && !ifacePattern.MatchString(n.Interface) {
			return fmt.Errorf("无效网卡名")
		}
		if n.PolicyRouting {
			g, err := netip.ParseAddr(n.Gateway)
			if err != nil || !g.Is4() || !g.IsGlobalUnicast() || n.Interface == "" {
				return fmt.Errorf("策略路由需要网卡和有效网关：%s", n.LocalIP)
			}
			if n.Gateway == n.LocalIP {
				return fmt.Errorf("网关不能是节点自身地址：%s", n.LocalIP)
			}
			p, err := netip.ParsePrefix(n.Prefix)
			if err != nil || !p.Addr().Is4() || p != p.Masked() || !p.Contains(netip.MustParseAddr(n.LocalIP)) {
				return fmt.Errorf("本机网段无效：%s", n.LocalIP)
			}
		}
		if s.Protocol == "wireguard" && !n.PolicyRouting {
			return fmt.Errorf("WireGuard 节点需要独立策略路由，以固定握手回包的源 IP")
		}
		if s.Protocol == "reality" || s.Protocol == "tuic" || s.Protocol == "vmess" {
			if !validUUID(n.UUID) {
				return fmt.Errorf("节点 UUID 无效")
			}
		}
		if s.Protocol == "reality" || s.Protocol == "wireguard" {
			priv, e := base64.RawURLEncoding.DecodeString(n.PrivateKey)
			if e != nil || len(priv) != 32 {
				return fmt.Errorf("节点 X25519 私钥无效")
			}
			pk, e := ecdh.X25519().NewPrivateKey(priv)
			if e != nil || base64.RawURLEncoding.EncodeToString(pk.PublicKey().Bytes()) != n.PublicKey {
				return fmt.Errorf("节点 X25519 公私钥不匹配")
			}
			if s.Protocol == "reality" {
				short, e := hex.DecodeString(n.ShortID)
				if e != nil || len(short) != 8 {
					return fmt.Errorf("REALITY short ID 无效")
				}
			}
			if s.Protocol == "wireguard" {
				for _, pair := range [][2]string{{n.ClientPrivateKey, n.ClientPublicKey}, {n.ProbePrivateKey, n.ProbePublicKey}} {
					b, e := base64.StdEncoding.DecodeString(pair[0])
					if e != nil || len(b) != 32 {
						return fmt.Errorf("WireGuard 客户端或诊断私钥无效")
					}
					k, e := ecdh.X25519().NewPrivateKey(b)
					if e != nil || base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()) != pair[1] {
						return fmt.Errorf("WireGuard 客户端或诊断密钥不匹配")
					}
				}
				if n.ClientPublicKey == n.ProbePublicKey {
					return fmt.Errorf("WireGuard 客户端和诊断密钥必须独立")
				}
			}
		} else if len(n.Password) < 16 {
			return fmt.Errorf("节点密码至少需要 16 字节")
		}
		if s.Protocol == "socks5" && (n.Username == "" || len(n.Username) > 255 || len(n.Password) > 255) {
			return fmt.Errorf("SOCKS5 用户名或密码长度无效")
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
		return s, fmt.Errorf("读取状态：%w", err)
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return s, fmt.Errorf("状态文件包含多余或损坏的数据")
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
