package handlers

import (
	"time"

	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/dragonfly"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/llm"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/nws"
)

type LLMHandler struct {
	LLMProvider     llm.Provider
	NWSClient       *nws.NWSClient
	DragonflyClient *dragonfly.DragonflyClient
	Timeout         time.Duration
}

func NewLLMHandler(provider llm.Provider, nc *nws.NWSClient, dc *dragonfly.DragonflyClient, timeout time.Duration) *LLMHandler {
	return &LLMHandler{
		LLMProvider:     provider,
		NWSClient:       nc,
		DragonflyClient: dc,
		Timeout:         timeout,
	}
}
