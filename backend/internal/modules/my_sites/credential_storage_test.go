package my_sites

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"transithub/backend/internal/modules/settings"
	"transithub/backend/internal/modules/upstream"
)

func TestStoredCredentialMigrationAndReads(t *testing.T) {
	ctx := context.Background()
	db := securityTestDatabase(t)
	if _, err := db.Exec(ctx, `CREATE TABLE users(id text primary key)`); err != nil {
		t.Fatal(err)
	}
	siteRepo := upstream.NewRepository(db)
	bindingRepo := NewRepository(db)
	settingsRepo := settings.NewRepository(db)
	for _, ensure := range []func(context.Context) error{siteRepo.EnsureSchema, bindingRepo.EnsureSchema, settingsRepo.EnsureSchema} {
		if err := ensure(ctx); err != nil {
			t.Fatal(err)
		}
	}
	session := upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: "https://example.test", AccessToken: "fixture-access", RefreshToken: "fixture-refresh", Cookie: "fixture-cookie"}
	site := upstream.Site{ID: "site", UserID: "u", AdminAccountID: "w", BaseURL: session.BaseURL, Session: &session}
	if err := siteRepo.SaveSite(ctx, site); err != nil {
		t.Fatal(err)
	}
	state := State{UserID: "u", AdminAccountID: "w", Session: session}
	if err := bindingRepo.Save(ctx, state); err != nil {
		t.Fatal(err)
	}
	conn := RealConnection{ID: "binding", UserID: "u", WorkspaceAdminAccountID: "w", UpstreamSiteID: "site", UpstreamKey: "fixture-api-key", CreatedAt: time.Now().Format(time.RFC3339)}
	if err := bindingRepo.SaveMonitorBinding(ctx, conn); err != nil {
		t.Fatal(err)
	}
	notifications := settings.DefaultNotificationChannelSettings()
	notifications.Telegram = []settings.TelegramChannelSettings{{BotToken: "fixture-bot-key"}}
	if err := settingsRepo.SaveNotificationChannels(ctx, "u", "w", notifications); err != nil {
		t.Fatal(err)
	}
	cipher, err := upstream.NewSiteCredentialCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	siteRepo.SetCredentialCipher(cipher)
	bindingRepo.SetCredentialCipher(cipher)
	settingsRepo.SetCredentialCipher(cipher)
	for range 2 {
		for _, migrate := range []func(context.Context) error{siteRepo.MigrateCredentials, bindingRepo.MigrateCredentials, settingsRepo.MigrateCredentials} {
			if err := migrate(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	var upstreamSession, workspaceSession, storedKey, fingerprint, notificationJSON string
	err = db.QueryRow(ctx, `SELECT (SELECT session::text FROM upstream_sites), (SELECT session::text FROM my_site_states),
		(SELECT upstream_key FROM real_connections), (SELECT upstream_key_fingerprint FROM real_connections),
		(SELECT settings::text FROM notification_channel_settings)`).Scan(&upstreamSession, &workspaceSession, &storedKey, &fingerprint, &notificationJSON)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{upstreamSession, workspaceSession, storedKey, notificationJSON} {
		if strings.Contains(raw, "fixture-") || !strings.Contains(raw, "enc:v1:") {
			t.Fatal("stored value is missing authenticated encryption")
		}
	}
	if fingerprint != connectionKeyMatch(cipher, conn.UpstreamKey) || fingerprint == "" {
		t.Fatal("encrypted key cannot be matched across consumers")
	}
	sites, err := siteRepo.ListSitesForUser(ctx, "u")
	if err != nil || len(sites) != 1 || sites[0].Session.AccessToken != session.AccessToken {
		t.Fatalf("upstream session did not round-trip: %v", err)
	}
	workspace, err := bindingRepo.Get(ctx, "u", "w")
	if err != nil || workspace == nil || workspace.Session.RefreshToken != session.RefreshToken {
		t.Fatalf("workspace session did not round-trip: %v", err)
	}
	conns, err := bindingRepo.ListRealConnections(ctx, "u", "w")
	if err != nil || len(conns) != 1 || conns[0].UpstreamKey != conn.UpstreamKey {
		t.Fatalf("binding did not round-trip: %v", err)
	}
	got, err := settingsRepo.GetNotificationChannels(ctx, "u", "w")
	if err != nil || len(got.Telegram) != 1 || got.Telegram[0].BotToken != notifications.Telegram[0].BotToken {
		t.Fatalf("notification secret did not round-trip: %v", err)
	}

	// New writes must stay encrypted after migrating older rows.
	conn.UpstreamKey = "fixture-new-key"
	if err := bindingRepo.SaveMonitorBinding(ctx, conn); err != nil {
		t.Fatal(err)
	}
	err = db.QueryRow(ctx, `SELECT upstream_key FROM real_connections WHERE id='binding'`).Scan(&storedKey)
	if err != nil || !strings.HasPrefix(storedKey, "enc:v1:") {
		t.Fatal("new binding write stored an unencrypted key", err)
	}
}
