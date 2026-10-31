package probe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"mipproxy.local/mipproxy/internal/core"
	"mipproxy.local/mipproxy/internal/export"
	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/network"
	"mipproxy.local/mipproxy/internal/store"
)

func FreeRange(count int) (int, error) {
	for base := 28000; base+count < 60000; base += count + 1 {
		listeners := []net.Listener{}
		ok := true
		for i := 0; i < count; i++ {
			l, e := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(base+i)))
			if e != nil {
				ok = false
				break
			}
			listeners = append(listeners, l)
		}
		for _, l := range listeners {
			l.Close()
		}
		if ok {
			return base, nil
		}
	}
	return 0, fmt.Errorf("û���㹻�ı���̽��˿�")
}

type Result struct {
	Name, TCPIP, UDPIP, TCPError, UDPError string
	UDPReceived                            int
	UDPRTT                                 time.Duration
}

func Doctor(ctx context.Context, s model.State, binaryPath string, out io.Writer) error {
	if err := core.Version(ctx, network.ExecRunner{}, binaryPath); err != nil {
		return err
	}
	base, err := FreeRange(len(s.Nodes))
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "mip-proxy-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	local := s
	local.Nodes = append([]model.Node{}, s.Nodes...)
	for i := range local.Nodes {
		local.Nodes[i].PublicIP = local.Nodes[i].LocalIP
		local.Nodes[i].PublicPort = s.ListenPort(local.Nodes[i])
	}
	file := filepath.Join(dir, "client.json")
	if err = store.Atomic(file, model.JSON(export.ProbeClient(local, base)), 0600); err != nil {
		return err
	}
	if err = core.Check(ctx, network.ExecRunner{}, binaryPath, file); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, binaryPath, "run", "-c", file)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return err
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if err = network.WaitPort(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(base))); err != nil {
		return err
	}
	fmt.Fprintln(out, "��鱾����ڵ���ʵ������֤��TCP ת���� UDP/STUN ���ڡ���")
	results := make([]Result, len(s.Nodes))
	limit := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, n := range s.Nodes {
		wg.Add(1)
		go func(i int, n model.Node) {
			defer wg.Done()
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-limit }()
			if ctx.Err() != nil {
				return
			}
			result := Result{Name: n.Name(s.Protocol)}
			ip, e := HTTPIP(ctx, base+i)
			result.TCPIP = ip
			if e != nil {
				result.TCPError = e.Error()
			} else if ip != n.PublicIP {
				result.TCPError = "���ڲ�ƥ�䣺Ԥ�� " + n.PublicIP
			}
			for attempt := 0; attempt < 3; attempt++ {
				if ctx.Err() != nil {
					return
				}
				start := time.Now()
				ip, e := STUNContext(ctx, base+i, "stun.l.google.com", 19302)
				if e != nil {
					result.UDPError = e.Error()
					continue
				}
				result.UDPReceived++
				result.UDPIP = ip
				result.UDPRTT += time.Since(start)
			}
			if result.UDPReceived > 0 {
				result.UDPRTT /= time.Duration(result.UDPReceived)
				result.UDPError = ""
				if result.UDPIP != n.PublicIP {
					result.UDPError = "���ڲ�ƥ�䣺Ԥ�� " + n.PublicIP
				}
			}
			results[i] = result
		}(i, n)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	failed := false
	for _, r := range results {
		fmt.Fprintf(out, "%s\n  TCP ���ڣ�%s", r.Name, r.TCPIP)
		if r.TCPError != "" {
			fmt.Fprintf(out, "  δͨ����%s", r.TCPError)
			failed = true
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, "  UDP ���ڣ�%s����Ӧ %d/3��ƽ������ %s", r.UDPIP, r.UDPReceived, r.UDPRTT.Round(time.Millisecond))
		if r.UDPError != "" {
			fmt.Fprintf(out, "  δͨ����%s", r.UDPError)
			failed = true
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out, "���ϴӷ��������������ⲿ��վ�ɴ����������֤������� RTT �������û����ڵ����Ϸ�ӳ١�")
	fmt.Fprintln(out, "UDP δ��ӦҲ�����ǹ��� STUN ������������ƣ����� TCP ����ͷ�����־�жϡ�")
	if failed {
		return fmt.Errorf("���ּ��δͨ��")
	}
	return nil
}

func HTTPIP(ctx context.Context, port int) (string, error) {
	t := &http.Transport{Proxy: http.ProxyURL(export.ProxyURL(port)), DisableKeepAlives: true}
	defer t.CloseIdleConnections()
	c := http.Client{Transport: t, Timeout: 12 * time.Second}
	req, e := http.NewRequestWithContext(ctx, "GET", "https://api.ipify.org", nil)
	if e != nil {
		return "", e
	}
	resp, e := c.Do(req)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, 128))
	if e != nil {
		return "", e
	}
	ip, e := netip.ParseAddr(strings.TrimSpace(string(b)))
	if e != nil || !ip.Is4() || resp.StatusCode != 200 {
		return "", fmt.Errorf("����̽����񷵻���Ч���")
	}
	return ip.String(), nil
}

// UDPExchange uses SOCKS5 UDP ASSOCIATE, keeping the TCP control connection alive.
func UDPExchange(proxyPort int, host string, port int, payload []byte) ([]byte, error) {
	return UDPExchangeContext(context.Background(), proxyPort, host, port, payload)
}

