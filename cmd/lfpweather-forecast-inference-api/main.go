package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"alpineworks.io/ootel"
	"github.com/gorilla/mux"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/config"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/dragonfly"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/handlers"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/llm"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/logging"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/middleware"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/nws"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/worker"
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
				ootel.ExporterTypePrometheus,
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

	// Initialize LLM provider based on configuration
	var llmProvider llm.Provider
	switch strings.ToLower(c.LLMProvider) {
	case "openai":
		llmProvider = llm.NewOpenAIProvider(c.OpenAIAPIKey, c.OpenAIModel, c.OpenAIBaseURL)
		slog.Info("using OpenAI-compatible provider", slog.String("model", c.OpenAIModel))
	case "anthropic":
		fallthrough
	default:
		llmProvider = llm.NewAnthropicProvider(c.AnthropicAPIKey, c.AnthropicModel)
		slog.Info("using Anthropic provider", slog.String("model", c.AnthropicModel))
	}

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

	llmHandler := handlers.NewLLMHandler(llmProvider, nwsClient, dragonflyClient, c.LLMHandlerTimeout)

	// Start background worker if enabled
	if c.WorkerEnabled {
		forecastWorker := worker.NewForecastWorker(
			llmProvider,
			nwsClient,
			dragonflyClient,
			c.WorkerInterval,
			c.WorkerTimeout,
			c.GridPoint,
		)
		go forecastWorker.Start(ctx)
	}

	router := mux.NewRouter()
	apiSubrouter := router.PathPrefix("/api").Subrouter()
	v1Subrouter := apiSubrouter.PathPrefix("/v1").Subrouter()
	forecastSubrouter := v1Subrouter.PathPrefix("/forecast").Subrouter()

	forecastSubrouter.HandleFunc("/summary", llmHandler.GetForecastSummary).Methods(http.MethodGet)
	forecastSubrouter.HandleFunc("/detailed", llmHandler.GetForcastPeriodsInformation).Methods(http.MethodGet)

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
