package channel_monitor

// Dispatch state is captured before the display source replaces model health.
// A public healthy report must not hide why an account was stopped.
func dispatchState(row ChannelStatus) string {
	if row.Schedulable == nil {
		return "unknown"
	}
	if *row.Schedulable {
		return "enabled"
	}
	if row.ManualPaused || row.SchedulableConflict || !row.SchedulableManaged && row.AutoEnableBlocked {
		return "manual"
	}
	if row.Status == StatusBalancePaused {
		return "balance"
	}
	if row.RateGateStatus == RateGateBlocked || row.RateGateStatus == RateGateMissing {
		return "rate"
	}
	if row.Status == StatusAutoPaused || row.SchedulableManaged && (row.Status == StatusFailed || row.AutoEnableBlocked) {
		return "auto"
	}
	return "disabled"
}
