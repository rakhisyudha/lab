// Command worker menjalankan pemroses AITask dan job terjadwal (abandon sesi,
// retention, unscore jawaban failed, pembersihan rate_buckets).
//
// Pada tahap tugas 1.1, binary ini hanya memuat dan memvalidasi konfigurasi.
// Loop worker dipasang pada tahap AI (fase 2).
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/rakhisyudha/lab/internal/platform/config"
)

var errNotYetImplemented = errors.New("worker AITask belum dipasang (fase 2)")

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "worker:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "worker: konfigurasi valid (deployment_mode=%s, queue_store=%s)\n",
		cfg.DeploymentMode, cfg.QueueStore)
	return errNotYetImplemented
}
