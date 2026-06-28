package platform

import "github.com/redis/go-redis/v9"

// NewRedisClient centralizes Redis client construction for API readiness checks,
// relay publishing, and worker consumption.
func NewRedisClient(cfg Config) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
}
