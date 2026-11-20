package api

import (
	"yomifolio/internal/config"
	"yomifolio/internal/metadata"
)

// ExternalSources replaces only external client creation, before the process starts.
// Factories must be safe for concurrent requests. Nil factories retain production defaults.
type ExternalSources struct {
	// Metadata serves source-keyed fetch/search, including LLM metadata aliases.
	Metadata func(config.Config, string) metadata.Provider
	// AI serves grouping, recommendations and connection tests with their resolved inputs.
	AI func(provider, apiMode, baseURL, requestPath, model, apiKey string, timeout int) metadata.AIProvider
}

func (s ExternalSources) metadata(cfg config.Config, name string) metadata.Provider {
	if s.Metadata != nil {
		return s.Metadata(cfg, name)
	}
	return metadata.NewConfiguredProvider(cfg, name)
}

func (s ExternalSources) ai(provider, apiMode, baseURL, requestPath, model, apiKey string, timeout int) metadata.AIProvider {
	if s.AI != nil {
		return s.AI(provider, apiMode, baseURL, requestPath, model, apiKey, timeout)
	}
	return metadata.NewAIProvider(provider, apiMode, baseURL, requestPath, model, apiKey, timeout)
}
func (s ExternalSources) configuredAI(cfg config.Config) metadata.AIProvider {
	return s.ai(cfg.LLM.Provider, cfg.LLM.APIMode, cfg.LLM.BaseURL, cfg.LLM.RequestPath, cfg.LLM.Model, cfg.LLM.APIKey, cfg.LLM.Timeout)
}
