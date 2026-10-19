package config

import "testing"

func TestLoadUsesDatabaseURLWhenProvided(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("DATABASE_URL", "postgres://db.example:5432/enitime_test")
	t.Setenv("DB_HOST", "ignored.example")
	t.Setenv("DB_USER", "ignored")
	t.Setenv("DB_NAME", "ignored")
	t.Setenv("JWT_SECRET", "jwt-test-secret")
	t.Setenv("TELEGRAM_BOT_TOKEN", "telegram-test-token")
	t.Setenv("TELEGRAM_ADMIN_CHAT_ID", "12345")

	cfg := Load()
	if cfg.Port != "9090" {
		t.Fatalf("Port = %q, want %q", cfg.Port, "9090")
	}
	if cfg.DatabaseURL != "postgres://db.example:5432/enitime_test" {
		t.Fatalf("DatabaseURL did not use DATABASE_URL")
	}
	if cfg.JWTSecret != "jwt-test-secret" || cfg.TelegramBotToken != "telegram-test-token" ||
		cfg.TelegramAdminChatID != "12345" {
		t.Fatal("Load() did not read authentication environment variables")
	}
}

func TestLoadBuildsDatabaseURLFromParts(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DB_HOST", "db.example")
	t.Setenv("DB_PORT", "5433")
	t.Setenv("DB_USER", "test user")
	t.Setenv("DB_PASSWORD", "test@password")
	t.Setenv("DB_NAME", "enitime_test")

	cfg := Load()
	if cfg.DatabaseURL != "postgres://test%20user:test%40password@db.example:5433/enitime_test" {
		t.Fatalf("unexpected constructed database URL")
	}
}

func TestLoadUsesDefaultsAndDoesNotBuildPartialDatabaseURL(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DB_HOST", "")
	t.Setenv("DB_USER", "")
	t.Setenv("DB_NAME", "")

	cfg := Load()
	if cfg.Port != "8080" {
		t.Fatalf("Port = %q, want %q", cfg.Port, "8080")
	}
	if cfg.DatabaseURL != "" {
		t.Fatalf("DatabaseURL = %q, want empty", cfg.DatabaseURL)
	}
}
