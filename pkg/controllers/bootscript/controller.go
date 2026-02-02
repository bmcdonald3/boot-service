// Copyright © 2025 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

// Package bootscript handles iPXE boot script generation for nodes
package bootscript

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/openchami/boot-service/pkg/client"
	"github.com/openchami/boot-service/pkg/resources/bootconfiguration"
	"github.com/openchami/boot-service/pkg/resources/node"
	"github.com/openchami/boot-service/pkg/validation"
)

// BootScriptController handles iPXE boot script generation
type BootScriptController struct { //nolint:revive
	client client.Client
	logger *log.Logger
	cache  *ScriptCache
}

// NewBootScriptController creates a new controller instance
func NewBootScriptController(client client.Client, logger *log.Logger) *BootScriptController {
	return &BootScriptController{
		client: client,
		logger: logger,
		cache:  NewScriptCache(5 * time.Minute), // 5 minute cache
	}
}

// NodeIdentifier represents different ways to identify a node
type NodeIdentifier struct {
	Value string
	Type  IdentifierType
}

// IdentifierType represents the type of node identifier
type IdentifierType int

// Different identifier types
const (
	IdentifierXName IdentifierType = iota
	IdentifierNID
	IdentifierMAC
	IdentifierUnknown
)

// GenerateBootScript generates an iPXE boot script for a node
func (c *BootScriptController) GenerateBootScript(ctx context.Context, identifier string, profile string) (string, error) {
	c.logger.Printf("Generating boot script for identifier: %s", identifier)

	// Check cache first
	cacheKey := c.generateCacheKey(identifier, profile)
	if cached, found := c.cache.Get(cacheKey); found {
		c.logger.Printf("Cache hit for identifier: %s", identifier)
		return cached, nil
	}

	// Parse and resolve node identifier
	nodeID := c.parseNodeIdentifier(identifier)
	node, err := c.resolveNode(ctx, nodeID)
	if err != nil {
		return c.generateErrorScript(fmt.Sprintf("Node resolution failed: %v", err)), nil
	}

	// Find best matching configuration
	config, err := c.findBootConfiguration(ctx, node, profile)
	if err != nil {
		c.logger.Printf("No configuration found for node %s: %v", node.Spec.XName, err)
		// Return minimal script for nodes without configuration
		return c.generateMinimalScript(identifier), nil
	}

	// Generate iPXE script
	script, err := c.buildIPXEScript(config, node)
	if err != nil {
		return c.generateErrorScript(fmt.Sprintf("Script generation failed: %v", err)), nil
	}

	// Cache the result
	configName := ""
	if config != nil {
		configName = config.GetName()
	}
	cacheKey = c.generateCacheKey(identifier, configName)
	c.cache.Set(cacheKey, script, node.Spec.XName, configName)

	c.logger.Printf("Generated boot script for node %s using config %s", node.Spec.XName, configName)
	return script, nil
}

// parseNodeIdentifier determines what type of identifier we're dealing with
func (c *BootScriptController) parseNodeIdentifier(identifier string) NodeIdentifier {
	// Check if it's an XName (format: x<cabinet>c<chassis>s<slot>b<blade>n<node>)
	if validation.ValidateXName(identifier) {
		return NodeIdentifier{Value: identifier, Type: IdentifierXName}
	}

	// Check if it's a numeric NID
	if nid, err := strconv.Atoi(identifier); err == nil && nid >= 0 {
		return NodeIdentifier{Value: identifier, Type: IdentifierNID}
	}

	// Check if it's a MAC address
	if validation.ValidateMAC(identifier) {
		return NodeIdentifier{Value: identifier, Type: IdentifierMAC}
	}

	return NodeIdentifier{Value: identifier, Type: IdentifierUnknown}
}

// resolveNode finds a node based on the identifier
func (c *BootScriptController) resolveNode(ctx context.Context, identifier NodeIdentifier) (*node.Node, error) {
	// Get all nodes
	nodes, err := c.client.GetNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting nodes: %w", err)
	}

	// Search for matching node
	for _, nodeItem := range nodes {
		switch identifier.Type {
		case IdentifierXName:
			if nodeItem.Spec.XName == identifier.Value {
				return &nodeItem, nil
			}
		case IdentifierNID:
			nid, _ := strconv.Atoi(identifier.Value)
			if int(nodeItem.Spec.NID) == nid {
				return &nodeItem, nil
			}
		case IdentifierMAC:
			if strings.EqualFold(nodeItem.Spec.BootMAC, identifier.Value) {
				return &nodeItem, nil
			}
		}
	}

	return nil, fmt.Errorf("node not found for identifier %s", identifier.Value)
}

