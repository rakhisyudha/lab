// Package ids membuat primary key aplikasi. Semua id berformat UUID v7 yang
// dibuat di aplikasi (design, konvensi 1.1; ASUMSI A-8).
package ids

import (
	"github.com/google/uuid"
)

// NewV7 membuat UUID v7 baru. Error hanya mungkin terjadi bila sumber acak
// sistem gagal.
func NewV7() (uuid.UUID, error) {
	return uuid.NewV7()
}
