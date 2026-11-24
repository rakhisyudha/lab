package evaluation

import (
	"math"
	"time"
)

// BackoffDelay menghitung jeda sebelum percobaan berikutnya. Nilai rand
// diharapkan berada pada [0,1); nilai di luar rentang itu dibatasi agar jeda
// tetap berada dalam batas yang ditentukan. Bila retryAfter tidak nil
// (dalam detik), jeda = retryAfter + rand()*1. Bila tidak, batas atas adalah
// min(cap, base*2^(attempt-1)) dan jeda = rand()*batas tersebut.
//
// Fungsi ini murni: seluruh sumber acakan dan waktu disuntikkan pemanggil
// (Requirement 26.4).
func BackoffDelay(attempt int, retryAfter *float64, base, cap float64, rand func() float64) time.Duration {
	r := rand()
	if r < 0 {
		r = 0
	}
	if r > 1 {
		r = 1
	}

	var detik float64
	if retryAfter != nil {
		detik = *retryAfter + r
	} else {
		a := attempt
		if a < 1 {
			a = 1
		}
		// Ldexp menghitung base*2^(a-1). Untuk a besar hasilnya +Inf dan
		// dibatasi oleh cap melalui math.Min, sehingga tidak meluap.
		batas := math.Min(cap, math.Ldexp(base, a-1))
		detik = r * batas
	}
	if detik < 0 {
		detik = 0
	}

	nanodetik := detik * float64(time.Second)
	if nanodetik > float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(nanodetik)
}

// SelectProvider memilih provider untuk percobaan berikutnya berdasarkan
// jumlah percobaan yang sudah dilakukan. Indeks awal adalah
// min(attemptsDone/maxPerProvider, len(providers)-1). Pemindaian dilakukan
// ke depan dari indeks tersebut untuk mencari provider pertama yang tidak
// sedang dalam cooldown (cooldown[provider] <= now). Bila semuanya sedang
// cooldown, dikembalikan providers[indeks awal].
//
// Pemanggil menjamin providers tidak kosong dan maxPerProvider >= 1;
// pertahanan tambahan ditambahkan agar fungsi tetap total.
func SelectProvider(providers []string, attemptsDone, maxPerProvider int, cooldown map[string]time.Time, now time.Time) string {
	if len(providers) == 0 {
		return ""
	}
	if maxPerProvider < 1 {
		maxPerProvider = 1
	}
	if attemptsDone < 0 {
		attemptsDone = 0
	}

	idx := attemptsDone / maxPerProvider
	if idx > len(providers)-1 {
		idx = len(providers) - 1
	}
	for i := idx; i < len(providers); i++ {
		sampai, ada := cooldown[providers[i]]
		if !ada || !sampai.After(now) {
			return providers[i]
		}
	}
	return providers[idx]
}
