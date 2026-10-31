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
		fmt.Fprintf(u.Out, "%s [%s]��", prompt, def)
	} else {
		fmt.Fprintf(u.Out, "%s��", prompt)
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
		fmt.Fprintf(u.Out, "������ %d�C%d ֮���һ�����֡�\n", min, max)
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
		fmt.Fprintln(u.Out, "������ y �� n��")
	}
}

func (u *UI) ChooseProtocol(current string) (string, error) {
	fmt.Fprintln(u.Out, "\n�������������������еĲο��˵�")
	fmt.Fprintln(u.Out, "���ݣ���������ʶ������̽��� UDP ���޳���������ʵ�������������")
	fmt.Fprintln(u.Out, "IP ��������ʱ���л�Э�鲻�ܱ�֤�ָ�����Ϸ�������б�ע��")
	fmt.Fprintln(u.Out)
	def := 1
	for i, p := range model.Protocols {
		if p.ID == current {
			def = i + 1
		}
		fmt.Fprintf(u.Out, "%d) %s\n   ��������%s\n   ��Ϸ��%s\n   ������%s\n\n", i+1, p.Name, p.Resistance, p.Gaming, p.Requirement)
	}
	choice, err := u.Number("ֻѡ��һ��Э�飬���� IP �ڵ�ͳһʹ��", def, 1, len(model.Protocols))
	if err != nil {
		return "", err
	}
	return model.Protocols[choice-1].ID, nil
}

func Selection(input string, count int) ([]int, error) {
	if strings.EqualFold(input, "a") || input == "ȫ��" {
		result := make([]int, count)
		for i := range result {
			result[i] = i
		}
		return result, nil
	}
	set := map[int]bool{}
	fields := strings.FieldsFunc(input, func(r rune) bool { return r == ',' || r == ' ' || r == '��' })
	for _, f := range fields {
		n, e := strconv.Atoi(f)
		if e != nil || n < 1 || n > count {
			return nil, fmt.Errorf("IP �����Ч")
		}
		set[n-1] = true
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("����ѡ��һ�� IP")
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
			return s, fmt.Errorf("�Զ�̽������ Linux ���������У�������ͨ�� --inventory CSV Ԥ��")
		}
		candidates, err = network.Discover(ctx, r)
	}
	if err != nil {
		return s, err
	}
	if len(candidates) == 0 {
		return s, fmt.Errorf("û�з��ֿ��õ�ַ����ʹ�� --inventory CSV ָ�������õĵ�ַ")
	}
	fmt.Fprintln(u.Out, "\n������ַ��ѡ������̽��ֻ��ʾ���ڣ�NAT ��վӳ��������ƽ̨һ�£���")
	for i, c := range candidates {
		public := c.PublicIP
		if public == "" {
			public = "����д/̽��"
		}
		fmt.Fprintf(u.Out, "%d) %s  ����=%s  ����=%s\n", i+1, c.LocalIP, c.Interface, public)
	}
	var chosen []int
	for {
		v, e := u.Ask("ѡ�� IP ��ţ����ŷָ���a ��ʾȫ��", "a")
		if e != nil {
			return s, e
		}
		chosen, err = Selection(v, len(candidates))
		if err == nil {
			break
		}
		fmt.Fprintln(u.Out, err)
	}
	portPrompt := "���нڵ�ı��������˿ڣ���ͬ IP �ɸ��ö˿ڣ�"
	if s.Protocol == "wireguard" {
		portPrompt = "WireGuard ��ʼ UDP �˿ڣ����ڵ� ID ��������˿ڣ�"
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
		fmt.Fprintln(u.Out, "WireGuard �Զ����ø� IP �Ĳ���·�ɣ�ȷ�� UDP ���ֻذ�ʹ�ö�Ӧ IP����Ҫ���������������Ρ�")
	} else {
		policy, err = u.Yes("���ö�������·�ɣ�������/������ʱ�������ã�", len(pairs) > 1)
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
			fmt.Fprintf(u.Out, "̽�� %s �Ĺ������ڡ���\n", c.LocalIP)
			c.PublicIP, _ = network.ProbePublicIP(ctx, c.LocalIP)
		}
		for {
			pub, e := u.Ask("���� "+c.LocalIP+" ��Ӧ�Ĺ��� IPv4", c.PublicIP)
			if e != nil {
				return s, e
			}
			ip, e := netip.ParseAddr(pub)
			if e != nil || !ip.Is4() || !ip.IsGlobalUnicast() || ip.IsPrivate() {
				fmt.Fprintln(u.Out, "�����빫�� IPv4 ��ַ��")
				continue
			}
			if publics[pub] {
				fmt.Fprintln(u.Out, "�ù��� IP ��ʹ�á���˶�һ��һ NAT�����߲���Ѷ����ͬ����ð�䲻ͬ���� IP��")
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
				n.Interface, err = u.Ask("��������"+n.LocalIP+"��", "")
				if err != nil {
					return s, err
				}
			}
			if n.Gateway == "" {
				n.Gateway, err = u.Ask("�� IP ��Ĭ�����أ�"+n.LocalIP+"��", "")
				if err != nil {
					return s, err
				}
			}
			if n.Prefix == "" {
				n.Prefix, err = u.Ask("�������� CIDR��"+n.LocalIP+"��", "")
				if err != nil {
					return s, err
				}
			}
		}
		s.Nodes = append(s.Nodes, n)
	}
	if s.Protocol == "reality" {
		s.ServerName, err = u.Ask("REALITY αװ����/SNI", s.ServerName)
		if err != nil {
			return s, err
		}
		s.RealityTarget, err = u.Ask("����Ŀ��������ͨ���� SNI ��ͬ��", s.ServerName)
		if err != nil {
			return s, err
		}
		s.RealityPort, err = u.Number("����Ŀ�� TLS �˿�", s.RealityPort, 1, 65535)
		if err != nil {
			return s, err
		}
	}
	if s.NeedsTLS() {
		def := s.ServerName
		if def == "www.microsoft.com" {
			def = ""
		}
		s.ServerName, err = u.Ask("�����֤������������нڵ㹲�� SNI��ʵ�����Ӹ��� IP��", def)
		if err != nil {
			return s, err
		}
		s.Certificate, err = u.Ask("PEM ֤��/�������ļ�·��", s.Certificate)
		if err != nil {
			return s, err
		}
		s.Certificate, err = filepath.Abs(s.Certificate)
		if err != nil {
			return s, err
		}
		s.Key, err = u.Ask("PEM ˽Կ�ļ�·��", s.Key)
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
		enabled, e := u.Yes("���� Salamander �������ͻ�����֧�֣���ı� HTTP/3 ������ۣ�", s.ObfsPassword != "")
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
	fmt.Fprintf(u.Out, "\nЭ�飺%s���ڵ㣺%d��ȫ��ʹ��ͬһЭ�飻�����˿ڣ�%d/%s\n", p.Name, len(s.Nodes), s.Port, s.Network())
	fmt.Fprintln(u.Out, "ȫ���ڵ��ʹ�� IPv4����ʽ����������̨�������� IPv6��")
	for _, n := range s.Nodes {
		fmt.Fprintf(u.Out, "  %s -> ���� %s:%d -> Ԥ�ڳ��� %s  ����·��=%t\n", s.Endpoint(n), n.LocalIP, s.ListenPort(n), n.PublicIP, n.PolicyRouting)
	}
}
