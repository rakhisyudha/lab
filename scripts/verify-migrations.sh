#!/usr/bin/env bash
# Verifikasi migrasi P0 (Requirement 2.6, 2.7):
#   1. up dari database kosong;
#   2. down penuh lalu up lagi;
#   3. bandingkan pg_dump --schema-only dengan snapshot yang di-commit.
#
# DATABASE_URL wajib mengarah ke DATABASE UJI KHUSUS, karena skrip ini menjalankan
# `migrate down --all` yang menghapus seluruh skema. Skrip ini tidak memakai pgvector.
#
# Pemakaian:
#   DATABASE_URL=postgres://user:pass@localhost:5432/backendlab_test \
#     ./scripts/verify-migrations.sh
#
# Perbarui snapshot setelah perubahan skema yang disengaja:
#   ./scripts/verify-migrations.sh --record
set -euo pipefail

cd "$(dirname "$0")/.."

: "${DATABASE_URL:?DATABASE_URL wajib diisi (database uji khusus)}"

MIGRATE=(go run ./cmd/migrate)
SNAPSHOT="migrations/schema_snapshot.sql"
TMPDIR_VERIFY="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_VERIFY"' EXIT

# pg_dump menulis token \restrict acak dan nomor versi yang tidak deterministik.
# Keduanya dibuang agar perbandingan snapshot stabil antar lingkungan.
normalize() {
  sed -E \
    -e '/^\\restrict /d' \
    -e '/^\\unrestrict /d' \
    -e '/^-- Dumped (from database|by pg_dump) version /d' \
    "$1" > "$2"
}

dump_schema() {
  local out="$1"
  pg_dump --schema-only --no-owner --no-privileges --dbname "$DATABASE_URL" > "$TMPDIR_VERIFY/raw.sql"
  normalize "$TMPDIR_VERIFY/raw.sql" "$out"
}

if [[ "${1:-}" == "--record" ]]; then
  echo "==> merekam snapshot skema dari database saat ini"
  dump_schema "$SNAPSHOT.tmp"
  mv "$SNAPSHOT.tmp" "$SNAPSHOT"
  echo "==> snapshot tersimpan di $SNAPSHOT"
  exit 0
fi

echo "==> down --all (kondisi awal bersih)"
"${MIGRATE[@]}" down --all

echo "==> up (database kosong)"
"${MIGRATE[@]}" up
dump_schema "$TMPDIR_VERIFY/schema_up1.sql"

echo "==> down --all lalu up lagi"
"${MIGRATE[@]}" down --all
"${MIGRATE[@]}" up
dump_schema "$TMPDIR_VERIFY/schema_up2.sql"

echo "==> bandingkan dengan snapshot yang di-commit"
if ! diff -u "$SNAPSHOT" "$TMPDIR_VERIFY/schema_up1.sql"; then
  echo "GAGAL: skema hasil up berbeda dari snapshot" >&2
  exit 1
fi
if ! diff -u "$TMPDIR_VERIFY/schema_up1.sql" "$TMPDIR_VERIFY/schema_up2.sql"; then
  echo "GAGAL: skema tidak stabil setelah down/up ulang" >&2
  exit 1
fi

echo "==> verifikasi migrasi berhasil"
