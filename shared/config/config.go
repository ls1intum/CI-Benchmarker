package config

import (
	"log/slog"
	"os"
	"sync"

	"github.com/spf13/viper"
)

type Config struct {
	ServerAddress string `mapstructure:"SERVER_ADDRESS"`
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

		_ = viper.BindEnv("SERVER_ADDRESS")

		if err := viper.Unmarshal(&cfg); err != nil {
			slog.Error("Failed to unmarshal config", "error", err)
			panic(err)
		}
	})
	return cfg
}
