package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rakhisyudha/lab/internal/auth"
	"github.com/rakhisyudha/lab/internal/platform/clock"
	"github.com/rakhisyudha/lab/internal/platform/db"
	"github.com/rakhisyudha/lab/internal/platform/settings"
)

const envBootstrapPassword = "BOOTSTRAP_ADMIN_PASSWORD"

// bootstrapAdmin membuat user admin pertama bila belum ada. Registrasi bersifat
// invite-only, sehingga bootstrap diperlukan untuk membuat Admin awal (design 1.2).
// Password hanya dibaca dari environment dan tidak pernah dicetak.
func bootstrapAdmin(args []string, stdout, stderr io.Writer, lookup func(string) (string, bool)) error {
	fset := newFlagSet("bootstrap-admin", stderr)
	email := fset.String("email", "", "email admin pertama")
	if err := fset.Parse(args); err != nil {
		return errUsage
	}
	rawEmail := strings.TrimSpace(*email)
	if rawEmail == "" {
		return errors.New("bootstrap-admin memerlukan --email")
	}
	password, ok := lookup(envBootstrapPassword)
	if !ok || strings.TrimSpace(password) == "" {
		return fmt.Errorf("%s wajib diisi", envBootstrapPassword)
	}
	databaseURL, _ := lookup(envDatabaseURL)
	if databaseURL == "" {
		return fmt.Errorf("%s wajib diisi", envDatabaseURL)
	}

	ctx := context.Background()
	pool, err := db.NewPool(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	c := clock.System{}
	settingsSvc := settings.New(settings.NewPostgresStore(pool), c, 30*time.Second)
	authSvc := auth.NewService(auth.NewStore(pool), auth.NewTokenIssuer("bootstrap-only"),
		settingsSvc, c, auth.DefaultPasswordPolicy(), 0)

	created, err := authSvc.BootstrapAdmin(ctx, rawEmail, password)
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintln(stdout, "migrate: admin pertama dibuat")
	} else {
		fmt.Fprintln(stdout, "migrate: admin sudah ada, tidak ada perubahan")
	}
	return nil
}
