package integration

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"mipproxy.local/mipproxy/internal/config"
	"mipproxy.local/mipproxy/internal/export"
	"mipproxy.local/mipproxy/internal/network"
	"mipproxy.local/mipproxy/internal/testutil"
)

func TestRealCoreRejectsUnboundIPv6(t *testing.T) {
	binary, address := os.Getenv("MIP_CORE"), os.Getenv("MIP_ECHO_IPV6")
	if binary == "" || address == "" {
		t.Skip("requires isolated IPv6 echo address and pinned core")
	}
	listener, err := net.Listen("tcp6", net.JoinHostPort(address, "0"))
	if err != nil {
		t.Fatal(err)
	}
	echo := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ipv6-egress") }))
	echo.Listener.Close()
	echo.Listener = listener
	echo.Start()
	defer echo.Close()
	for index, blocked := range []bool{false, true} {
		t.Run(fmt.Sprint("blocked=", blocked), func(t *testing.T) {
			s := testutil.State(t, "shadowsocks")
			s.Port = 38000 + index
			server, err := config.Server(s)
			if err != nil {
				t.Fatal(err)
			}
			if !blocked {
				server["route"].(config.Object)["rules"] = server["route"].(config.Object)["rules"].([]any)[1:]
			}
			for i := range s.Nodes {
				local := fmt.Sprintf("127.0.0.%d", i+2)
				server["inbounds"].([]any)[i].(config.Object)["listen"] = local
				server["outbounds"].([]any)[i].(config.Object)["inet4_bind_address"] = local
				s.Nodes[i].PublicIP, s.Nodes[i].PublicPort = local, s.Port
			}
			startCore(t, binary, server)
			base := 38100 + index*10
			startCore(t, binary, export.ProbeClient(s, base))
			if err = network.WaitPort(context.Background(), fmt.Sprintf("127.0.0.1:%d", base)); err != nil {
				t.Fatal(err)
			}
			transport := &http.Transport{Proxy: http.ProxyURL(export.ProxyURL(base))}
			defer transport.CloseIdleConnections()
			client := http.Client{Transport: transport, Timeout: 3 * time.Second}
			response, err := client.Get(echo.URL)
			success := false
			if err == nil {
				success = response.StatusCode == 200
				response.Body.Close()
			}
			if success == blocked {
				t.Fatalf("IPv6 rejection=%v, request succeeded=%v, error=%v", blocked, success, err)
			}
		})
	}
}
