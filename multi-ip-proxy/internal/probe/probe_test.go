package probe

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestSTUNChecksTransactionAndParsesMappedIP(t *testing.T) {
	b := make([]byte, 32)
	binary.BigEndian.PutUint16(b, 0x101)
	binary.BigEndian.PutUint16(b[2:], 12)
	binary.BigEndian.PutUint32(b[4:], 0x2112a442)
	copy(b[8:], []byte("123456789012"))
	binary.BigEndian.PutUint16(b[20:], 0x20)
	binary.BigEndian.PutUint16(b[22:], 8)
	b[25] = 1
	binary.BigEndian.PutUint32(b[28:], 0xcb00710a^0x2112a442)
	ip, e := ParseSTUN(b, []byte("123456789012"))
	if e != nil || ip != "203.0.113.10" {
		t.Fatal(ip, e)
	}
	if _, e = ParseSTUN(b, []byte("different-id")); e == nil {
		t.Fatal("accepted foreign transaction")
	}
}

func TestCancellationInterruptsBlockedSOCKSHandshake(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.ReadFull(conn, make([]byte, 3))
		cancel()
		_, _ = io.Copy(io.Discard, conn)
	}()
	start := time.Now()
	_, err = UDPExchangeContext(ctx, listener.Addr().(*net.TCPAddr).Port, "example.com", 1234, nil)
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatal("cancellation did not interrupt handshake", err)
	}
}

func TestUDPRejectsTruncatedAndFragmentedPackets(t *testing.T) {
	for _, b := range [][]byte{{0, 0, 0, 1}, {0, 0, 1, 1, 127, 0, 0, 1, 0, 1}, {0, 0, 0, 3, 100}} {
		if _, e := UDPBody(b); e == nil {
			t.Fatal("accepted malformed packet")
		}
	}
}
