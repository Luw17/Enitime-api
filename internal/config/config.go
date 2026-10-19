package config

import (
	"net"
	"net/url"
	"os"
)

type Config struct {
	Port                string
	DatabaseURL         string
	JWTSecret           string
	TelegramBotToken    string
	TelegramAdminChatID string
}

func Load() Config {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = buildDatabaseURL()
	}

	return Config{
		Port:                getEnv("PORT", "8080"),
		DatabaseURL:         databaseURL,
		JWTSecret:           os.Getenv("JWT_SECRET"),
		TelegramBotToken:    os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramAdminChatID: os.Getenv("TELEGRAM_ADMIN_CHAT_ID"),
	}
}

func buildDatabaseURL() string {
	host := os.Getenv("DB_HOST")
	user := os.Getenv("DB_USER")
	databaseName := os.Getenv("DB_NAME")
	if host == "" || user == "" || databaseName == "" {
		return ""
	}

	databaseURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, os.Getenv("DB_PASSWORD")),
		Host:   net.JoinHostPort(host, getEnv("DB_PORT", "5432")),
		Path:   databaseName,
	}

	return databaseURL.String()
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}

	return fallback
}
