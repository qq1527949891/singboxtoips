package system

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"sync"
	"time"
)

// Coalesce concurrent reloads and continue serving a valid certificate during
// an incomplete renewal write. Never fall back to an expired certificate.
type certificateReloader struct {
	mu        sync.Mutex
	load      func() (*tls.Certificate, error)
	now       func() time.Time
	lastCheck time.Time
	pair      *tls.Certificate
	err       error
}

func (c *certificateReloader) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.lastCheck.IsZero() || now.Sub(c.lastCheck) >= time.Second {
		pair, err := c.load()
		if err == nil {
			if pair == nil || len(pair.Certificate) == 0 {
				err = fmt.Errorf("HTTPS 证书为空")
			} else if pair.Leaf == nil {
				pair.Leaf, err = x509.ParseCertificate(pair.Certificate[0])
			}
		}
		if err == nil && (now.Before(pair.Leaf.NotBefore) || !now.Before(pair.Leaf.NotAfter)) {
			err = fmt.Errorf("HTTPS 证书尚未生效或已过期")
		}
		if err == nil {
			c.pair = pair
		}
		c.lastCheck, c.err = now, err
	}
	if c.pair != nil && !now.Before(c.pair.Leaf.NotBefore) && now.Before(c.pair.Leaf.NotAfter) {
		return c.pair, nil
	}
	if c.err != nil {
		return nil, c.err
	}
	return nil, fmt.Errorf("HTTPS 证书尚未就绪或已过期")
}
