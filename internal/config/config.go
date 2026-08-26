// Package config handles centralized application configuration, loading
// environment variables from .env files or system environment, and validating
// all settings on startup.
package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port               string
	MongoURI           string
	DBName             string
	PasetoKey          []byte
	Environment        string
	ShutdownTimeoutSec int
}

// Load reads configuration from environment variables (loading .env if available)
// and validates all required fields before returning the Config struct.
func Load() (*Config, error) {
	// Attempt to load local .env file
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found or error loading it, using system environment variables")
	}

	port := getEnv("PORT", "8081")
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	mongoURI := getEnv("MONGO_URI", "mongodb://localhost:27017")
	dbName := getEnv("DB_NAME", "auditly")
	envMode := getEnv("ENV", getEnv("ENVIRONMENT", "development"))

	shutdownTimeout := 10
	if st := os.Getenv("SHUTDOWN_TIMEOUT_SEC"); st != "" {
		if val, err := strconv.Atoi(st); err == nil && val > 0 {
			shutdownTimeout = val
		}
	}

	// Format PASETO 32-byte key
	rawKey := []byte(os.Getenv("PASETO_SYMMETRIC_KEY"))
	var pKey []byte
	if len(rawKey) == 0 {
		pKey = []byte("yellow-submarine-yellow-submarine") // 32 bytes default
	} else if len(rawKey) < 32 {
		pKey = make([]byte, 32)
		copy(pKey, rawKey)
	} else {
		pKey = rawKey[:32]
	}

	cfg := &Config{
		Port:               port,
		MongoURI:           mongoURI,
		DBName:             dbName,
		PasetoKey:          pKey,
		Environment:        envMode,
		ShutdownTimeoutSec: shutdownTimeout,
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// Validate checks that critical configuration settings are non-empty.
func (c *Config) Validate() error {
	if c.MongoURI == "" {
		return fmt.Errorf("MONGO_URI cannot be empty")
	}
	if c.Port == "" {
		return fmt.Errorf("PORT cannot be empty")
	}
	if len(c.PasetoKey) != 32 {
		return fmt.Errorf("PASETO key must be exactly 32 bytes (got %d)", len(c.PasetoKey))
	}
	return nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
