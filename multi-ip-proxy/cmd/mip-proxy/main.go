package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"mipproxy.local/mipproxy/internal/core"
	"mipproxy.local/mipproxy/internal/export"
	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/network"
	"mipproxy.local/mipproxy/internal/store"
	"mipproxy.local/mipproxy/internal/system"
	"mipproxy.local/mipproxy/internal/ui"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() { <-ctx.Done(); cancel() }()
	if err := run(ctx, os.Args[1:]); err != nil {
		if err != io.EOF {
			fmt.Fprintln(os.Stderr, "错误：", err)
			os.Exit(1)
		}
	}
}

func requireLinux() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("部署/路由管理仅支持 Linux；本机可生成预览和客户端配置")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("部署/路由管理需要 root，请使用 sudo")
	}
	return nil
}

func ensureCore(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err == nil {
		return core.Version(ctx, network.ExecRunner{}, path)
	} else if !os.IsNotExist(err) {
		return err
	}
	fmt.Printf("下载并校验官方 sing-box %s……\n", model.CoreVersion)
	return core.Install(ctx, path)
}

func run(ctx context.Context, args []string) error {
	l := system.Paths("/")
	r := network.ExecRunner{}
	terminal := ui.New(os.Stdin, os.Stdout)
	terminal.Context = ctx
	if len(args) == 0 {
		return menu(ctx, terminal, l, r)
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	switch args[0] {
	case "help", "--help", "-h":
		help()
		return nil
	case "version", "--version":
		fmt.Printf("mip-proxy 0.1.6 / sing-box %s\n", model.CoreVersion)
		return nil
	case "wizard":
		inventory := fs.String("inventory", "", "本机/公网 IP 映射 CSV")
		out := fs.String("out", "", "仅生成预览目录")
		apply := fs.Bool("apply", false, "生成后部署到本机 Linux")
		advanced := fs.Bool("advanced", false, "自定义 IP、端口、策略路由与证书")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		terminal.Advanced = *advanced
		if *apply {
			if err := requireLinux(); err != nil {
				return err
			}
		}
		var old *model.State
		if s, e := model.Load(l.State); e == nil {
			old = &s
		}
		s, err := terminal.Wizard(ctx, r, *inventory, old)
		if err != nil {
			return err
		}
		if *out != "" {
			if err = store.Bundle(s, *out); err != nil {
				return err
			}
			fmt.Println("预览及节点配置：", *out)
		}
		deploy := *apply || (*out == "" && !terminal.Advanced)
		if !deploy && *out == "" {
			answer, e := terminal.Number("1 部署到本机；2 仅生成配置", 1, 1, 2)
			if e != nil {
				return e
			}
			deploy = answer == 1
			if !deploy {
				path, e := terminal.Ask("输出目录", "mip-proxy-output")
				if e != nil {
					return e
				}
				return store.Bundle(s, path)
			}
		}
		if deploy {
			if err = requireLinux(); err != nil {
				return err
			}
			if err = ensureCore(ctx, l.Core); err != nil {
				return err
			}
			if s.AutomaticCertificate() {
				fmt.Fprintln(terminal.Out, "正在部署并等待 IP 证书签发，最多 3 分钟；验证 IP 的 TCP 80 需允许公网访问。")
			}
			if err = system.Apply(ctx, l, r, s); err != nil {
				return err
			}
			installed(terminal, l, s)
		}
		return nil
	case "generate":
		state := fs.String("state", "", "状态文件")
		out := fs.String("out", "mip-proxy-output", "输出目录")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *state == "" {
			return fmt.Errorf("需要 --state")
		}
		s, err := model.Load(*state)
		if err != nil {
			return err
		}
		return store.Bundle(s, *out)
	case "apply":
		state := fs.String("state", l.State, "状态文件")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := requireLinux(); err != nil {
			return err
		}
		if err := ensureCore(ctx, l.Core); err != nil {
			return err
		}
		if err := system.ApplyFile(ctx, l, r, *state); err != nil {
			return err
		}
		s, err := model.Load(l.State)
		if err != nil {
			return err
		}
		installed(terminal, l, s)
		return nil
	case "export":
		state := fs.String("state", l.State, "状态文件")
		out := fs.String("out", l.Exports, "导出目录")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := system.Export(l, *state, *out); err != nil {
			return err
		}
		fmt.Println("节点已导出：", *out)
		return nil
	case "status":
		s, err := model.Load(l.State)
		if err != nil {
			return err
		}
		terminal.Summary(s)
		b, e := r.Run(ctx, "systemctl", "status", system.ProxyService, "--no-pager")
		fmt.Print(string(b))
		return e
	case "doctor":
		state := fs.String("state", l.State, "状态文件")
		binary := fs.String("core", l.Core, "sing-box 路径")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return system.Doctor(ctx, l, *state, *binary, os.Stdout)
	case "core":
		def := "sing-box"
		if runtime.GOOS == "windows" {
			def += ".exe"
		}
		output := fs.String("out", def, "核心保存路径")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path, err := filepath.Abs(*output)
		if err != nil {
			return err
		}
		return ensureCore(ctx, path)
	case "routes":
		if len(args) < 2 || (args[1] != "up" && args[1] != "down") {
			return fmt.Errorf("用法：routes up|down --state FILE")
		}
		state := fs.String("state", l.State, "状态文件")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if err := requireLinux(); err != nil {
			return err
		}
		s, err := model.Load(*state)
		if err != nil {
			return err
		}
		if args[1] == "up" {
			return network.Up(ctx, r, s)
		}
		return network.Down(ctx, r, s)
	case "ipv6":
		if len(args) != 2 || args[1] != "disable" {
			return fmt.Errorf("用法：ipv6 disable")
		}
		if err := requireLinux(); err != nil {
			return err
		}
		return system.DisableIPv6(ctx, l, r)
	case "rollback":
		if err := requireLinux(); err != nil {
			return err
		}
		return system.Rollback(ctx, l, r)
	case "cover":
		return system.ServeCover(ctx)
	case "subscriptions":
		if len(args) == 1 || len(args) == 2 && args[1] == "show" {
			return system.ShowSubscriptions(l, os.Stdout)
		}
		if len(args) != 2 || (args[1] != "serve" && args[1] != "check") {
			return fmt.Errorf("用法：subscriptions [show|check|serve]")
		}
		if err := requireLinux(); err != nil {
			return err
		}
		if args[1] == "check" {
			return system.CheckSubscriptions(ctx, l)
		}
		return system.ServeSubscriptions(ctx, l)
	case "demo":
		protocol := fs.String("protocol", "reality", "协议 ID 或 all（仅离线示例）")
		out := fs.String("out", "demo", "输出目录")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *protocol == "all" {
			for _, p := range model.Protocols {
				if err := demo(p.ID, filepath.Join(*out, p.ID)); err != nil {
					return err
				}
			}
			return nil
		}
		return demo(*protocol, *out)
	default:
		return fmt.Errorf("未知命令 %q；运行 mip-proxy help", args[0])
	}
}

func installed(t *ui.UI, l system.Layout, s model.State) {
	t.Summary(s)
	fmt.Fprintln(t.Out, "已启动并设置开机恢复。节点文件：", l.Exports)
	fmt.Fprintln(t.Out, "服务器 IPv6 已禁用并持久化，全部节点仅使用 IPv4。")
	if s.Protocol == "wireguard" {
		fmt.Fprintln(t.Out, "请在云安全组、防火墙和 NAT 放行摘要所列的各节点 UDP 端口。")
	} else if s.Protocol == "socks5" {
		fmt.Fprintf(t.Out, "SOCKS5：放行 %d/TCP；UDP 中继另用动态端口，需按客户端来源放行并配置 NAT，详见使用说明。\n", s.Port)
	} else {
		fmt.Fprintf(t.Out, "外部连接还需要云安全组、主机防火墙和 NAT 放行 %d/%s。工具未清空或替换现有防火墙。\n", s.Port, s.Network())
	}
	fmt.Fprintln(t.Out, "运行 mip-proxy doctor 检查认证、转发和实际公网出口。")
	nodes, err := export.Console(s)
	if err != nil {
		fmt.Fprintln(t.Out, "节点打印失败：", err)
		return
	}
	fmt.Fprintln(t.Out, "\n代理节点信息：")
	fmt.Fprint(t.Out, nodes)
	if links := export.Subscriptions(s); len(links) != 0 {
		fmt.Fprintf(t.Out, "\nHTTPS 订阅地址（TCP %d 需允许公网访问）：\n", s.SubscriptionPort)
		for _, link := range links {
			fmt.Fprintf(t.Out, "%s：\n%s\n", link.Name, link.URL)
		}
	}
}

func menu(ctx context.Context, t *ui.UI, l system.Layout, r network.Runner) error {
	for {
		fmt.Println("\n多 IP 代理节点工具\n1) 创建/更新节点（交互选择一种协议）\n2) 查看状态\n3) 导出节点\n4) 检测节点\n5) 回滚上次部署\n0) 退出")
		choice, err := t.Number("选择", 1, 0, 5)
		if err != nil {
			return err
		}
		if choice == 0 {
			return nil
		}
		switch choice {
		case 1:
			if err = requireLinux(); err == nil {
				var old *model.State
				if loaded, e := model.Load(l.State); e == nil {
					old = &loaded
				}
				var s model.State
				s, err = t.Wizard(ctx, r, "", old)
				if err == nil {
					action := 1
					if t.Advanced {
						action, err = t.Number("1 应用以上配置；2 仅导出预览", 1, 1, 2)
					}
					if err == nil {
						if action == 1 {
							err = ensureCore(ctx, l.Core)
							if err == nil {
								if s.AutomaticCertificate() {
									fmt.Fprintln(t.Out, "正在部署并等待 IP 证书签发，最多 3 分钟；验证 IP 的 TCP 80 需允许公网访问。")
								}
								err = system.Apply(ctx, l, r, s)
							}
							if err == nil {
								installed(t, l, s)
								return nil
							}
						} else {
							var path string
							path, err = t.Ask("预览目录", "mip-proxy-output")
							if err == nil {
								err = store.Bundle(s, path)
							}
						}
					}
				}
			}
		case 2:
			err = run(ctx, []string{"status"})
		case 3:
			err = run(ctx, []string{"export"})
		case 4:
			err = run(ctx, []string{"doctor"})
		case 5:
			err = run(ctx, []string{"rollback"})
		}
		if err != nil {
			if err == io.EOF || errors.Is(err, context.Canceled) {
				return err
			}
			fmt.Fprintln(t.Out, "操作失败：", err)
		}
	}
}

func help() {
	fmt.Println(`mip-proxy：Linux 多 IP 单协议代理节点工具

无参数运行：中文交互菜单，协议按抗封锁需求参考排序。

  wizard [--advanced] [--inventory ip-map.csv] [--out preview] [--apply]
  generate --state state.json --out bundle
  apply --state state.json
  export [--state state.json] [--out export]
  status
  doctor [--state state.json] [--core /path/to/sing-box]
  rollback
  ipv6 disable
  subscriptions [show|check|serve]
  core --out /path/to/sing-box
  demo --protocol reality|trojan|hysteria2|vmess|tuic|shadowsocks|wireguard|socks5|all --out demo

所有真实节点统一使用一种协议；更换协议时整体替换。
默认只选择协议，其他可识别设置自动采用推荐值；不带 --out 时默认部署。--advanced 可自定义。
自动探测/部署支持 Linux IPv4、systemd，amd64/arm64。
导出分享链接、Base64 链接集合、Mihomo YAML、sing-box JSON；WireGuard 导出 .conf。
预览不修改系统服务/路由。TLS 协议支持自动申请免费 IP 证书及自动续期，也可使用已有证书。
抗封锁参考顺序不代表任何网络下的实测排名。`)
}

func demo(protocol, dir string) error {
	if _, ok := model.Lookup(protocol); !ok {
		return fmt.Errorf("无效协议")
	}
	if err := store.OwnDirectory(dir); err != nil {
		return err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	s := model.State{Schema: 1, Protocol: protocol, Port: 443, CoreVersion: model.CoreVersion, ServerName: "example.com", RealityTarget: "www.microsoft.com", RealityPort: 443}
	for i, ip := range []string{"192.0.2.10", "192.0.2.11"} {
		n, e := model.NewNode(i+1, ip, ip)
		if e != nil {
			return e
		}
		if protocol == "wireguard" {
			n.PolicyRouting = true
			n.Interface = "eth0"
			n.Gateway = "192.0.2.1"
			n.Prefix = "192.0.2.0/24"
		}
		s.Nodes = append(s.Nodes, n)
	}
	if s.NeedsTLS() {
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			return e
		}
		serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if e != nil {
			return e
		}
		template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "example.com"}, DNSNames: []string{"example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		der, e := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
		if e != nil {
			return e
		}
		pk, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			return e
		}
		s.Certificate = filepath.Join(abs, "demo-cert.pem")
		s.Key = filepath.Join(abs, "demo-key.pem")
		if e = store.Atomic(s.Certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
			return e
		}
		if e = store.Atomic(s.Key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}), 0600); e != nil {
			return e
		}
	}
	if err = store.Bundle(s, dir); err != nil {
		return err
	}
	fmt.Println("已生成离线示例（文档保留 IP/短期自签证书，不能直接部署）：", dir)
	return nil
}
