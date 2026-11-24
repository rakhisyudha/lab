// Package provider mendefinisikan antarmuka model AI dan adapter-nya.
// Kredensial provider hanya dibaca dari environment (Requirement 18.6).
package provider

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Request adalah satu pemanggilan model.
type Request struct {
	System          string
	User            string
	MaxOutputTokens int
	Temperature     float64
	Timeout         time.Duration
}

// Response adalah hasil pemanggilan model beserta pemakaian token.
type Response struct {
	Text         string
	InputTokens  int
	OutputTokens int
	HTTPStatus   int
}

// Provider adalah adapter satu model AI.
type Provider interface {
	Name() string
	Complete(ctx context.Context, req Request) (Response, error)
}

// Jenis error provider yang dipakai gateway untuk fallback dan backoff.
type RateLimited struct {
	RetryAfter time.Duration
}

func (e *RateLimited) Error() string { return "provider: rate limited" }

type Timeout struct{}

func (e *Timeout) Error() string { return "provider: timeout" }

type Server struct {
	Status int
}

func (e *Server) Error() string { return fmt.Sprintf("provider: server error %d", e.Status) }

type Client struct {
	Status int
}

func (e *Client) Error() string { return fmt.Sprintf("provider: client error %d", e.Status) }

// AsError mengembalikan jenis error provider bawa salah satu cocok.
func AsError(err error) (kind string, retryAfter time.Duration) {
	var rl *RateLimited
	if errors.As(err, &rl) {
		return "rate_limited", rl.RetryAfter
	}
	var to *Timeout
	if errors.As(err, &to) {
		return "timeout", 0
	}
	var se *Server
	if errors.As(err, &se) {
		return "server_error", 0
	}
	var ce *Client
	if errors.As(err, &ce) {
		return "client_error", 0
	}
	return "unknown", 0
}
