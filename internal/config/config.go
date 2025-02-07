package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	LogLevel string `env:"LOG_LEVEL" envDefault:"error"`

	AnthropicAPIKey         string        `env:"ANTHROPIC_API_KEY"`
	AnthropicHandlerTimeout time.Duration `env:"ANTHROPIC_HANDLER_TIMEOUT" envDefault:"10s"`

	NWSClientTimeout time.Duration `env:"NWS_CLIENT_TIMEOUT" envDefault:"5s"`

	AuthenticationEnabled bool     `env:"AUTHENTICATION_ENABLED" envDefault:"false"`
	APIKeys               []string `env:"API_KEYS" envSeparator:","`

	DragonflyHost        string        `env:"DRAGONFLY_HOST,required"`
	DragonflyPort        int           `env:"DRAGONFLY_PORT" envDefault:"6379"`
	DragonflyAuth        string        `env:"DRAGONFLY_AUTH"`
	DragonflyKeyPrefix   string        `env:"DRAGONFLY_KEY_PREFIX" envDefault:"lfia"`
	CacheResultsDuration time.Duration `env:"CACHE_RESULTS_DURATION" envDefault:"6h"`

	MetricsEnabled bool `env:"METRICS_ENABLED" envDefault:"true"`
	MetricsPort    int  `env:"METRICS_PORT" envDefault:"8081"`

	TracingEnabled    bool    `env:"TRACING_ENABLED" envDefault:"false"`
	TracingSampleRate float64 `env:"TRACING_SAMPLERATE" envDefault:"0.01"`
	TracingService    string  `env:"TRACING_SERVICE" envDefault:"katalog-agent"`
	TracingVersion    string  `env:"TRACING_VERSION"`
}

func NewConfig() (*Config, error) {
	var cfg Config

	err := env.Parse(&cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}
