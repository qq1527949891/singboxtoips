package system

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/store"
)

func BenchmarkSubscriptionRequests(b *testing.B) {
	l := Paths(b.TempDir())
	s := model.State{Schema: 1, Protocol: "trojan", Port: 443, ServerName: "example.com", Certificate: "unused.pem", Key: "unused.key", SubscriptionPort: 8443, CoreVersion: model.CoreVersion}
	s.SubscriptionToken, _ = model.NewSubscriptionToken()
	for i := 1; i <= 64; i++ {
		node, err := model.NewNode(i, fmt.Sprintf("10.0.0.%d", i), fmt.Sprintf("203.0.113.%d", i))
		if err != nil {
			b.Fatal(err)
		}
		s.Nodes = append(s.Nodes, node)
	}
	for path, data := range map[string][]byte{l.State: model.JSON(s), l.Lock: nil} {
		if err := store.Atomic(path, data, 0600); err != nil {
			b.Fatal(err)
		}
	}
	handler := SubscriptionHandler(l)
	request := httptest.NewRequest(http.MethodGet, "/"+s.SubscriptionToken+"/mihomo.yaml", nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			b.Fatal(response.Code)
		}
	}
}
