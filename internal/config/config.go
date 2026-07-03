package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	SessionTTL      time.Duration
	Environment     string
	DatabaseURL     string
	UploadDir       string
}

func Load() (Config, error) {
	if err := loadDotEnv(".env", ".env.local"); err != nil {
		return Config{}, err
	}

	cfg := Config{
		Addr:            envString("ADDR", getRandomDefaultPort(2000, 9000)),
		ReadTimeout:     envDuration("READ_TIMEOUT", 5*time.Second),
		WriteTimeout:    envDuration("WRITE_TIMEOUT", 10*time.Second),
		IdleTimeout:     envDuration("IDLE_TIMEOUT", 120*time.Second),
		ShutdownTimeout: envDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
		SessionTTL:      envDuration("SESSION_TTL", 24*time.Hour),
		Environment:     envString("APP_ENV", "production"),
		DatabaseURL:     envString("DATABASE_URL", ""),
		UploadDir:       envString("UPLOAD_DIR", "uploads"),
	}

	if cfg.Addr == "" {
		return Config{}, fmt.Errorf("ADDR cannot be empty")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

func LogLevel() slog.Level {
	if envBool("DEBUG", false) {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

func loadDotEnv(files ...string) error {
	protected := map[string]bool{}
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			protected[key] = true
		}
	}

	for _, file := range files {
		if err := loadDotEnvFile(file, protected); err != nil {
			return err
		}
	}
	return nil
}

func loadDotEnvFile(path string, protected map[string]bool) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("load %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		key, value, ok, err := parseEnvLine(scanner.Text())
		if err != nil {
			return fmt.Errorf("load %s line %d: %w", path, lineNumber, err)
		}
		if !ok || protected[key] {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set %s from %s: %w", key, path, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

func parseEnvLine(line string) (string, string, bool, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false, nil
	}
	line = strings.TrimPrefix(line, "export ")
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false, fmt.Errorf("expected KEY=VALUE")
	}
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false, fmt.Errorf("invalid key %q", key)
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		quote := value[0]
		if (quote == '\'' || quote == '"') && value[len(value)-1] == quote {
			value = value[1 : len(value)-1]
		}
	}
	return key, value, true, nil
}

func envString(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return duration
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getRandomDefaultPort(min, max int) string {
	val := time.Now().UnixNano() % int64(max-min+1)
	if val < 0 {
		val = -val
	}
	return fmt.Sprintf(":%d", int64(min)+val)
}
