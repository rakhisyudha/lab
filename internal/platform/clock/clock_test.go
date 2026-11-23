package clock

import (
	"testing"
	"time"
)

func TestSystemNowIsUTC(t *testing.T) {
	var c Clock = System{}
	if loc := c.Now().Location(); loc != time.UTC {
		t.Fatalf("System.Now() location = %v, want UTC", loc)
	}
}

func TestFakeReturnsSetTimeInUTC(t *testing.T) {
	jakarta := time.FixedZone("WIB", 7*60*60)
	start := time.Date(2025, 1, 1, 7, 0, 0, 0, jakarta) // setara 2025-01-01 00:00 UTC

	f := NewFake(start)
	got := f.Now()
	if !got.Equal(start) {
		t.Fatalf("Now() = %v, want instant %v", got, start)
	}
	if got.Location() != time.UTC {
		t.Fatalf("Now() location = %v, want UTC", got.Location())
	}
}

func TestFakeAdvanceAndSet(t *testing.T) {
	base := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	f := NewFake(base)

	f.Advance(90 * time.Second)
	if want := base.Add(90 * time.Second); !f.Now().Equal(want) {
		t.Fatalf("after Advance: Now() = %v, want %v", f.Now(), want)
	}

	f.Set(base.Add(time.Hour))
	if want := base.Add(time.Hour); !f.Now().Equal(want) {
		t.Fatalf("after Set: Now() = %v, want %v", f.Now(), want)
	}
}

func TestFakeConcurrentAccess(t *testing.T) {
	f := NewFake(time.Unix(0, 0))
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				f.Advance(time.Millisecond)
				_ = f.Now()
			}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	if want := time.Unix(0, 0).Add(1000 * time.Millisecond); !f.Now().Equal(want) {
		t.Fatalf("Now() = %v, want %v", f.Now(), want)
	}
}
