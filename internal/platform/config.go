package platform

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr           string
	DatabaseURL        string
	RedisAddr          string
	RedisPassword      string
	RedisDB            int
	StreamName         string
	ConsumerGroup      string
	Workers            int
	HTTPTimeout        time.Duration
	RelayPollInterval  time.Duration
	WorkerBlockTimeout time.Duration
}

// LoadConfig reads environment variables once at process startup. Invalid
// numeric values are errors rather than silent defaults because timing and worker
// counts directly affect reliability behavior.
func LoadConfig() (Config, error) {
	redisDB, err := envInt("RC_NOTIFY_REDIS_DB", 0)
	if err != nil {
		return Config{}, err
	}
	workers, err := positiveEnvInt("RC_NOTIFY_WORKERS", 2)
	if err != nil {
		return Config{}, err
	}
	httpTimeout, err := positiveEnvInt("RC_NOTIFY_HTTP_TIMEOUT_SECONDS", 5)
	if err != nil {
		return Config{}, err
	}
	relayPoll, err := positiveEnvInt("RC_NOTIFY_RELAY_POLL_MS", 500)
	if err != nil {
		return Config{}, err
	}
	workerBlock, err := positiveEnvInt("RC_NOTIFY_WORKER_BLOCK_MS", 1000)
	if err != nil {
		return Config{}, err
	}

	return Config{
		HTTPAddr:           env("RC_NOTIFY_ADDR", ":8080"),
		DatabaseURL:        env("RC_NOTIFY_DATABASE_URL", "postgres://notify:notify@localhost:5432/notify?sslmode=disable"),
		RedisAddr:          env("RC_NOTIFY_REDIS_ADDR", "localhost:6379"),
		RedisPassword:      os.Getenv("RC_NOTIFY_REDIS_PASSWORD"),
		RedisDB:            redisDB,
		StreamName:         env("RC_NOTIFY_STREAM", "notification-deliveries"),
		ConsumerGroup:      env("RC_NOTIFY_CONSUMER_GROUP", "notification-workers"),
		Workers:            workers,
		HTTPTimeout:        time.Duration(httpTimeout) * time.Second,
		RelayPollInterval:  time.Duration(relayPoll) * time.Millisecond,
		WorkerBlockTimeout: time.Duration(workerBlock) * time.Millisecond,
	}, nil
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func positiveEnvInt(key string, fallback int) (int, error) {
	parsed, err := envInt(key, fallback)
	if err != nil {
		return 0, err
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return parsed, nil
}
