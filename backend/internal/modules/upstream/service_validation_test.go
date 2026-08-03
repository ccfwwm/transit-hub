package upstream

import (
	"strings"
	"testing"
)

func TestValidateCreateAllowsNewAPISystemAccessToken(t *testing.T) {
	dto := CreateRequest{
		Name:         "New-API token site",
		SiteURL:      "https://example.com",
		Platform:     PlatformNewAPI,
		AuthMode:     AuthModeToken,
		Account:      "935",
		AccessToken:  "system-access-token",
		RechargeRate: 1,
	}

	if err := validateCreate(dto); err != nil {
		t.Fatalf("validateCreate rejected a New-API system access token: %v", err)
	}
}

func TestValidateUpdateAllowsNewAPISystemAccessToken(t *testing.T) {
	dto := UpdateRequest{
		Name:         "New-API token site",
		SiteURL:      "https://example.com",
		Platform:     PlatformNewAPI,
		AuthMode:     AuthModeToken,
		Account:      "935",
		AccessToken:  "system-access-token",
		RechargeRate: 1,
	}

	if err := validateUpdate(dto); err != nil {
		t.Fatalf("validateUpdate rejected a New-API system access token: %v", err)
	}
}

func TestValidateNewAPISystemAccessTokenRequiresUserID(t *testing.T) {
	createDTO := CreateRequest{
		Name:         "New-API token site",
		SiteURL:      "https://example.com",
		Platform:     PlatformNewAPI,
		AuthMode:     AuthModeToken,
		AccessToken:  "system-access-token",
		RechargeRate: 1,
	}
	if err := validateCreate(createDTO); err == nil || !strings.Contains(err.Error(), "account") {
		t.Fatalf("validateCreate did not report the missing New-API user ID: %v", err)
	}

	updateDTO := UpdateRequest{
		Name:         createDTO.Name,
		SiteURL:      createDTO.SiteURL,
		Platform:     createDTO.Platform,
		AuthMode:     createDTO.AuthMode,
		AccessToken:  createDTO.AccessToken,
		RechargeRate: createDTO.RechargeRate,
	}
	if err := validateUpdate(updateDTO); err == nil || !strings.Contains(err.Error(), "account") {
		t.Fatalf("validateUpdate did not report the missing New-API user ID: %v", err)
	}
}
