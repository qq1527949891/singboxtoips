package integration

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mipproxy.local/mipproxy/internal/config"
	"mipproxy.local/mipproxy/internal/export"
	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/network"
	"mipproxy.local/mipproxy/internal/probe"
	"mipproxy.local/mipproxy/internal/testutil"
)

// Run the actual sing-box ACME client against Pebble, with real HTTP-01
// validation. Test roots are scoped to these child processes, never installed.
func TestAutomaticIPCertificateAndTwoNodeClients(t *testing.T) {
	core, pebble := os.Getenv("MIP_CORE"), os.Getenv("MIP_PEBBLE")
	self, selfErr := os.Readlink("/proc/self/ns/net")
	init, initErr := os.Readlink("/proc/1/ns/net")
	if core == "" || pebble == "" || os.Getenv("MIP_ISOLATED_NETWORK") != "1" || selfErr != nil || initErr != nil || self == init {
		t.Skip("requires pinned core/Pebble and a separate network namespace")
	}
	for _, address := range []string{"203.0.113.10/32", "203.0.113.11/32"} {
		address := address
		runIP(t, "address", "add", address, "dev", "lo")
		t.Cleanup(func() { runIP(t, "address", "del", address, "dev", "lo") })
	}
	dir := t.TempDir()
	certificate, key := testutil.Cert(t, "127.0.0.1")
	caTransportPEM, err := os.ReadFile(certificate)
	if err != nil {
		t.Fatal(err)
	}
	pebbleConfig := map[string]any{"pebble": map[string]any{
		"listenAddress": "127.0.0.1:14000", "managementListenAddress": "127.0.0.1:15000",
		"certificate": certificate, "privateKey": key, "httpPort": 80, "tlsPort": 443,
		"keyAlgorithm": "ecdsa", "retryAfter": map[string]int{"authz": 1, "order": 1},
		"profiles": map[string]any{"shortlived": map[string]any{"description": "IP certificate test", "validityPeriod": 518400}},
	}}
	pebblePath := filepath.Join(dir, "pebble.json")
	if err = os.WriteFile(pebblePath, model.JSON(pebbleConfig), 0600); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(dir, "pebble.log"))
	if err != nil {
		t.Fatal(err)
	}
	caProcess := exec.Command(pebble, "-config", pebblePath)
	caProcess.Env = append(os.Environ(), "PEBBLE_VA_NOSLEEP=1", "PEBBLE_VA_ALWAYS_VALID=0", "PEBBLE_WFE_NONCEREJECT=0")
	caProcess.Stdout, caProcess.Stderr = log, log
	if err = caProcess.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = caProcess.Process.Kill()
		_ = caProcess.Wait()
		_ = log.Close()
		if t.Failed() {
			data, _ := os.ReadFile(log.Name())
			t.Logf("Pebble logs:\n%s", data)
		}
	})
	if err = network.WaitPort(context.Background(), "127.0.0.1:15000"); err != nil {
		t.Fatal(err)
	}
	transportRoots := x509.NewCertPool()
	transportRoots.AppendCertsFromPEM(caTransportPEM)
	https := http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: transportRoots}}, Timeout: 5 * time.Second}
	defer https.CloseIdleConnections()
	response, err := https.Get("https://127.0.0.1:15000/roots/0")
	if err != nil {
		t.Fatal(err)
	}
	issuedRoot, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatal("read test CA root", err)
	}
	echoIP := os.Getenv("MIP_ECHO_IP")
	echoListener, err := net.Listen("tcp4", net.JoinHostPort(echoIP, "0"))
	if err != nil {
		t.Fatal(err)
	}
	echo := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address, _, _ := net.SplitHostPort(r.RemoteAddr)
		_, _ = io.WriteString(w, address)
	}))
	echo.Listener.Close()
	echo.Listener = echoListener
	echo.Start()
	defer echo.Close()
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(echoIP)})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, address, err := udp.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			_, _ = udp.WriteToUDP(append([]byte(address.IP.String()+"|"), buffer[:n]...), address)
		}
	}()
	cache := filepath.Join(dir, "acme")
	for index, protocol := range []string{"hysteria2", "tuic", "trojan", "vmess"} {
		t.Run(protocol, func(t *testing.T) {
			s := testutil.State(t, protocol)
			s.Port, s.CertificateMode, s.ServerName = 41000+index, "acme-ip", s.Nodes[0].PublicIP
			s.Certificate, s.Key = "", ""
			for i := range s.Nodes {
				s.Nodes[i].LocalIP = s.Nodes[i].PublicIP
			}
			server, err := config.ServerWithACMEDirectory(s, cache)
			if err != nil {
				t.Fatal(err)
			}
			server["certificate"] = map[string]any{"certificate": []string{string(caTransportPEM)}}
			provider := server["certificate_providers"].([]any)[0].(config.Object)
			provider["provider"] = "https://127.0.0.1:14000/dir"
			startCore(t, core, server)
			var leaf *x509.Certificate
			deadline := time.Now().Add(35 * time.Second)
			for time.Now().Before(deadline) && leaf == nil {
				paths, _ := filepath.Glob(filepath.Join(cache, "certificates", "*", "*", "*.crt"))
				for _, path := range paths {
					data, _ := os.ReadFile(path)
					block, _ := pem.Decode(data)
					if block != nil {
						candidate, parseErr := x509.ParseCertificate(block.Bytes)
						if parseErr == nil && candidate.VerifyHostname(s.ServerName) == nil {
							leaf = candidate
						}
					}
				}
				if leaf == nil {
					time.Sleep(100 * time.Millisecond)
				}
			}
			if leaf == nil {
				t.Fatal("ACME did not complete real HTTP-01 validation and cache an IP certificate")
			}
			if leaf.PublicKeyAlgorithm != x509.ECDSA {
				t.Fatal("incompatible automatic key algorithm")
			}
			base := 42000 + index*10
			client := export.ProbeClient(s, base)
			for _, node := range client["outbounds"].([]any) {
				node.(map[string]any)["tls"].(map[string]any)["certificate"] = []string{string(issuedRoot)}
			}
			startCore(t, core, client)
			if err = network.WaitPort(context.Background(), net.JoinHostPort("127.0.0.1", strconv.Itoa(base))); err != nil {
				t.Fatal(err)
			}
			checkClients := func(port int) {
				for i, node := range s.Nodes {
					httpClient := http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(export.ProxyURL(port + i))}, Timeout: 8 * time.Second}
					defer httpClient.CloseIdleConnections()
					response, err := httpClient.Get(echo.URL)
					if err != nil {
						t.Fatal(err)
					}
					body, err := io.ReadAll(response.Body)
					response.Body.Close()
					if err != nil || string(body) != node.LocalIP {
						t.Fatalf("TCP IP source mismatch: %q: %v", body, err)
					}
					reply, err := probe.UDPExchange(port+i, echoIP, udp.LocalAddr().(*net.UDPAddr).Port, []byte("game-packet"))
					if err != nil || string(reply) != node.LocalIP+"|game-packet" {
						t.Fatalf("UDP IP source mismatch: %q: %v", reply, err)
					}
				}
			}
			checkClients(base)
			if mihomo := os.Getenv("MIP_MIHOMO"); mihomo != "" {
				proxies, listeners := []any{}, []any{}
				for i, node := range s.Nodes {
					proxy := export.MihomoNode(s, node)
					sum := sha256.Sum256(leaf.Raw)
					proxy["fingerprint"] = hex.EncodeToString(sum[:])
					proxies = append(proxies, proxy)
					listeners = append(listeners, map[string]any{"name": fmt.Sprint("node-", i), "type": "mixed", "listen": "127.0.0.1", "port": base + 5 + i, "proxy": node.Name(s.Protocol)})
				}
				startMihomo(t, mihomo, map[string]any{"log-level": "warning", "listeners": listeners, "proxies": proxies, "rules": []string{"MATCH,REJECT"}})
				if err = network.WaitPort(context.Background(), net.JoinHostPort("127.0.0.1", strconv.Itoa(base+5))); err != nil {
					t.Fatal(err)
				}
				checkClients(base + 5)
			}
		})
	}
	data, _ := os.ReadFile(log.Name())
	if !strings.Contains(string(data), "http://203.0.113.10:80/.well-known/acme-challenge/") {
		t.Fatal("test CA did not perform HTTP-01 validation")
	}
}
