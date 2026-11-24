// Package ratelimit menyediakan token bucket untuk pembatasan laju HTTP dan
// penyedia AI. Perhitungan token murni (TryTake) dipisahkan dari penyimpanan
// agar batas kapasitas dan laju dapat diuji secara properti (Requirement 16.6, 16.7, 26.4).
package ratelimit

import (
	"math"
	"time"
)

// WaitInfinite adalah bentuk yang dapat direpresentasikan dari +Inf untuk
// time.Duration (int64 tidak memiliki nilai tak hingga). Nilai ini dipakai bila
// cost melebihi kapasitas, sehingga request tidak mungkin berhasil setelah
// menunggu berapa pun lamanya.
const WaitInfinite = time.Duration(math.MaxInt64)

// Bucket adalah state token bucket untuk satu kunci. Capacity dan RatePerSec
// menjadi konfigurasi; Tokens dan UpdatedAt menjadi state yang disimpan store.
type Bucket struct {
	Capacity   float64
	RatePerSec float64
	Tokens     float64
	UpdatedAt  time.Time
}

// TryTake mengambil cost token dari bucket pada waktu now. Fungsi ini murni:
// bucket masukan tidak diubah, hasilnya dikembalikan sebagai nilai baru.
//
// Isi ulang dilakukan lebih dulu:
//
//	tokens = min(capacity, tokens + (now - updated_at) * rate)
//	updated_at = now
//
// Bila cost > Capacity, hasil wait = WaitInfinite (+Inf) dan token tidak diambil
// (bucket hanya terisi ulang). Bila token mencukupi, token dikurangi cost dan
// wait = 0. Selain itu, wait = (cost - tokens) / rate dan token tidak diubah.
func TryTake(b Bucket, cost float64, now time.Time) (Bucket, time.Duration) {
	// Elapsed negatif (waktu mundur) dianggap nol agar token tidak berkurang.
	elapsed := now.Sub(b.UpdatedAt).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	tokens := b.Tokens + elapsed*b.RatePerSec
	switch {
	case tokens < 0:
		tokens = 0
	case tokens > b.Capacity:
		tokens = b.Capacity
	}
	b.Tokens = tokens
	b.UpdatedAt = now

	if cost > b.Capacity {
		return b, WaitInfinite
	}
	if cost <= 0 {
		return b, 0
	}

	// Toleransi relatif kecil mengatasi pembulatan saat wait dikonversi ke
	// Nanodetik, sehingga percobaan ulang tepat setelah wait selalu berhasil.
	tol := 1e-9 * math.Max(1, math.Abs(b.Capacity))
	if b.Tokens+tol >= cost {
		b.Tokens -= cost
		if b.Tokens < 0 {
			b.Tokens = 0
		}
		return b, 0
	}
	if b.RatePerSec <= 0 {
		// Tanpa isi ulang, bucket tidak akan pernah mencukupi.
		return b, WaitInfinite
	}

	seconds := (cost - b.Tokens) / b.RatePerSec
	// Pembulatan ke atas memastikan waktu tunggu tidak pernah kurang dari yang
	// dibutuhkan ketika dikonversi ke time.Duration.
	nanos := math.Ceil(seconds * float64(time.Second))
	if !(nanos < float64(math.MaxInt64)) {
		return b, WaitInfinite
	}
	return b, time.Duration(nanos)
}

// isInfiniteWait melaporkan apakah wait berarti tak hingga. Pendekatan ini
// menerima baik sentinel waktu maupun +Inf bila suatu hari tipe hasil diubah.
func isInfiniteWait(wait time.Duration) bool {
	return wait == WaitInfinite || math.IsInf(float64(wait), 1)
}
