package system

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"mipproxy.local/mipproxy/internal/config"
)

const auxiliaryConnectionLimit = 64

// Bound auxiliary HTTP connections, including clients that never finish TLS
// or HTTP. This does not limit authenticated proxy connections in sing-box.
type boundedListener struct {
	net.Listener
	slots chan struct{}
	done  chan struct{}
	once  sync.Once
}

func limitConnections(listener net.Listener, maximum int) *boundedListener {
	return &boundedListener{Listener: listener, slots: make(chan struct{}, maximum), done: make(chan struct{})}
}

func (l *boundedListener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	connection, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &boundedConnection{Conn: connection, release: func() { <-l.slots }}, nil
}

func (l *boundedListener) Close() error {
	var err error
	l.once.Do(func() { close(l.done); err = l.Listener.Close() })
	return err
}

type boundedConnection struct {
	net.Conn
	release func()
	once    sync.Once
}

func (c *boundedConnection) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}

func auxiliaryHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192,
		// These small download endpoints need no multiplexing. Keeping HTTP/1
		// also makes the connection bound a bound on concurrent handlers.
		TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){}}
}

func serveHTTP(ctx context.Context, server *http.Server, listener net.Listener, secure bool) error {
	listener = limitConnections(listener, auxiliaryConnectionLimit)
	defer listener.Close()
	defer server.Close()
	stop := context.AfterFunc(ctx, func() { _ = server.Close() })
	defer stop()
	var err error
	if secure {
		err = server.ServeTLS(listener, "", "")
	} else {
		err = server.Serve(listener)
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func ServeCover(ctx context.Context) error {
	listener, err := net.Listen("tcp4", "127.0.0.1:18080")
	if err != nil {
		return err
	}
	server := auxiliaryHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = io.WriteString(w, config.CoverHTML)
	}))
	return serveHTTP(ctx, server, listener, false)
}
