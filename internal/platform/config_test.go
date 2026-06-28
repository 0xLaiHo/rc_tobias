package platform

import "testing"

func TestLoadConfigUsesDefaults(t *testing.T) {
	t.Setenv("RC_NOTIFY_ADDR", "")
	t.Setenv("RC_NOTIFY_DATABASE_URL", "")
	t.Setenv("RC_NOTIFY_REDIS_ADDR", "")
	t.Setenv("RC_NOTIFY_WORKERS", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.DatabaseURL == "" {
		t.Fatal("DatabaseURL is empty")
	}
	if cfg.RedisAddr != "localhost:6379" {
		t.Fatalf("RedisAddr = %q, want localhost:6379", cfg.RedisAddr)
	}
	if cfg.Workers != 2 {
		t.Fatalf("Workers = %d, want 2", cfg.Workers)
	}
}

func TestLoadConfigReadsEnvironment(t *testing.T) {
	t.Setenv("RC_NOTIFY_ADDR", ":9090")
	t.Setenv("RC_NOTIFY_DATABASE_URL", "postgres://example")
	t.Setenv("RC_NOTIFY_REDIS_ADDR", "redis:6379")
	t.Setenv("RC_NOTIFY_WORKERS", "4")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("HTTPAddr = %q, want :9090", cfg.HTTPAddr)
	}
	if cfg.DatabaseURL != "postgres://example" {
		t.Fatalf("DatabaseURL = %q, want postgres://example", cfg.DatabaseURL)
	}
	if cfg.RedisAddr != "redis:6379" {
		t.Fatalf("RedisAddr = %q, want redis:6379", cfg.RedisAddr)
	}
	if cfg.Workers != 4 {
		t.Fatalf("Workers = %d, want 4", cfg.Workers)
	}
}

func TestLoadConfigRejectsInvalidNumbers(t *testing.T) {
	t.Setenv("RC_NOTIFY_WORKERS", "zero")

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig returned nil error, want invalid integer error")
	}
}
