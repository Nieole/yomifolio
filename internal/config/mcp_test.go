package config_test

import (
	"testing"
	"yomifolio/internal/config"
)

func TestMCPConfigurationRequiresExplicitPrivatePlaintext(t *testing.T) {
	for _, tc := range []struct {
		url          string
		allow, valid bool
	}{
		{"", false, true}, {"https://library.example", false, true}, {"http://127.0.0.1:8080", true, true}, {"http://[::1]:8080", true, true}, {"http://192.168.1.3:8080", true, true},
		{"http://127.0.0.1:8080", false, false}, {"http://library.example", true, false}, {"http://8.8.8.8", true, false}, {"https://user:password@library.example", false, false}, {"https://library.example/path", false, false}, {"https://library.example?token=secret", false, false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			cfg := config.Config{}
			config.NormalizeConfig(&cfg)
			cfg.MCP.PublicURL = tc.url
			cfg.MCP.AllowLANHTTP = tc.allow
			result := config.ValidateConfigValues(&cfg)
			found := false
			for _, issue := range result.Issues {
				if issue.Field == "mcp.public_url" {
					found = true
				}
			}
			if found == tc.valid {
				t.Fatalf("valid=%v issues=%+v", tc.valid, result.Issues)
			}
		})
	}
}
