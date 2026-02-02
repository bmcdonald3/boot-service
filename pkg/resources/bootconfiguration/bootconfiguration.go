// Copyright © 2025 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

// Package bootconfiguration defines the BootConfiguration resource
package bootconfiguration

import (
	"context"
	"errors"

	"github.com/openchami/boot-service/pkg/validation"
	"github.com/openchami/fabrica/pkg/resource"
)

// BootConfiguration represents a BootConfiguration resource
type BootConfiguration struct {
	resource.Resource
	Spec   BootConfigurationSpec   `json:"spec"`
	Status BootConfigurationStatus `json:"status,omitempty"`
}

// BootConfigurationSpec defines the desired state of BootConfiguration
type BootConfigurationSpec struct { // nolint:revive
	// Node identification (one or more required)
	Hosts  []string `json:"hosts,omitempty"`
	MACs   []string `json:"macs,omitempty"`
	NIDs   []int32  `json:"nids,omitempty"`
	Groups []string `json:"groups,omitempty"` // Support for inventory service groups

	Profile string `json:"profile,omitempty"`

	// Boot configuration (kernel required)
	Kernel string `json:"kernel"`
	Initrd string `json:"initrd,omitempty"`
	Params string `json:"params,omitempty"`

	// Priority for conflict resolution
	Priority int `json:"priority,omitempty"`
}

// BootConfigurationStatus defines the observed state of BootConfiguration
type BootConfigurationStatus struct { // nolint:revive
	Phase       string   `json:"phase,omitempty"`       // Active, Pending, Failed
	LastUpdated string   `json:"lastUpdated,omitempty"` // RFC3339 timestamp
	AppliedTo   []string `json:"appliedTo,omitempty"`   // List of nodes using this config
	Error       string   `json:"error,omitempty"`       // Error message if any
}

// Validate implements custom validation logic for BootConfiguration
func (r *BootConfiguration) Validate(ctx context.Context) error { //nolint:revive
	// Kernel is required
	if r.Spec.Kernel == "" {
		return errors.New("kernel field is required")
	}

	// Validate hosts using XName format
	for _, host := range r.Spec.Hosts {
		if !validation.ValidateXNameOrDefault(host) {
			return errors.New("invalid host XName format: " + host)
		}
	}

	// Validate MAC addresses
	for _, mac := range r.Spec.MACs {
		if !validation.ValidateMAC(mac) {
			return errors.New("invalid MAC address format: " + mac)
		}
	}

	// Validate kernel URL/path
	if !validation.ValidateURLOrPath(r.Spec.Kernel) {
		return errors.New("invalid kernel URL or path: " + r.Spec.Kernel)
	}

	// Validate initrd URL/path if provided
	if r.Spec.Initrd != "" && !validation.ValidateURLOrPathOptional(r.Spec.Initrd) {
		return errors.New("invalid initrd URL or path: " + r.Spec.Initrd)
	}

	// Validate priority range
	if r.Spec.Priority < 0 || r.Spec.Priority > 100 {
		return errors.New("priority must be between 0 and 100")
	}

	// Ensure at least one targeting method is specified
	if len(r.Spec.Hosts) == 0 && len(r.Spec.MACs) == 0 && len(r.Spec.NIDs) == 0 && len(r.Spec.Groups) == 0 {
		return errors.New("at least one targeting method (hosts, macs, nids, or groups) must be specified")
	}

	return nil
}

func init() {
	// Register resource type prefix for storage
	resource.RegisterResourcePrefix("BootConfiguration", "boo")
}
