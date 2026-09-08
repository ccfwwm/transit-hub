package upstream

import "testing"

func TestNewAPIGroupsKeepsGroupsWithoutRatioAndNormalizesPlatforms(t *testing.T) {
	groups := newAPIGroups(
		map[string]any{"data": map[string]any{"configured": map[string]any{"ratio": 1.0}}},
		map[string]any{"data": map[string]any{
			"usable_group": map[string]any{"configured": "configured", "unpriced": "unpriced"},
			"group_ratio":  map[string]any{"configured": 1.0, "unpriced": 0},
		}},
	)
	if len(groups) != 2 {
		t.Fatalf("expected both configured and unpriced groups, got %#v", groups)
	}
	byName := map[string]GroupInfo{}
	for _, group := range groups {
		byName[group.Name] = group
	}
	if _, ok := byName["unpriced"]; !ok {
		t.Fatalf("expected unpriced group to be preserved")
	}
}

func TestNormalizeGroupPlatformAliasesAndMultiPlatformValues(t *testing.T) {
	if got := normalizeGroupPlatform("xai, claude, kimi, xai"); got != "anthropic, grok, kimi" {
		t.Fatalf("unexpected normalized platform list %q", got)
	}
}
