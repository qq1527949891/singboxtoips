package system

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mipproxy.local/mipproxy/internal/config"
	"mipproxy.local/mipproxy/internal/core"
	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/network"
	"mipproxy.local/mipproxy/internal/store"
)

const ProxyService = "mip-proxy.service"
const RoutesService = "mip-proxy-routes.service"
const CoverService = "mip-proxy-cover.service"

var Services = []string{ProxyService, CoverService, RoutesService}

type Layout struct{ Root, Data, State, Config, App, Core, Units, Exports, Pending, Latest, Lock, IPv6Config string }

func Paths(root string) Layout {
	root = filepath.Clean(root)
	l := Layout{Root: root, Data: filepath.Join(root, "var/lib/mip-proxy"), Units: filepath.Join(root, "etc/systemd/system"), App: filepath.Join(root, "usr/local/bin/mip-proxy"), Core: filepath.Join(root, "usr/local/lib/mip-proxy/sing-box"), Config: filepath.Join(root, "etc/mip-proxy/server.json")}
	l.State = filepath.Join(l.Data, "state.json")
	l.Exports = filepath.Join(l.Data, "export")
	l.Pending = filepath.Join(l.Data, "pending.json")
	l.Latest = filepath.Join(l.Data, "latest-backup.json")
	l.Lock = filepath.Join(l.Data, "operation.lock")
	l.IPv6Config = filepath.Join(root, "etc/sysctl.d/99-mip-proxy-ipv6.conf")
	return l
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(s) + `"`
}
func Units(l Layout, s model.State) map[string][]byte {
	extra := ""
	if s.Protocol == "trojan" {
		extra = "Requires=" + CoverService + "\nAfter=" + CoverService + "\n"
	}
	proxy := fmt.Sprintf("[Unit]\nDescription=Multi IP proxy nodes\nWants=network-online.target\nAfter=network-online.target %s\nRequires=%s\n%s\n[Service]\nType=simple\nExecStartPre=%s check -c %s\nExecStart=%s run -c %s\nRestart=on-failure\nRestartSec=3\nLimitNOFILE=65536\nUMask=0077\nNoNewPrivileges=yes\nPrivateTmp=yes\nProtectHome=yes\nProtectSystem=strict\nCapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE CAP_NET_RAW\n\n[Install]\nWantedBy=multi-user.target\n", RoutesService, RoutesService, extra, quote(l.Core), quote(l.Config), quote(l.Core), quote(l.Config))
	proxy = strings.Replace(proxy, "ExecStartPre=", "ExecStartPre="+quote(l.App)+" ipv6 disable\nExecStartPre=", 1)
	routes := fmt.Sprintf("[Unit]\nDescription=Routes for multi IP proxy nodes\nWants=network-online.target\nAfter=network-online.target\nBefore=%s\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=%s routes up --state %s\nExecStop=%s routes down --state %s\nTimeoutStartSec=90\nTimeoutStopSec=90\nUMask=0077\n\n[Install]\nWantedBy=multi-user.target\n", ProxyService, quote(l.App), quote(l.State), quote(l.App), quote(l.State))
	cover := fmt.Sprintf("[Unit]\nDescription=HTTP fallback for Trojan listener\nAfter=network.target\n\n[Service]\nType=simple\nExecStart=%s cover\nRestart=on-failure\nUMask=0077\nNoNewPrivileges=yes\nPrivateTmp=yes\nProtectHome=yes\nProtectSystem=strict\nCapabilityBoundingSet=\n\n[Install]\nWantedBy=multi-user.target\n", quote(l.App))
	return map[string][]byte{ProxyService: []byte(proxy), RoutesService: []byte(routes), CoverService: []byte(cover)}
}

type SavedFile struct {
	Path   string `json:"path"`
	Data   []byte `json:"data,omitempty"`
	Exists bool   `json:"exists"`
	Mode   uint32 `json:"mode"`
}
type Backup struct {
	Files         []SavedFile       `json:"files"`
	Active        map[string]bool   `json:"active"`
	Enabled       map[string]bool   `json:"enabled"`
	Previous      *model.State      `json:"previous,omitempty"`
	Attempted     *model.State      `json:"attempted,omitempty"`
	IPv6          map[string]string `json:"ipv6,omitempty"`
	IPv6Addresses []byte            `json:"ipv6_addresses,omitempty"`
	IPv6Routes    []byte            `json:"ipv6_routes,omitempty"`
}

