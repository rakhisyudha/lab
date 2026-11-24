package evaluation

import (
	"fmt"
	"math"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// TestPropW21_BackoffBerbatas memeriksa Property 21 bagian jeda: untuk
// attempt >= 1 dan base/cap positif, jeda tanpa retry_after berada di [0,
// min(cap, base*2^(attempt-1))], sedangkan jeda dengan retry_after selalu
// minimal sebesar retry_after tersebut.
func TestPropW21_BackoffBerbatas(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		attempt := rapid.IntRange(1, 60).Draw(t, "attempt")
		base := rapid.Float64Range(0.05, 20).Draw(t, "base")
		capNilai := rapid.Float64Range(0.05, 100).Draw(t, "cap")
		r := rapid.Float64Range(0, 1).Draw(t, "r")
		acakan := func() float64 { return r }

		batas := math.Min(capNilai, math.Ldexp(base, attempt-1))
		jeda := BackoffDelay(attempt, nil, base, capNilai, acakan)
		if jeda < 0 {
			t.Fatalf("jeda negatif: %v", jeda)
		}
		if float64(jeda) > batas*float64(time.Second)+1 {
			t.Fatalf("jeda %v melebihi batas %v", jeda, time.Duration(batas*float64(time.Second)))
		}

		retryAfter := rapid.Float64Range(0, 30).Draw(t, "retry_after")
		jedaRetry := BackoffDelay(attempt, &retryAfter, base, capNilai, acakan)
		if float64(jedaRetry) < retryAfter*float64(time.Second)-1 {
			t.Fatalf("jeda retry_after %v lebih kecil dari %v detik", jedaRetry, retryAfter)
		}
	})
}

// TestPropW21_PemilihanProviderMonoton memeriksa Property 21 bagian
// pemilihan provider: indeks yang dipilih tidak pernah menurun ketika
// attemptsDone bertambah, selalu berada di rentang providers, dan percobaan
// pertama memilih primary bila tidak ada cooldown.
func TestPropW21_PemilihanProviderMonoton(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(1, 6).Draw(t, "jumlah_provider")
		providers := make([]string, n)
		for i := range providers {
			providers[i] = fmt.Sprintf("p%d", i)
		}
		maxPerProvider := rapid.IntRange(1, 5).Draw(t, "maks_per_provider")
		now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

		cooldown := map[string]time.Time{}
		for _, p := range providers {
			switch rapid.IntRange(0, 2).Draw(t, "cooldown_"+p) {
			case 0: // tidak ada entri cooldown
			case 1: // cooldown sudah lewat
				cooldown[p] = now.Add(-time.Second)
			case 2: // masih dalam cooldown
				dur := rapid.IntRange(1, 100).Draw(t, "durasi_"+p)
				cooldown[p] = now.Add(time.Duration(dur) * time.Second)
			}
		}

		sebelumnya := -1
		for attemptsDone := 0; attemptsDone <= 30; attemptsDone++ {
			got := SelectProvider(providers, attemptsDone, maxPerProvider, cooldown, now)
			idx := -1
			for i, p := range providers {
				if p == got {
					idx = i
					break
				}
			}
			if idx < 0 || idx >= n {
				t.Fatalf("provider yang dipilih tidak valid: %q", got)
			}
			if idx < sebelumnya {
				t.Fatalf("indeks menurun: %d menjadi %d", sebelumnya, idx)
			}
			sebelumnya = idx
		}

		if got := SelectProvider(providers, 0, maxPerProvider, nil, now); got != providers[0] {
			t.Fatalf("percobaan pertama tanpa cooldown: %q, ingin %q", got, providers[0])
		}
	})
}