// findBootConfiguration finds the best matching configuration for a node
func (c *BootScriptController) findBootConfiguration(ctx context.Context, node *node.Node, profile string) (*bootconfiguration.BootConfiguration, error) {
    // Get all boot configurations
    configs, err := c.client.GetBootConfigurations(ctx)
    if err != nil {
        return nil, fmt.Errorf("getting boot configurations: %w", err)
    }

    // Helper to score candidates for a specific profile
    findBestCandidate := func(targetProfile string) *bootconfiguration.BootConfiguration {
        var candidates []configCandidate
        
        for _, configItem := range configs {
            // FILTER: Only consider configs matching the requested profile
            // Treat empty profile in config as "default"
            configProfile := configItem.Spec.Profile
            if configProfile == "" {
                configProfile = "default"
            }
            
            // Normalize target
            effectiveTarget := targetProfile
            if effectiveTarget == "" {
                effectiveTarget = "default"
            }

            if configProfile != effectiveTarget {
                continue
            }

            score := c.calculateConfigScore(&configItem, node)
            if score > 0 {
                candidates = append(candidates, configCandidate{config: &configItem, score: score})
            }
        }

        if len(candidates) == 0 {
            return nil
        }

        // Sort by score (descending) and priority (descending)
        sort.Slice(candidates, func(i, j int) bool {
            if candidates[i].score != candidates[j].score {
                return candidates[i].score > candidates[j].score
            }
            return candidates[i].config.Spec.Priority > candidates[j].config.Spec.Priority
        })

        return candidates[0].config
    }

    // 1. Try to find match for requested profile
    if profile != "" && profile != "default" {
        if match := findBestCandidate(profile); match != nil {
            return match, nil
        }
        c.logger.Printf("No config found for profile '%s', falling back to default", profile)
    }

    // 2. Fallback to default profile
    if match := findBestCandidate("default"); match != nil {
        return match, nil
    }

    return nil, fmt.Errorf("no matching configurations found for node %s", node.Spec.XName)
}

// calculateConfigScore determines how well a configuration matches a node
func (c *BootScriptController) calculateConfigScore(config *bootconfiguration.BootConfiguration, node *node.Node) int {
	score := 0

	// Host/XName pattern matching
	for _, host := range config.Spec.Hosts {
		if c.matchesPattern(host, node.Spec.XName) || c.matchesPattern(host, node.Spec.Hostname) {
			score += 50
		}
	}

	// MAC address matching
	for _, mac := range config.Spec.MACs {
		if strings.EqualFold(mac, node.Spec.BootMAC) {
			score += 100 // Exact MAC match is highest priority
		}
	}

	// NID matching
	for _, nid := range config.Spec.NIDs {
		if nid == node.Spec.NID {
			score += 75
		}
	}

	// Group matching
	for _, configGroup := range config.Spec.Groups {
		for _, nodeGroup := range node.Spec.Groups {
			if configGroup == nodeGroup {
				score += 25
			}
		}
	}

	// Base score for any configuration (fallback)
	if score == 0 && len(config.Spec.Hosts) == 0 && len(config.Spec.MACs) == 0 &&
		len(config.Spec.NIDs) == 0 && len(config.Spec.Groups) == 0 {
		score = 1 // Default/catch-all configuration
	}

	return score
}

// matchesPattern checks if a pattern matches a value (supports wildcards)
func (c *BootScriptController) matchesPattern(pattern, value string) bool {
	// Simple pattern matching - could be enhanced with regex later
	if pattern == "*" {
		return true
	}
	if pattern == value {
		return true
	}
	// TODO: Add more sophisticated pattern matching if needed
	return false
}

// generateMinimalScript creates a minimal iPXE script for nodes without configuration
func (c *BootScriptController) generateMinimalScript(identifier string) string {
	// Use a simple string replacement for the minimal template
	script := MinimalIPXETemplate
	script = strings.ReplaceAll(script, "{{.Identifier}}", identifier)

	return script
}

// generateErrorScript creates an error iPXE script
func (c *BootScriptController) generateErrorScript(errorMsg string) string {
	// Use a simple string replacement for the error template
	script := ErrorIPXETemplate
	script = strings.ReplaceAll(script, "{{.Error}}", errorMsg)

	return script
}
