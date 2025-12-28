# lfpweather-forecast-inference-api

A weather forecast inference API that uses LLMs to generate human-friendly weather summaries from National Weather Service (NWS) data.

## Overview

This application fetches weather forecast data from the NWS API and uses an LLM (Anthropic Claude or OpenAI-compatible models) to generate:

- **Forecast Summaries**: Concise, human-readable weather summaries with appropriate weather icons
- **Detailed Forecast Periods**: Enriched forecast data with time-of-day indicators, weather icons, and Beaufort wind scale classifications

Results are cached in Dragonfly (Redis-compatible) for fast responses, with a background worker that periodically regenerates forecasts.

## Features

- **Multiple LLM Providers**: Support for Anthropic Claude and OpenAI-compatible APIs (including local LLMs like llama.cpp)
- **Background Generation**: Configurable worker that pre-generates forecasts on a schedule
- **Caching**: Redis-compatible caching with Dragonfly for fast API responses
- **Observability**: Prometheus metrics and OpenTelemetry tracing support
- **API Authentication**: Optional API key authentication

## Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   NWS API       │────▶│  Forecast API   │────▶│   LLM Provider  │
│  (weather data) │     │   (this app)    │     │ (Anthropic/     │
└─────────────────┘     └────────┬────────┘     │  OpenAI/Local)  │
                                 │              └─────────────────┘
                                 │
                                 ▼
                        ┌─────────────────┐
                        │   Dragonfly     │
                        │   (cache)       │
                        └─────────────────┘
```

## API Endpoints

### GET `/api/v1/forecast/summary`

Returns a concise weather forecast summary.

**Response:**
```json
{
  "summary": "Tonight, mostly cloudy with a low around 54. Sunday, mostly sunny with a high near 74. Winds light and variable.",
  "icon": "cloud-moon",
  "last_updated": "2024-12-27T10:30:00Z"
}
```

### GET `/api/v1/forecast/detailed`

Returns detailed forecast information for all available periods.

**Response:**
```json
{
  "periods": [
    {
      "name": "Tonight",
      "time_of_day": "night",
      "icon": "cloud-moon",
      "beaufort": "Light air",
      "detailed_forecast": "Mostly cloudy, with a low around 54.",
      "short_forecast": "Mostly Cloudy",
      "start_time": "2024-12-27T18:00:00-08:00",
      "end_time": "2024-12-28T06:00:00-08:00",
      "temperature": 54,
      "wind_speed": "2 mph",
      "wind_direction": "E"
    }
  ],
  "last_updated": "2024-12-27T10:30:00Z"
}
```

### Available Weather Icons

The LLM selects from these icons based on forecast conditions:

`cloud`, `cloud-drizzle`, `cloud-fog`, `cloud-hail`, `cloud-lightning`, `cloud-moon`, `cloud-moon-rain`, `cloud-rain`, `cloud-rain-wind`, `cloud-snow`, `cloud-sun`, `cloud-sun-rain`, `cloudy`, `snowflake`, `sun`, `sun-snow`, `thermometer-snowflake`, `thermometer-sun`, `wind`

## Configuration

All configuration is done via environment variables:

### LLM Provider

| Variable | Default | Description |
|----------|---------|-------------|
| `LLM_PROVIDER` | `anthropic` | LLM provider: `anthropic` or `openai` |
| `ANTHROPIC_API_KEY` | - | Anthropic API key (required if using Anthropic) |
| `ANTHROPIC_MODEL` | `claude-sonnet-4-5` | Anthropic model to use |
| `OPENAI_API_KEY` | - | OpenAI API key (required if using OpenAI) |
| `OPENAI_MODEL` | `gpt-4o` | OpenAI model to use |
| `OPENAI_BASE_URL` | - | Custom base URL for OpenAI-compatible APIs |
| `LLM_HANDLER_TIMEOUT` | `10s` | Timeout for LLM requests |

### Background Worker

| Variable | Default | Description |
|----------|---------|-------------|
| `WORKER_ENABLED` | `true` | Enable background forecast generation |
| `WORKER_INTERVAL` | `30m` | Interval between generation runs |
| `WORKER_TIMEOUT` | `60s` | Timeout for each generation run |
| `GRID_POINT` | `SEW/127,75` | NWS grid point for forecasts |

### Cache (Dragonfly/Redis)

| Variable | Default | Description |
|----------|---------|-------------|
| `DRAGONFLY_HOST` | - | **Required.** Dragonfly/Redis host |
| `DRAGONFLY_PORT` | `6379` | Dragonfly/Redis port |
| `DRAGONFLY_AUTH` | - | Dragonfly/Redis password |
| `DRAGONFLY_KEY_PREFIX` | `lfia` | Cache key prefix |
| `CACHE_RESULTS_DURATION` | `6h` | How long to cache results |

### NWS Client

| Variable | Default | Description |
|----------|---------|-------------|
| `NWS_CLIENT_TIMEOUT` | `5s` | Timeout for NWS API requests |

### Authentication

| Variable | Default | Description |
|----------|---------|-------------|
| `AUTHENTICATION_ENABLED` | `false` | Enable API key authentication |
| `API_KEYS` | - | Comma-separated list of valid API keys |

### Observability

| Variable | Default | Description |
|----------|---------|-------------|
| `LOG_LEVEL` | `error` | Log level: `debug`, `info`, `warn`, `error` |
| `METRICS_ENABLED` | `true` | Enable Prometheus metrics |
| `METRICS_PORT` | `8081` | Prometheus metrics port |
| `TRACING_ENABLED` | `false` | Enable OpenTelemetry tracing |
| `TRACING_SAMPLERATE` | `0.01` | Trace sample rate |
| `TRACING_SERVICE` | `katalog-agent` | Service name for traces |
| `TRACING_VERSION` | - | Service version for traces |

## Quick Start

### Using Docker Compose

1. Copy the example environment file and add your API keys:
   ```bash
   cp .env.example .env
   # Edit .env with your API keys
   ```

2. Start the stack:
   ```bash
   docker-compose up
   ```

3. Access the API:
   ```bash
   curl -H "X-API-Key: f791709e0fc2a4eabfdca42a50d905a8" \
        http://localhost:8080/api/v1/forecast/summary
   ```

### Using a Local LLM

To use a local OpenAI-compatible LLM server (like llama.cpp):

```bash
# In .env
OPENAI_API_KEY=dummy
OPENAI_BASE_URL=http://localhost:8080/v1

# In docker-compose.yml or environment
LLM_PROVIDER=openai
OPENAI_MODEL=your-model-name
```

## Development

### Prerequisites

- Go 1.21+
- Docker and Docker Compose (for local development)

### Building

```bash
go build ./...
```

### Running Locally

```bash
# Set required environment variables
export DRAGONFLY_HOST=localhost
export ANTHROPIC_API_KEY=your-key
# ... other config

go run ./cmd/lfpweather-forecast-inference-api
```

## Observability

### Prometheus Metrics

Metrics are exposed on port 8081 (configurable) at `/metrics`.

### Grafana

The Docker Compose stack includes Grafana at http://localhost:3000 with pre-configured dashboards.

### Tracing

OpenTelemetry traces can be exported to Tempo (included in Docker Compose) or any OTLP-compatible backend.

## License

See [LICENSE](LICENSE) for details.
