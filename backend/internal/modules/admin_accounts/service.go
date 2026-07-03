package admin_accounts

import (
	"context"
	"strings"
)

type Service struct{ repository *Repository }

func NewService(repository *Repository) *Service { return &Service{repository: repository} }

func (s *Service) EnsureSchema(ctx context.Context) error { return s.repository.EnsureSchema(ctx) }

func (s *Service) List(ctx context.Context, userID string) ([]Account, error) {
	return s.repository.List(ctx, userID)
}

func (s *Service) Current(ctx context.Context, userID string) (*Account, error) {
	return s.repository.Current(ctx, userID)
}

func (s *Service) CurrentID(ctx context.Context, userID string) (string, error) {
	return s.repository.CurrentID(ctx, userID)
}

func (s *Service) RequireCurrentID(ctx context.Context, userID string) (string, error) {
	id, err := s.CurrentID(ctx, userID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(id) == "" {
		return "", requestError(ErrorNoCurrentAccount)
	}
	return id, nil
}

func (s *Service) UpsertAndSwitch(ctx context.Context, userID string, input UpsertInput) (Account, error) {
	input.Platform = strings.TrimSpace(input.Platform)
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.Identity = strings.TrimSpace(input.Identity)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.AuthMethod = strings.TrimSpace(input.AuthMethod)
	if input.Platform == "" || input.Identity == "" {
		return Account{}, requestError(ErrorRequest)
	}
	return s.repository.UpsertAndSwitch(ctx, userID, input)
}

func (s *Service) Switch(ctx context.Context, userID string, accountID string) (*Account, error) {
	account, err := s.repository.Switch(ctx, userID, strings.TrimSpace(accountID))
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, requestError(ErrorNotFound)
	}
	return account, nil
}

func (s *Service) Update(ctx context.Context, userID string, accountID string, req UpdateRequest) (*Account, error) {
	account, err := s.repository.Update(ctx, userID, strings.TrimSpace(accountID), req.DisplayName)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, requestError(ErrorNotFound)
	}
	return account, nil
}
