package dashboard

import (
	"context"
	"crypto/sha256"
	"strings"
	"time"
)

const passwordReloginCooldown = 2 * time.Minute

type passwordReloginAttempt struct {
	credential [32]byte
	done       chan struct{}
	finished   bool
	retryAfter time.Time
	record     *AdminSession
	ok         bool
	err        error
}

// Coalesce channel workers recovering the same workspace. A rejected saved
// password must not be retried once per channel or on every dashboard poll.
func (s *Service) reloginWithStoredPassword(ctx context.Context, userID, adminAccountID string, record *AdminSession) (*AdminSession, bool, error) {
	if record == nil || record.AuthMethod != AuthMethodPassword || strings.TrimSpace(record.Identity) == "" || record.Password == "" {
		return record, false, nil
	}
	latest, err := s.store.Get(ctx, userID, adminAccountID)
	if err != nil {
		return record, false, err
	}
	if latest == nil || latest.AuthMethod != AuthMethodPassword || latest.Password == "" {
		return record, false, nil
	}
	// An explicit login or another worker may already have replaced this token.
	if !sessionEqual(latest.Session, record.Session) {
		if err := s.platform.VerifyAdmin(latest.Session); err == nil {
			return latest, true, nil
		}
	}
	record = latest
	credential := sha256.Sum256([]byte(record.Platform + "\x00" + record.BaseURL + "\x00" + record.Identity + "\x00" + record.Password))
	key := userID + "\x00" + adminAccountID
	s.reloginMu.Lock()
	if previous := s.reloginAttempts[key]; previous != nil && previous.credential == credential {
		if !previous.finished {
			s.reloginMu.Unlock()
			select {
			case <-ctx.Done():
				return record, false, ctx.Err()
			case <-previous.done:
				return previous.record, previous.ok, previous.err
			}
		}
		if !previous.ok && time.Now().Before(previous.retryAfter) {
			s.reloginMu.Unlock()
			return record, false, nil
		}
	}
	if s.reloginAttempts == nil {
		s.reloginAttempts = make(map[string]*passwordReloginAttempt)
	}
	attempt := &passwordReloginAttempt{credential: credential, done: make(chan struct{})}
	s.reloginAttempts[key] = attempt
	s.reloginMu.Unlock()

	attempt.record, attempt.ok, attempt.err = s.recoverWithLatestPassword(ctx, userID, adminAccountID, record)
	s.reloginMu.Lock()
	attempt.finished = true
	attempt.retryAfter = time.Now().Add(passwordReloginCooldown)
	close(attempt.done)
	s.reloginMu.Unlock()
	return attempt.record, attempt.ok, attempt.err
}

func (s *Service) recoverWithLatestPassword(ctx context.Context, userID, adminAccountID string, record *AdminSession) (*AdminSession, bool, error) {
	// A worker can read the old session just before the previous attempt saves its
	// result. Recheck after joining the serialized recovery path, rather than
	// reusing a cached successful token that may since have expired or been revoked.
	latest, err := s.store.Get(ctx, userID, adminAccountID)
	if err != nil {
		return record, false, err
	}
	if latest == nil || latest.AuthMethod != AuthMethodPassword || latest.Password == "" {
		return record, false, nil
	}
	if !sessionEqual(latest.Session, record.Session) {
		if err := s.platform.VerifyAdmin(latest.Session); err == nil {
			return latest, true, nil
		}
	}
	return s.performPasswordRelogin(ctx, userID, adminAccountID, latest)
}
