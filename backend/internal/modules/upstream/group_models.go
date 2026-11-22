package upstream

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Read-only group directory fallback when no Key exists yet. A directory does
// not establish health and does not create a temporary credential.
func (s *PlatformService) FetchGroupModelIDs(ctx context.Context, site Site, groupID string) ([]string, error) {
	if site.Session == nil {
		return nil, fmt.Errorf("上游未登录")
	}
	session := *site.Session
	opts := newAPIRequestOptions(session)
	opts.Context = ctx
	path := "/api/pricing"
	if session.Platform == PlatformSub2API {
		path = "/api/v1/groups/available"
		opts = requestOptions{Context: ctx, AccessToken: session.AccessToken, TokenType: session.TokenType, InsecureSkipTLS: site.SkipTLSVerify || session.InsecureSkipTLS}
	}
	r, e := s.httpClient.requestJSON(strings.TrimRight(site.BaseURL, "/")+path, opts)
	seen := map[string]bool{}
	if session.Platform == PlatformNewAPI {
		if e != nil {
			return nil, e
		}
		for _, o := range newAPIHealthOffers(r.Payload) {
			for _, g := range o.groups {
				if g == groupID {
					seen[o.model] = true
				}
			}
		}
	} else {
		collect := func(payload any) {
			for _, raw := range dataArray(payload) {
				m, ok := raw.(map[string]any)
				if !ok || groupID2(m) != groupID {
					continue
				}
				for _, field := range []string{"models", "allowed_models", "supported_models"} {
					if ids, ok := m[field].([]any); ok {
						for _, id := range ids {
							v, _ := id.(string)
							if model, ok := id.(map[string]any); ok {
								v = safeString(model, "id")
								if v == "" {
									v = safeString(model, "model")
								}
							}
							if v = strings.TrimSpace(v); v != "" {
								seen[v] = true
							}
						}
					}
				}
			}
		}
		if e == nil {
			collect(r.Payload)
		}
		if len(seen) == 0 {
			// Marketplace forks expose model objects here even when the standard
			// available-groups endpoint only carries group names and ratios.
			fallback, err := s.httpClient.requestJSON(strings.TrimRight(site.BaseURL, "/")+"/api/v1/marketplace/models", opts)
			if err == nil {
				collect(fallback.Payload)
			} else if e != nil {
				return nil, e
			}
		}
	}
	ids := []string{}
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
