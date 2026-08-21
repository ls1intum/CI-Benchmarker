package config

import (
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
)

// Version is stamped into every run row so exported data records which build of
// the benchmarker produced it, and is logged at startup. Override at build time
// with -ldflags "-X github.com/Hades-Scheduler/CI-Benchmarker/shared/config.Version=<sha>";
// the Dockerfile does this from its VERSION build argument.
var Version = "dev"

// Config holds every runtime knob. Load generation defaults live here rather
// than in the individual executors so that all variants are driven identically.
type Config struct {
	ServerAddress string `mapstructure:"SERVER_ADDRESS"`

	// DBPath is where the benchmark database lives. In the container this
	// points into a mounted volume so a --force-recreate cannot destroy a
	// dataset.
	DBPath string `mapstructure:"DB_PATH"`

	// CallbackBaseURL is the benchmarker's own externally reachable base URL.
	// It is handed to the system under test as the status callback target, so
	// the SUT can report completion. Without it, Hades has nowhere to report
	// and completion would have to be inferred again.
	CallbackBaseURL string `mapstructure:"CALLBACK_BASE_URL"`

	// DefaultConcurrency caps in-flight submissions when a request does not
	// specify one. Identical for every variant by construction.
	DefaultConcurrency int `mapstructure:"DEFAULT_CONCURRENCY"`
	// DefaultRatePerSecond is the offered submission rate when a request does
	// not specify one. 0 means submit as fast as the concurrency cap allows.
	DefaultRatePerSecond float64 `mapstructure:"DEFAULT_RATE_PER_SECOND"`

	// HTTP client settings, shared by every executor.
	HTTPTimeoutSeconds      int `mapstructure:"HTTP_TIMEOUT_SECONDS"`
	HTTPDialTimeoutSeconds  int `mapstructure:"HTTP_DIAL_TIMEOUT_SECONDS"`
	HTTPMaxIdleConnsPerHost int `mapstructure:"HTTP_MAX_IDLE_CONNS_PER_HOST"`
	// HTTPMaxConnsPerHost defaults to 0 (unlimited) on purpose: a hard cap
	// silently queues submissions and charges the wait to the client timeout.
	HTTPMaxConnsPerHost int `mapstructure:"HTTP_MAX_CONNS_PER_HOST"`
}

var (
	cfg  Config
	once sync.Once
)

func GetEnv(key string) string {
	return os.Getenv(key)
}

// Load reads configuration from the environment and an optional .env file.
func Load() Config {
	once.Do(func() {
		viper.AutomaticEnv()

		viper.SetConfigName(".env")
		viper.SetConfigType("env")
		viper.AddConfigPath(".")
		if err := viper.ReadInConfig(); err != nil {
			slog.Warn("No .env file found or failed to load it", "error", err)
		}

		viper.SetDefault("SERVER_ADDRESS", "8080")
		viper.SetDefault("DB_PATH", "benchmark.db")
		viper.SetDefault("CALLBACK_BASE_URL", "")
		viper.SetDefault("DEFAULT_CONCURRENCY", 64)
		viper.SetDefault("DEFAULT_RATE_PER_SECOND", 0)
		viper.SetDefault("HTTP_TIMEOUT_SECONDS", 30)
		viper.SetDefault("HTTP_DIAL_TIMEOUT_SECONDS", 10)
		viper.SetDefault("HTTP_MAX_IDLE_CONNS_PER_HOST", 1024)
		viper.SetDefault("HTTP_MAX_CONNS_PER_HOST", 0)

		for _, key := range []string{
			"SERVER_ADDRESS", "DB_PATH", "CALLBACK_BASE_URL",
			"DEFAULT_CONCURRENCY", "DEFAULT_RATE_PER_SECOND",
			"HTTP_TIMEOUT_SECONDS", "HTTP_DIAL_TIMEOUT_SECONDS",
			"HTTP_MAX_IDLE_CONNS_PER_HOST", "HTTP_MAX_CONNS_PER_HOST",
		} {
			_ = viper.BindEnv(key)
		}

		if err := viper.Unmarshal(&cfg); err != nil {
			slog.Error("Failed to unmarshal config", "error", err)
			panic(err)
		}
	})
	return cfg
}

// StatusCallbackURL is the URL a system under test should POST terminal status
// to. Empty when CALLBACK_BASE_URL is unset, in which case the target must be
// supplied per request.
func (c Config) StatusCallbackURL() string {
	if c.CallbackBaseURL == "" {
		return ""
	}
	return strings.TrimRight(c.CallbackBaseURL, "/") + "/v1/callback"
}

// HTTPTimeout returns the shared client timeout.
func (c Config) HTTPTimeout() time.Duration {
	return time.Duration(c.HTTPTimeoutSeconds) * time.Second
}

// HTTPDialTimeout returns the shared client dial timeout.
func (c Config) HTTPDialTimeout() time.Duration {
	return time.Duration(c.HTTPDialTimeoutSeconds) * time.Second
}
