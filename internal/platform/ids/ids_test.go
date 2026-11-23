package ids

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewV7ReturnsVersion7AndRFCVariant(t *testing.T) {
	id, err := NewV7()
	if err != nil {
		t.Fatalf("NewV7() error = %v", err)
	}
	if v := id.Version(); v != 7 {
		t.Fatalf("version = %d, want 7", v)
	}
	if id.Variant() != uuid.RFC4122 {
		t.Fatalf("variant = %v, want RFC4122", id.Variant())
	}
}

func TestNewV7IsUniqueAndTimeOrdered(t *testing.T) {
	first, err := NewV7()
	if err != nil {
		t.Fatalf("NewV7() error = %v", err)
	}
	time.Sleep(2 * time.Millisecond) // selisih milidetik agar bagian waktu UUID berbeda
	second, err := NewV7()
	if err != nil {
		t.Fatalf("NewV7() error = %v", err)
	}
	if first == second {
		t.Fatal("two consecutive NewV7 values are equal")
	}
	if first.String() >= second.String() {
		t.Fatalf("ids not time-ordered: %s >= %s", first, second)
	}
}
