package network

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Select an unused subscription port without stopping existing listeners or
// binding a public socket during the wizard. Reserved ports include listeners
// which the new deployment will create. A nil runner supports offline previews.
func RecommendedTCPPort(ctx context.Context, runner Runner, local string, preferred int, reserved ...int) (int, error) {
	var output []byte
	if runner != nil {
		var err error
		output, err = runner.Run(ctx, "ss", "-H", "-l", "-n", "-t")
		if err != nil {
			return 0, err
		}
	}
	busy := map[int]bool{}
	for _, port := range reserved {
		busy[port] = true
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		host, port, err := net.SplitHostPort(fields[3])
		if err != nil {
			continue
		}
		hostIP := net.ParseIP(host)
		if host == "*" || host == "0.0.0.0" || host == "::" || hostIP != nil && hostIP.Equal(net.ParseIP(local)) {
			number, _ := strconv.Atoi(port)
			busy[number] = true
		}
	}
	for _, port := range []int{preferred, 8443, 9443, 18443} {
		if port > 0 && port <= 65535 && !busy[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("推荐的 HTTPS 订阅端口已占用，请通过 --advanced 指定其他端口")
}
