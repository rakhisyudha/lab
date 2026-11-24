// Package prompts menyimpan berkas kontrak prompt versi pertama yang
// disematkan ke dalam biner sehingga seed-prompts tidak bergantung pada
// direktori kerja saat runtime.
package prompts

import "embed"

// FS memuat seluruh kontrak prompt pada direktori <task_type>/v1.json.
//
//go:embed */*.json
var FS embed.FS
