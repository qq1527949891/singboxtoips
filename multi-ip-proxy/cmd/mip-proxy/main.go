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
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"mipproxy.local/mipproxy/internal/config"
	"mipproxy.local/mipproxy/internal/core"
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
			fmt.Fprintln(os.Stderr, "����", err)
			os.Exit(1)
		}
	}
}

func requireLinux() error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("����/·�ɹ�����֧�� Linux������������Ԥ���Ϳͻ�������")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("����/·�ɹ�����Ҫ root����ʹ�� sudo")
	}
	return nil
}

func ensureCore(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err == nil {
		return core.Version(ctx, network.ExecRunner{}, path)
	} else if !os.IsNotExist(err) {
		return err
	}
	fmt.Printf("���ز�У��ٷ� sing-box %s����\n", model.CoreVersion)
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
		fmt.Printf("mip-proxy 0.1.1 / sing-box %s\n", model.CoreVersion)
		return nil
	case "wizard":
		inventory := fs.String("inventory", "", "����/���� IP ӳ�� CSV")
		out := fs.String("out", "", "������Ԥ��Ŀ¼")
		apply := fs.Bool("apply", false, "���ɺ��𵽱��� Linux")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
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
			fmt.Println("Ԥ�����ڵ����ã�", *out)
		}
		deploy := *apply
		if !deploy && *out == "" {
			answer, e := terminal.Number("1 ���𵽱�����2 ����������", 1, 1, 2)
			if e != nil {
				return e
			}
			deploy = answer == 1
			if !deploy {
				path, e := terminal.Ask("���Ŀ¼", "mip-proxy-output")
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
			if err = system.Apply(ctx, l, r, s); err != nil {
				return err
			}
			installed(terminal, l, s)
		}
		return nil
	case "generate":
		state := fs.String("state", "", "״̬�ļ�")
		out := fs.String("out", "mip-proxy-output", "���Ŀ¼")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *state == "" {
			return fmt.Errorf("��Ҫ --state")
		}
		s, err := model.Load(*state)
		if err != nil {
			return err
		}
		return store.Bundle(s, *out)
	case "apply":
		state := fs.String("state", l.State, "״̬�ļ�")
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
		state := fs.String("state", l.State, "״̬�ļ�")
		out := fs.String("out", l.Exports, "����Ŀ¼")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := system.Export(l, *state, *out); err != nil {
			return err
		}
		fmt.Println("�ڵ��ѵ�����", *out)
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
		state := fs.String("state", l.State, "״̬�ļ�")
		binary := fs.String("core", l.Core, "sing-box ·��")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return system.Doctor(ctx, l, *state, *binary, os.Stdout)
	case "core":
		def := "sing-box"
		if runtime.GOOS == "windows" {
			def += ".exe"
		}
		output := fs.String("out", def, "���ı���·��")
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
			return fmt.Errorf("�÷���routes up|down --state FILE")
		}
		state := fs.String("state", l.State, "״̬�ļ�")
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
			return fmt.Errorf("�÷���ipv6 disable")
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
		server := http.Server{Addr: "127.0.0.1:18080", ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = io.WriteString(w, config.CoverHTML)
		})}
		go func() { <-ctx.Done(); _ = server.Close() }()
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case "demo":
		protocol := fs.String("protocol", "reality", "Э�� ID �� all��������ʾ����")
		out := fs.String("out", "demo", "���Ŀ¼")
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
		return fmt.Errorf("δ֪���� %q������ mip-proxy help", args[0])
	}
}

func installed(t *ui.UI, l system.Layout, s model.State) {
	t.Summary(s)
	fmt.Println("�����������ÿ����ָ����ڵ��ļ���", l.Exports)
	fmt.Println("������ IPv6 �ѽ��ò��־û���ȫ���ڵ��ʹ�� IPv4��")
	if s.Protocol == "wireguard" {
		fmt.Println("�����ư�ȫ�顢����ǽ�� NAT ����ժҪ���еĸ��ڵ� UDP �˿ڡ�")
	} else if s.Protocol == "socks5" {
		fmt.Printf("SOCKS5������ %d/TCP��UDP �м����ö�̬�˿ڣ��谴�ͻ�����Դ���в����� NAT�����ʹ��˵����\n", s.Port)
	} else {
		fmt.Printf("�ⲿ���ӻ���Ҫ�ư�ȫ�顢��������ǽ�� NAT ���� %d/%s������δ��ջ��滻���з���ǽ��\n", s.Port, s.Network())
	}
	fmt.Println("���� mip-proxy doctor �����֤��ת����ʵ�ʹ������ڡ�")
}

func menu(ctx context.Context, t *ui.UI, l system.Layout, r network.Runner) error {
	for {
		fmt.Println("\n�� IP �����ڵ㹤��\n1) ����/���½ڵ㣨����ѡ��һ��Э�飩\n2) �鿴״̬\n3) �����ڵ�\n4) ���ڵ�\n5) �ع��ϴβ���\n0) �˳�")
		choice, err := t.Number("ѡ��", 1, 0, 5)
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
					var action int
					action, err = t.Number("1 Ӧ���������ã�2 ������Ԥ��", 1, 1, 2)
					if err == nil {
						if action == 1 {
							err = ensureCore(ctx, l.Core)
							if err == nil {
								err = system.Apply(ctx, l, r, s)
							}
							if err == nil {
								installed(t, l, s)
							}
						} else {
							var path string
							path, err = t.Ask("Ԥ��Ŀ¼", "mip-proxy-output")
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
			fmt.Fprintln(t.Out, "����ʧ�ܣ�", err)
		}
	}
}

func help() {
	fmt.Println(`mip-proxy��Linux �� IP ��Э������ڵ㹤��

�޲������У����Ľ����˵���Э�鰴����������ο�����

  wizard [--inventory ip-map.csv] [--out preview] [--apply]
  generate --state state.json --out bundle
  apply --state state.json
  export [--state state.json] [--out export]
  status
  doctor [--state state.json] [--core /path/to/sing-box]
  rollback
  ipv6 disable
  core --out /path/to/sing-box
  demo --protocol reality|trojan|hysteria2|vmess|tuic|shadowsocks|wireguard|socks5|all --out demo

������ʵ�ڵ�ͳһʹ��һ��Э�飻����Э��ʱ�����滻��
�Զ�̽��/����֧�� Linux IPv4��systemd��amd64/arm64��
�����������ӡ�Base64 ���Ӽ��ϡ�Mihomo YAML��sing-box JSON��WireGuard ���� .conf��
Ԥ�����޸�ϵͳ����/·�ɡ�TLS Э����Ҫ�ͻ������ε�����֤�顣
�������ο�˳�򲻴����κ������µ�ʵ��������`)
}

func demo(protocol, dir string) error {
	if _, ok := model.Lookup(protocol); !ok {
		return fmt.Errorf("��ЧЭ��")
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
	fmt.Println("����������ʾ�����ĵ����� IP/������ǩ֤�飬����ֱ�Ӳ��𣩣�", dir)
	return nil
}
