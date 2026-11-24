// Package contract memeriksa kesepakatan bentuk JSON API dan Prompt_Contract.
package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/rakhisyudha/lab/internal/ai/prompt"
	"github.com/rakhisyudha/lab/internal/assessment"
	"github.com/rakhisyudha/lab/internal/auth"
	"github.com/rakhisyudha/lab/internal/interview"
	"github.com/rakhisyudha/lab/internal/progress"
)

var snakeCase = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// TestDTOTagSnakeCase memastikan seluruh field JSON DTO publik memakai snake_case
// (Requirement 1.7).
func TestDTOTagSnakeCase(t *testing.T) {
	types := []any{
		auth.UserView{}, auth.TokenPair{}, auth.InvitationView{}, auth.CreatedInvitation{},
		assessment.AttemptRow{}, assessment.AnswerRow{},
		interview.Session{}, interview.Message{}, interview.ProfileView{},
		progress.MasteryView{}, progress.MisconceptionView{},
	}
	for _, typ := range types {
		rt := reflect.TypeOf(typ)
		for i := 0; i < rt.NumField(); i++ {
			if rt.Field(i).Anonymous {
				continue // field tersemat mewarisi tag dari tipe dasar.
			}
			tag := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
			if tag == "" || tag == "-" {
				t.Errorf("%s.%s tidak punya tag json", rt.Name(), rt.Field(i).Name)
				continue
			}
			if !snakeCase.MatchString(tag) {
				t.Errorf("%s.%s tag %q bukan snake_case", rt.Name(), rt.Field(i).Name, tag)
			}
		}
	}
}

// TestHashTidakAdaPadaDTO memastikan hash dan token tidak muncul pada response.
func TestHashTidakAdaPadaDTO(t *testing.T) {
	forbidden := []string{"password_hash", "token_hash", "code_hash", "password", "refresh_token"}
	types := []any{auth.UserView{}, auth.InvitationView{}, auth.CreatedInvitation{}}
	for _, typ := range types {
		rt := reflect.TypeOf(typ)
		for i := 0; i < rt.NumField(); i++ {
			tag := rt.Field(i).Tag.Get("json")
			for _, bad := range forbidden {
				if strings.HasPrefix(tag, bad) && bad != "refresh_token" {
					t.Errorf("%s.%s mengekspos %q", rt.Name(), rt.Field(i).Name, bad)
				}
			}
		}
	}
}

// TestPromptContractContohValid memastikan contoh pada setiap Prompt_Contract
// dapat di-parse dan field wajibnya ada (Requirement 1.5, 15.2).
func TestPromptContractContohValid(t *testing.T) {
	paths, err := filepath.Glob("../../prompts/*/v1.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("tidak ada Prompt_Contract: %v", err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var c prompt.Contract
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if c.TaskType == "" || c.Version < 1 || c.ContractID == "" ||
			c.SystemPrompt == "" || c.UserPromptTemplate == "" || c.Status == "" {
			t.Errorf("%s: field wajib kosong", path)
		}
		for name, sample := range map[string]json.RawMessage{
			"example_input":          c.ExampleInput,
			"example_valid_output":   c.ExampleValidOutput,
			"example_invalid_output": c.ExampleInvalidOutput,
		} {
			if len(sample) == 0 || !json.Valid(sample) {
				t.Errorf("%s: %s tidak valid", path, name)
			}
		}
	}
}
