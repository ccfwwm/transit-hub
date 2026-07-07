package dashboard

import (
	"context"
	"errors"
	"testing"

	"transithub/backend/internal/modules/upstream"
)

// fakeMySiteSync 记录 SyncAdminSession 的调用参数，供测试断言刷新成功后是否同步到 my_site_states。
type fakeMySiteSync struct {
	called         bool
	userID         string
	adminAccountID string
	session        upstream.Session
	identity       string
}

func (f *fakeMySiteSync) SyncAdminSession(ctx context.Context, userID string, adminAccountID string, session upstream.Session, identity string) {
	f.called = true
	f.userID = userID
	f.adminAccountID = adminAccountID
	f.session = session
	f.identity = identity
}

func newRefreshTestService(store *fakeSessionStore, platform *fakePlatformClient, mySync *fakeMySiteSync) *Service {
	service := NewService(store, platform)
	service.SetAdminAccountService(&fakeAdminAccounts{current: map[string]string{"user-1": "account-1"}})
	if mySync != nil {
		service.SetMySiteSync(mySync)
	}
	return service
}

// TestRefreshAdminSession_Success 覆盖：refresh 成功且 VerifyAdmin 成功，返回 authenticated=true，
// 且写回 store、并调用 mySiteSync.SyncAdminSession。
func TestRefreshAdminSession_Success(t *testing.T) {
	store := newFakeSessionStore()
	store.set("user-1", "account-1", AdminSession{
		Platform: PlatformSub2API,
		Identity: "admin@example.com",
		Session:  authenticatedSession(),
	})
	refreshed := upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: "https://example.com", AccessToken: "new-token"}
	platform := &fakePlatformClient{refreshSessionResult: &refreshed}
	mySync := &fakeMySiteSync{}
	service := newRefreshTestService(store, platform, mySync)

	resp, err := service.RefreshAdminSession(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !resp.Authenticated {
		t.Fatal("expected authenticated=true")
	}

	saved, _ := store.Get(context.Background(), "user-1", "account-1")
	if saved == nil || saved.Session.AccessToken != "new-token" {
		t.Fatalf("expected store to be updated with refreshed session, got %+v", saved)
	}

	if !mySync.called {
		t.Fatal("expected mySiteSync.SyncAdminSession to be called")
	}
	if mySync.session.AccessToken != "new-token" {
		t.Fatalf("expected mySiteSync to receive refreshed session, got %+v", mySync.session)
	}
}

// TestRefreshAdminSession_RefreshFailed 覆盖：RefreshSession 失败返回 ErrorAdminOnly，不写回 store。
func TestRefreshAdminSession_RefreshFailed(t *testing.T) {
	store := newFakeSessionStore()
	store.set("user-1", "account-1", AdminSession{
		Platform: PlatformSub2API,
		Session:  authenticatedSession(),
	})
	platform := &fakePlatformClient{refreshSessionErr: errors.New("refresh token expired")}
	mySync := &fakeMySiteSync{}
	service := newRefreshTestService(store, platform, mySync)

	_, err := service.RefreshAdminSession(context.Background(), "user-1")
	assertAdminOnlyError(t, err)
	if mySync.called {
		t.Fatal("expected mySiteSync not to be called on refresh failure")
	}
}

// TestRefreshAdminSession_VerifyFailed 覆盖：refresh 成功但 VerifyAdmin 失败，返回 ErrorAdminOnly，不写回 store。
func TestRefreshAdminSession_VerifyFailed(t *testing.T) {
	store := newFakeSessionStore()
	store.set("user-1", "account-1", AdminSession{
		Platform: PlatformSub2API,
		Session:  authenticatedSession(),
	})
	platform := &fakePlatformClient{verifyAdminErr: errors.New("not admin")}
	mySync := &fakeMySiteSync{}
	service := newRefreshTestService(store, platform, mySync)

	_, err := service.RefreshAdminSession(context.Background(), "user-1")
	assertAdminOnlyError(t, err)

	saved, _ := store.Get(context.Background(), "user-1", "account-1")
	if saved == nil || saved.Session.AccessToken != authenticatedSession().AccessToken {
		t.Fatalf("expected old session to remain untouched, got %+v", saved)
	}
	if mySync.called {
		t.Fatal("expected mySiteSync not to be called on verify failure")
	}
}

// TestLoginPasswordStoresCredentialForRelogin 覆盖：账号密码登录成功后保存密码，
// 这样后台会话 refresh token 和 access token 都失效时可以自动重新登录。
func TestLoginPasswordStoresCredentialForRelogin(t *testing.T) {
	store := newFakeSessionStore()
	platform := &fakePlatformClient{
		loginSub2APIAdminResult: &upstream.Session{
			Platform:    upstream.PlatformSub2API,
			BaseURL:     "https://example.com",
			AccessToken: "fresh-token",
		},
	}
	service := newRefreshTestService(store, platform, nil)

	_, err := service.Login(context.Background(), "user-1", LoginRequest{
		Platform:   PlatformSub2API,
		SiteURL:    "https://example.com",
		AuthMethod: AuthMethodPassword,
		Email:      "admin@example.com",
		Password:   "secret-password",
	})
	if err != nil {
		t.Fatalf("expected login to succeed, got %v", err)
	}

	saved, _ := store.Get(context.Background(), "user-1", "account-1")
	if saved == nil {
		t.Fatal("expected saved admin session")
	}
	if saved.Password != "secret-password" {
		t.Fatalf("expected password credential to be stored for relogin, got %q", saved.Password)
	}
}

