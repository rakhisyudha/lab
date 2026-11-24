package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// HTTPProvider memanggil endpoint chat-completions yang kompatibel dengan
// format OpenAI. BaseURL, model, dan API key berasal dari konfigurasi.
type HTTPProvider struct {
	name    string
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func NewHTTPProvider(name, baseURL, model, apiKey string) *HTTPProvider {
	return &HTTPProvider{
		name: name, baseURL: strings.TrimRight(baseURL, "/"), model: model, apiKey: apiKey,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (p *HTTPProvider) Name() string { return p.name }

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Complete mengirim request dan memetakan status HTTP ke jenis error provider.
func (p *HTTPProvider) Complete(ctx context.Context, req Request) (Response, error) {
	body, err := json.Marshal(chatRequest{
		Model: p.model,
		Messages: []chatMessage{
			{Role: "system", Content: req.System},
			{Role: "user", Content: req.User},
		},
		Temperature: req.Temperature,
		MaxTokens:   req.MaxOutputTokens,
	})
	if err != nil {
		return Response{}, err
	}
	callCtx := ctx
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost,
		p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		if callCtx.Err() != nil {
			return Response{}, &Timeout{}
		}
		return Response{}, &Server{Status: 0}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Response{}, err
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		var retry time.Duration
		if v := resp.Header.Get("Retry-After"); v != "" {
			if secs, err := strconv.Atoi(v); err == nil {
				retry = time.Duration(secs) * time.Second
			}
		}
		return Response{HTTPStatus: resp.StatusCode}, &RateLimited{RetryAfter: retry}
	case resp.StatusCode >= 500:
		return Response{HTTPStatus: resp.StatusCode}, &Server{Status: resp.StatusCode}
	case resp.StatusCode >= 400:
		return Response{HTTPStatus: resp.StatusCode}, &Client{Status: resp.StatusCode}
	}
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Response{HTTPStatus: resp.StatusCode}, err
	}
	if len(parsed.Choices) == 0 {
		return Response{HTTPStatus: resp.StatusCode}, fmt.Errorf("provider: respons tanpa choices")
	}
	return Response{
		Text:         parsed.Choices[0].Message.Content,
		InputTokens:  parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
		HTTPStatus:   resp.StatusCode,
	}, nil
}
