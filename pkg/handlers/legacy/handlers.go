// Copyright © 2025 OpenCHAMI a Series of LF Projects, LLC
//
// SPDX-License-Identifier: MIT

// Package legacy provides legacy BSS API handlers for backward compatibility
package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/openchami/boot-service/pkg/client"
	"github.com/openchami/boot-service/pkg/controllers/bootscript"
	"github.com/openchami/boot-service/pkg/resources/bootconfiguration"
)

// BootController interface for boot script generation
type BootController interface {
	GenerateBootScript(ctx context.Context, identifier string, profile string) (string, error)
}

// LegacyHandler handles legacy BSS API requests
type LegacyHandler struct { //nolint:revive
	client     client.Client
	controller BootController
	logger     *log.Logger
}

// NewLegacyHandler creates a new legacy API handler with standard controller
func NewLegacyHandler(c client.Client, logger *log.Logger) *LegacyHandler {
	controller := bootscript.NewBootScriptController(c, logger)
	return &LegacyHandler{
		client:     c,
		controller: controller,
		logger:     logger,
	}
}

// NewLegacyHandlerWithController creates a new legacy API handler with a custom controller
func NewLegacyHandlerWithController(c client.Client, controller BootController, logger *log.Logger) *LegacyHandler {
	return &LegacyHandler{
		client:     c,
		controller: controller,
		logger:     logger,
	}
}

// RegisterRoutes registers legacy BSS API routes
func (h *LegacyHandler) RegisterRoutes(r chi.Router) {
	r.Route("/boot/v1", func(r chi.Router) {
		// Boot parameters endpoints
		r.Route("/bootparameters", func(r chi.Router) {
			r.Get("/", h.GetBootParameters)
			r.Post("/", h.CreateBootParameters)
			r.Put("/", h.UpdateBootParameters)
			r.Delete("/", h.DeleteBootParameters)
		})

		// Boot script endpoint
		r.Get("/bootscript", h.GetBootScript)

		// Service endpoints
		r.Route("/service", func(r chi.Router) {
			r.Get("/status", h.GetServiceStatus)
			r.Get("/version", h.GetServiceVersion)
		})
	})
}

// GetBootParameters handles GET /boot/v1/bootparameters
func (h *LegacyHandler) GetBootParameters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters for filtering
	host := r.URL.Query().Get("host")
	mac := r.URL.Query().Get("mac")
	nid := r.URL.Query().Get("nid")
	name := r.URL.Query().Get("name")

	// Get all boot configurations
	configs, err := h.client.GetBootConfigurations(ctx)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to retrieve boot parameters", err.Error())
		return
	}

	// Filter configurations based on query parameters
	var filteredConfigs []bootconfiguration.BootConfiguration
	if host != "" || mac != "" || nid != "" || name != "" {
		identifiers := ParseNodeIdentifiersFromQuery(host, mac, nid, name)
		filteredConfigs = h.filterConfigurationsByIdentifiers(configs, identifiers)
	} else {
		filteredConfigs = configs
	}

	// Convert to legacy format
	var legacyParams []BootParameters
	for _, config := range filteredConfigs {
		legacyParam := ConvertBootConfigurationToLegacy(&config)
		legacyParams = append(legacyParams, legacyParam)
	}

	response := BootParametersResponse{
		BootParameters: legacyParams,
	}

	h.writeJSON(w, http.StatusOK, response)
}

// CreateBootParameters handles POST /boot/v1/bootparameters
func (h *LegacyHandler) CreateBootParameters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req BootParametersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request format", err.Error())
		return
	}

	// Generate a name for the configuration
	name := h.generateConfigName(req)

	// Convert to modern BootConfiguration
	config := ConvertLegacyRequestToBootConfiguration(req)
	config.Metadata.Name = name

	// Create the configuration
	createReq := client.CreateBootConfigurationRequest{
		Name:                  name,
		BootConfigurationSpec: config.Spec,
	}

	createdConfig, err := h.client.CreateBootConfiguration(ctx, createReq)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to create boot parameters", err.Error())
		return
	}

	// Convert back to legacy format and return
	legacyParam := ConvertBootConfigurationToLegacy(createdConfig)
	response := BootParametersResponse{
		BootParameters: []BootParameters{legacyParam},
	}

	h.writeJSON(w, http.StatusCreated, response)
}

