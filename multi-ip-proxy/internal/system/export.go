package system

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"mipproxy.local/mipproxy/internal/export"
	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/probe"
	"mipproxy.local/mipproxy/internal/store"
)

func exportFiles(s model.State) (map[string][]byte, error) { return export.Files(s) }

func managedRead(l Layout, state, output string, read func() error) error {
	return managedReadWithLock(l, state, output, lock, read)
}

func managedReadWithLock(l Layout, state, output string, acquire func(string) (func(), error), read func() error) error {
	absolute := func(path string) string { p, _ := filepath.Abs(path); return filepath.Clean(p) }
	if absolute(state) == absolute(l.State) || output != "" && absolute(output) == absolute(l.Exports) {
		unlock, err := acquire(l.Lock)
		if err != nil {
			return err
		}
		defer unlock()
		if _, err = os.Stat(l.Pending); err == nil {
			return fmt.Errorf("上次部署尚未完成，请先 apply 或 rollback 恢复后再导出/检测")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return read()
}

func Export(l Layout, state, output string) error {
	return managedRead(l, state, output, func() error {
		s, err := model.Load(state)
		if err != nil {
			return err
		}
		return store.Export(s, output)
	})
}

func ShowSubscriptions(l Layout, out io.Writer) error {
	return managedRead(l, l.State, "", func() error {
		state, err := model.Load(l.State)
		if err != nil {
			return err
		}
		links := export.Subscriptions(state)
		if len(links) == 0 {
			return fmt.Errorf("当前配置没有 HTTPS 订阅，请通过向导创建/更新 Trojan、Hysteria2、TUIC 或 WireGuard 节点")
		}
		for _, link := range links {
			fmt.Fprintf(out, "%s：\n%s\n", link.Name, link.URL)
		}
		return nil
	})
}

func Doctor(ctx context.Context, l Layout, state, binary string, out io.Writer) error {
	return managedRead(l, state, "", func() error {
		s, err := model.Load(state)
		if err != nil {
			return err
		}
		return probe.Doctor(ctx, s, binary, out)
	})
}
