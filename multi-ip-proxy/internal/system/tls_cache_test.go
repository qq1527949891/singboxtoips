package system

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCertificateReloadCoalescesConcurrentHandshakes(t *testing.T) {
	now := time.Now()
	pair := &tls.Certificate{Certificate: [][]byte{{1}}, Leaf: &x509.Certificate{NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}}
	var loads atomic.Int32
	cache := certificateReloader{now: func() time.Time { return now }, load: func() (*tls.Certificate, error) {
		loads.Add(1)
		return pair, nil
	}}
	var workers sync.WaitGroup
	for i := 0; i < 128; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if actual, err := cache.get(nil); err != nil || actual != pair {
				t.Error("certificate unavailable", err)
			}
		}()
	}
	workers.Wait()
	if loads.Load() != 1 {
		t.Fatal("concurrent handshakes repeatedly scanned certificates", loads.Load())
	}
}

func TestCertificateRenewalKeepsValidPairAndRejectsExpiredFallback(t *testing.T) {
	now := time.Now()
	first := &tls.Certificate{Certificate: [][]byte{{1}}, Leaf: &x509.Certificate{NotBefore: now.Add(-time.Hour), NotAfter: now.Add(5 * time.Second)}}
	renewed := &tls.Certificate{Certificate: [][]byte{{2}}, Leaf: &x509.Certificate{NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}}
	pair, loadError := first, error(nil)
	loads := 0
	cache := certificateReloader{now: func() time.Time { return now }, load: func() (*tls.Certificate, error) {
		loads++
		return pair, loadError
	}}
	if got, err := cache.get(nil); err != nil || got != first {
		t.Fatal("initial certificate", err)
	}
	now = now.Add(time.Second)
	loadError = errors.New("renewal key temporarily missing")
	if got, err := cache.get(nil); err != nil || got != first {
		t.Fatal("transient renewal error discarded a valid certificate", err)
	}
	for i := 0; i < 64; i++ {
		cache.get(nil)
	}
	if loads != 2 {
		t.Fatal("failed renewal was retried on every handshake", loads)
	}
	now = now.Add(5 * time.Second)
	if got, err := cache.get(nil); err == nil || got != nil {
		t.Fatal("expired cached certificate was served")
	}
	now = now.Add(time.Second)
	pair, loadError = renewed, nil
	if got, err := cache.get(nil); err != nil || got != renewed {
		t.Fatal("renewed certificate was not loaded", err)
	}
}

func TestCertificateReloadRecoversFromMissingStartupFiles(t *testing.T) {
	now := time.Now()
	loadError := errors.New("certificate not issued yet")
	cache := certificateReloader{now: func() time.Time { return now }, load: func() (*tls.Certificate, error) {
		return &tls.Certificate{Certificate: [][]byte{{1}}, Leaf: &x509.Certificate{NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}}, loadError
	}}
	if _, err := cache.get(nil); err == nil {
		t.Fatal("missing startup certificate accepted")
	}
	loadError = nil
	now = now.Add(time.Second)
	if _, err := cache.get(nil); err != nil {
		t.Fatal("certificate cache never recovered", err)
	}
}
