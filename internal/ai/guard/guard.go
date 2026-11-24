// Package guard berisi pertahanan terhadap prompt injection pada teks peserta didik.
// Teks peserta didik selalu diperlakukan sebagai data, bukan instruksi.
package guard

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Batas panjang teks peserta didik, dihitung dalam rune.
const maxLearnerRunes = 8000

// panjangPotongan adalah panjang maksimum cuplikan sinyal, dihitung dalam rune.
const panjangPotongan = 300

// pengganti dipakai untuk menetralkan token yang menyerupai delimiter.
const pengganti = "[FILTERED]"

// Sanitize menetralkan teks peserta didik sebelum disisipkan ke dalam prompt.
// Fungsi ini mengganti kemunculan "LEARNER_DATA", "<<<", ">>>", dan nonce aktif
// dengan "[FILTERED]", membuang karakter kontrol (kecuali newline dan tab),
// menormalkan akhir baris, dan memotong hasil menjadi paling banyak 8000 rune.
func Sanitize(s, nonce string) string {
	// Normalisasi akhir baris terlebih dahulu agar CR tidak ikut terbuang sebagai kontrol.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\n' || r == '\t' {
			b.WriteRune(r)
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	s = b.String()

	// Nonce aktif diganti lebih dulu agar tidak menyisakan fragmen delimiter.
	// Nonce kosong tidak boleh diganti karena ReplaceAll dengan "" akan menyisipkan
	// pengganti di antara setiap rune.
	if nonce != "" {
		s = strings.ReplaceAll(s, nonce, pengganti)
	}
	s = strings.ReplaceAll(s, "LEARNER_DATA", pengganti)
	s = strings.ReplaceAll(s, "<<<", pengganti)
	s = strings.ReplaceAll(s, ">>>", pengganti)

	// Pemotongan dilakukan terakhir supaya panjang akhir tetap terjamin.
	if utf8.RuneCountInString(s) > maxLearnerRunes {
		runes := []rune(s)
		s = string(runes[:maxLearnerRunes])
	}
	return s
}

// Signal adalah satu pola heuristik yang terdeteksi pada teks peserta didik.
// Deteksi hanya bersifat pelaporan dan tidak menghentikan penilaian.
type Signal struct {
	Detected bool   // selalu true untuk sinyal yang dikembalikan Detect.
	Pattern  string // nama pola, misalnya "abaikan_instruksi".
	Excerpt  string // cuplikan teks asli, maksimum 300 rune.
}

// pola memetakan nama pola ke daftar fragmen case-insensitive yang dicari.
var pola = []struct {
	nama    string
	fragmen []string
}{
	{"abaikan_instruksi", []string{"abaikan instruksi", "abaikan semua instruksi", "abaikan aturan"}},
	{"ignore_previous", []string{"ignore previous", "ignore all previous", "disregard previous"}},
	{"system_prompt", []string{"system prompt", "system message", "instruksi sistem"}},
	{"you_are_now", []string{"you are now", "act as", "pretend to be"}},
	{"delimiter_lookalike", []string{"<<<", ">>>", "learner_data", "end_learner_data", "learner_answer", "transcript_", "<< ", " >>"}},
	{"fake_json_output", []string{"```json", "json output", "output sebagai json", "kembalikan json", "return json", "\"concept_results\"", "\"score_hint\"", "\"misconception_codes\"", "\"injection_suspected\"", "\"severity\""}},
}

// Detect mencari pola injeksi heuristik pada s dan mengembalikan sinyal yang cocok.
// Fungsi ini tidak memblokir pemrosesan; pemanggil bertugas mencatat kejadian dan
// meneruskan penilaian. Satu sinyal dikembalikan per pola yang cocok.
func Detect(s string) []Signal {
	lower := strings.ToLower(s)
	var hasil []Signal
	for _, p := range pola {
		for _, fragmen := range p.fragmen {
			idx := strings.Index(lower, fragmen)
			if idx < 0 {
				continue
			}
			hasil = append(hasil, Signal{
				Detected: true,
				Pattern:  p.nama,
				Excerpt:  potongDariRune(s, idx, panjangPotongan),
			})
			break
		}
	}
	return hasil
}

// potongDariRune mengambil potongan sepanjang n rune mulai dari indeks byte awal.
// Indeks byte dari strings.ToLower tetap dapat dipakai karena fragmen yang dicari
// berupa ASCII dan indeksnya berada pada batas rune.
func potongDariRune(s string, mulaiByte, n int) string {
	if mulaiByte < 0 {
		mulaiByte = 0
	}
	if mulaiByte > len(s) {
		mulaiByte = len(s)
	}
	// Hitung jumlah rune sebelum titik mulai agar potongan dihitung dalam rune.
	runes := []rune(s)
	awal := utf8.RuneCountInString(s[:mulaiByte])
	if awal > len(runes) {
		awal = len(runes)
	}
	akhir := awal + n
	if akhir > len(runes) {
		akhir = len(runes)
	}
	return string(runes[awal:akhir])
}
