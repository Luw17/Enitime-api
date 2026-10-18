package config

import "os"

type Config struct {
	Port           string
	DatabaseDriver string
	DatabaseDSN    string
}

func Load() Config {
	return Config{
		Port:           getEnv("PORT", "8080"),
		DatabaseDriver: os.Getenv("DATABASE_DRIVER"),
		DatabaseDSN:    os.Getenv("DATABASE_DSN"),
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}

	return fallback
}
