#!/usr/bin/env bash
# Gerbang CI: pastikan seluruh Property_Test wajib hadir dan lulus.
set -euo pipefail
cd "$(dirname "$0")/.."

REQUIRED="3 5 8 9 11 12 13 14 15 16 17 18 19 20 21 22 27 28 29"

missing=0
for n in $REQUIRED; do
  if ! grep -Rqs --include='*_test.go' -E "func TestPropW${n}[^0-9]" .; then
    echo "gerbang: TestPropW${n}_ tidak ditemukan" >&2
    missing=1
  fi
done
if [ "$missing" -ne 0 ]; then
  exit 1
fi

echo "gerbang: seluruh property wajib hadir"
go vet ./...
if [ -n "$(gofmt -l .)" ]; then
  echo "gerbang: gofmt menemukan berkas yang belum diformat" >&2
  gofmt -l .
  exit 1
fi
go test ./...
