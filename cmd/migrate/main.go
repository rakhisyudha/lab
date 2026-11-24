// Command migrate menjalankan migrasi database dengan golang-migrate.
//
// Pemakaian:
//
//	migrate up [--include-p1]
//	migrate down --all [--include-p1]
//
// DATABASE_URL wajib diisi. Nilainya tidak pernah dicetak, karena memuat kredensial.
// `down` menghapus seluruh skema migrasi dan hanya untuk development dan CI.
// Produksi memakai migrasi koreksi (lihat design, bagian Strategi migrasi).
//
// Perintah seed-prompts dan bootstrap-admin dipasang pada tugas lain.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
)

// errUsage dikembalikan bila perintah atau flag tidak dikenal.
var errUsage = errors.New("pemakaian: migrate up [--include-p1] | migrate down --all [--include-p1]")

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr, os.LookupEnv); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

// run menjalankan satu perintah. lookup dipisah agar mudah diuji.
func run(args []string, stdout, stderr io.Writer, lookup func(string) (string, bool)) error {
	if len(args) == 0 {
		return errUsage
	}
	cmd, rest := args[0], args[1:]

	switch cmd {
	case "up":
		fset := newFlagSet("up", stderr)
		includeP1 := fset.Bool("include-p1", false, "ikut sertakan migrasi P1 (bernomor 0100 ke atas)")
		if err := fset.Parse(rest); err != nil {
			return errUsage
		}
		return withMigrator(lookup, *includeP1, stdout, func(m *migrate.Migrate) error {
			return up(m, stdout)
		})

	case "down":
		fset := newFlagSet("down", stderr)
		all := fset.Bool("all", false, "rollback seluruh migrasi (wajib, sebagai pengaman)")
		includeP1 := fset.Bool("include-p1", false, "ikut sertakan migrasi P1 (bernomor 0100 ke atas)")
		if err := fset.Parse(rest); err != nil {
			return errUsage
		}
		if !*all {
			return errors.New("down memerlukan --all untuk rollback seluruh migrasi")
		}
		return withMigrator(lookup, *includeP1, stdout, func(m *migrate.Migrate) error {
			return down(m, stdout)
		})

	default:
		return fmt.Errorf("perintah %q tidak dikenal. %w", cmd, errUsage)
	}
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fset := flag.NewFlagSet(name, flag.ContinueOnError)
	fset.SetOutput(stderr)
	return fset
}

// up menerapkan seluruh migrasi yang belum dijalankan. Tidak ada perubahan bukan error.
func up(m *migrate.Migrate, stdout io.Writer) error {
	err := m.Up()
	if errors.Is(err, migrate.ErrNoChange) {
		fmt.Fprintln(stdout, "migrate: skema sudah pada versi terbaru")
		return nil
	}
	if err != nil {
		return fmt.Errorf("up: %w", err)
	}
	return reportVersion(m, stdout)
}

// down membatalkan seluruh migrasi sampai skema kosong.
func down(m *migrate.Migrate, stdout io.Writer) error {
	err := m.Down()
	if errors.Is(err, migrate.ErrNoChange) {
		fmt.Fprintln(stdout, "migrate: tidak ada migrasi yang perlu di-rollback")
		return nil
	}
	if err != nil {
		return fmt.Errorf("down: %w", err)
	}
	fmt.Fprintln(stdout, "migrate: seluruh migrasi di-rollback")
	return nil
}

func reportVersion(m *migrate.Migrate, stdout io.Writer) error {
	version, dirty, err := m.Version()
	if err != nil {
		return fmt.Errorf("membaca versi skema: %w", err)
	}
	fmt.Fprintf(stdout, "migrate: skema pada versi %d (dirty=%t)\n", version, dirty)
	return nil
}

// withMigrator membuka Migrate, menjalankan fn, lalu menutup koneksi dan sumber.
func withMigrator(lookup func(string) (string, bool), includeP1 bool, stdout io.Writer, fn func(*migrate.Migrate) error) error {
	dbURL, _ := lookup(envDatabaseURL)
	m, err := openMigrator(dbURL, includeP1, stdout)
	if err != nil {
		return err
	}

	runErr := fn(m)
	srcErr, dbErr := m.Close()
	return errors.Join(runErr, srcErr, dbErr)
}
