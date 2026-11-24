package evaluation

import (
	"sort"
	"strconv"
	"strings"
)

// StatusTranslation yang dapat disajikan kepada Learner (Requirement 3.5, 5.4).
const TranslationPublished = "published"

// ParseAcceptLanguage mengurai header Accept-Language menjadi daftar tag
// bahasa yang diurutkan menurun berdasarkan parameter q (default q=1).
// Tag dengan q=0 diabaikan. Urutan relatif untuk q yang sama dipertahankan.
func ParseAcceptLanguage(header string) []string {
	type weighted struct {
		tag string
		q   float64
		idx int
	}
	var candidates []weighted
	for i, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag, q := part, 1.0
		if pos := strings.Index(part, ";"); pos >= 0 {
			tag = strings.TrimSpace(part[:pos])
			for _, param := range strings.Split(part[pos+1:], ";") {
				param = strings.TrimSpace(param)
				if strings.HasPrefix(strings.ToLower(param), "q=") {
					v, err := strconv.ParseFloat(strings.TrimSpace(param[2:]), 64)
					if err != nil || v < 0 || v > 1 {
						q = 0
					} else {
						q = v
					}
				}
			}
		}
		if tag == "" || q <= 0 {
			continue
		}
		candidates = append(candidates, weighted{tag: tag, q: q, idx: i})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].q != candidates[j].q {
			return candidates[i].q > candidates[j].q
		}
		return candidates[i].idx < candidates[j].idx
	})
	tags := make([]string, 0, len(candidates))
	for _, c := range candidates {
		tags = append(tags, c.tag)
	}
	return tags
}

// BaseTag memetakan tag regional ke tag bahasa dasar: "id-ID" menjadi "id".
// Tag tanpa subtag dikembalikan apa adanya dalam huruf kecil.
func BaseTag(tag string) string {
	tag = strings.TrimSpace(tag)
	if i := strings.IndexAny(tag, "-_"); i > 0 {
		return strings.ToLower(tag[:i])
	}
	return strings.ToLower(tag)
}

// ResolveLocale menentukan Locale permintaan dengan urutan prioritas:
// query parameter, Accept-Language, preferensi Learner, lalu Default_Locale
// (Requirement 5.1). Tag regional dipetakan ke tag dasar sebelum dicek
// (Requirement 5.2). Locale yang tidak didukung jatuh ke Default_Locale
// (Requirement 5.3). Hasil selalu merupakan Locale yang didukung bila default
// termasuk dalam supported; bila tidak, dikembalikan default apa adanya.
func ResolveLocale(query, acceptLanguage, userLocale, defaultLocale string, supported map[string]bool) string {
	candidates := make([]string, 0, 5)
	candidates = append(candidates, query)
	candidates = append(candidates, ParseAcceptLanguage(acceptLanguage)...)
	candidates = append(candidates, userLocale, defaultLocale, defaultLocale)
	for _, candidate := range candidates {
		tag := BaseTag(candidate)
		if tag != "" && supported[tag] {
			return tag
		}
	}
	return BaseTag(defaultLocale)
}

// Localized menyimpan satu Translation untuk sebuah Locale beserta statusnya.
type Localized[T any] struct {
	Locale string
	Status string
	Value  T
}

// Entity adalah entitas induk konten dengan seluruh Translation-nya.
type Entity[T any] struct {
	ID           string
	Translations []Localized[T]
}

// Served adalah entitas yang disajikan: hanya berisi field dari Translation
// pada Locale permintaan (Requirement 5.7, 5.8).
type Served[T any] struct {
	ID     string
	Locale string
	Value  T
}

// FilterPublished mengembalikan hanya entitas yang memiliki Translation
// berstatus published pada locale (Requirement 5.4, 5.5). Urutan entitas
// dipertahankan. Result tidak pernah nil.
func FilterPublished[T any](items []Entity[T], locale string) []Served[T] {
	out := make([]Served[T], 0, len(items))
	for _, item := range items {
		for _, tr := range item.Translations {
			if tr.Locale == locale && tr.Status == TranslationPublished {
				out = append(out, Served[T]{ID: item.ID, Locale: locale, Value: tr.Value})
				break
			}
		}
	}
	return out
}

// GetDetail mencari satu entitas berdasarkan id dan locale. Bila tidak ada
// Published_Translation pada locale tersebut, dikembalikan ok=false beserta
// daftar Locale yang memiliki Published_Translation terurut (Requirement 5.6).
func GetDetail[T any](items []Entity[T], id, locale string) (served Served[T], availableLocales []string, ok bool) {
	available := map[string]struct{}{}
	for _, item := range items {
		if item.ID != id {
			continue
		}
		for _, tr := range item.Translations {
			if tr.Status != TranslationPublished {
				continue
			}
			available[tr.Locale] = struct{}{}
			if tr.Locale == locale {
				return Served[T]{ID: item.ID, Locale: locale, Value: tr.Value}, nil, true
			}
		}
	}
	for loc := range available {
		availableLocales = append(availableLocales, loc)
	}
	sort.Strings(availableLocales)
	return Served[T]{}, availableLocales, false
}
