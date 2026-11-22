package dashboard

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"transithub/backend/internal/modules/upstream"
)

func rejectedPasswordSession() AdminSession {
	return AdminSession{Platform: PlatformSub2API, BaseURL: "https://example.com", AuthMethod: AuthMethodPassword,
		Identity: "admin@example.com", Password: "old-password", Session: authenticatedSession()}
}

func TestPasswordReloginFailureCoolsDownPerWorkspaceAndNewCredentialsRecover(t *testing.T) {
	store := newFakeSessionStore()
	record := rejectedPasswordSession()
	store.set("user-1", "account-1", record)
	platform := &fakePlatformClient{loginSub2APIAdminErr: errors.New("invalid credentials")}
	s := NewService(store, platform)
	for i := 0; i < 50; i++ {
		_, ok, _ := s.reloginWithStoredPassword(context.Background(), "user-1", "account-1", &record)
		if ok {
			t.Fatal("rejected password recovered")
		}
	}
	if platform.loginSub2APIAdminCalls != 1 {
		t.Fatalf("made %d login attempts, want 1", platform.loginSub2APIAdminCalls)
	}
	store.set("user-1", "account-2", record)
	s.reloginWithStoredPassword(context.Background(), "user-1", "account-2", &record)
	if platform.loginSub2APIAdminCalls != 2 {
		t.Fatal("another workspace was blocked by the cooldown")
	}

	record.Password = "new-password"
	store.set("user-1", "account-1", record)
	platform.loginSub2APIAdminErr = nil
	platform.loginSub2APIAdminResult = &upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: record.BaseURL, AccessToken: "fresh-token"}
	_, ok, err := s.reloginWithStoredPassword(context.Background(), "user-1", "account-1", &record)
	if err != nil || !ok || platform.loginSub2APIAdminCalls != 3 {
		t.Fatalf("updated credentials did not recover: ok=%v err=%v calls=%d", ok, err, platform.loginSub2APIAdminCalls)
	}
}

type lockedSessionStore struct {
	mu sync.Mutex
	*fakeSessionStore
}

func (s *lockedSessionStore) Get(ctx context.Context, user, account string) (*AdminSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeSessionStore.Get(ctx, user, account)
}
func (s *lockedSessionStore) Save(ctx context.Context, user, account string, record AdminSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fakeSessionStore.Save(ctx, user, account, record)
}

type blockingPasswordPlatform struct {
	*fakePlatformClient
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (p *blockingPasswordPlatform) LoginSub2APIAdmin(base, email, password string) (upstream.Session, error) {
	if p.calls.Add(1) == 1 {
		close(p.entered)
	}
	<-p.release
	return upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: base, AccessToken: "fresh-token"}, nil
}

func TestConcurrentChannelWorkersShareOnePasswordRelogin(t *testing.T) {
	store := &lockedSessionStore{fakeSessionStore: newFakeSessionStore()}
	record := rejectedPasswordSession()
	store.Save(context.Background(), "user-1", "account-1", record)
	platform := &blockingPasswordPlatform{fakePlatformClient: &fakePlatformClient{verifyAdminErrByToken: map[string]error{"token": errors.New("revoked")}}, entered: make(chan struct{}), release: make(chan struct{})}
	s := NewService(store, platform)
	var workers sync.WaitGroup
	for i := 0; i < 40; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			got, ok, err := s.reloginWithStoredPassword(context.Background(), "user-1", "account-1", &record)
			if err != nil || !ok || got.Session.AccessToken != "fresh-token" {
				t.Errorf("recovery failed: ok=%v err=%v", ok, err)
			}
		}()
	}
	<-platform.entered
	close(platform.release)
	workers.Wait()
	if platform.calls.Load() != 1 {
		t.Fatalf("made %d concurrent login attempts, want 1", platform.calls.Load())
	}
}

func TestPasswordReloginDoesNotReuseAnEarlierRevokedSuccess(t *testing.T) {
	store := newFakeSessionStore()
	record := rejectedPasswordSession()
	store.set("user-1", "account-1", record)
	platform := &fakePlatformClient{
		verifyAdminErr:          errors.New("revoked"),
		loginSub2APIAdminResult: &upstream.Session{Platform: upstream.PlatformSub2API, BaseURL: record.BaseURL, AccessToken: "first-token"},
	}
	s := NewService(store, platform)
	if _, ok, err := s.reloginWithStoredPassword(context.Background(), "user-1", "account-1", &record); err != nil || !ok {
		t.Fatalf("initial recovery failed: ok=%v err=%v", ok, err)
	}
	record.Session.AccessToken = "another-revoked-token"
	store.set("user-1", "account-1", record)
	platform.loginSub2APIAdminResult.AccessToken = "second-token"
	got, ok, err := s.reloginWithStoredPassword(context.Background(), "user-1", "account-1", &record)
	if err != nil || !ok || got.Session.AccessToken != "second-token" || platform.loginSub2APIAdminCalls != 2 {
		t.Fatalf("returned an earlier success: ok=%v err=%v calls=%d", ok, err, platform.loginSub2APIAdminCalls)
	}
}

func TestPasswordReloginRetriesAfterCooldownAndRespectsClearedPassword(t *testing.T) {
	store := newFakeSessionStore()
	record := rejectedPasswordSession()
	store.set("user-1", "account-1", record)
	platform := &fakePlatformClient{loginSub2APIAdminErr: errors.New("temporary failure")}
	s := NewService(store, platform)
	s.reloginWithStoredPassword(context.Background(), "user-1", "account-1", &record)
	s.reloginAttempts["user-1\x00account-1"].retryAfter = time.Now().Add(-time.Second)
	s.reloginWithStoredPassword(context.Background(), "user-1", "account-1", &record)
	if platform.loginSub2APIAdminCalls != 2 {
		t.Fatal("did not retry after cooldown expired")
	}
	cleared := record
	cleared.Password = ""
	store.set("user-1", "account-1", cleared)
	s.reloginWithStoredPassword(context.Background(), "user-1", "account-1", &record)
	if platform.loginSub2APIAdminCalls != 2 {
		t.Fatal("used a stale password after the saved password was cleared")
	}
}
