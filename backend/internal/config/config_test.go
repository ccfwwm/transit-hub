package config

import "testing"

func TestSetEnvLineOverridesWhenKeyMarkedForOverride(t *testing.T) {
	t.Setenv("SOME_KEY", "online-value")

	setEnvLine("SOME_KEY=local-value", map[string]struct{}{"SOME_KEY": {}})

	if got := envOrDefault("SOME_KEY", ""); got != "local-value" {
		t.Fatalf("expected local value to override existing value, got %q", got)
	}
}

func TestSetEnvLinePreservesExistingValuesByDefault(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://online")

	setEnvLine("DATABASE_URL=postgres://local", nil)

	if got := envOrDefault("DATABASE_URL", ""); got != "postgres://online" {
		t.Fatalf("expected existing env value to be preserved, got %q", got)
	}
}
