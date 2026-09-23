package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvDoesNotOverrideExistingEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("IMOGI_DOTENV_TEST=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IMOGI_DOTENV_TEST", "from-environment")
	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("IMOGI_DOTENV_TEST"); got != "from-environment" {
		t.Fatalf("dotenv value = %q, want existing environment value", got)
	}
}

func TestLoadDotEnvReadsMissingEnvironmentValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("IMOGI_DOTENV_TEST=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous, hadPrevious := os.LookupEnv("IMOGI_DOTENV_TEST")
	if err := os.Unsetenv("IMOGI_DOTENV_TEST"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadPrevious {
			_ = os.Setenv("IMOGI_DOTENV_TEST", previous)
		} else {
			_ = os.Unsetenv("IMOGI_DOTENV_TEST")
		}
	})
	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("IMOGI_DOTENV_TEST"); got != "from-file" {
		t.Fatalf("dotenv value = %q, want file value", got)
	}
}
