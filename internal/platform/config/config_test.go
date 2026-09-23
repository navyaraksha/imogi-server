package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func setValidEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/imogi")
	t.Setenv("GOOGLE_CLIENT_ID", "client-id.apps.googleusercontent.com")
	t.Setenv("SENSITIVE_DATA_ENCRYPTION_KEY_BASE64", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("e", 32))))
	t.Setenv("SENSITIVE_DATA_LOOKUP_KEY_BASE64", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("l", 32))))
}

func TestLoadUsesSafeDefaultsAndNormalizesBootstrapEmails(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("GOOGLE_BOOTSTRAP_PLATFORM_ADMIN_EMAILS", " Admin@example.com, second@example.com ")
	t.Setenv("HTTP_ALLOWED_ORIGINS", " http://localhost:5173,https://imogi.example.com ")
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if config.HTTPAddr != ":8080" || config.DatabaseMaxConns != 10 || config.DatabaseMinConns != 1 {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	if config.SessionTTL != 8*time.Hour || config.SessionCookieSecure {
		t.Fatalf("unexpected session defaults: ttl=%s secure=%t", config.SessionTTL, config.SessionCookieSecure)
	}
	if len(config.BootstrapAdminEmails) != 2 || config.BootstrapAdminEmails[0] != "admin@example.com" {
		t.Fatalf("bootstrap emails = %#v", config.BootstrapAdminEmails)
	}
	if len(config.HTTPAllowedOrigins) != 2 || config.HTTPAllowedOrigins[0] != "http://localhost:5173" {
		t.Fatalf("allowed origins = %#v", config.HTTPAllowedOrigins)
	}
}

func TestLoadRequiresSecrets(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("GOOGLE_CLIENT_ID", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing Google client ID was accepted")
	}
}

func TestLoadRejectsInvalidEncryptionKeyLength(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("SENSITIVE_DATA_ENCRYPTION_KEY_BASE64", base64.StdEncoding.EncodeToString([]byte("short")))
	if _, err := Load(); err == nil {
		t.Fatal("invalid encryption key length was accepted")
	}
}
