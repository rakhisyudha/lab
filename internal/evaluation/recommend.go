package evaluation

import (
	"sort"
	"strings"
)

// Kode aturan rekomendasi beserta prioritasnya (desain 4.7).
const (
	rekAturanPrasyarat  = "PREREQUISITE_NOT_MASTERED"
	rekAturanPenguasaan = "LOW_MASTERY"
	rekAturanPetaJalan  = "ROADMAP_RELEVANT"
	rekAturanKurikulum  = "CURRICULUM_ORDER"
)

// RecommendTopic adalah tema yang boleh muncul dalam rekomendasi. Position
// dipakai sebagai pengurut deterministik ketika prioritasnya sama.
type RecommendTopic struct {
	ID       string
	Slug     string
	Position int
}

// MasteryInfo adalah keadaan penguasaan satu tema. Tema tanpa entri di peta
// dianggap NOT_STARTED.
type MasteryInfo struct {
	Status   string
	AvgLast3 *float64
}

// Recommendation adalah satu saran belajar. Rule memuat kode aturan,
// prioritas (1..4), dan parameter yang menjelaskan alasan aturan tersebut.
type Recommendation struct {
	TopicID string
	Action  string
	Rule    map[string]any
}

// rekLangkah menyimpan kemunculan pertama sebuah tema di peta jalan pemelajar.
type rekLangkah struct {
	index    int
	position int
}

// Recommend menghasilkan paling banyak limit rekomendasi deterministik.
// Satu tema hanya mendapat satu aturan berprioritas tertinggi; tema yang
// sudah MASTERED tidak disertakan. Fungsi ini murni.
func Recommend(topics []RecommendTopic, prereq map[string][]string, mastery map[string]MasteryInfo,
	roadmapOrders [][]string, lowMasteryThreshold float64, limit int) []Recommendation {

	hasil := make([]Recommendation, 0)
	if limit <= 0 {
		return hasil
	}

	// Buang duplikat tema dan tema yang sudah dikuasai.
	dipilih := make([]RecommendTopic, 0, len(topics))
	posisi := make(map[string]int, len(topics))
	slug := make(map[string]string, len(topics))
	terlihat := make(map[string]bool, len(topics))
	for _, topic := range topics {
		if terlihat[topic.ID] {
			continue
		}
		terlihat[topic.ID] = true
		if rekSudahDikuasai(topic.ID, mastery) {
			continue
		}
		dipilih = append(dipilih, topic)
		posisi[topic.ID] = topic.Position
		slug[topic.ID] = topic.Slug
	}
	if len(dipilih) == 0 {
		return hasil
	}

	// Tema yang relevan adalah tema yang muncul di salah satu peta jalan.
	petaJalan := map[string]rekLangkah{}
	for i, order := range roadmapOrders {
		for j, id := range order {
			if _, ok := posisi[id]; !ok {
				continue
			}
			if _, ada := petaJalan[id]; !ada {
				petaJalan[id] = rekLangkah{index: i, position: j}
			}
		}
	}

	for _, topic := range dipilih {
		info := mastery[topic.ID]
		status := rekNormaStatus(info.Status)

		kode, prioritas, params := "", 0, map[string]any{}
		frontier := rekFrontier(topic.ID, prereq, mastery)
		switch {
		case len(frontier) > 0:
			kode, prioritas = rekAturanPrasyarat, 1
			params = map[string]any{"unmet_prerequisites": frontier}
		case status == "NEEDS_REVIEW":
			kode, prioritas = rekAturanPenguasaan, 2
			params = map[string]any{
				"status":    status,
				"avg_last3": rekNilaiRata(info.AvgLast3),
				"threshold": lowMasteryThreshold,
			}
		case (status == "IN_PROGRESS" || status == "PRACTICING") &&
			info.AvgLast3 != nil && *info.AvgLast3 < lowMasteryThreshold:
			kode, prioritas = rekAturanPenguasaan, 2
			params = map[string]any{
				"status":    status,
				"avg_last3": *info.AvgLast3,
				"threshold": lowMasteryThreshold,
			}
		default:
			if langkah, ada := petaJalan[topic.ID]; ada {
				kode, prioritas = rekAturanPetaJalan, 3
				params = map[string]any{
					"roadmap_index": langkah.index,
					"position":      langkah.position,
				}
			} else {
				kode, prioritas = rekAturanKurikulum, 4
				params = map[string]any{"position": topic.Position}
			}
		}

		hasil = append(hasil, Recommendation{
			TopicID: topic.ID,
			Action:  rekAksi(status, kode),
			Rule: map[string]any{
				"code":     kode,
				"priority": prioritas,
				"params":   params,
			},
		})
	}

	sort.Slice(hasil, func(i, j int) bool {
		pi, pj := rekPrioritas(hasil[i]), rekPrioritas(hasil[j])
		if pi != pj {
			return pi < pj
		}
		ti, tj := posisi[hasil[i].TopicID], posisi[hasil[j].TopicID]
		if ti != tj {
			return ti < tj
		}
		if hasil[i].TopicID != hasil[j].TopicID {
			return hasil[i].TopicID < hasil[j].TopicID
		}
		return slug[hasil[i].TopicID] < slug[hasil[j].TopicID]
	})
	if len(hasil) > limit {
		hasil = hasil[:limit]
	}
	return hasil
}

