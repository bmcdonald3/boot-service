// Copyright © 2025 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

package bootscript

import (
	"context"
	"log"

	"github.com/openchami/boot-service/pkg/client"
	"github.com/openchami/boot-service/pkg/clients/hsm"
)

// EnhancedBootScriptController extends the basic controller with HSM integration
type EnhancedBootScriptController struct {
	*BootScriptController
	hsmIntegration *hsm.IntegrationService
}

// NewEnhancedBootScriptController creates a new enhanced controller with HSM integration
func NewEnhancedBootScriptController(bootClient client.Client, hsmConfig hsm.IntegrationConfig, logger *log.Logger) *EnhancedBootScriptController {
	// Create base controller
	baseController := NewBootScriptController(bootClient, logger)

	// Create HSM integration service
	hsmIntegration := hsm.NewIntegrationService(hsmConfig, bootClient, logger)

	return &EnhancedBootScriptController{
		BootScriptController: baseController,
		hsmIntegration:       hsmIntegration,
	}
}

// GenerateBootScriptWithHSM generates a boot script using HSM for node resolution if needed
func (c *EnhancedBootScriptController) GenerateBootScriptWithHSM(ctx context.Context, identifier string) (string, error) {
	c.logger.Printf("Generating boot script for identifier: %s (with HSM fallback)", identifier)

	// First try the standard resolution
	script, err := c.GenerateBootScript(ctx, identifier, "")
	if err == nil {
		return script, nil
	}

	c.logger.Printf("Standard resolution failed for %s, trying HSM fallback: %v", identifier, err)

	// If standard resolution fails, try HSM-enhanced resolution
	node, err := c.hsmIntegration.ResolveNodeByIdentifier(ctx, identifier)
	if err != nil {
		c.logger.Printf("HSM fallback also failed for %s: %v", identifier, err)
		// Return minimal script as final fallback
		return c.generateMinimalScript(identifier), nil
	}

	c.logger.Printf("HSM resolved node %s for identifier %s", node.Spec.XName, identifier)

	// Now try to generate script with the HSM-resolved node
	script, err = c.GenerateBootScript(ctx, node.Spec.XName, "")
	if err != nil {
		c.logger.Printf("Failed to generate script for HSM-resolved node %s: %v", node.Spec.XName, err)
		return c.generateMinimalScript(identifier), nil
	}

	return script, nil
}

// StartHSMSync starts the HSM synchronization worker
func (c *EnhancedBootScriptController) StartHSMSync(ctx context.Context) {
	c.hsmIntegration.StartSyncWorker(ctx)
}

// SyncFromHSM manually triggers HSM synchronization
func (c *EnhancedBootScriptController) SyncFromHSM(ctx context.Context) error {
	return c.hsmIntegration.SyncNodesFromHSM(ctx)
}

// GetHSMStats returns HSM integration statistics
func (c *EnhancedBootScriptController) GetHSMStats(ctx context.Context) (map[string]interface{}, error) {
	return c.hsmIntegration.GetHSMStats(ctx)
}

// HealthCheck performs comprehensive health checks including HSM
func (c *EnhancedBootScriptController) HealthCheck(ctx context.Context) error {
	return c.hsmIntegration.HealthCheck(ctx)
}
