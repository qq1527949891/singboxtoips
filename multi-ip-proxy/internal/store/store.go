package store

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mipproxy.local/mipproxy/internal/config"
	"mipproxy.local/mipproxy/internal/export"
	"mipproxy.local/mipproxy/internal/model"
)

func Atomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mip-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}

func Export(s model.State, dir string) error {
	files, err := export.Files(s)
	if err != nil {
		return err
	}
	if err = OwnDirectory(dir); err != nil {
		return err
	}
	previous, err := ExportNames(dir)
	if err != nil {
		return err
	}
	for name, data := range files {
		if err = Atomic(filepath.Join(dir, name), data, 0600); err != nil {
			return err
		}
	}
	for _, name := range previous {
		if _, keep := files[name]; !keep {
			if err = os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	names := []string{}
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return Atomic(filepath.Join(dir, ".export-manifest.json"), model.JSON(names), 0600)
}

func IsExportName(name string) bool {
	if filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, fixed := range []string{"links.txt", "subscription.txt", "mihomo.yaml", "mihomo-provider.yaml", "sing-box.json", "nodes.csv", "ʹ��˵��.txt", ".mip-proxy", ".export-manifest.json"} {
		if name == fixed {
			return true
		}
	}
	if strings.HasSuffix(name, "-wireguard.conf") {
		ip, e := netip.ParseAddr(strings.TrimSuffix(name, "-wireguard.conf"))
		return e == nil && ip.Is4()
	}
	return false
}

func ExportNames(dir string) ([]string, error) {
	b, e := os.ReadFile(filepath.Join(dir, ".export-manifest.json"))
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var names []string
	if e = json.Unmarshal(b, &names); e != nil {
		return nil, e
	}
	for _, name := range names {
		if !IsExportName(name) {
			return nil, fmt.Errorf("�����嵥�������ܹ������ļ���")
		}
	}
	return names, nil
}

func Bundle(s model.State, dir string) error {
	c, err := config.Server(s)
	if err != nil {
		return err
	}
	if err = OwnDirectory(dir); err != nil {
		return err
	}
	if err = Atomic(filepath.Join(dir, "server.json"), model.JSON(c), 0600); err != nil {
		return err
	}
	if err = Atomic(filepath.Join(dir, "state.json"), model.JSON(s), 0600); err != nil {
		return err
	}
	return Export(s, filepath.Join(dir, "export"))
}

func OwnDirectory(dir string) error {
	if fi, err := os.Lstat(dir); err == nil && (!fi.IsDir() || fi.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("���·����������ͨĿ¼")
	}
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) > 0 {
		if _, err = os.Stat(filepath.Join(dir, ".mip-proxy")); err != nil {
			return fmt.Errorf("Ŀ¼ %s ���������ļ�����ѡ���Ŀ¼", dir)
		}
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err = os.Chmod(dir, 0700); err != nil {
		return err
	}
	return Atomic(filepath.Join(dir, ".mip-proxy"), []byte("mip-proxy owned directory\n"), 0600)
}
