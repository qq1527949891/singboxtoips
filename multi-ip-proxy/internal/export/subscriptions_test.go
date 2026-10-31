package export_test

import (
	"net/url"
	"strings"
	"testing"

	"mipproxy.local/mipproxy/internal/export"
	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/testutil"
)

func TestSubscriptionURLsCoverRequestedClientFormats(t *testing.T) {
	for _, protocol := range []string{"trojan", "hysteria2", "tuic", "wireguard"} {
		s := testutil.State(t, protocol)
		s.SubscriptionToken, _ = model.NewSubscriptionToken()
		s.SubscriptionPort = 8443
		links := export.Subscriptions(s)
		want := 4
		if protocol == "wireguard" {
			want = 3 + len(s.Nodes)
		}
		if len(links) != want {
			t.Fatal("missing subscription formats")
		}
		for _, link := range links {
			u, err := url.Parse(link.URL)
			if err != nil || u.Scheme != "https" || u.Host != s.ServerName+":8443" || !strings.HasPrefix(u.Path, "/"+s.SubscriptionToken+"/") {
				t.Fatal("invalid authenticated HTTPS subscription URL")
			}
		}
	}
}
