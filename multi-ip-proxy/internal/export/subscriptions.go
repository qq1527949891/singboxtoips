package export

import (
	"net"
	"net/url"
	"strconv"

	"mipproxy.local/mipproxy/internal/model"
)

type SubscriptionLink struct{ Name, URL string }

func Subscriptions(s model.State) []SubscriptionLink {
	if !s.SubscriptionEnabled() {
		return nil
	}
	link := func(name, file string) SubscriptionLink {
		u := url.URL{Scheme: "https", Host: net.JoinHostPort(s.ServerName, strconv.Itoa(s.SubscriptionPort)), Path: "/" + s.SubscriptionToken + "/" + file}
		return SubscriptionLink{Name: name, URL: u.String()}
	}
	links := []SubscriptionLink{}
	if s.Protocol != "wireguard" {
		links = append(links, link("通用 URI/Base64 订阅", "subscription.txt"))
	}
	links = append(links, link("Mihomo 完整配置订阅", "mihomo.yaml"), link("Mihomo proxy-provider", "mihomo-provider.yaml"), link("sing-box 配置地址", "sing-box.json"))
	if s.Protocol == "wireguard" {
		for _, node := range s.Nodes {
			links = append(links, link("WireGuard 原生配置 "+node.PublicIP, node.Name(s.Protocol)+".conf"))
		}
	}
	return links
}
