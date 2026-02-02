// Copyright © 2025 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package bootscript

import (
	"context"
	"log"

	"github.com/openchami/boot-service/pkg/client"
	"github.com/openchami/boot-service/pkg/clients/hsm"
	"github.com/openchami/boot-service/pkg/clients/local"
	"github.com/openchami/boot-service/pkg/resources/node"
)

// NodeProvider interface for different node resolution backends
type NodeProvider interface {
	ResolveNodeByIdentifier(ctx context.Context, identifier string) (*node.Node, error)
	HealthCheck(ctx context.Context) error
	GetStats(ctx context.Context) map[string]interface{}
}

// SyncProvider interface for providers that support background synchronization
type SyncProvider interface {
	NodeProvider
	StartSyncWorker(ctx context.Context)
}

// FlexibleBootScriptController provides boot script generation with pluggable node providers
type FlexibleBootScriptController struct {
	*BootScriptController
	nodeProvider NodeProvider
	syncProvider SyncProvider // Optional - only set if provider supports sync
	providerType string
	logger       *log.Logger
}

// ProviderConfig holds configuration for different provider types
type ProviderConfig struct {
	Type       string                   `yaml:"type"` // "hsm" or "yaml"
	HSMConfig  *hsm.IntegrationConfig   `yaml:"hsm_config,omitempty"`
	YAMLConfig *local.IntegrationConfig `yaml:"yaml_config,omitempty"`
}

// NewFlexibleBootScriptController creates a controller with the specified provider
func NewFlexibleBootScriptController(bootClient client.Client, config ProviderConfig, logger *log.Logger) (*FlexibleBootScriptController, error) {
	// Create base controller
	baseController := NewBootScriptController(bootClient, logger)

	controller := &FlexibleBootScriptController{
		BootScriptController: baseController,
		providerType:         config.Type,
		logger:               logger,
	}

	// Initialize the specified provider
	switch config.Type {
	case "hsm":
		if config.HSMConfig == nil {
			logger.Printf("No HSM config provided, using default")
			defaultConfig := hsm.DefaultIntegrationConfig()
			config.HSMConfig = &defaultConfig
		}

		hsmIntegration := hsm.NewIntegrationService(*config.HSMConfig, bootClient, logger)
		controller.nodeProvider = hsmIntegration
		controller.syncProvider = hsmIntegration
		logger.Printf("Initialized with HSM provider")

	case "yaml":
		if config.YAMLConfig == nil {
			logger.Printf("No YAML config provided, using default")
			defaultConfig := local.DefaultIntegrationConfig()
			config.YAMLConfig = &defaultConfig
		}

		yamlIntegration, err := local.NewIntegrationService(*config.YAMLConfig, bootClient, logger)
		if err != nil {
			return nil, err
		}
		controller.nodeProvider = yamlIntegration
		if config.YAMLConfig.SyncEnabled {
			controller.syncProvider = yamlIntegration
		}
		logger.Printf("Initialized with YAML provider from file: %s", config.YAMLConfig.YAMLFile)

	default:
		logger.Printf("Unknown provider type: %s, using basic controller only", config.Type)
	}

	return controller, nil
}

// GenerateBootScriptWithFallback generates a boot script with external provider fallback
func (c *FlexibleBootScriptController) GenerateBootScriptWithFallback(ctx context.Context, identifier string) (string, error) {
	c.logger.Printf("Generating boot script for identifier: %s (provider: %s)", identifier, c.providerType)

	// First try the standard resolution
	script, err := c.GenerateBootScript(ctx, identifier, "")
	if err == nil {
		return script, nil
	}

	// If no external provider is configured, return minimal script
	if c.nodeProvider == nil {
		c.logger.Printf("Standard resolution failed for %s, no external provider configured: %v", identifier, err)
		return c.generateMinimalScript(identifier), nil
	}

	c.logger.Printf("Standard resolution failed for %s, trying %s provider: %v", identifier, c.providerType, err)

	// Try external provider resolution
	node, err := c.nodeProvider.ResolveNodeByIdentifier(ctx, identifier)
	if err != nil {
		c.logger.Printf("%s provider fallback also failed for %s: %v", c.providerType, identifier, err)
		// Return minimal script as final fallback
		return c.generateMinimalScript(identifier), nil
	}

	c.logger.Printf("%s provider resolved node %s for identifier %s", c.providerType, node.Spec.XName, identifier)

	// Now try to generate script with the resolved node
	script, err = c.GenerateBootScript(ctx, node.Spec.XName, "")
	if err != nil {
		c.logger.Printf("Failed to generate script for %s-resolved node %s: %v", c.providerType, node.Spec.XName, err)
		return c.generateMinimalScript(identifier), nil
	}

	return script, nil
}

// StartBackgroundSync starts background synchronization if the provider supports it
func (c *FlexibleBootScriptController) StartBackgroundSync(ctx context.Context) {
	if c.syncProvider == nil {
		c.logger.Printf("Provider %s does not support background sync", c.providerType)
		return
	}

	c.logger.Printf("Starting background sync with %s provider", c.providerType)
	c.syncProvider.StartSyncWorker(ctx)
}

// GetProviderStats returns statistics from the current provider
func (c *FlexibleBootScriptController) GetProviderStats(ctx context.Context) map[string]interface{} {
	if c.nodeProvider == nil {
		return map[string]interface{}{
			"provider_type":       c.providerType,
			"provider_configured": false,
		}
	}

	stats := c.nodeProvider.GetStats(ctx)
	stats["provider_type"] = c.providerType
	stats["provider_configured"] = true
	stats["sync_supported"] = c.syncProvider != nil

	return stats
}

// HealthCheck performs comprehensive health checks including the external provider
func (c *FlexibleBootScriptController) HealthCheck(ctx context.Context) error {
	if c.nodeProvider == nil {
		return nil // No external provider to check
	}

	return c.nodeProvider.HealthCheck(ctx)
}

// GetProviderType returns the configured provider type
func (c *FlexibleBootScriptController) GetProviderType() string {
	return c.providerType
}

// NewHSMController creates a controller specifically configured for HSM
func NewHSMController(bootClient client.Client, hsmConfig hsm.IntegrationConfig, logger *log.Logger) *FlexibleBootScriptController {
	config := ProviderConfig{
		Type:      "hsm",
		HSMConfig: &hsmConfig,
	}

	controller, err := NewFlexibleBootScriptController(bootClient, config, logger)
	if err != nil {
		logger.Printf("Failed to create HSM controller: %v", err)
		return nil
	}

	return controller
}

// NewYAMLController creates a controller specifically configured for YAML
func NewYAMLController(bootClient client.Client, yamlConfig local.IntegrationConfig, logger *log.Logger) *FlexibleBootScriptController {
	config := ProviderConfig{
		Type:       "yaml",
		YAMLConfig: &yamlConfig,
	}

	controller, err := NewFlexibleBootScriptController(bootClient, config, logger)
	if err != nil {
		logger.Printf("Failed to create YAML controller: %v", err)
		return nil
	}

	return controller
}
