// Package clock menyediakan abstraksi waktu agar job, kedaluwarsa token, dan
// logika berbasis waktu dapat diuji tanpa bergantung pada jam sistem.
package clock

import (
	"sync"
	"time"
)

// Clock adalah sumber waktu aplikasi. Semua pemanggilan waktu di kode bisnis
// lewat interface ini (lihat design, "Konvensi implementasi").
type Clock interface {
	Now() time.Time
}

// System adalah Clock nyata yang membaca jam sistem dan mengembalikan waktu UTC.
type System struct{}

// Now mengembalikan waktu sekarang dalam UTC.
func (System) Now() time.Time {
	return time.Now().UTC()
}

// Fake adalah Clock palsu untuk pengujian. Waktunya dikendalikan penguji dan
// aman dipakai dari beberapa goroutine.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake membuat Fake yang memulai dari waktu t. Waktu disimpan dalam UTC.
func NewFake(t time.Time) *Fake {
	return &Fake{now: t.UTC()}
}

// Now mengembalikan waktu palsu saat ini.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Set mengganti waktu palsu dengan t (disimpan dalam UTC).
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t.UTC()
}

// Advance memajukan waktu palsu sebesar d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}
