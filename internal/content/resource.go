package content

// Resource mendeskripsikan satu entitas induk konten beserta tabel
// Translation-nya. Semua identifier berasal dari daftar tetap di bawah,
// sehingga aman dipakai pada SQL dinamis (tidak ada input user sebagai nama
// tabel/kolom).
type Resource struct {
	Name             string
	Table            string
	TranslationTable string
	OwnerColumn      string
	ParentColumn     string
	TextColumns      []string
	EntityColumns    []string
	Slug             bool
	Position         bool
	SoftDelete       bool
	Active           bool
}

// Resources memetakan nama rute admin ke deskriptor.
var Resources = map[string]Resource{
	"topics": {
		Name: "topics", Table: "topics", TranslationTable: "topic_translations",
		OwnerColumn: "topic_id", TextColumns: []string{"title", "summary"},
		EntityColumns: []string{"slug", "position", "difficulty", "is_active"},
		Slug:          true, Position: true, SoftDelete: true, Active: true,
	},
	"materials": {
		Name: "materials", Table: "materials", TranslationTable: "material_translations",
		OwnerColumn: "material_id", ParentColumn: "topic_id",
		TextColumns:   []string{"title", "summary", "body_md"},
		EntityColumns: []string{"topic_id", "slug", "position", "estimated_minutes", "is_active"},
		Slug:          true, Position: true, SoftDelete: true, Active: true,
	},
	"concepts": {
		Name: "concepts", Table: "concepts", TranslationTable: "concept_translations",
		OwnerColumn: "concept_id", ParentColumn: "topic_id",
		TextColumns:   []string{"name", "description"},
		EntityColumns: []string{"topic_id", "code", "is_active"},
		Active:        true,
	},
	"questions": {
		Name: "questions", Table: "questions", TranslationTable: "question_translations",
		OwnerColumn: "question_id", ParentColumn: "topic_id",
		TextColumns:   []string{"prompt", "reference_answer", "evaluation_notes"},
		EntityColumns: []string{"topic_id", "cognitive_level", "difficulty", "purpose", "weight", "is_active"},
		SoftDelete:    true, Active: true,
	},
	"roadmaps": {
		Name: "roadmaps", Table: "roadmaps", TranslationTable: "roadmap_translations",
		OwnerColumn: "roadmap_id", TextColumns: []string{"title", "description"},
		EntityColumns: []string{"slug", "is_active"}, Slug: true, Active: true,
	},
	"misconceptions": {
		Name: "misconceptions", Table: "misconceptions", TranslationTable: "misconception_translations",
		OwnerColumn: "misconception_id", ParentColumn: "topic_id",
		TextColumns:   []string{"title", "description"},
		EntityColumns: []string{"topic_id", "code", "severity", "is_active"},
		Active:        true,
	},
}

// FindResource mengembalikan deskriptor untuk nama rute, atau false.
func FindResource(name string) (Resource, bool) {
	r, ok := Resources[name]
	return r, ok
}

// AllowedEntityFields adalah daftar kolom entitas yang dapat ditulis Admin.
func AllowedEntityFields(r Resource) map[string]bool {
	out := make(map[string]bool, len(r.EntityColumns))
	for _, c := range r.EntityColumns {
		out[c] = true
	}
	return out
}
