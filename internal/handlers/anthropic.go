package handlers

import (
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/dragonfly"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/nws"
)

type AnthropicHandler struct {
	AnthropicClient *anthropic.Client
	NWSClient       *nws.NWSClient
	DragonflyClient *dragonfly.DragonflyClient
	Timeout         time.Duration
}

func NewAnthropicHandler(ac *anthropic.Client, nc *nws.NWSClient, dc *dragonfly.DragonflyClient, timeout time.Duration) *AnthropicHandler {
	return &AnthropicHandler{
		AnthropicClient: ac,
		NWSClient:       nc,
		DragonflyClient: dc,
		Timeout:         timeout,
	}
}
