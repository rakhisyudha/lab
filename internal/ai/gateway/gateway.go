// Package gateway menjalankan satu AITask: render prompt, memeriksa token bucket,
// memanggil provider, memvalidasi output, retry, dan fallback.
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rakhisyudha/lab/internal/ai/guard"
	"github.com/rakhisyudha/lab/internal/ai/prompt"
	"github.com/rakhisyudha/lab/internal/ai/provider"
	"github.com/rakhisyudha/lab/internal/ai/schema"
	"github.com/rakhisyudha/lab/internal/ai/taskqueue"
	"github.com/rakhisyudha/lab/internal/evaluation"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/ratelimit"
	"github.com/rakhisyudha/lab/internal/platform/settings"
)

var (
	ErrProviderUnavailable = errors.New("gateway: seluruh provider gagal")
	ErrValidation          = errors.New("gateway: output gagal validasi")
	ErrThrottled           = errors.New("gateway: token bucket belum cukup")
)

// ThrottleInfo memberitahu worker untuk menjadwalkan ulang AITask.
type ThrottleInfo struct {
	RunAfter time.Time
}

func (e *ThrottleInfo) Error() string { return "gateway: throttle" }

type SettingsReader interface {
	Snapshot(ctx context.Context, keys ...string) (settings.Snapshot, error)
}

// UsageWriter mencatat satu baris ai_usage_logs per percobaan.
type UsageWriter func(ctx context.Context, attempt provider.Response, providerName, modelName string,
	status string, latency time.Duration) error

// InjectionWriter mencatat sinyal deteksi injeksi.
type InjectionWriter func(ctx context.Context, signals []guard.Signal) error

// Input adalah data satu pemanggilan Evaluator.
type Input struct {
	Task            taskqueue.Task
	Contract        prompt.Contract
	Data            map[string]any
	LearnerData     string
	AllowedConcepts []string
	CatalogCodes    []string
	DetectedCodes   []string
	ModelName       string
	WriteUsage      UsageWriter
	WriteInjection  InjectionWriter
}

// Result adalah hasil pipeline yang sudah tervalidasi.
type Result struct {
	Raw    json.RawMessage
	Output schema.Output
	Signal evaluation.EvaluatorSignal
}

// Gateway menjalankan pipeline Evaluator.
type Gateway struct {
	providers []provider.Provider
	buckets   ratelimit.Store
	settings  SettingsReader
	clock     clock.Clock
	mu        sync.Mutex
	cooldown  map[string]time.Time
}

func New(providers []provider.Provider, buckets ratelimit.Store, s SettingsReader, c clock.Clock) *Gateway {
	if c == nil {
		c = clock.System{}
	}
	return &Gateway{providers: providers, buckets: buckets, settings: s, clock: c, cooldown: map[string]time.Time{}}
}

