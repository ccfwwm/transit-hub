package monitor_media

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BlobStore keeps request payloads out of SQL/Redis and encrypts provider URLs.
// Its directory must be on the application's persistent data mount.
type BlobStore struct {
	Root string
	AEAD cipher.AEAD
}

func NewBlobStore(root, secret string) (*BlobStore, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("media storage needs the stable application secret")
	}
	key := sha256.Sum256([]byte("sub2api-media-v1:" + secret))
	block, e := aes.NewCipher(key[:])
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(root, 0700); e != nil {
		return nil, e
	}
	return &BlobStore{root, a}, nil
}
func (b *BlobStore) Seal(raw []byte) ([]byte, error) {
	nonce := make([]byte, b.AEAD.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return b.AEAD.Seal(nonce, nonce, raw, []byte("media-v1")), nil
}
func (b *BlobStore) Open(raw []byte) ([]byte, error) {
	n := b.AEAD.NonceSize()
	if len(raw) < n {
		return nil, fmt.Errorf("invalid media blob")
	}
	return b.AEAD.Open(nil, raw[:n], raw[n:], []byte("media-v1"))
}
func (b *BlobStore) Path(id, key string) (string, error) {
	if !strings.HasPrefix(id, "mt_") || strings.ContainsAny(id+key, "/\\.\x00") {
		return "", fail(400, "invalid_blob_id", "Invalid storage identifier")
	}
	dir := filepath.Join(b.Root, id)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	return filepath.Join(dir, key), nil
}
func (b *BlobStore) WriteJSON(id, key string, value any) (string, error) {
	p, e := b.Path(id, key)
	if e != nil {
		return "", e
	}
	raw, e := json.Marshal(value)
	if e != nil {
		return "", e
	}
	encrypted, e := b.Seal(raw)
	if e != nil {
		return "", e
	}
	e = atomicWrite(p, encrypted)
	return p, e
}
func (b *BlobStore) ReadJSON(path string, value any) error {
	relative, e := filepath.Rel(b.Root, path)
	if e != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("outside media storage")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	raw, e = b.Open(raw)
	if e != nil {
		return e
	}
	return json.Unmarshal(raw, value)
}
func atomicWrite(path string, raw []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".media-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(raw)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(tmp, path)
}

func publicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() && !net.ParseIP("169.254.169.254").Equal(ip)
}

// Every redirect and DNS resolution is checked, including download domains.
// Authorization is only supplied to the configured origin, never to a CDN.
func SafeClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	transport := &http.Transport{Proxy: nil, ResponseHeaderTimeout: 150 * time.Second, MaxIdleConns: 40, IdleConnTimeout: 60 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, e := net.SplitHostPort(address)
			if e != nil {
				return nil, e
			}
			ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
			if e != nil {
				return nil, e
			}
			for _, ip := range ips {
				if !allowPrivate && !publicIP(ip.IP) {
					return nil, fmt.Errorf("non-public media address")
				}
			}
			for _, ip := range ips {
				conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				if e == nil {
					return conn, nil
				}
			}
			return nil, fmt.Errorf("media origin unreachable")
		}}
	return &http.Client{Transport: transport, Timeout: 180 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return fmt.Errorf("redirect limit")
		}
		if req.URL.Scheme != "https" && !allowPrivate {
			return fmt.Errorf("HTTPS required")
		}
		req.Header.Del("Authorization")
		req.Header.Del("x-goog-api-key")
		req.Header.Del("Cookie")
		return nil
	}}
}
func originURL(account Account, path string) (string, error) {
	base, e := url.Parse(strings.TrimRight(account.BaseURL, "/"))
	if e != nil || base.Hostname() == "" || base.User != nil {
		return "", fail(422, "invalid_origin", "Invalid channel origin")
	}
	// Configured origin may include a path prefix; never resolve task IDs as URLs.
	escaped := strings.TrimRight(base.EscapedPath(), "/") + path
	if strings.HasSuffix(strings.TrimRight(base.Path, "/"), "/v1") && strings.HasPrefix(path, "/v1/") {
		escaped = strings.TrimRight(base.EscapedPath(), "/") + strings.TrimPrefix(path, "/v1")
	}
	decoded, err := url.PathUnescape(escaped)
	if err != nil {
		return "", err
	}
	base.Path = decoded
	base.RawPath = escaped
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}
func (b *BlobStore) Download(ctx context.Context, client *http.Client, t StoredTask, a Artifact, account Account, max int64) (Artifact, string, string, error) {
	target, e := url.Parse(a.Source)
	if e != nil {
		return a, "", "", e
	}
	origin, _ := url.Parse(account.BaseURL)
	if !target.IsAbs() {
		target = origin.ResolveReference(target)
	}
	if target.User != nil || target.Hostname() == "" {
		return a, "", "", fmt.Errorf("invalid artifact URL")
	}
	if target.Scheme != "https" && (target.Scheme != "http" || !strings.HasPrefix(account.BaseURL, "http://127.0.0.1")) {
		return a, "", "", fmt.Errorf("artifact requires HTTPS")
	}
	if !strings.EqualFold(target.Host, origin.Host) && len(t.Snapshot.Capability.ArtifactHosts) > 0 && !contains(t.Snapshot.Capability.ArtifactHosts, target.Hostname()) {
		return a, "", "", fmt.Errorf("unconfigured artifact host")
	}
	req, e := http.NewRequestWithContext(ctx, "GET", target.String(), nil)
	if e != nil {
		return a, "", "", e
	}
	if strings.EqualFold(target.Host, origin.Host) {
		req.Header.Set("Authorization", "Bearer "+account.Key)
	}
	resp, e := client.Do(req)
	if e != nil {
		return a, "", "", fmt.Errorf("artifact fetch failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return a, "", "", fmt.Errorf("artifact HTTP %d", resp.StatusCode)
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if e != nil || int64(len(raw)) > max || len(raw) == 0 {
		return a, "", "", fmt.Errorf("artifact missing or exceeds limit")
	}
	mime := strings.Split(resp.Header.Get("Content-Type"), ";")[0]
	if mime == "" || mime == "application/octet-stream" {
		mime = http.DetectContentType(raw)
	}
	if !strings.HasPrefix(mime, a.Type+"/") {
		return a, "", "", fmt.Errorf("unexpected artifact MIME")
	}
	path, e := b.Path(t.ID, a.Key)
	if e != nil {
		return a, "", "", e
	}
	if e = atomicWrite(path, raw); e != nil {
		return a, "", "", e
	}
	size := int64(len(raw))
	a.Size = &size
	a.MIME = mime
	a.Available = true
	sum := sha256.Sum256(raw)
	return a, path, hex.EncodeToString(sum[:]), nil
}
