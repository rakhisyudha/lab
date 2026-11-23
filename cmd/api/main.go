// Command api adalah HTTP server Backend_Lab (Gin, prefix /api/v1).
//
// Pada tahap tugas 1.1, binary ini hanya memuat dan memvalidasi konfigurasi
// (termasuk aturan penyimpanan queue dan rate limit). Server HTTP dipasang di
// tugas 1.2.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/rakhisyudha/lab/internal/platform/config"
)

var errNotYetImplemented = errors.New("server HTTP belum dipasang (tugas 1.2)")

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "api: konfigurasi valid (deployment_mode=%s, rate_limit_store=%s, queue_store=%s)\n",
		cfg.DeploymentMode, cfg.RateLimitStore, cfg.QueueStore)
	return errNotYetImplemented
}