func UDPExchangeContext(ctx context.Context, proxyPort int, host string, port int, payload []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	dialer := net.Dialer{Timeout: 3 * time.Second}
	c, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(proxyPort)))
	if err != nil {
		return nil, err
	}
	defer c.Close()
	stopTCP := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stopTCP()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err = c.Write([]byte{5, 1, 0}); err != nil {
		return nil, err
	}
	response := make([]byte, 2)
	if _, err = io.ReadFull(c, response); err != nil {
		return nil, err
	}
	if response[0] != 5 || response[1] != 0 {
		return nil, fmt.Errorf("SOCKS5 ��֤Э��ʧ��")
	}
	if _, err = c.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return nil, err
	}
	header := make([]byte, 4)
	if _, err = io.ReadFull(c, header); err != nil {
		return nil, err
	}
	if header[0] != 5 || header[1] != 0 {
		return nil, fmt.Errorf("SOCKS5 UDP ASSOCIATE ���ܾ�")
	}
	var addr string
	switch header[3] {
	case 1:
		b := make([]byte, 4)
		if _, err = io.ReadFull(c, b); err != nil {
			return nil, err
		}
		addr = net.IP(b).String()
	case 4:
		b := make([]byte, 16)
		if _, err = io.ReadFull(c, b); err != nil {
			return nil, err
		}
		addr = net.IP(b).String()
	case 3:
		b := []byte{0}
		if _, err = io.ReadFull(c, b); err != nil {
			return nil, err
		}
		name := make([]byte, int(b[0]))
		if _, err = io.ReadFull(c, name); err != nil {
			return nil, err
		}
		addr = string(name)
	default:
		return nil, fmt.Errorf("SOCKS5 ��ַ������Ч")
	}
	p := make([]byte, 2)
	if _, err = io.ReadFull(c, p); err != nil {
		return nil, err
	}
	if addr == "0.0.0.0" || addr == "::" {
		addr = "127.0.0.1"
	}
	udpAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(addr, strconv.Itoa(int(binary.BigEndian.Uint16(p)))))
	if err != nil {
		return nil, err
	}
	udp, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return nil, err
	}
	defer udp.Close()
	stopUDP := context.AfterFunc(ctx, func() { _ = udp.Close() })
	defer stopUDP()
	_ = udp.SetDeadline(time.Now().Add(4 * time.Second))
	packet := []byte{0, 0, 0, 3, byte(len(host))}
	if len(host) > 255 {
		return nil, fmt.Errorf("Ŀ����������")
	}
	packet = append(packet, host...)
	packet = append(packet, byte(port>>8), byte(port))
	packet = append(packet, payload...)
	if _, err = udp.Write(packet); err != nil {
		return nil, err
	}
	buffer := make([]byte, 65535)
	count, err := udp.Read(buffer)
	if err != nil {
		return nil, err
	}
	return UDPBody(buffer[:count])
}

func UDPBody(packet []byte) ([]byte, error) {
	if len(packet) < 4 || packet[0] != 0 || packet[1] != 0 || packet[2] != 0 {
		return nil, fmt.Errorf("��Ч���Ƭ�� SOCKS5 UDP ��Ӧ")
	}
	offset := 4
	switch packet[3] {
	case 1:
		offset += 4
	case 4:
		offset += 16
	case 3:
		if len(packet) < 5 {
			return nil, io.ErrUnexpectedEOF
		}
		offset += 1 + int(packet[4])
	default:
		return nil, fmt.Errorf("δ֪ SOCKS5 ��ַ����")
	}
	offset += 2
	if len(packet) < offset {
		return nil, io.ErrUnexpectedEOF
	}
	return packet[offset:], nil
}

func STUN(proxyPort int, host string, port int) (string, error) {
	return STUNContext(context.Background(), proxyPort, host, port)
}

func STUNContext(ctx context.Context, proxyPort int, host string, port int) (string, error) {
	request := make([]byte, 20)
	binary.BigEndian.PutUint16(request, 1)
	binary.BigEndian.PutUint32(request[4:], 0x2112a442)
	if _, err := rand.Read(request[8:]); err != nil {
		return "", err
	}
	response, err := UDPExchangeContext(ctx, proxyPort, host, port, request)
	if err != nil {
		return "", err
	}
	return ParseSTUN(response, request[8:])
}

func ParseSTUN(b, transaction []byte) (string, error) {
	if len(b) < 20 || len(transaction) != 12 || binary.BigEndian.Uint16(b) != 0x0101 || binary.BigEndian.Uint32(b[4:]) != 0x2112a442 || string(b[8:20]) != string(transaction) {
		return "", fmt.Errorf("STUN ��Ӧͷ������ ID ��Ч")
	}
	length := int(binary.BigEndian.Uint16(b[2:]))
	if length+20 > len(b) {
		return "", io.ErrUnexpectedEOF
	}
	for pos := 20; pos+4 <= 20+length; {
		typ, n := binary.BigEndian.Uint16(b[pos:]), int(binary.BigEndian.Uint16(b[pos+2:]))
		start := pos + 4
		if start+n > 20+length {
			return "", io.ErrUnexpectedEOF
		}
		if typ == 0x0020 && n >= 8 && b[start+1] == 1 {
			ip := binary.BigEndian.Uint32(b[start+4:]) ^ 0x2112a442
			out := make(net.IP, 4)
			binary.BigEndian.PutUint32(out, ip)
			return out.String(), nil
		}
		pos = start + (n+3)&^3
	}
	return "", fmt.Errorf("STUN ��Ӧû�� IPv4 XOR-MAPPED-ADDRESS")
}
