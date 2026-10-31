package system

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mipproxy.local/mipproxy/internal/network"
)

const ipv6SysctlConfig = "# Managed by mip-proxy. Restored by deployment rollback.\n-net.ipv6.conf.all.disable_ipv6 = 1\n-net.ipv6.conf.default.disable_ipv6 = 1\n"

func ipv6Base(l Layout) string { return filepath.Join(l.Root, "proc/sys/net/ipv6/conf") }

func ipv6Values(l Layout) (map[string]string, error) {
	entries, err := os.ReadDir(ipv6Base(l))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(ipv6Base(l), entry.Name(), "disable_ipv6"))
		if err != nil {
			return nil, err
		}
		values[entry.Name()] = strings.TrimSpace(string(b))
	}
	return values, validateIPv6Values(values)
}

func validateIPv6Values(values map[string]string) error {
	for name, value := range values {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00\r\n") || (value != "0" && value != "1") {
			return fmt.Errorf("IPv6 �ָ���¼������Ч�ӿڻ򿪹�ֵ")
		}
	}
	return nil
}

func ipv6Enabled(values map[string]string) bool {
	for _, value := range values {
		if value == "0" {
			return true
		}
	}
	return false
}

// DisableIPv6 changes runtime switches. Apply owns the persistent config file.
func DisableIPv6(ctx context.Context, l Layout, r network.Runner) error {
	values, err := ipv6Values(l)
	if err != nil || len(values) == 0 {
		return err
	}
	if !ipv6Enabled(values) {
		return nil
	}
	if _, err = r.Run(ctx, "sysctl", "-w", "net/ipv6/conf/all/disable_ipv6=1", "net/ipv6/conf/default/disable_ipv6=1"); err != nil {
		return fmt.Errorf("���÷����� IPv6��%w", err)
	}
	values, err = ipv6Values(l)
	if err != nil {
		return err
	}
	if ipv6Enabled(values) {
		return fmt.Errorf("�������������� IPv6 �Ľӿڣ�����δ���")
	}
	return nil
}

func restoreIPv6(ctx context.Context, l Layout, r network.Runner, b Backup) error {
	if len(b.IPv6) == 0 {
		return nil
	}
	current, err := ipv6Values(l)
	if err != nil {
		return err
	}
	names := []string{"all", "default"}
	interfaces := []string{}
	for name := range b.IPv6 {
		if name != "all" && name != "default" {
			interfaces = append(interfaces, name)
		}
	}
	sort.Strings(interfaces)
	names = append(names, interfaces...)
	for _, name := range names {
		value, saved := b.IPv6[name]
		if _, exists := current[name]; !saved || !exists || current[name] == value {
			continue
		}
		if _, err = r.Run(ctx, "sysctl", "-w", "net/ipv6/conf/"+name+"/disable_ipv6="+value); err != nil {
			return fmt.Errorf("�ָ� IPv6 ���أ�%w", err)
		}
		current, err = ipv6Values(l)
		if err != nil {
			return err
		}
	}
	inputRunner, ok := r.(interface {
		RunInput(context.Context, []byte, ...string) ([]byte, error)
	})
	for _, saved := range []struct {
		data []byte
		kind string
	}{{b.IPv6Addresses, "address"}, {b.IPv6Routes, "route"}} {
		if len(saved.data) == 0 {
			continue
		}
		if !ok {
			return fmt.Errorf("��ǰִ������֧�ָֻ� IPv6 ��ַ/·��")
		}
		if _, err = inputRunner.RunInput(ctx, saved.data, "ip", "-6", saved.kind, "restore"); err != nil {
			return fmt.Errorf("�ָ� IPv6 %s��%w", saved.kind, err)
		}
	}
	return nil
}
