package integration

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

func startCore(t *testing.T, binary string, c map[string]any) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, model.JSON(c), 0600); err != nil {
		t.Fatal(err)
	}
	check := exec.Command(binary, "check", "-c", path)
	if b, err := check.CombinedOutput(); err != nil {
		t.Fatalf("core validation: %v\n%s", err, b)
	}
	log, err := os.Create(filepath.Join(dir, "core.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "run", "-c", path)
	cmd.Stdout = log
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = log.Close()
		if t.Failed() {
			b, _ := os.ReadFile(log.Name())
			t.Logf("core logs:\n%s", b)
		}
	})
	time.Sleep(300 * time.Millisecond)
}

func TestRealCoreTwoIPsTCPAndUDP(t *testing.T) {
	binary := os.Getenv("MIP_CORE")
	if binary == "" {
		t.Skip("set MIP_CORE to the pinned sing-box executable")
	}
	if err := exec.Command(binary, "version").Run(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	echoIP := os.Getenv("MIP_ECHO_IP")
	if echoIP == "" {
		echoIP = "127.0.0.1"
	}
	tcpEcho := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		_, _ = io.WriteString(w, host)
	}))
	tcpEcho.Listener.Close()
	echoListener, err := net.Listen("tcp", net.JoinHostPort(echoIP, "0"))
	if err != nil {
		t.Fatal(err)
	}
	tcpEcho.Listener = echoListener
	tcpEcho.Start()
	defer tcpEcho.Close()
	udpEcho, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(echoIP)})
	if err != nil {
		t.Fatal(err)
	}
	defer udpEcho.Close()
	go func() {
		buffer := make([]byte, 8192)
		for {
			n, addr, e := udpEcho.ReadFromUDP(buffer)
			if e != nil {
				return
			}
			_, _ = udpEcho.WriteToUDP(append([]byte(addr.IP.String()+"|"), buffer[:n]...), addr)
		}
	}()
	cover := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "cover-page") }))
	defer cover.Close()
	coverPort, _ := strconv.Atoi(strings.Split(cover.Listener.Addr().String(), ":")[1])
	certPath, keyPath := testutil.Cert(t, "example.com")
	certPEM, _ := os.ReadFile(certPath)
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "target-page") }))
	target.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13, NextProtos: []string{"h2", "http/1.1"}}
	target.Config.ErrorLog = log.New(io.Discard, "", 0)
	target.StartTLS()
	defer target.Close()
	targetPort, _ := strconv.Atoi(strings.Split(target.Listener.Addr().String(), ":")[1])
	for index, p := range model.Protocols {
		t.Run(p.ID, func(t *testing.T) {
			if p.ID == "wireguard" && net.ParseIP(echoIP).IsLoopback() {
				t.Skip("WireGuard rejects martian loopback destinations; set MIP_ECHO_IP inside an isolated namespace")
			}
			if p.ID == "wireguard" {
				self, selfErr := os.Readlink("/proc/self/ns/net")
				init, initErr := os.Readlink("/proc/1/ns/net")
				if os.Getenv("MIP_ISOLATED_NETWORK") != "1" || selfErr != nil || initErr != nil || self == init {
					t.Skip("WireGuard routing test requires integration/run-isolated.sh in a separate network namespace")
				}
			}
			s := testutil.State(t, p.ID)
			s.Port = 35000 + index
			if s.NeedsTLS() {
				s.Certificate = certPath
				s.Key = keyPath
			}
			server, err := config.Server(s)
			if err != nil {
				t.Fatal(err)
			}
			if p.ID == "wireguard" {
				// Emulate a remote reply route while both peers are in one namespace.
				// The host namespace is never used by run-isolated.sh.
				runIP(t, "rule", "del", "priority", "0")
				runIP(t, "rule", "add", "priority", "32765", "lookup", "local")
				t.Cleanup(func() {
					runIP(t, "rule", "add", "priority", "0", "lookup", "local")
					runIP(t, "rule", "del", "priority", "32765", "lookup", "local")
				})
				for i, n := range s.Nodes {
					table, mark := strconv.Itoa(n.Table()), strconv.Itoa(n.Mark())
					runIP(t, "route", "add", "local", "127.0.0.0/8", "dev", "lo", "src", fmt.Sprintf("127.0.0.%d", i+2), "table", table)
					runIP(t, "rule", "add", "priority", strconv.Itoa(i+1), "fwmark", mark, "lookup", table)
					t.Cleanup(func() {
						runIP(t, "rule", "del", "fwmark", mark, "lookup", table)
						runIP(t, "route", "del", "local", "127.0.0.0/8", "dev", "lo", "table", table)
					})
				}
			}
			for i, n := range s.Nodes {
				local := fmt.Sprintf("127.0.0.%d", i+2)
				ins := server["inbounds"].([]any)
				if s.Protocol == "wireguard" {
					ins = server["endpoints"].([]any)
				}
				in := ins[i].(config.Object)
				if s.Protocol == "wireguard" {
					in["inet4_bind_address"] = local
					in["routing_mark"] = n.Mark()
					in["bind_interface"] = "lo"
				} else {
					in["listen"] = local
				}
				out := server["outbounds"].([]any)[i].(config.Object)
				out["inet4_bind_address"] = local
				if p.ID == "wireguard" {
					out["bind_interface"] = "lo"
				}
				dns := server["dns"].(config.Object)["servers"].([]any)[i].(config.Object)
				dns["inet4_bind_address"] = local
				if p.ID == "wireguard" {
					dns["bind_interface"] = "lo"
				}
				if s.Protocol == "reality" {
					handshake := in["tls"].(config.Object)["reality"].(config.Object)["handshake"].(config.Object)
					handshake["server"] = "127.0.0.1"
					handshake["server_port"] = targetPort
					handshake["inet4_bind_address"] = local
				}
				if s.Protocol == "trojan" {
					in["fallback"] = config.Object{"server": "127.0.0.1", "server_port": coverPort}
				}
				s.Nodes[i].PublicIP = local
				s.Nodes[i].PublicPort = s.ListenPort(s.Nodes[i])
			}
			startCore(t, binary, server)
			base := 36000 + index*10
			client := export.ProbeClient(s, base)
			if s.NeedsTLS() {
				for _, v := range client["outbounds"].([]any) {
					v.(map[string]any)["tls"].(map[string]any)["certificate"] = []string{string(certPEM)}
				}
			}
			startCore(t, binary, client)
			if err = network.WaitPort(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(base))); err != nil {
				t.Fatal(err)
			}
			for i := range s.Nodes {
				transport := &http.Transport{Proxy: http.ProxyURL(export.ProxyURL(base + i))}
				defer transport.CloseIdleConnections()
				hc := http.Client{Transport: transport, Timeout: 8 * time.Second}
				resp, e := hc.Get(tcpEcho.URL)
				if e != nil {
					t.Fatalf("TCP node %d: %v", i, e)
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				expected := fmt.Sprintf("127.0.0.%d", i+2)
				if string(body) != expected {
					t.Fatalf("TCP source: got %q, want %q", body, expected)
				}
				for _, size := range []int{64, 1200} {
					payload := strings.Repeat("g", size)
					reply, e := probe.UDPExchange(base+i, echoIP, udpEcho.LocalAddr().(*net.UDPAddr).Port, []byte(payload))
					if e != nil {
						t.Fatalf("UDP node %d/%d bytes: %v", i, size, e)
					}
					if string(reply) != expected+"|"+payload {
						t.Fatalf("wrong UDP source or payload on node %d", i)
					}
				}
			}
			if mihomo := os.Getenv("MIP_MIHOMO"); mihomo != "" {
				proxies, listeners := []any{}, []any{}
				for i, n := range s.Nodes {
					p := export.MihomoNode(s, n)
					if s.NeedsTLS() {
						sum := sha256.Sum256(cert.Certificate[0])
						p["fingerprint"] = hex.EncodeToString(sum[:])
					}
					proxies = append(proxies, p)
					listeners = append(listeners, map[string]any{"name": fmt.Sprintf("probe-%d", i), "type": "mixed", "listen": "127.0.0.1", "port": base + 5 + i, "proxy": n.Name(s.Protocol)})
				}
				startMihomo(t, mihomo, map[string]any{"log-level": "warning", "listeners": listeners, "proxies": proxies, "rules": []string{"MATCH,REJECT"}})
				if e := network.WaitPort(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(base+5))); e != nil {
					t.Fatal(e)
				}
				for i := range s.Nodes {
					hc := http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(export.ProxyURL(base + 5 + i))}, Timeout: 8 * time.Second}
					resp, e := hc.Get(tcpEcho.URL)
					if e != nil {
						t.Fatal("Mihomo TCP:", e)
					}
					body, bodyErr := io.ReadAll(resp.Body)
					resp.Body.Close()
					hc.CloseIdleConnections()
					expected := fmt.Sprintf("127.0.0.%d", i+2)
					if string(body) != expected {
						t.Fatalf("Mihomo TCP source: %q; HTTP %d; read error: %v", body, resp.StatusCode, bodyErr)
					}
					reply, e := probe.UDPExchange(base+5+i, echoIP, udpEcho.LocalAddr().(*net.UDPAddr).Port, []byte("game-packet"))
					if e != nil {
						t.Fatal("Mihomo UDP:", e)
					}
					if string(reply) != expected+"|game-packet" {
						t.Fatalf("Mihomo UDP source: %q", reply)
					}
				}
			}
			// Generated exports must remain strict; custom certificate trust above is test-only.
			files, e := export.Files(testutil.State(t, p.ID))
			if e != nil {
				t.Fatal(e)
			}
			var exported any
			if e = json.Unmarshal(files["sing-box.json"], &exported); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func runIP(t *testing.T, args ...string) {
	t.Helper()
	if b, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
		t.Fatalf("ip %v: %v: %s", args, err, b)
	}
}

func startMihomo(t *testing.T, binary string, c map[string]any) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, model.JSON(c), 0600); err != nil {
		t.Fatal(err)
	}
	check := exec.Command(binary, "-t", "-d", dir, "-f", path)
	if b, e := check.CombinedOutput(); e != nil {
		t.Fatalf("Mihomo config: %v\n%s", e, b)
	}
	logFile, err := os.Create(filepath.Join(dir, "mihomo.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-d", dir, "-f", path)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = logFile.Close()
		if t.Failed() {
			b, _ := os.ReadFile(logFile.Name())
			t.Logf("Mihomo logs:\n%s", b)
		}
	})
}
