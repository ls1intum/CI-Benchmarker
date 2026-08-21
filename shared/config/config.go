package config

import (
	"log/slog"
	"os"
	"sync"

	"github.com/spf13/viper"
)

// Version identifies the build. Override at build time with
// -ldflags "-X github.com/Hades-Scheduler/CI-Benchmarker/shared/config.Version=<sha>";
// the Dockerfile does this from its VERSION build argument.
var Version = "dev"

type Config struct {
	ServerAddress string `mapstructure:"SERVER_ADDRESS"`

	// DBPath is where the benchmark database lives. It is configurable so the
	// container can keep it on a mounted volume: the database is the result of
	// a measurement campaign, and a `docker compose up --force-recreate` must
	// not be able to destroy it.
	DBPath string `mapstructure:"DB_PATH"`
}

var (
	cfg  Config
	once sync.Once
)

func GetEnv(key string) string {
	return os.Getenv(key)
}

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

		_ = viper.BindEnv("SERVER_ADDRESS")
		_ = viper.BindEnv("DB_PATH")

		if err := viper.Unmarshal(&cfg); err != nil {
			slog.Error("Failed to unmarshal config", "error", err)
			panic(err)
		}
	})
	return cfg
}