func managedPaths(l Layout, s model.State) ([]string, error) {
	paths := []string{l.State, l.Config, l.App, l.Latest, l.IPv6Config}
	for _, name := range Services {
		paths = append(paths, filepath.Join(l.Units, name))
	}
	f, err := storeFiles(s)
	if err != nil {
		return nil, err
	}
	for name := range f {
		paths = append(paths, filepath.Join(l.Exports, name))
	}
	names, err := store.ExportNames(l.Exports)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if _, exists := f[name]; !exists {
			paths = append(paths, filepath.Join(l.Exports, name))
		}
	}
	paths = append(paths, filepath.Join(l.Exports, ".mip-proxy"))
	paths = append(paths, filepath.Join(l.Exports, ".export-manifest.json"))
	return paths, nil
}

// Kept in one place so backups include all credential-bearing export files.
func storeFiles(s model.State) (map[string][]byte, error) { return exportFiles(s) }

func snapshot(ctx context.Context, l Layout, r network.Runner, s model.State, old *model.State) (Backup, error) {
	b := Backup{Active: map[string]bool{}, Enabled: map[string]bool{}, Previous: old, Attempted: &s}
	var err error
	b.IPv6, err = ipv6Values(l)
	if err != nil {
		return b, err
	}
	if ipv6Enabled(b.IPv6) {
		b.IPv6Addresses, err = r.Run(ctx, "ip", "-6", "address", "save")
		if err != nil {
			return b, err
		}
		b.IPv6Routes, err = r.Run(ctx, "ip", "-6", "route", "save", "table", "all")
		if err != nil {
			return b, err
		}
	}
	paths, err := managedPaths(l, s)
	if err != nil {
		return b, err
	}
	for _, path := range paths {
		fi, err := os.Lstat(path)
		if os.IsNotExist(err) {
			b.Files = append(b.Files, SavedFile{Path: path})
			continue
		}
		if err != nil {
			return b, err
		}
		if !fi.Mode().IsRegular() {
			return b, fmt.Errorf("受管理路径不是普通文件：%s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return b, err
		}
		b.Files = append(b.Files, SavedFile{Path: path, Data: data, Exists: true, Mode: uint32(fi.Mode().Perm())})
	}
	for _, name := range Services {
		_, e := r.Run(ctx, "systemctl", "is-active", "--quiet", name)
		b.Active[name] = e == nil
		_, e = r.Run(ctx, "systemctl", "is-enabled", "--quiet", name)
		b.Enabled[name] = e == nil
	}
	return b, nil
}

func writeBackup(l Layout, b Backup) (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	path := filepath.Join(l.Data, "backups", time.Now().UTC().Format("20060102T150405.000000000")+"-"+hex.EncodeToString(buf)+".json")
	if err := store.Atomic(path, model.JSON(b), 0600); err != nil {
		return "", err
	}
	return path, nil
}

func readBackup(l Layout, path string) (Backup, error) {
	var b Backup
	abs, _ := filepath.Abs(path)
	base, _ := filepath.Abs(filepath.Join(l.Data, "backups"))
	rel, err := filepath.Rel(base, abs)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return b, fmt.Errorf("备份路径不属于此工具")
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return b, err
	}
	if err = json.Unmarshal(data, &b); err != nil {
		return b, err
	}
	// Recovery must not depend on the current state or export manifest being valid.
	allowed := []string{l.State, l.Config, l.App, l.Latest, l.IPv6Config}
	for _, name := range Services {
		allowed = append(allowed, filepath.Join(l.Units, name))
	}
	allowedMap := map[string]bool{}
	for _, p := range allowed {
		allowedMap[filepath.Clean(p)] = true
	}
	for _, f := range b.Files {
		relExport, relErr := filepath.Rel(l.Exports, f.Path)
		if !allowedMap[filepath.Clean(f.Path)] && !(relErr == nil && store.IsExportName(relExport)) {
			return b, fmt.Errorf("备份包含不受管理的路径")
		}
	}
	if b.Previous != nil {
		if err = b.Previous.Validate(); err != nil {
			return b, err
		}
	}
	if b.Attempted != nil {
		if err = b.Attempted.Validate(); err != nil {
			return b, err
		}
	}
	if err = validateIPv6Values(b.IPv6); err != nil {
		return b, err
	}
	return b, nil
}

func missingUnit(ctx context.Context, r network.Runner, name string) bool {
	b, err := r.Run(ctx, "systemctl", "show", "--property=LoadState", "--value", name)
	return err == nil && strings.TrimSpace(string(b)) == "not-found"
}

