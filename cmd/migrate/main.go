// Command migrate menjalankan migrasi database (up, down), seed prompt, dan
// bootstrap admin.
//
// Pada tahap tugas 1.1, perintah belum tersedia. Implementasi golang-migrate
// dipasang pada tugas 2.1.
package main

import (
	"errors"
	"fmt"
	"os"
)

var errNotYetImplemented = errors.New("perintah migrasi belum dipasang (tugas 2.1)")

func main() {
	fmt.Fprintln(os.Stderr, "migrate:", errNotYetImplemented)
	os.Exit(1)
}
