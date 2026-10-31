package system

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestBoundedListenerReleasesConnectionsAndStops(t *testing.T) {
	raw, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := limitConnections(raw, 2)
	defer listener.Close()
	accept := func() net.Conn {
		t.Helper()
		client, err := net.Dial("tcp4", raw.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		connection, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { connection.Close() })
		return connection
	}
	first, second := accept(), accept()
	accepted := make(chan net.Conn, 1)
	failed := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			failed <- err
		} else {
			accepted <- connection
		}
	}()
	third, err := net.Dial("tcp4", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close()
	select {
	case connection := <-accepted:
		connection.Close()
		t.Fatal("accepted more than two connections")
	case err := <-failed:
		t.Fatal(err)
	case <-time.After(50 * time.Millisecond):
	}
	first.Close()
	first.Close() // A second close must not release another connection's slot.
	select {
	case connection := <-accepted:
		defer connection.Close()
	case err := <-failed:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("closing a connection did not release its slot")
	}
	if len(listener.slots) != 2 {
		t.Fatal("incorrect active connection count", len(listener.slots))
	}
	done := make(chan error, 1)
	go func() { _, err := listener.Accept(); done <- err }()
	listener.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("closed listener accepted a connection")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener close left Accept blocked")
	}
	second.Close()
}

func TestAuxiliaryServerReleasesIncompleteRequestBodies(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := auxiliaryHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "cover")
	}))
	if server.ReadTimeout <= 0 || server.WriteTimeout <= 0 || server.IdleTimeout <= 0 {
		t.Fatal("missing resource deadlines")
	}
	server.ReadTimeout = 150 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serveHTTP(ctx, server, listener, false) }()
	connection, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = fmt.Fprint(connection, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 4096\r\n\r\nx")
	if err != nil {
		t.Fatal(err)
	}
	// net/http drains small unread bodies before flushing the response. Only a
	// read deadline frees this connection when the body never arrives.
	_, err = io.ReadAll(connection)
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("unfinished request body retained its connection")
	}
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP server did not stop")
	}
}

func TestAuxiliaryServerRepeatedShutdownReclaimsGoroutines(t *testing.T) {
	baseline := runtime.NumGoroutine()
	beforeFiles, filesError := os.ReadDir("/proc/self/fd")
	for i := 0; i < 24; i++ {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		server := auxiliaryHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- serveHTTP(ctx, server, listener, false) }()
		var clients []net.Conn
		for j := 0; j < 4; j++ {
			connection, err := net.DialTimeout("tcp4", listener.Addr().String(), time.Second)
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			clients = append(clients, connection)
			fmt.Fprint(connection, "GET / HTTP/1.1\r\n")
		}
		cancel()
		select {
		case err = <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("shutdown left blocked connections")
		}
		for _, connection := range clients {
			connection.Close()
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > baseline+4 && time.Now().Before(deadline) {
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
	}
	if count := runtime.NumGoroutine(); count > baseline+4 {
		t.Fatalf("goroutines retained after 24 shutdowns: before %d, after %d", baseline, count)
	}
	if filesError == nil {
		afterFiles, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		if len(afterFiles) > len(beforeFiles)+4 {
			t.Fatalf("file descriptors retained after 24 shutdowns: before %d, after %d", len(beforeFiles), len(afterFiles))
		}
	}
}

func TestAuxiliaryServeErrorDoesNotRetainCancellationWatchers(t *testing.T) {
	baseline := runtime.NumGoroutine()
	// A live cancellable context used to retain one watcher per failed server.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var workers sync.WaitGroup
	for i := 0; i < 24; i++ {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		listener.Close()
		workers.Add(1)
		go func() {
			defer workers.Done()
			_ = serveHTTP(ctx, auxiliaryHTTPServer(http.NotFoundHandler()), listener, false)
		}()
	}
	workers.Wait()
	if count := runtime.NumGoroutine(); count > baseline+4 {
		t.Fatalf("failed servers retained cancellation watchers: before %d, after %d", baseline, count)
	}
}
