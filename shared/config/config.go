package config

import (
	"github.com/spf13/viper"
	"log/slog"
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

func Load() Config {
	once.Do(func() {
		viper.SetConfigName(".env")
		viper.SetConfigType("env")
		viper.AddConfigPath(".")
		viper.AutomaticEnv()

		if err := viper.ReadInConfig(); err != nil {
			slog.Warn("No .env file found or failed to load it", "error", err)
		}

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
