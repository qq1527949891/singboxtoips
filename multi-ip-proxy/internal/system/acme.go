package system

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mipproxy.local/mipproxy/internal/network"
)

// Certmagic writes complete certificate chains to .crt files, independently of
// private account and certificate keys. Verify issuance before exporting nodes:
// ManageAsync alone only indicates that a background job has been scheduled.
func issuedCertificate(directory, identity string, roots *x509.CertPool) (bool, error) {
	ready := false
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if os.IsNotExist(walkErr) {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if ready || !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".crt") {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > 1<<20 {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var chain []*x509.Certificate
		for len(data) != 0 {
			block, rest := pem.Decode(data)
			if block == nil {
				break
			}
			data = rest
			if block.Type != "CERTIFICATE" {
				continue
			}
			certificate, parseErr := x509.ParseCertificate(block.Bytes)
			if parseErr != nil {
				return nil // A concurrent atomic replacement can be retried.
			}
			chain = append(chain, certificate)
		}
		if len(chain) == 0 || chain[0].PublicKeyAlgorithm != x509.ECDSA || chain[0].NotAfter.Before(time.Now().Add(5*time.Minute)) {
			return nil
		}
		intermediates := x509.NewCertPool()
		for _, certificate := range chain[1:] {
			intermediates.AddCert(certificate)
		}
		_, err = chain[0].Verify(x509.VerifyOptions{DNSName: identity, Roots: roots, Intermediates: intermediates})
		ready = err == nil
		return nil
	})
	return ready, err
}

func waitCertificate(ctx context.Context, l Layout, runner network.Runner, identity string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	for {
		ready, err := issuedCertificate(l.ACME, identity, nil)
		if err != nil {
			return fmt.Errorf("读取自动证书：%w", err)
		}
		if ready {
			return nil
		}
		if _, err = runner.Run(ctx, "systemctl", "is-active", "--quiet", ProxyService); err != nil {
			return fmt.Errorf("申请证书期间代理服务退出：%w", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待 IP 证书签发失败（%v）；请确认 %s 的 TCP 80 在防火墙/安全组/NAT 中允许公网访问，并检查 journalctl -u mip-proxy.service", ctx.Err(), identity)
		case <-time.After(time.Second):
		}
	}
}