// Run menjalankan satu Evaluator hingga berhasil, habis retry, atau provider gagal.
func (g *Gateway) Run(ctx context.Context, in Input) (Result, error) {
	if len(g.providers) == 0 {
		return Result{}, ErrProviderUnavailable
	}
	nonce := newNonce()
	rendered, err := prompt.Render(in.Contract, in.Data, in.LearnerData, nonce)
	if err != nil {
		return Result{}, err
	}
	if in.WriteInjection != nil {
		if signals := guard.Detect(in.LearnerData); len(signals) > 0 {
			_ = in.WriteInjection(ctx, signals)
		}
	}

	snap, err := g.settings.Snapshot(ctx, "ai.max_validation_retries", "ai.provider_max_attempts",
		"ai.backoff_base_seconds", "ai.backoff_cap_seconds")
	if err != nil {
		return Result{}, err
	}
	maxValidation := int(snap.Int("ai.max_validation_retries"))
	if maxValidation < 0 {
		maxValidation = 2
	}
	maxPerProvider := int(snap.Int("ai.provider_max_attempts"))
	if maxPerProvider < 1 {
		maxPerProvider = 2
	}
	base := snap.Float("ai.backoff_base_seconds")
	if base <= 0 {
		base = 2
	}
	capSeconds := snap.Float("ai.backoff_cap_seconds")
	if capSeconds <= 0 {
		capSeconds = 60
	}
	estimated := in.Task.Attempts
	if estimated < 1 {
		estimated = 1
	}

	validationRetries := 0
	attemptsDone := 0
	for attemptsDone < in.Task.MaxAttempts {
		now := g.clock.Now()
		name := evaluation.SelectProvider(providerNames(g.providers), attemptsDone, maxPerProvider, g.cooldowns(), now)
		p := findProvider(g.providers, name)
		if p == nil {
			return Result{}, ErrProviderUnavailable
		}

		// RPM dan TPM harus sama-sama cukup; bila tidak, task dijadwalkan ulang.
		waitRPM, err := g.buckets.TryTake(ctx, "provider:"+name+":rpm", 60, 1, 1, now)
		if err != nil {
			return Result{}, err
		}
		if waitRPM > 0 {
			return Result{}, &ThrottleInfo{RunAfter: now.Add(waitRPM)}
		}
		waitTPM, err := g.buckets.TryTake(ctx, "provider:"+name+":tpm", 100000, 100000.0/60, float64(estimated), now)
		if err != nil {
			return Result{}, err
		}
		if waitTPM > 0 {
			return Result{}, &ThrottleInfo{RunAfter: now.Add(waitTPM)}
		}

		started := now
		resp, callErr := p.Complete(ctx, provider.Request{
			System: rendered.System, User: rendered.User,
			MaxOutputTokens: 1024, Temperature: 0.2, Timeout: 30 * time.Second,
		})
		if in.WriteUsage != nil {
			status := "success"
			if callErr != nil {
				status = "provider_error"
				kind, _ := provider.AsError(callErr)
				if kind == "rate_limited" {
					status = "rate_limited"
				} else if kind == "timeout" {
					status = "timeout"
				}
			}
			_ = in.WriteUsage(ctx, resp, name, in.ModelName, status, g.clock.Now().Sub(started))
		}
		if callErr == nil {
			out, err := schema.ValidateEvaluatorOutput(json.RawMessage(resp.Text),
				in.AllowedConcepts, in.CatalogCodes, in.DetectedCodes)
			if err == nil {
				return Result{
					Raw: json.RawMessage(resp.Text), Output: out, Signal: toSignal(out),
				}, nil
			}
			if validationRetries < maxValidation {
				validationRetries++
				continue
			}
			return Result{}, fmt.Errorf("%w: %v", ErrValidation, err)
		}

		kind, retryAfter := provider.AsError(callErr)
		randFn := func() float64 { return float64(randUint64()>>11) / float64(1<<53) }
		switch kind {
		case "rate_limited":
			var retry *float64
			if retryAfter > 0 {
				v := retryAfter.Seconds()
				retry = &v
			}
			g.setCooldown(name, now.Add(evaluation.BackoffDelay(attemptsDone+1, retry, base, capSeconds, randFn)))
		case "client_error":
			g.setCooldown(name, now.Add(5*time.Minute))
		}
		attemptsDone++
	}
	return Result{}, ErrProviderUnavailable
}

func toSignal(out schema.Output) evaluation.EvaluatorSignal {
	concepts := make([]evaluation.ConceptCoverage, 0, len(out.ConceptResults))
	for _, c := range out.ConceptResults {
		concepts = append(concepts, evaluation.ConceptCoverage{
			ConceptCode: c.ConceptCode, Coverage: c.Coverage, Evidence: c.Evidence,
		})
	}
	return evaluation.EvaluatorSignal{
		ConceptResults: concepts, MisconceptionCodes: out.MisconceptionCodes,
		ResolvedMisconceptionCodes: out.ResolvedMisconceptionCodes,
		Relevance:                  out.Relevance, ScoreHint: out.ScoreHint, Confidence: out.Confidence,
		Feedback: out.Feedback,
	}
}

func (g *Gateway) cooldowns() map[string]time.Time {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make(map[string]time.Time, len(g.cooldown))
	for k, v := range g.cooldown {
		out[k] = v
	}
	return out
}

func (g *Gateway) setCooldown(name string, until time.Time) {
	g.mu.Lock()
	g.cooldown[name] = until
	g.mu.Unlock()
}

func providerNames(ps []provider.Provider) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name())
	}
	return out
}

func findProvider(ps []provider.Provider, name string) provider.Provider {
	for _, p := range ps {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

func newNonce() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func randUint64() uint64 {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return uint64(time.Now().UnixNano())
	}
	var v uint64
	for _, b := range buf {
		v = v<<8 | uint64(b)
	}
	return v
}
