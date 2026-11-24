package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/rakhisyudha/lab/internal/ai/prompt"
	"github.com/rakhisyudha/lab/internal/platform/db"
	"github.com/rakhisyudha/lab/prompts"
)

// seedPrompts memasukkan kontrak prompt bawaan ke tabel prompt_contracts.
// Idempoten berdasarkan (task_type, version, content_sha256): ON CONFLICT DO
// NOTHING menjaga kontrak yang sudah diubah Admin tetap utuh.
func seedPrompts(args []string, stdout, stderr io.Writer, lookup func(string) (string, bool)) error {
	fset := newFlagSet("seed-prompts", stderr)
	if err := fset.Parse(args); err != nil {
		return errUsage
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

	inserted := 0
	// Setiap task_type berada dalam direktori sendiri: <task>/v1.json.
	roots, err := fs.Glob(prompts.FS, "*/*.json")
	if err != nil {
		return err
	}
	for _, path := range roots {
		raw, err := fs.ReadFile(prompts.FS, path)
		if err != nil {
			return err
		}
		var contract prompt.Contract
		if err := json.Unmarshal(raw, &contract); err != nil {
			return fmt.Errorf("membaca %s: %w", path, err)
		}
		if contract.TaskType == "" || contract.Version < 1 {
			return fmt.Errorf("kontrak %s tidak valid", path)
		}
		sum := sha256.Sum256(raw)
		hash := hex.EncodeToString(sum[:])
		status := contract.Status
		if status == "" {
			status = "active"
		}
		var activatedAt any
		var waiver any
		if status == "active" {
			activatedAt = time.Now().UTC()
			waiver = "bootstrap"
		}
		tag, err := pool.Exec(ctx, `INSERT INTO prompt_contracts
			(task_type, version, system_prompt, user_prompt_template, input_schema,
			 output_schema, parameters, status, content_sha256, activated_at, activation_waiver_reason)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (task_type, version) DO NOTHING`,
			contract.TaskType, contract.Version, contract.SystemPrompt, contract.UserPromptTemplate,
			contract.InputSchema, contract.OutputSchema, contract.Parameters, status, hash,
			activatedAt, waiver)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			inserted++
		}
	}
	fmt.Fprintf(stdout, "migrate: prompt contracts tersedia (%d baru dimasukkan)\n", inserted)
	return nil
}
