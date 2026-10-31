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
	absolute := func(path string) string { p, _ := filepath.Abs(path); return filepath.Clean(p) }
	if absolute(state) == absolute(l.State) || output != "" && absolute(output) == absolute(l.Exports) {
		unlock, err := lock(l.Lock)
		if err != nil {
			return err
		}
		defer unlock()
		if _, err = os.Stat(l.Pending); err == nil {
			return fmt.Errorf("�ϴβ�����δ��ɣ����� apply �� rollback �ָ����ٵ���/���")
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

func Doctor(ctx context.Context, l Layout, state, binary string, out io.Writer) error {
	return managedRead(l, state, "", func() error {
		s, err := model.Load(state)
		if err != nil {
			return err
		}
		return probe.Doctor(ctx, s, binary, out)
	})
}
