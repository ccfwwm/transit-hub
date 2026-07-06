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
