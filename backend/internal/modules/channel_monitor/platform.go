package channel_monitor

import "transithub/backend/internal/modules/upstream"

type PlatformAdapter struct {
	platform *upstream.PlatformService
}

func NewPlatformAdapter(platform *upstream.PlatformService) PlatformAdapter {
	return PlatformAdapter{platform: platform}
}

func (a PlatformAdapter) TestSub2APIAdminAccount(session upstream.Session, accountID string, options AccountTestOptions) (AccountTestResult, error) {
	result, err := a.platform.TestSub2APIAdminAccount(session, accountID, upstream.Sub2APIAccountTestOptions{
		ModelID: options.ModelID,
		Prompt:  options.Prompt,
	})
	if err != nil {
		return AccountTestResult{}, err
	}
	return AccountTestResult{
		Success:   result.Success,
		Message:   result.Message,
		LatencyMS: result.LatencyMS,
		Model:     result.Model,
	}, nil
}

func (a PlatformAdapter) SetSub2APIAdminAccountSchedulable(session upstream.Session, accountID string, schedulable bool) error {
	return a.platform.SetSub2APIAdminAccountSchedulable(session, accountID, schedulable)
}

func (a PlatformAdapter) ListSub2APIAdminAccounts(session upstream.Session) ([]AdminAccountStatus, error) {
	accounts, err := a.platform.ListSub2APIAdminAccounts(session)
	if err != nil {
		return nil, err
	}
	statuses := make([]AdminAccountStatus, 0, len(accounts))
	for _, account := range accounts {
		statuses = append(statuses, AdminAccountStatus{
			ID:             account.ID,
			Name:           account.Name,
			Schedulable:    account.Schedulable,
			RateMultiplier: account.RateMultiplier,
			Priority:       account.Priority,
		})
	}
	return statuses, nil
}

func (a PlatformAdapter) UpdateSub2APIAdminAccountPriority(session upstream.Session, accountID string, priority int) error {
	return a.platform.UpdateSub2APIAdminAccountPriority(session, accountID, priority)
}
