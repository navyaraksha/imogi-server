package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

type publicConfig struct {
	GoogleClientID string `json:"googleClientId"`
	APIBaseURL     string `json:"apiBaseUrl"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	envPath := flag.String("env", ".env", "environment file")
	outputPath := flag.String("out", "../web-test/config.js", "generated browser config path")
	flag.Parse()
	if err := godotenv.Load(*envPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load environment: %w", err)
	}
	clientID := strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
	if clientID == "" {
		return errors.New("GOOGLE_CLIENT_ID is required")
	}
	apiBaseURL := strings.TrimSpace(os.Getenv("WEB_TEST_API_BASE_URL"))
	if apiBaseURL == "" {
		apiBaseURL = "http://localhost:8080"
	}
	configJSON, err := json.MarshalIndent(publicConfig{GoogleClientID: clientID, APIBaseURL: apiBaseURL}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode browser config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(*outputPath), 0o755); err != nil {
		return fmt.Errorf("create browser config directory: %w", err)
	}
	content := "window.IMOGI_CONFIG = " + string(configJSON) + ";\n"
	if err := os.WriteFile(*outputPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write browser config: %w", err)
	}
	return nil
}
