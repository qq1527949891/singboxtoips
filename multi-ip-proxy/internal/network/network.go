package network

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"mipproxy.local/mipproxy/internal/model"
)

type Runner interface {
	Run(context.Context, ...string) ([]byte, error)
}
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	return runCommand(ctx, nil, args...)
}

func (ExecRunner) RunInput(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	return runCommand(ctx, input, args...)
}

func runCommand(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("缺少命令")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	if input != nil {
		// ip route restore needs a seekable stdin (ftell), not a pipe.
		f, err := os.CreateTemp("", ".mip-command-stdin-")
		if err != nil {
			return nil, err
		}
		defer os.Remove(f.Name())
		defer f.Close()
		if _, err = f.Write(input); err != nil {
			return nil, err
		}
		if _, err = f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		cmd.Stdin = f
	}
	b, err := cmd.CombinedOutput()
	if err != nil {
		return b, fmt.Errorf("%s：%w：%s", args[0], err, strings.TrimSpace(string(b)))
	}
	return b, nil
}

type Candidate struct {
	LocalIP, PublicIP, Interface, Gateway, Prefix string
	AmbiguousGateway                              bool
}
type AddrInfo struct {
	Family    string `json:"family"`
	Local     string `json:"local"`
	PrefixLen int    `json:"prefixlen"`
	Scope     string `json:"scope"`
}
type Interface struct {
	Name      string     `json:"ifname"`
	Flags     []string   `json:"flags"`
	Addresses []AddrInfo `json:"addr_info"`
}
type Route struct {
	Dst     string `json:"dst"`
	Gateway string `json:"gateway"`
	Dev     string `json:"dev"`
	Metric  int    `json:"metric"`
	Table   any    `json:"table"`
}

func Discover(ctx context.Context, r Runner) ([]Candidate, error) {
	a, err := r.Run(ctx, "ip", "-j", "-4", "address", "show")
	if err != nil {
		return nil, err
	}
	b, err := r.Run(ctx, "ip", "-j", "-4", "route", "show", "table", "all")
	if err != nil {
		return nil, err
	}
	return ParseInventory(a, b)
}

func ParseInventory(addrJSON, routeJSON []byte) ([]Candidate, error) {
	var interfaces []Interface
	var routes []Route
	if err := json.Unmarshal(addrJSON, &interfaces); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(routeJSON, &routes); err != nil {
		return nil, err
	}
	gateways := map[string][]string{}
	for _, r := range routes {
		// Do not re-import our own policy routes as gateway hints on the next run.
		table := intValue(r.Table)
		if table >= 42001 && table <= 46095 {
			continue
		}
		// Never infer an interface from an address label.
		if r.Dst != "default" || r.Dev == "" || r.Gateway == "" {
			continue
		}
		seen := false
		for _, g := range gateways[r.Dev] {
			if g == r.Gateway {
				seen = true
			}
		}
		if !seen {
			gateways[r.Dev] = append(gateways[r.Dev], r.Gateway)
		}
	}
	result := []Candidate{}
	for _, iface := range interfaces {
		if len(gateways[iface.Name]) == 0 {
			continue
		}
		up := false
		for _, flag := range iface.Flags {
			if flag == "UP" {
				up = true
			}
		}
		if !up {
			continue
		}
		for _, a := range iface.Addresses {
			ip, err := netip.ParseAddr(a.Local)
			if err != nil || !ip.Is4() || !ip.IsGlobalUnicast() || a.Scope != "global" {
				continue
			}
			prefix := netip.PrefixFrom(ip, a.PrefixLen).Masked().String()
			c := Candidate{LocalIP: a.Local, Interface: iface.Name, Prefix: prefix, AmbiguousGateway: len(gateways[iface.Name]) != 1}
			if !c.AmbiguousGateway {
				c.Gateway = gateways[iface.Name][0]
			}
			if !ip.IsPrivate() {
				c.PublicIP = a.Local
			}
			result = append(result, c)
		}
	}
	return result, nil
}

// ProbePublicIP discovers only the observed egress; it does not prove inbound NAT mapping.
func ProbePublicIP(ctx context.Context, source string) (string, error) {
	d := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(source)}, Timeout: 4 * time.Second}
	t := &http.Transport{DialContext: d.DialContext, Proxy: nil, DisableKeepAlives: true}
	defer t.CloseIdleConnections()
	client := http.Client{Transport: t, Timeout: 7 * time.Second}
	var last error
	for _, endpoint := range []string{"https://api.ipify.org", "https://checkip.amazonaws.com"} {
		req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			last = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 128))
		resp.Body.Close()
		ip, parseErr := netip.ParseAddr(strings.TrimSpace(string(body)))
		if readErr == nil && resp.StatusCode == 200 && parseErr == nil && ip.Is4() && ip.IsGlobalUnicast() && !ip.IsPrivate() {
			return ip.String(), nil
		}
		last = fmt.Errorf("公网探测返回无效结果")
	}
	return "", last
}

// ReadCSV deliberately retains one row per local address instead of indexing by public IP.
func ReadCSV(r io.Reader) ([]Candidate, error) {
	rows, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("CSV 至少包含表头和一行地址")
	}
	columns := map[string]int{}
	for i, k := range rows[0] {
		columns[strings.TrimPrefix(strings.TrimSpace(k), "\ufeff")] = i
	}
	for _, k := range []string{"local_ip", "public_ip"} {
		if _, ok := columns[k]; !ok {
			return nil, fmt.Errorf("CSV 缺少 %s", k)
		}
	}
	result := []Candidate{}
	for line, row := range rows[1:] {
		get := func(k string) string {
			if i, ok := columns[k]; ok && i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
		c := Candidate{LocalIP: get("local_ip"), PublicIP: get("public_ip"), Interface: get("interface"), Gateway: get("gateway"), Prefix: get("prefix")}
		for _, s := range []string{c.LocalIP, c.PublicIP} {
			a, e := netip.ParseAddr(s)
			if e != nil || !a.Is4() || !a.IsGlobalUnicast() {
				return nil, fmt.Errorf("CSV 第 %d 行地址无效：%q", line+2, s)
			}
		}
		result = append(result, c)
	}
	return result, nil
}

func VerifyLocal(ctx context.Context, r Runner, s model.State) error {
	b, err := r.Run(ctx, "ip", "-j", "-4", "address", "show")
	if err != nil {
		return err
	}
	var ifaces []Interface
	if err = json.Unmarshal(b, &ifaces); err != nil {
		return err
	}
	for _, n := range s.Nodes {
		found := false
		for _, iface := range ifaces {
			for _, a := range iface.Addresses {
				if a.Local == n.LocalIP && (n.Interface == "" || n.Interface == iface.Name) {
					found = true
				}
			}
		}
		if !found {
			return fmt.Errorf("本机没有地址 %s（网卡 %s）；请先在云平台和系统中正确配置地址", n.LocalIP, n.Interface)
		}
	}
	return nil
}

func WaitPort(ctx context.Context, address string) error {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			c.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("等待本地代理端口 %s 超时", address)
}

func intValue(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		i, _ := strconv.ParseInt(strings.Split(t, "/")[0], 0, 64)
		return int(i)
	}
	return 0
}
