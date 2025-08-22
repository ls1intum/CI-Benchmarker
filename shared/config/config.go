package config

import (
	"github.com/spf13/viper"
	"log/slog"
	"os"
	"sync"
)

type Config struct {
	ServerAddress string `mapstructure:"SERVER_ADDRESS"`
	HadesHost     string `mapstructure:"HADES_HOST"`
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

		_ = viper.BindEnv("HADES_HOST")
		_ = viper.BindEnv("SERVER_ADDRESS")

		if err := viper.Unmarshal(&cfg); err != nil {
			slog.Error("Failed to unmarshal config", "error", err)
			panic(err)
		}

		if cfg.HadesHost == "" {
			slog.Error("HADES_HOST is required but not set")
			panic("HADES_HOST is required but not set")
		}
	})
	return cfg
}
