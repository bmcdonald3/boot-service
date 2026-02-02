// SPDX-FileCopyrightText: 2025 OpenCHAMI Contributors
//
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/openchami/boot-service/pkg/client"
	"github.com/openchami/boot-service/pkg/controllers/bootscript"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <node-identifier> [profile]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  node-identifier can be XName, NID, or MAC address\n")
		os.Exit(1)
	}

	identifier := os.Args[1]
	
	profile := ""
	if len(os.Args) > 2 {
		profile = os.Args[2]
	}

	// Create client
	bootClient, err := client.NewClient("http://localhost:8080", &http.Client{Timeout: 30 * time.Second})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Create controller
	logger := log.New(os.Stderr, "demo: ", log.LstdFlags)
	controller := bootscript.NewBootScriptController(*bootClient, logger)

	// Generate boot script
	ctx := context.Background()
	
	script, err := controller.GenerateBootScript(ctx, identifier, profile)
	if err != nil {
		log.Fatalf("Failed to generate boot script: %v", err)
	}

	// Output the script
	fmt.Printf("# Profile: %s\n", profile)
	fmt.Print(script)
}