package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"mipproxy.local/mipproxy/internal/config"
	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/network"
)

type UI struct {
	In      *bufio.Reader
	Out     io.Writer
	Context context.Context
}

func New(in io.Reader, out io.Writer) *UI {
	return &UI{In: bufio.NewReader(in), Out: out, Context: context.Background()}
}
func (u *UI) Ask(prompt, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(u.Out, "%s [%s]：", prompt, def)
	} else {
		fmt.Fprintf(u.Out, "%s：", prompt)
	}
	type answer struct {
		line string
		err  error
	}
	result := make(chan answer, 1)
	go func() { line, err := u.In.ReadString('\n'); result <- answer{line, err} }()
	var line string
	var err error
	select {
	case <-u.Context.Done():
		return "", u.Context.Err()
	case response := <-result:
		line, err = response.line, response.err
	}
	if err != nil && !(err == io.EOF && line != "") {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		line = def
	}
	return line, nil
}
func (u *UI) Number(prompt string, def, min, max int) (int, error) {
	for {
		v, e := u.Ask(prompt, strconv.Itoa(def))
		if e != nil {
			return 0, e
		}
		n, e := strconv.Atoi(v)
		if e == nil && n >= min && n <= max {
			return n, nil
		}
		fmt.Fprintf(u.Out, "请输入 %d–%d 之间的一个数字。\n", min, max)
	}
}
func (u *UI) Yes(prompt string, def bool) (bool, error) {
	d := "n"
	if def {
		d = "y"
	}
	for {
		v, e := u.Ask(prompt+" (y/n)", d)
		if e != nil {
			return false, e
		}
		switch strings.ToLower(v) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(u.Out, "请输入 y 或 n。")
	}
}

func (u *UI) ChooseProtocol(current string) (string, error) {
	fmt.Fprintln(u.Out, "\n按抗封锁需求优先排列的参考菜单")
	fmt.Fprintln(u.Out, "依据：常见流量识别、主动探测和 UDP 受限场景；不是实测或永久排名。")
	fmt.Fprintln(u.Out, "IP 本身被封时，切换协议不能保证恢复。游戏表现另行标注。")
	fmt.Fprintln(u.Out)
	def := 1
	for i, p := range model.Protocols {
		if p.ID == current {
			def = i + 1
		}
		fmt.Fprintf(u.Out, "%d) %s\n   抗封锁：%s\n   游戏：%s\n   条件：%s\n\n", i+1, p.Name, p.Resistance, p.Gaming, p.Requirement)
	}
	choice, err := u.Number("只选择一种协议，所有 IP 节点统一使用", def, 1, len(model.Protocols))
	if err != nil {
		return "", err
	}
	return model.Protocols[choice-1].ID, nil
}

func Selection(input string, count int) ([]int, error) {
	if strings.EqualFold(input, "a") || input == "全部" {
		result := make([]int, count)
		for i := range result {
			result[i] = i
		}
		return result, nil
	}
	set := map[int]bool{}
	fields := strings.FieldsFunc(input, func(r rune) bool { return r == ',' || r == ' ' || r == '，' })
	for _, f := range fields {
		n, e := strconv.Atoi(f)
		if e != nil || n < 1 || n > count {
			return nil, fmt.Errorf("IP 序号无效")
		}
		set[n-1] = true
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("至少选择一个 IP")
	}
	result := []int{}
	for i := range set {
		result = append(result, i)
	}
	sort.Ints(result)
	return result, nil
}