func restore(ctx context.Context, l Layout, r network.Runner, b Backup, current *model.State) error {
	var failures []string
	for _, name := range Services {
		if _, err := r.Run(ctx, "systemctl", "stop", name); err != nil && !missingUnit(ctx, r, name) {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("停止服务失败，尚未恢复文件：%s", strings.Join(failures, "；"))
	}
	for _, name := range Services {
		if _, err := r.Run(ctx, "systemctl", "disable", name); err != nil && !missingUnit(ctx, r, name) {
			failures = append(failures, err.Error())
		}
	}
	if b.Attempted != nil {
		// Persisted before mutation, so a failed/partial restore cannot lose the
		// new route inventory after state.json has already been restored.
		current = b.Attempted
	}
	if current != nil {
		if err := network.Down(ctx, r, *current); err != nil {
			failures = append(failures, err.Error())
		}
	}
	for _, f := range b.Files {
		var err error
		if f.Exists {
			err = store.Atomic(f.Path, f.Data, os.FileMode(f.Mode))
		} else {
			err = os.Remove(f.Path)
			if os.IsNotExist(err) {
				err = nil
			}
		}
		if err != nil {
			failures = append(failures, err.Error())
		}
	}
	if err := restoreIPv6(ctx, l, r, b); err != nil {
		failures = append(failures, err.Error())
	}
	if _, err := r.Run(ctx, "systemctl", "daemon-reload"); err != nil {
		failures = append(failures, err.Error())
	}
	if b.Previous != nil && (b.Active[RoutesService] || b.Active[ProxyService]) {
		if err := network.Up(ctx, r, *b.Previous); err != nil {
			failures = append(failures, err.Error())
		}
	}
	for _, name := range []string{RoutesService, CoverService, ProxyService} {
		if b.Enabled[name] {
			if _, err := r.Run(ctx, "systemctl", "enable", name); err != nil {
				failures = append(failures, err.Error())
			}
		}
		if b.Active[name] {
			if _, err := r.Run(ctx, "systemctl", "start", name); err != nil {
				failures = append(failures, err.Error())
			}
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("恢复过程存在错误：%s", strings.Join(failures, "；"))
	}
	return nil
}

func Recover(ctx context.Context, l Layout, r network.Runner) error {
	data, err := os.ReadFile(l.Pending)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var path string
	if err = json.Unmarshal(data, &path); err != nil {
		return err
	}
	b, err := readBackup(l, path)
	if err != nil {
		return err
	}
	var current *model.State
	if s, e := model.Load(l.State); e == nil {
		current = &s
	}
	if err = restore(ctx, l, r, b, current); err != nil {
		return err
	}
	return os.Remove(l.Pending)
}

func Apply(ctx context.Context, l Layout, r network.Runner, s model.State) (err error) {
	unlock, err := lock(l.Lock)
	if err != nil {
		return err
	}
	defer unlock()
	if err = Recover(ctx, l, r); err != nil {
		return fmt.Errorf("恢复上次中断的部署：%w", err)
	}
	return apply(ctx, l, r, s)
}

// ApplyFile recovers under the same lock before loading managed state. A
// partially written or missing state must not prevent transaction recovery.
func ApplyFile(ctx context.Context, l Layout, r network.Runner, state string) error {
	unlock, err := lock(l.Lock)
	if err != nil {
		return err
	}
	defer unlock()
	if err = Recover(ctx, l, r); err != nil {
		return fmt.Errorf("恢复上次中断的部署：%w", err)
	}
	s, err := model.Load(state)
	if err != nil {
		return err
	}
	return apply(ctx, l, r, s)
}

func apply(ctx context.Context, l Layout, r network.Runner, s model.State) (err error) {
	if err = s.Validate(); err != nil {
		return err
	}
	if _, err = r.Run(ctx, "systemctl", "show", "--property=Version", "--value"); err != nil {
		return fmt.Errorf("需要可用的 systemd 服务管理器：%w", err)
	}
	if err = network.VerifyLocal(ctx, r, s); err != nil {
		return err
	}
	var old *model.State
	if loaded, e := model.Load(l.State); e == nil {
		old = &loaded
	} else if !os.IsNotExist(e) {
		return fmt.Errorf("旧状态无效，未覆盖：%w", e)
	}
	if old == nil {
		for _, path := range []string{l.Config, l.IPv6Config, filepath.Join(l.Units, ProxyService), filepath.Join(l.Units, RoutesService), filepath.Join(l.Units, CoverService)} {
			if _, e := os.Lstat(path); e == nil {
				return fmt.Errorf("发现同名配置但没有本工具状态，未覆盖：%s", path)
			} else if !os.IsNotExist(e) {
				return e
			}
		}
	}
	if err = network.CheckConflicts(ctx, r, s, old); err != nil {
		return err
	}
	if old != nil {
		if err = network.CheckConflicts(ctx, r, *old, old); err != nil {
			return err
		}
	}
	if err = core.Version(ctx, r, l.Core); err != nil {
		return err
	}
	candidate, err := config.Server(s)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(l.Data, "candidate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	path := filepath.Join(tmp, "server.json")
	if err = store.Atomic(path, model.JSON(candidate), 0600); err != nil {
		return err
	}
	if err = core.Check(ctx, r, l.Core, path); err != nil {
		return fmt.Errorf("核心配置检查失败，旧服务未修改：%w", err)
	}
	// Refuse unrelated files in the export directory before changing services.
	if err = store.OwnDirectory(l.Exports); err != nil {
		return err
	}
	b, err := snapshot(ctx, l, r, s, old)
	if err != nil {
		return err
	}
	backupPath, err := writeBackup(l, b)
	if err != nil {
		return err
	}
	if err = store.Atomic(l.Pending, model.JSON(backupPath), 0600); err != nil {
		return err
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		recoveryErr := restore(context.Background(), l, r, b, &s)
		if recoveryErr != nil {
			err = fmt.Errorf("%v；%w；备份：%s", err, recoveryErr, backupPath)
		} else {
			_ = os.Remove(l.Pending)
			err = fmt.Errorf("%v；已恢复部署前状态", err)
		}
	}()
	for _, name := range Services {
		if b.Active[name] {
			if _, err = r.Run(ctx, "systemctl", "stop", name); err != nil {
				return err
			}
		}
	}
	if old != nil {
		if err = network.Down(ctx, r, *old); err != nil {
			return err
		}
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	selfData, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	if err = store.Atomic(l.App, selfData, 0755); err != nil {
		return err
	}
	if err = store.Atomic(l.Config, model.JSON(candidate), 0600); err != nil {
		return err
	}
	if err = store.Atomic(l.State, model.JSON(s), 0600); err != nil {
		return err
	}
	if err = store.Atomic(l.IPv6Config, []byte(ipv6SysctlConfig), 0644); err != nil {
		return err
	}
	if err = DisableIPv6(ctx, l, r); err != nil {
		return err
	}
	for name, data := range Units(l, s) {
		if err = store.Atomic(filepath.Join(l.Units, name), data, 0644); err != nil {
			return err
		}
	}
	if _, err = r.Run(ctx, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err = network.Up(ctx, r, s); err != nil {
		return err
	}
	for _, name := range []string{RoutesService, ProxyService} {
		if _, err = r.Run(ctx, "systemctl", "enable", name); err != nil {
			return err
		}
	}
	if s.Protocol == "trojan" {
		if _, err = r.Run(ctx, "systemctl", "enable", CoverService); err != nil {
			return err
		}
	} else {
		if _, err = r.Run(ctx, "systemctl", "disable", CoverService); err != nil {
			return err
		}
	}
	if _, err = r.Run(ctx, "systemctl", "start", ProxyService); err != nil {
		return err
	}
	for i := 0; i < 4; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		if _, err = r.Run(ctx, "systemctl", "is-active", "--quiet", ProxyService); err != nil {
			return fmt.Errorf("代理服务启动后退出：%w", err)
		}
	}
	if err = store.Export(s, l.Exports); err != nil {
		return err
	}
	if err = store.Atomic(l.Latest, model.JSON(backupPath), 0600); err != nil {
		return err
	}
	if err = os.Remove(l.Pending); err != nil {
		return err
	}
	committed = true
	return nil
}

func Rollback(ctx context.Context, l Layout, r network.Runner) error {
	unlock, err := lock(l.Lock)
	if err != nil {
		return err
	}
	defer unlock()
	if err = Recover(ctx, l, r); err != nil {
		return err
	}
	data, err := os.ReadFile(l.Latest)
	if err != nil {
		return err
	}
	var path string
	if err = json.Unmarshal(data, &path); err != nil {
		return err
	}
	b, err := readBackup(l, path)
	if err != nil {
		return err
	}
	var current *model.State
	if s, e := model.Load(l.State); e == nil {
		current = &s
	}
	if err = store.Atomic(l.Pending, model.JSON(path), 0600); err != nil {
		return err
	}
	if err = restore(ctx, l, r, b, current); err != nil {
		return err
	}
	if err = os.Remove(l.Pending); err != nil {
		return err
	}
	if err = os.Remove(l.Latest); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
