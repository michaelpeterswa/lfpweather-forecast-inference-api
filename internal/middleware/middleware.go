package middleware

import (
	"fmt"
	"net/http"

	"alpineworks.io/rfc9457"
)

type AuthenticationMode int

type AuthenticationMiddlewareClient struct {
	Mode    AuthenticationMode
	APIKeys []string
}

type AuthenticationMiddlewareOption func(*AuthenticationMiddlewareClient)

func WithAPIKeys(apiKeys []string) AuthenticationMiddlewareOption {
	return func(c *AuthenticationMiddlewareClient) {
		c.Mode = AuthenticationModeAPIKey
		c.APIKeys = apiKeys
	}
}

func NewAuthenticationMiddlewareClient(opts ...AuthenticationMiddlewareOption) *AuthenticationMiddlewareClient {
	c := &AuthenticationMiddlewareClient{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

const (
	AuthenticationModeAPIKey AuthenticationMode = iota
)

func (amc *AuthenticationMiddlewareClient) AuthenticationMiddleware(next http.Handler) http.Handler {
	switch amc.Mode {
	case AuthenticationModeAPIKey:
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")

			valid := false
			for _, key := range amc.APIKeys {
				if apiKey == key {
					valid = true
					break
				}
			}

			if !valid {
				rfc9457.NewRFC9457(
					rfc9457.WithTitle("invalid api key"),
					rfc9457.WithDetail(fmt.Sprintf("%s is not a valid api key", apiKey)),
					rfc9457.WithInstance(r.URL.Path),
					rfc9457.WithStatus(http.StatusUnauthorized),
				).ServeHTTP(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	default:
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rfc9457.NewRFC9457(
				rfc9457.WithTitle("invalid authentication mode"),
				rfc9457.WithDetail("authentication middleware is misconfigured"),
				rfc9457.WithInstance(r.URL.Path),
				rfc9457.WithStatus(http.StatusInternalServerError),
			).ServeHTTP(w, r)
		})
	}
}
