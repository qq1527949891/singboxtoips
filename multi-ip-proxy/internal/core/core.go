package core

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mipproxy.local/mipproxy/internal/model"
	"mipproxy.local/mipproxy/internal/network"
)

var Digests = map[string]string{
	"linux/amd64":   "699a1b510aa4396b2176d32d1181920399ebf1e267835da6f3b598d0f7c24968",
	"linux/arm64":   "436441f76ee2f7fe21b5b2d426d3cb261a480455a704865789ad533e4273ec26",
	"windows/amd64": "99185d75d314a07dbba4a130c8ad986cbeb629a07927975636b341ca61f991df",
}

func Install(ctx context.Context, dest string) error {
	digest, ok := Digests[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return fmt.Errorf("��֧�ִ�ƽ̨��֧�� Linux amd64/arm64")
	}
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	name := fmt.Sprintf("sing-box-%s-%s-%s.%s", model.CoreVersion, runtime.GOOS, runtime.GOARCH, ext)
	endpoint := "https://github.com/SagerNet/sing-box/releases/download/v" + model.CoreVersion + "/" + name
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return err
	}
	client := http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("��������ʧ�ܣ�HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return err
	}
	hash := sha256.Sum256(b)
	if hex.EncodeToString(hash[:]) != digest {
		return fmt.Errorf("���� SHA256 У��ʧ�ܣ�δ��װ")
	}
	binaryName := "sing-box"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	var payload []byte
	if ext == "zip" {
		z, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if e != nil {
			return e
		}
		for _, f := range z.File {
			if filepath.Base(f.Name) == binaryName {
				r, e := f.Open()
				if e != nil {
					return e
				}
				payload, err = io.ReadAll(io.LimitReader(r, 128<<20))
				r.Close()
				if err != nil {
					return err
				}
				break
			}
		}
	} else {
		gz, e := gzip.NewReader(bytes.NewReader(b))
		if e != nil {
			return e
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			h, e := tr.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
			if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == binaryName {
				payload, err = io.ReadAll(io.LimitReader(tr, 128<<20))
				if err != nil {
					return err
				}
				break
			}
		}
	}
	if len(payload) == 0 {
		return fmt.Errorf("ѹ����û�к��Ŀ�ִ���ļ�")
	}
	if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(dest), ".core-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(0755); err == nil {
		_, err = f.Write(payload)
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = Version(ctx, network.ExecRunner{}, temp); err != nil {
		return err
	}
	return os.Rename(temp, dest)
}

func Version(ctx context.Context, r network.Runner, path string) error {
	b, err := r.Run(ctx, path, "version")
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(b), "sing-box version "+model.CoreVersion+"\n") && !strings.HasPrefix(string(b), "sing-box version "+model.CoreVersion+"\r\n") {
		return fmt.Errorf("��Ҫ sing-box %s�����ֲ����ݰ汾", model.CoreVersion)
	}
	return nil
}

func Check(ctx context.Context, r network.Runner, binary, path string) error {
	_, err := r.Run(ctx, binary, "check", "-c", path)
	return err
}
