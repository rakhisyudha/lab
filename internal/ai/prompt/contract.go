// Package prompt memuat kontrak prompt dan menyusun dua pesan untuk provider AI.
// Pesan system tidak pernah memuat teks peserta didik; pesan user memuatnya di
// dalam blok delimiter dengan nonce yang berbeda pada setiap pemanggilan.
package prompt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/rakhisyudha/lab/internal/ai/guard"
)

// Contract adalah satu versi kontrak prompt untuk sebuah task_type.
type Contract struct {
	ContractID           string          `json:"contract_id"`
	TaskType             string          `json:"task_type"`
	Version              int             `json:"version"`
	LocalePolicy         string          `json:"locale_policy"`
	SystemPrompt         string          `json:"system_prompt"`
	UserPromptTemplate   string          `json:"user_prompt_template"`
	InputSchema          json.RawMessage `json:"input_schema"`
	OutputSchema         json.RawMessage `json:"output_schema"`
	Parameters           json.RawMessage `json:"parameters"`
	Status               string          `json:"status"`
	ExampleInput         json.RawMessage `json:"example_input"`
	ExampleValidOutput   json.RawMessage `json:"example_valid_output"`
	ExampleInvalidOutput json.RawMessage `json:"example_invalid_output"`
}

// Rendered adalah hasil penyusunan pesan system dan user.
type Rendered struct {
	System string
	User   string
}

// awalLearner dan akhirLearner adalah pembuka dan penutup blok data peserta didik.
const (
	awalLearner  = "<<<LEARNER_DATA_"
	akhirLearner = "<<<END_LEARNER_DATA_"
	penutup      = ">>>"
)

// kunciLearner adalah daftar nama placeholder yang dicadangkan untuk blok data
// peserta didik. Kunci tersebut ditimpa oleh Render agar nilai dari pemanggil
// tidak pernah masuk ke pesan system dan tidak pernah keluar dari delimiter.
var kunciLearner = []string{
	"LearnerData", "learner_data", "learnerData",
	"LearnerAnswer", "learner_answer", "learnerAnswer",
	"LearnerText", "learner_text", "LearnerInput", "learner_input",
	"Learner", "learner", "UntrustedData", "untrusted_data",
	"Answer", "answer", "LastAnswer", "last_answer",
	"Transcript", "transcript",
}

// kunciKonteks adalah placeholder dikenal yang bisa dipakai template. Nilai
// yang tidak diberikan pemanggil diisi string kosong agar hasil tidak memuat
// penanda "<no value>".
var kunciKonteks = []string{
	"ContextJSON", "context_json", "Question", "question",
	"Concepts", "concepts", "Locale", "locale",
	"MisconceptionCatalog", "misconception_catalog", "DetectedCodes", "detected_codes",
	"FocusConcepts", "focus_concepts", "FocusMisconceptions", "focus_misconceptions",
}

// Render menyusun pesan system dan user dari sebuah kontrak. Teks peserta didik
// dinetralkan dengan guard.Sanitize, dibungkus delimiter bernonce, dan hanya
// disisipkan ke pesan user. Template dilewatkan melalui text/template dengan
// peta data yang diberikan pemanggil; teks peserta didik tidak pernah dievaluasi
// sebagai template.
func Render(c Contract, data map[string]any, learnerData, nonce string) (Rendered, error) {
	bersih := guard.Sanitize(learnerData, nonce)
	blok := awalLearner + nonce + penutup + "\n" + bersih + "\n" + akhirLearner + nonce + penutup

	// Data system tidak boleh memuat placeholder data peserta didik.
	systemData := salinData(data)
	isiNilaiAwal(systemData)
	for _, k := range kunciLearner {
		systemData[k] = ""
	}
	systemData["Nonce"] = nonce
	systemData["nonce"] = nonce
	isiSystem, err := jalankanTemplate("system", c.SystemPrompt, systemData)
	if err != nil {
		return Rendered{}, err
	}
	if strings.Contains(isiSystem, blok) ||
		strings.Contains(isiSystem, awalLearner+nonce+penutup) ||
		strings.Contains(isiSystem, akhirLearner+nonce+penutup) {
		return Rendered{}, fmt.Errorf("prompt: pesan system memuat blok data peserta didik")
	}

	// Data user memuat blok lengkap di bawah semua nama placeholder cadangan.
	userData := salinData(data)
	isiNilaiAwal(userData)
	for _, k := range kunciLearner {
		userData[k] = blok
	}
	userData["Nonce"] = nonce
	userData["nonce"] = nonce
	isiUser, err := jalankanTemplate("user", c.UserPromptTemplate, userData)
	if err != nil {
		return Rendered{}, err
	}

	// Jika template tidak menyebut placeholder apa pun, blok tetap dijamin ada
	// tepat satu kali pada pesan user.
	if !strings.Contains(isiUser, blok) {
		if isiUser != "" && !strings.HasSuffix(isiUser, "\n") {
			isiUser += "\n"
		}
		isiUser += blok
	}

	return Rendered{System: isiSystem, User: isiUser}, nil
}

// Load membaca berkas <dir>/<task>/v1.json untuk setiap subdirektori dan
// mengembalikan peta kontrak yang dikunci berdasarkan task_type.
func Load(dir string) (map[string]Contract, error) {
	entri, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("prompt: gagal membaca direktori %q: %w", dir, err)
	}

	hasil := make(map[string]Contract)
	for _, e := range entri {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "v1.json")
		mentah, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("prompt: gagal membaca %q: %w", path, err)
		}
		var c Contract
		if err := json.Unmarshal(mentah, &c); err != nil {
			return nil, fmt.Errorf("prompt: kontrak %q tidak valid: %w", path, err)
		}
		kunci := c.TaskType
		if kunci == "" {
			kunci = e.Name()
		}
		hasil[kunci] = c
	}
	if len(hasil) == 0 {
		return nil, fmt.Errorf("prompt: tidak ada kontrak pada direktori %q", dir)
	}
	return hasil, nil
}

// salinData menyalin peta agar pemanggilan Render tidak mengubah data pemanggil.
func salinData(data map[string]any) map[string]any {
	salinan := make(map[string]any, len(data)+len(kunciKonteks)+len(kunciLearner)+2)
	for k, v := range data {
		salinan[k] = v
	}
	return salinan
}

// isiNilaiAwal menambahkan placeholder konteks yang tidak diberikan pemanggil.
func isiNilaiAwal(data map[string]any) {
	for _, k := range kunciKonteks {
		if _, ada := data[k]; !ada {
			data[k] = ""
		}
	}
}

// jalankanTemplate mengurai dan mengeksekusi satu template teks.
func jalankanTemplate(nama, teks string, data map[string]any) (string, error) {
	tmpl, err := template.New(nama).Option("missingkey=zero").Parse(teks)
	if err != nil {
		return "", fmt.Errorf("prompt: gagal mengurai template %s: %w", nama, err)
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, data); err != nil {
		return "", fmt.Errorf("prompt: gagal menjalankan template %s: %w", nama, err)
	}
	return b.String(), nil
}