// UpdateBootParameters handles PUT /boot/v1/bootparameters
func (h *LegacyHandler) UpdateBootParameters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req BootParametersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid request format", err.Error())
		return
	}

	// For update, we need to find existing configurations that match the identifiers
	// This is a simplified implementation - in a real scenario, you might want more sophisticated matching
	configs, err := h.client.GetBootConfigurations(ctx)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to retrieve existing configurations", err.Error())
		return
	}

	// Find configurations that match any of the provided identifiers
	identifiers := append(req.Hosts, req.Macs...)
	identifiers = append(identifiers, req.Nids...)

	matchingConfigs := h.filterConfigurationsByIdentifiers(configs, identifiers)

	if len(matchingConfigs) == 0 {
		h.writeError(w, http.StatusNotFound, "No matching boot parameters found", "")
		return
	}

	// Update the first matching configuration (simplified approach)
	configToUpdate := matchingConfigs[0]
	updateReq := client.UpdateBootConfigurationRequest{
		BootConfigurationSpec: bootconfiguration.BootConfigurationSpec{
			Hosts:    req.Hosts,
			MACs:     req.Macs,
			Groups:   configToUpdate.Spec.Groups, // Preserve existing groups
			Kernel:   req.Kernel,
			Initrd:   req.Initrd,
			Params:   req.Params,
			Priority: configToUpdate.Spec.Priority, // Preserve existing priority
		},
	}

	// Convert string NIDs to int32
	for _, nidStr := range req.Nids {
		if nid, err := strconv.Atoi(nidStr); err == nil {
			updateReq.NIDs = append(updateReq.NIDs, int32(nid))
		}
	}

	updatedConfig, err := h.client.UpdateBootConfiguration(ctx, configToUpdate.Metadata.UID, updateReq)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to update boot parameters", err.Error())
		return
	}

	// Convert back to legacy format and return
	legacyParam := ConvertBootConfigurationToLegacy(updatedConfig)
	response := BootParametersResponse{
		BootParameters: []BootParameters{legacyParam},
	}

	h.writeJSON(w, http.StatusOK, response)
}

// DeleteBootParameters handles DELETE /boot/v1/bootparameters
func (h *LegacyHandler) DeleteBootParameters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters to identify which configurations to delete
	host := r.URL.Query().Get("host")
	mac := r.URL.Query().Get("mac")
	nid := r.URL.Query().Get("nid")
	name := r.URL.Query().Get("name")

	if host == "" && mac == "" && nid == "" && name == "" {
		h.writeError(w, http.StatusBadRequest, "Missing identifier", "At least one identifier (host, mac, nid, or name) must be provided")
		return
	}

	// Get all configurations and filter by identifiers
	configs, err := h.client.GetBootConfigurations(ctx)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to retrieve configurations", err.Error())
		return
	}

	identifiers := ParseNodeIdentifiersFromQuery(host, mac, nid, name)
	matchingConfigs := h.filterConfigurationsByIdentifiers(configs, identifiers)

	if len(matchingConfigs) == 0 {
		h.writeError(w, http.StatusNotFound, "No matching boot parameters found", "")
		return
	}

	// Delete all matching configurations
	var deletedConfigs []BootParameters
	for _, config := range matchingConfigs {
		err := h.client.DeleteBootConfiguration(ctx, config.Metadata.UID)
		if err != nil {
			h.logger.Printf("Warning: Failed to delete configuration %s: %v", config.Metadata.UID, err)
			continue
		}
		legacyParam := ConvertBootConfigurationToLegacy(&config)
		deletedConfigs = append(deletedConfigs, legacyParam)
	}

	response := BootParametersResponse{
		BootParameters: deletedConfigs,
	}

	h.writeJSON(w, http.StatusOK, response)
}

