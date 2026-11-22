package upstream

import (
	"crypto/md5"
	"encoding/hex"
	"net/url"
	"strings"
)

// Contains only whitelist metadata; never carries a Key value, cookie or token.
type KeySecurityObservation struct {
	BaseURL         string
	KeyID           string
	GroupID         string
	AllowedIPs      []string
	AuthFingerprint string
}

func KeyObservationAuth(accessToken, cookie, userID string) string {
	sum := md5.Sum([]byte(accessToken + "|" + cookie + "|" + userID))
	return hex.EncodeToString(sum[:])
}

func (c *HTTPClient) SetKeyObserver(observer func([]KeySecurityObservation)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keyObserver = observer
}
func (c *HTTPClient) notifyKeyResponse(rawURL string, payload any, options ...requestOptions) {
	c.mu.Lock()
	observer := c.keyObserver
	c.mu.Unlock()
	if observer == nil {
		return
	}
	u, e := url.Parse(rawURL)
	if e != nil {
		return
	}
	index := strings.Index(u.Path, "/api/v1/keys")
	platform := PlatformSub2API
	if index < 0 {
		index = strings.Index(u.Path, "/api/token")
		platform = PlatformNewAPI
	}
	if index < 0 {
		return
	}
	base := u.Scheme + "://" + u.Host + u.Path[:index]
	records := dataArray(payload)
	if len(records) == 0 {
		record := dataRecord(payload)
		if groupID2(record) != "" {
			records = []any{record}
		}
	}
	values := []KeySecurityObservation{}
	fingerprint := ""
	if len(options) > 0 {
		o := options[0]
		fingerprint = KeyObservationAuth(o.AccessToken, o.Cookie, o.UserID)
	}
	for _, raw := range records {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := groupID2(record)
		if id == "" {
			continue
		}
		ips := []string{}
		if platform == PlatformNewAPI {
			v, exists := record["allow_ips"]
			if !exists {
				continue
			}
			s, ok := v.(string)
			if !ok && v != nil {
				continue
			}
			ips = strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' })
		} else {
			v, exists := record["ip_whitelist"]
			if !exists {
				continue
			}
			if v != nil {
				arr, ok := v.([]any)
				if !ok {
					continue
				}
				valid := true
				for _, v := range arr {
					ip, ok := v.(string)
					if !ok {
						valid = false
						break
					}
					ips = append(ips, ip)
				}
				if !valid {
					continue
				}
			}
		}
		values = append(values, KeySecurityObservation{BaseURL: base, KeyID: id, GroupID: SecurityKeyGroup(platform, record), AllowedIPs: ips, AuthFingerprint: fingerprint})
	}
	if len(values) > 0 {
		go observer(values)
	}
}
