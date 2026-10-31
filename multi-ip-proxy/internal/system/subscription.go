package system

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"mipproxy.local/mipproxy/internal/export"
	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/store"
)

// Generate only client exports. No directory is exposed as a web root, so
// state, backups, ACME account keys and server private keys cannot be fetched.
func SubscriptionHandler(l Layout) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		token, filename, found := strings.Cut(strings.TrimPrefix(request.URL.Path, "/"), "/")
		if !found || strings.ContainsAny(filename, "/\\") || filename == "" {
			http.NotFound(w, request)
			return
		}
		status := http.StatusOK
		var data []byte
		err := managedReadWithLock(l, l.State, "", readLock, func() error {
			state, err := model.Load(l.State)
			if err != nil {
				return err
			}
			if !state.SubscriptionEnabled() || subtle.ConstantTimeCompare([]byte(token), []byte(state.SubscriptionToken)) != 1 {
				status = http.StatusNotFound
				return nil
			}
			files, err := export.Files(state)
			if err != nil {
				return err
			}
			var exists bool
			data, exists = files[filename]
			if !exists {
				status = http.StatusNotFound
			}
			return nil
		})
		if err != nil {
			http.Error(w, "Subscription temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		if status != http.StatusOK {
			http.NotFound(w, request)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if strings.HasSuffix(filename, ".json") {
			w.Header().Set("Content-Type", "application/json")
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		if request.Method == http.MethodGet {
			_, _ = w.Write(data)
		}
	})
}

func subscriptionCertificate(l Layout, state model.State) (*tls.Certificate, error) {
	return loadSubscriptionCertificate(l, state, nil)
}

func loadSubscriptionCertificate(l Layout, state model.State, roots *x509.CertPool) (*tls.Certificate, error) {
	certificatePath, keyPath := l.SubscriptionTLS, l.SubscriptionTLS
	if state.AutomaticCertificate() {
		var err error
		certificatePath, err = cachedCertificatePath(l.ACME, state.ServerName, roots, 0)
		if err != nil {
			return nil, err
		}
		if certificatePath == "" {
			return nil, fmt.Errorf("自动证书尚未就绪")
		}
		keyPath = strings.TrimSuffix(certificatePath, ".crt") + ".key"
	}
	pair, err := tls.LoadX509KeyPair(certificatePath, keyPath)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	if err = leaf.VerifyHostname(state.ServerName); err != nil {
		return nil, err
	}
	if time.Now().Before(leaf.NotBefore) || time.Now().After(leaf.NotAfter) {
		return nil, fmt.Errorf("订阅 HTTPS 证书尚未生效或已过期")
	}
	return &pair, nil
}

func ServeSubscriptions(ctx context.Context, l Layout) error {
	// Startup is requested by apply while its operation lock is held. Read the
	// already-written state here; request handlers reject pending transactions.
	state, err := model.Load(l.State)
	if err != nil {
		return err
	}
	if !state.SubscriptionEnabled() {
		return fmt.Errorf("当前配置未启用订阅服务")
	}
	listener, err := net.Listen("tcp4", state.SubscriptionListenAddress())
	if err != nil {
		return fmt.Errorf("HTTPS 订阅端口 %s 无法监听：%w", state.SubscriptionListenAddress(), err)
	}
	defer listener.Close()
	server := http.Server{
		Handler: SubscriptionHandler(l), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return subscriptionCertificate(l, state) }},
	}
	go func() { <-ctx.Done(); _ = server.Close() }()
	err = server.ServeTLS(listener, "", "")
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func CheckSubscriptions(ctx context.Context, l Layout) error {
	state, err := model.Load(l.State)
	if err != nil {
		return err
	}
	if !state.SubscriptionEnabled() {
		return fmt.Errorf("当前配置未启用订阅")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return err
	}
	if !state.AutomaticCertificate() {
		// Pin the administrator's supplied certificate for this local readiness
		// check. Remote clients still perform their own normal trust validation.
		data, err := os.ReadFile(l.SubscriptionTLS)
		if err != nil {
			return err
		}
		roots.AppendCertsFromPEM(data)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: time.Second}, Config: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: state.ServerName, RootCAs: roots}}
	for {
		connection, err := dialer.DialContext(ctx, "tcp4", state.SubscriptionListenAddress())
		if err == nil {
			connection.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("HTTPS 订阅未就绪，请检查证书和 TCP %d：%w", state.SubscriptionPort, err)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// Manual source paths may be under /root, hidden by systemd ProtectHome. A
// validated private PEM copy is managed transactionally with the deployment.
func writeSubscriptionTLS(l Layout, data []byte) error {
	if len(data) != 0 {
		return store.Atomic(l.SubscriptionTLS, data, 0600)
	}
	err := os.Remove(l.SubscriptionTLS)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