// GetBootScript handles GET /boot/v1/bootscript
func (h *LegacyHandler) GetBootScript(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse query parameters for node identification
	host := r.URL.Query().Get("host")
	mac := r.URL.Query().Get("mac")
	nid := r.URL.Query().Get("nid")

	// Create boot script request
	req := BootScriptRequest{
		Host:   host,
		Mac:    mac,
		Nid:    nid,
		Format: r.URL.Query().Get("format"), // defaults to "ipxe"
	}

	// Extract the node identifier
	identifier := ExtractNodeIdentifier(req)
	if identifier == "" {
		h.writeError(w, http.StatusBadRequest, "Missing node identifier", "At least one node identifier (host, mac, or nid) must be provided")
		return
	}

	profile := r.URL.Query().Get("profile")

	script, err := h.controller.GenerateBootScript(ctx, identifier, profile)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "Failed to generate boot script", err.Error())
		return
	}

	// Return the script as plain text (iPXE format)
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(script)) //nolint:errcheck
}

// GetServiceStatus handles GET /boot/v1/service/status
func (h *LegacyHandler) GetServiceStatus(w http.ResponseWriter, r *http.Request) { //nolint:revive
	status := CreateServiceStatus("2.0.0-fabrica")
	h.writeJSON(w, http.StatusOK, status)
}

// GetServiceVersion handles GET /boot/v1/service/version
func (h *LegacyHandler) GetServiceVersion(w http.ResponseWriter, r *http.Request) { //nolint:revive
	version := CreateServiceVersion("2.0.0-fabrica", "2025-10-08", "main")
	h.writeJSON(w, http.StatusOK, version)
}

// Helper methods

func (h *LegacyHandler) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Printf("Error encoding JSON response: %v", err)
	}
}

func (h *LegacyHandler) writeError(w http.ResponseWriter, status int, title, detail string) {
	errorResp := CreateErrorResponse(status, title, detail)
	h.writeJSON(w, status, errorResp)
}

func (h *LegacyHandler) generateConfigName(req BootParametersRequest) string {
	// Generate a name based on the first identifier
	if len(req.Hosts) > 0 {
		return fmt.Sprintf("legacy-%s", strings.ReplaceAll(req.Hosts[0], ".", "-"))
	}
	if len(req.Macs) > 0 {
		return fmt.Sprintf("legacy-%s", strings.ReplaceAll(req.Macs[0], ":", "-"))
	}
	if len(req.Nids) > 0 {
		return fmt.Sprintf("legacy-nid-%s", req.Nids[0])
	}
	return fmt.Sprintf("legacy-config-%d", len(req.Hosts)+len(req.Macs)+len(req.Nids))
}

func (h *LegacyHandler) filterConfigurationsByIdentifiers(configs []bootconfiguration.BootConfiguration, identifiers []string) []bootconfiguration.BootConfiguration {
	var matching []bootconfiguration.BootConfiguration

	for _, config := range configs {
		if h.configMatchesIdentifiers(config, identifiers) {
			matching = append(matching, config)
		}
	}

	return matching
}

func (h *LegacyHandler) configMatchesIdentifiers(config bootconfiguration.BootConfiguration, identifiers []string) bool {
	for _, identifier := range identifiers {
		// Check hosts
		for _, host := range config.Spec.Hosts {
			if host == identifier {
				return true
			}
		}

		// Check MACs
		for _, mac := range config.Spec.MACs {
			if mac == identifier {
				return true
			}
		}

		// Check NIDs
		if nid, err := strconv.Atoi(identifier); err == nil {
			for _, configNid := range config.Spec.NIDs {
				if int32(nid) == configNid {
					return true
				}
			}
		}

		// Check groups
		for _, group := range config.Spec.Groups {
			if group == identifier {
				return true
			}
		}
	}

	return false
}
