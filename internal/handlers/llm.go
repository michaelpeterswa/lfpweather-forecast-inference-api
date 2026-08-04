package handlers

import (
	"time"

	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/dragonfly"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/generate"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/llm"
	"github.com/michaelpeterswa/lfpweather-forecast-inference-api/internal/nws"
)

type LLMHandler struct {
	LLMProvider     llm.Provider
	NWSClient       *nws.NWSClient
	DragonflyClient *dragonfly.DragonflyClient
	Generator       *generate.Generator
	Timeout         time.Duration
}

func NewLLMHandler(provider llm.Provider, nc *nws.NWSClient, dc *dragonfly.DragonflyClient, generator *generate.Generator, timeout time.Duration) *LLMHandler {
	return &LLMHandler{
		LLMProvider:     provider,
		NWSClient:       nc,
		DragonflyClient: dc,
		Generator:       generator,
		Timeout:         timeout,
	}
}
