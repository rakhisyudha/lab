package provider

import (
	"context"
	"fmt"
	"sync"
)

// RecordedResponse adalah satu respons terekam dari golden_recordings.
type RecordedResponse struct {
	RawText      string `json:"raw_text"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

// ReplayProvider memutar respons terekam secara berurutan. Dipakai oleh mode
// `recorded` dan pipeline CI tanpa memanggil model nyata (Requirement 17.12).
type ReplayProvider struct {
	name      string
	mu        sync.Mutex
	responses []RecordedResponse
	next      int
}

func NewReplayProvider(name string, responses []RecordedResponse) *ReplayProvider {
	return &ReplayProvider{name: name, responses: append([]RecordedResponse(nil), responses...)}
}

func (p *ReplayProvider) Name() string { return p.name }

func (p *ReplayProvider) Complete(_ context.Context, _ Request) (Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.next >= len(p.responses) {
		return Response{}, fmt.Errorf("provider replay: rekaman habis untuk %s", p.name)
	}
	r := p.responses[p.next]
	p.next++
	return Response{
		Text:         r.RawText,
		InputTokens:  r.InputTokens,
		OutputTokens: r.OutputTokens,
		HTTPStatus:   200,
	}, nil
}

// Remaining menghitung sisa respons yang belum diputar.
func (p *ReplayProvider) Remaining() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.responses) - p.next
}
