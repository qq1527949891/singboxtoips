package system

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"mipproxy.local/mipproxy/internal/store"
)

func TestManagedReadersRejectUnfinishedDeployment(t *testing.T) {
	l := Paths(t.TempDir())
	if err := store.Atomic(l.Pending, []byte(`"unfinished"`), 0600); err != nil {
		t.Fatal(err)
	}
	// A missing state would give a different error: readers must stop before
	// loading partial state or replacing an export from an interrupted apply.
	for name, read := range map[string]func() error{
		"export": func() error { return Export(l, l.State, l.Exports) },
		"doctor": func() error { return Doctor(context.Background(), l, l.State, "unused", io.Discard) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := read(); err == nil || !strings.Contains(err.Error(), "上次部署尚未完成") {
				t.Fatalf("read partial managed state: %v", err)
			}
		})
	}
	if _, err := os.Stat(l.Exports); !os.IsNotExist(err) {
		t.Fatal("export changed during interrupted deployment", err)
	}
}
