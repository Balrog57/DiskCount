package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadEnvFileAndImportableValues(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	env := "TELEGRAM_BOT_TOKEN=abc\nDATABASE_URL=postgres://ignored\nPRICEPERGIG_ENABLED=false\nUNKNOWN=value\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEEPA_API_KEY", "test-keepa-value")

	values := ImportableEnvValues()
	if values["TELEGRAM_BOT_TOKEN"] != "abc" {
		t.Fatalf("token not imported: %q", values["TELEGRAM_BOT_TOKEN"])
	}
	if values["DATABASE_URL"] != "" {
		t.Fatalf("bootstrap DATABASE_URL must not be imported as app config")
	}
	if values["UNKNOWN"] != "" {
		t.Fatalf("unknown key imported: %q", values["UNKNOWN"])
	}
	if values["KEEPA_API_KEY"] != "test-keepa-value" {
		t.Fatalf("env override not imported")
	}
}

func TestLoadWithAppValuesOverridesEnvButKeepsBootstrap(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	env := "TELEGRAM_BOT_TOKEN=from-env\nDATABASE_URL=postgres://boot\nWEB_ADMIN_ADDR=:47832\nPRICEPERGIG_ENABLED=true\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := LoadWithAppValues(map[string]string{
		"TELEGRAM_BOT_TOKEN":  "from-db",
		"PRICEPERGIG_ENABLED": "false",
		"PRICEPERTB_URLS":     "",
	})
	if cfg.TelegramBotToken != "from-db" {
		t.Fatalf("app config did not override env: %q", cfg.TelegramBotToken)
	}
	if cfg.DatabaseURL != "postgres://boot" {
		t.Fatalf("bootstrap database URL changed: %q", cfg.DatabaseURL)
	}
	if cfg.WebAdminAddr != ":47832" {
		t.Fatalf("web addr mismatch: %q", cfg.WebAdminAddr)
	}
	if cfg.PricePerGigEnabled {
		t.Fatalf("bool app config override ignored")
	}
	if len(cfg.PricePerTBURLs) != 0 {
		t.Fatalf("explicit empty list should disable default URLs: %#v", cfg.PricePerTBURLs)
	}
}

func TestValidateRejectsBadConfig(t *testing.T) {
	cfg := &Config{
		RequestTimeoutSeconds:    -1,
		PerRequestTimeoutSeconds: 20,
		RetryMaxAttempts:         -1,
		RetryBaseDelaySeconds:    -1,
		ByparrURL:                "://invalid",
		CircuitBreakerThreshold:  0,
		CircuitBreakerTimeoutS:   -1,
	}
	errs := cfg.Validate()
	if len(errs) < 5 {
		t.Fatalf("expected several validation errors, got %d: %v", len(errs), errs)
	}
}

func TestIsSourceEnabled(t *testing.T) {
	c := &Config{}
	if !c.IsSourceEnabled("diskprices") {
		t.Fatal("empty EnabledSources should mean all enabled")
	}
	c.EnabledSources = []string{"diskprices", "pricepergig"}
	if !c.IsSourceEnabled("diskprices") {
		t.Fatal("explicit list should allow listed source")
	}
	if c.IsSourceEnabled("idealo") {
		t.Fatal("explicit list should reject unlisted source")
	}
}