// rekNormaStatus menyamakan bentuk status agar "MASTERED", "mastered", dan
// variasi lain diperlakukan identik.
func rekNormaStatus(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

// rekNilaiRata mengembalikan nol untuk rata-rata yang belum tersedia.
func rekNilaiRata(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// rekSudahDikuasai mengecek status MASTERED, termasuk untuk tema tanpa entri.
func rekSudahDikuasai(id string, mastery map[string]MasteryInfo) bool {
	return rekNormaStatus(mastery[id].Status) == "MASTERED"
}

// rekLeluhur mengumpulkan seluruh leluhur transitif dari sebuah tema.
// Kunjungan yang sudah tercatat mencegah perulangan pada graf ber-siklus.
func rekLeluhur(id string, prereq map[string][]string) map[string]bool {
	leluhur := map[string]bool{}
	var kunjungi func(string)
	kunjungi = func(saatIni string) {
		for _, calon := range prereq[saatIni] {
			if calon == id || leluhur[calon] {
				continue
			}
			leluhur[calon] = true
			kunjungi(calon)
		}
	}
	kunjungi(id)
	return leluhur
}

// rekFrontier mengembalikan leluhur yang belum dikuasai dan tidak memiliki
// leluhur lain yang juga belum dikuasai (batas terdekat yang menghalangi).
func rekFrontier(id string, prereq map[string][]string, mastery map[string]MasteryInfo) []string {
	belum := make([]string, 0)
	for calon := range rekLeluhur(id, prereq) {
		if rekSudahDikuasai(calon, mastery) {
			continue
		}
		terhalang := false
		for atas := range rekLeluhur(calon, prereq) {
			if !rekSudahDikuasai(atas, mastery) {
				terhalang = true
				break
			}
		}
		if !terhalang {
			belum = append(belum, calon)
		}
	}
	sort.Strings(belum)
	return belum
}

// rekAksi menentukan tindakan: tinjauan untuk LOW_MASTERY, tes untuk tema
// yang sedang berjalan, dan bahan belajar untuk keadaan lainnya.
func rekAksi(status, kode string) string {
	if kode == rekAturanPenguasaan {
		return "review"
	}
	if status == "IN_PROGRESS" || status == "PRACTICING" {
		return "take_assessment"
	}
	return "study_material"
}

// rekPrioritas membaca prioritas dari aturan yang sudah dibangun Recommend.
func rekPrioritas(rec Recommendation) int {
	if p, ok := rec.Rule["priority"].(int); ok {
		return p
	}
	return 0
}
