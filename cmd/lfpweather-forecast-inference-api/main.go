package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/alpineworks/ootel"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/gorilla/mux"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/config"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/dragonfly"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/handlers"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/logging"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/middleware"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/nws"
)

func main() {
	slogHandler := slog.NewJSONHandler(os.Stdout, nil)
	slog.SetDefault(slog.New(slogHandler))

	slog.Info("welcome to lfpweather-forecast-inference-api!")

	c, err := config.NewConfig()
	if err != nil {
		slog.Error("could not create config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	slogLevel, err := logging.LogLevelToSlogLevel(c.LogLevel)
	if err != nil {
		slog.Error("could not parse log level", slog.String("error", err.Error()))
		os.Exit(1)
	}

	slog.SetLogLoggerLevel(slogLevel)

	ctx := context.Background()

	ootelClient := ootel.NewOotelClient(
		ootel.WithMetricConfig(
			ootel.NewMetricConfig(
				c.MetricsEnabled,
				c.MetricsPort,
			),
		),
		ootel.WithTraceConfig(
			ootel.NewTraceConfig(
				c.TracingEnabled,
				c.TracingSampleRate,
				c.TracingService,
				c.TracingVersion,
			),
		),
	)

	shutdown, err := ootelClient.Init(ctx)
	if err != nil {
		panic(err)
	}

	defer func() {
		_ = shutdown(ctx)
	}()

	client := anthropic.NewClient(
		option.WithAPIKey(c.AnthropicAPIKey),
	)

	nwsClient := nws.NewNWSClient(&http.Client{
		Timeout: c.NWSClientTimeout,
	})

	dragonflyClient, err := dragonfly.NewDragonflyClient(
		c.DragonflyHost,
		c.DragonflyPort,
		c.DragonflyAuth,
		c.CacheResultsDuration,
		c.DragonflyKeyPrefix,
	)
	if err != nil {
		slog.Error("could not create dragonfly client", slog.String("error", err.Error()))
		os.Exit(1)
	}

	anthropicHandler := handlers.NewAnthropicHandler(client, nwsClient, dragonflyClient, c.AnthropicHandlerTimeout)

	router := mux.NewRouter()
	apiSubrouter := router.PathPrefix("/api").Subrouter()
	v1Subrouter := apiSubrouter.PathPrefix("/v1").Subrouter()
	forecastSubrouter := v1Subrouter.PathPrefix("/forecast").Subrouter()

	forecastSubrouter.HandleFunc("/summary", anthropicHandler.GetForecastSummary).Methods(http.MethodGet)
	forecastSubrouter.HandleFunc("/detailed", anthropicHandler.GetForcastPeriodsInformation).Methods(http.MethodGet)

	if c.AuthenticationEnabled {
		authenticationMiddleware := middleware.NewAuthenticationMiddlewareClient(
			middleware.WithAPIKeys(c.APIKeys),
		)
		apiSubrouter.Use(authenticationMiddleware.AuthenticationMiddleware)
	}

	slog.Info("starting server", slog.String("port", "8080"))
	if err := http.ListenAndServe(":8080", router); err != nil {
		slog.Error("could not start server", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
