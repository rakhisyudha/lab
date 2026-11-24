package prompts

import (
	"encoding/json"
	"testing"
)

func TestFSMemuatKontrak(t *testing.T) {
	for _, task := range []string{"answer_evaluation", "interview_followup", "interview_evaluation"} {
		path := task + "/v1.json"
		mentah, err := FS.ReadFile(path)
		if err != nil {
			t.Fatalf("gagal membaca %s: %v", path, err)
		}
		var kontrak struct {
			TaskType   string          `json:"task_type"`
			Version    int             `json:"version"`
			Status     string          `json:"status"`
			Parameters json.RawMessage `json:"parameters"`
		}
		if err := json.Unmarshal(mentah, &kontrak); err != nil {
			t.Fatalf("%s bukan JSON valid: %v", path, err)
		}
		if kontrak.TaskType != task || kontrak.Version != 1 || kontrak.Status != "active" {
			t.Fatalf("%q memuat kontrak yang tidak sesuai: %+v", path, kontrak)
		}
		if len(kontrak.Parameters) == 0 {
			t.Fatalf("%q tidak memuat parameters", path)
		}
	}
}