// TestCurrentAdminSession_ReloginsWithStoredPasswordWhenVerifyFails 覆盖：下游模块
// 获取当前 admin session 时，如果旧 token 已失效，应自动用保存的账号密码重登。
func TestCurrentAdminSession_ReloginsWithStoredPasswordWhenVerifyFails(t *testing.T) {
	store := newFakeSessionStore()
	store.set("user-1", "account-1", AdminSession{
		Platform:   PlatformSub2API,
		BaseURL:    "https://example.com",
		AuthMethod: AuthMethodPassword,
		Identity:   "admin@example.com",
		Password:   "secret-password",
		Session:    authenticatedSession(),
	})
	fresh := upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: "https://example.com", AccessToken: "fresh-token"}
	platform := &fakePlatformClient{
		verifyAdminErrByToken:   map[string]error{"token": errors.New("expired access token")},
		loginSub2APIAdminResult: &fresh,
	}
	mySync := &fakeMySiteSync{}
	service := newRefreshTestService(store, platform, mySync)

	session, identity, ok, err := service.CurrentAdminSession(context.Background(), "user-1", "account-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !ok {
		t.Fatal("expected session to be recovered")
	}
	if identity != "admin@example.com" {
		t.Fatalf("expected identity to be preserved, got %q", identity)
	}
	if session.AccessToken != "fresh-token" {
		t.Fatalf("expected recovered session token, got %+v", session)
	}
	if platform.loginSub2APIAdminCalls != 1 {
		t.Fatalf("expected one password relogin, got %d", platform.loginSub2APIAdminCalls)
	}
	if platform.loginSub2APIAdminPassword != "secret-password" {
		t.Fatalf("expected stored password to be used, got %q", platform.loginSub2APIAdminPassword)
	}
	if !mySync.called || mySync.session.AccessToken != "fresh-token" {
		t.Fatalf("expected recovered session to be synced to my_site_states, got %+v", mySync)
	}
}

// TestRefreshAdminSession_FallsBackToPasswordRelogin 覆盖：主动刷新时 refresh token
// 失效后，不应直接要求重新登录；若保存了密码，应自动重新登录并写回新 session。
func TestRefreshAdminSession_FallsBackToPasswordRelogin(t *testing.T) {
	store := newFakeSessionStore()
	store.set("user-1", "account-1", AdminSession{
		Platform:   PlatformSub2API,
		BaseURL:    "https://example.com",
		AuthMethod: AuthMethodPassword,
		Identity:   "admin@example.com",
		Password:   "secret-password",
		Session: upstream.Session{
			Platform:     upstream.PlatformSub2API,
			BaseURL:      "https://example.com",
			AccessToken:  "old-token",
			RefreshToken: "expired-refresh-token",
		},
	})
	fresh := upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: "https://example.com", AccessToken: "fresh-token"}
	platform := &fakePlatformClient{
		refreshSessionErr:       errors.New("refresh token expired"),
		loginSub2APIAdminResult: &fresh,
	}
	mySync := &fakeMySiteSync{}
	service := newRefreshTestService(store, platform, mySync)

	resp, err := service.RefreshAdminSession(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("expected refresh to recover by password relogin, got %v", err)
	}
	if !resp.Authenticated {
		t.Fatal("expected authenticated=true")
	}
	saved, _ := store.Get(context.Background(), "user-1", "account-1")
	if saved == nil || saved.Session.AccessToken != "fresh-token" {
		t.Fatalf("expected recovered session to be saved, got %+v", saved)
	}
	if !mySync.called || mySync.session.AccessToken != "fresh-token" {
		t.Fatalf("expected recovered session to be synced, got %+v", mySync)
	}
}

// TestRefreshAdminSession_NoSession 覆盖：当前无 admin session 时返回明确错误。
func TestRefreshAdminSession_NoSession(t *testing.T) {
	store := newFakeSessionStore()
	platform := &fakePlatformClient{}
	service := newRefreshTestService(store, platform, nil)

	_, err := service.RefreshAdminSession(context.Background(), "user-1")
	assertAdminOnlyError(t, err)
}

func assertAdminOnlyError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var reqErr requestError
	if !errors.As(err, &reqErr) || reqErr.Error() != ErrorAdminOnly {
		t.Fatalf("expected ErrorAdminOnly, got %v", err)
	}
}
