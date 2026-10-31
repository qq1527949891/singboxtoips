package network

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Select an unused subscription port without stopping existing listeners or
// binding a public socket during the wizard.
func RecommendedTCPPort(ctx context.Context, runner Runner, local string, preferred int) (int, error) {
	output, err := runner.Run(ctx, "ss", "-H", "-l", "-n", "-t")
	if err != nil {
		return 0, err
	}
	busy := map[int]bool{}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		host, port, err := net.SplitHostPort(fields[3])
		if err != nil {
			continue
		}
		if host == local || host == "*" || host == "0.0.0.0" || host == "::" {
			number, _ := strconv.Atoi(port)
			busy[number] = true
		}
	}
	for _, port := range []int{preferred, 8443, 9443, 18443} {
		if port > 0 && !busy[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("推荐的 HTTPS 订阅端口已占用，请通过 --advanced 指定其他端口")
}