func (u *UI) Wizard(ctx context.Context, r network.Runner, inventory string, old *model.State) (model.State, error) {
	s := model.State{Schema: 1, Port: 443, CoreVersion: model.CoreVersion, RealityPort: 443, ServerName: "www.microsoft.com", RealityTarget: "www.microsoft.com"}
	if old != nil {
		s = *old
		s.Nodes = nil
	}
	protocol, err := u.ChooseProtocol(s.Protocol)
	if err != nil {
		return s, err
	}
	s.Protocol = protocol
	var candidates []network.Candidate
	if inventory != "" {
		f, e := os.Open(inventory)
		if e != nil {
			return s, e
		}
		candidates, err = network.ReadCSV(f)
		f.Close()
	} else {
		if runtime.GOOS != "linux" {
			return s, fmt.Errorf("自动探测需在 Linux 服务器运行；本机可通过 --inventory CSV 预览")
		}
		candidates, err = network.Discover(ctx, r)
	}
	if err != nil {
		return s, err
	}
	if len(candidates) == 0 {
		return s, fmt.Errorf("没有发现可用地址；可使用 --inventory CSV 指定已配置的地址")
	}
	fmt.Fprintln(u.Out, "\n本机地址候选（公网探测只表示出口，NAT 入站映射须与云平台一致）：")
	for i, c := range candidates {
		public := c.PublicIP
		if public == "" {
			public = "待填写/探测"
		}
		fmt.Fprintf(u.Out, "%d) %s  网卡=%s  公网=%s\n", i+1, c.LocalIP, c.Interface, public)
	}
	var chosen []int
	for {
		v, e := u.Ask("选择 IP 序号，逗号分隔；a 表示全部", "a")
		if e != nil {
			return s, e
		}
		chosen, err = Selection(v, len(candidates))
		if err == nil {
			break
		}
		fmt.Fprintln(u.Out, err)
	}
	portPrompt := "所有节点的本机监听端口（不同 IP 可复用端口）"
	if s.Protocol == "wireguard" {
		portPrompt = "WireGuard 起始 UDP 端口（按节点 ID 分配独立端口）"
	}
	s.Port, err = u.Number(portPrompt, s.Port, 1, 65535)
	if err != nil {
		return s, err
	}
	pairs := map[string]bool{}
	for _, i := range chosen {
		c := candidates[i]
		pairs[c.Interface+"/"+c.Gateway] = true
	}
	policy := s.Protocol == "wireguard"
	if policy {
		fmt.Fprintln(u.Out, "WireGuard 自动启用各 IP 的策略路由，确保 UDP 握手回包使用对应 IP；需要网卡、网关与网段。")
	} else {
		policy, err = u.Yes("启用独立策略路由（多网卡/多网关时建议启用）", len(pairs) > 1)
		if err != nil {
			return s, err
		}
	}
	maxID := 0
	existing := map[string]model.Node{}
	if old != nil {
		for _, n := range old.Nodes {
			existing[n.LocalIP] = n
			if n.ID > maxID {
				maxID = n.ID
			}
		}
	}
	publics := map[string]bool{}
	for _, idx := range chosen {
		c := candidates[idx]
		if c.PublicIP == "" {
			fmt.Fprintf(u.Out, "探测 %s 的公网出口……\n", c.LocalIP)
			c.PublicIP, _ = network.ProbePublicIP(ctx, c.LocalIP)
		}
		for {
			pub, e := u.Ask("本机 "+c.LocalIP+" 对应的公网 IPv4", c.PublicIP)
			if e != nil {
				return s, e
			}
			ip, e := netip.ParseAddr(pub)
			if e != nil || !ip.Is4() || !ip.IsGlobalUnicast() || ip.IsPrivate() {
				fmt.Fprintln(u.Out, "请输入公网 IPv4 地址。")
				continue
			}
			if publics[pub] {
				fmt.Fprintln(u.Out, "该公网 IP 已使用。请核对一对一 NAT；工具不会把多个相同出口冒充不同公网 IP。")
				continue
			}
			c.PublicIP = pub
			publics[pub] = true
			break
		}
		n, ok := existing[c.LocalIP]
		if !ok {
			maxID++
			n, err = model.NewNode(maxID, c.LocalIP, c.PublicIP)
			if err != nil {
				return s, err
			}
		}
		n.PublicIP = c.PublicIP
		if n.Username == "" {
			n.Username = fmt.Sprintf("node-%d", n.ID)
		}
		if err = n.EnsureClientKey(); err != nil {
			return s, err
		}
		n.Interface = c.Interface
		n.Gateway = c.Gateway
		n.Prefix = c.Prefix
		n.PolicyRouting = policy
		if policy {
			if n.Interface == "" {
				n.Interface, err = u.Ask("网卡名（"+n.LocalIP+"）", "")
				if err != nil {
					return s, err
				}
			}
			if n.Gateway == "" {
				n.Gateway, err = u.Ask("该 IP 的默认网关（"+n.LocalIP+"）", "")
				if err != nil {
					return s, err
				}
			}
			if n.Prefix == "" {
				n.Prefix, err = u.Ask("本机网段 CIDR（"+n.LocalIP+"）", "")
				if err != nil {
					return s, err
				}
			}
		}
		s.Nodes = append(s.Nodes, n)
	}
	if s.Protocol == "reality" {
		s.ServerName, err = u.Ask("REALITY 伪装域名/SNI", s.ServerName)
		if err != nil {
			return s, err
		}
		s.RealityTarget, err = u.Ask("握手目标域名（通常与 SNI 相同）", s.ServerName)
		if err != nil {
			return s, err
		}
		s.RealityPort, err = u.Number("握手目标 TLS 端口", s.RealityPort, 1, 65535)
		if err != nil {
			return s, err
		}
	}
	if s.NeedsTLS() {
		def := s.ServerName
		if def == "www.microsoft.com" {
			def = ""
		}
		s.ServerName, err = u.Ask("你持有证书的域名（所有节点共用 SNI，实际连接各自 IP）", def)
		if err != nil {
			return s, err
		}
		s.Certificate, err = u.Ask("PEM 证书/完整链文件路径", s.Certificate)
		if err != nil {
			return s, err
		}
		s.Certificate, err = filepath.Abs(s.Certificate)
		if err != nil {
			return s, err
		}
		s.Key, err = u.Ask("PEM 私钥文件路径", s.Key)
		if err != nil {
			return s, err
		}
		s.Key, err = filepath.Abs(s.Key)
		if err != nil {
			return s, err
		}
		if _, _, err = config.Certificate(s); err != nil {
			return s, err
		}
	}
	if s.Protocol == "hysteria2" {
		enabled, e := u.Yes("启用 Salamander 混淆（客户端须支持；会改变 HTTP/3 流量外观）", s.ObfsPassword != "")
		if e != nil {
			return s, e
		}
		if enabled && s.ObfsPassword == "" {
			n, e := model.NewNode(1, "", "")
			if e != nil {
				return s, e
			}
			s.ObfsPassword = n.Password
		}
		if !enabled {
			s.ObfsPassword = ""
		}
	} else {
		s.ObfsPassword = ""
	}
	if err = s.Validate(); err != nil {
		return s, err
	}
	u.Summary(s)
	return s, nil
}

func (u *UI) Summary(s model.State) {
	p, _ := model.Lookup(s.Protocol)
	fmt.Fprintf(u.Out, "\n协议：%s；节点：%d；全部使用同一协议；本机端口：%d/%s\n", p.Name, len(s.Nodes), s.Port, s.Network())
	fmt.Fprintln(u.Out, "全部节点仅使用 IPv4；正式部署会禁用整台服务器的 IPv6。")
	for _, n := range s.Nodes {
		fmt.Fprintf(u.Out, "  %s -> 本机 %s:%d -> 预期出口 %s  策略路由=%t\n", s.Endpoint(n), n.LocalIP, s.ListenPort(n), n.PublicIP, n.PolicyRouting)
	}
}
