#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
if [[ -x "$ROOT/.tools/go/bin/go.exe" ]]; then export PATH="$ROOT/.tools/go/bin:$PATH"; fi
export GOBIN="$ROOT/.tools/bin"
mkdir -p "$GOBIN" reports

install_tool() {
  local binary=$1
  local package=$2
  if [[ ! -x "$GOBIN/$binary" && ! -x "$GOBIN/$binary.exe" ]]; then
    go install "$package"
  fi
}

tool() {
  local binary=$1
  if [[ -x "$GOBIN/$binary.exe" ]]; then printf '%s' "$GOBIN/$binary.exe"; else printf '%s' "$GOBIN/$binary"; fi
}

install_tool staticcheck honnef.co/go/tools/cmd/staticcheck@2026.2
install_tool gosec github.com/securego/gosec/v2/cmd/gosec@v2.28.0
install_tool govulncheck golang.org/x/vuln/cmd/govulncheck@v1.7.0
install_tool gitleaks github.com/zricethezav/gitleaks/v8@v8.30.1
install_tool go-licenses github.com/google/go-licenses/v2@v2.0.1
if [[ ! -x "$GOBIN/syft" && ! -x "$GOBIN/syft.exe" ]]; then
  mkdir -p "$ROOT/.tools/downloads"
  case "$(uname -s)" in
    Linux*)
      syft_archive="$ROOT/.tools/downloads/syft_1.51.0_linux_amd64.tar.gz"
      curl --fail --location --silent --show-error https://github.com/anchore/syft/releases/download/v1.51.0/syft_1.51.0_linux_amd64.tar.gz -o "$syft_archive"
      printf '%s *%s\n' 2a2e837a2c8d59ec9af5472ee22d3b04ee463c4e44476ecf993fd1e5ab6ebc7f "$syft_archive" | sha256sum --check
      tar -xzf "$syft_archive" -C "$GOBIN" syft
      ;;
    MINGW*|MSYS*)
      syft_archive="$ROOT/.tools/downloads/syft_1.51.0_windows_amd64.zip"
      curl --fail --location --silent --show-error https://github.com/anchore/syft/releases/download/v1.51.0/syft_1.51.0_windows_amd64.zip -o "$syft_archive"
      printf '%s *%s\n' fc5ffaeffb993576ece9c791da5a688fb2c8969a1479bbfe58583672c64da336 "$syft_archive" | sha256sum --check
      unzip -j -q "$syft_archive" syft.exe -d "$GOBIN"
      ;;
    *) echo 'unsupported platform for pinned Syft archive' >&2; exit 1 ;;
  esac
fi

unformatted=$(gofmt -l cmd internal)
if [[ -n "$unformatted" ]]; then
  printf 'gofmt required:\n%s\n' "$unformatted" >&2
  exit 1
fi

go mod verify
go vet ./...
"$(tool staticcheck)" ./...
go test ./...
go test -race ./...
"$(tool gosec)" -quiet ./cmd/... ./internal/...
# Runner paths are repository-contained and argv is fixed; OS RSS sampling requires audited unsafe Windows calls.
(cd benchmarks/runtime/runner && "$(tool gosec)" -quiet -exclude=G103,G204,G301,G302,G304,G306 ./...)
(cd benchmarks/runtime/go-candidate && "$(tool gosec)" -quiet ./...)
"$(tool govulncheck)" ./...
"$(tool gitleaks)" git . --config .gitleaks.toml --log-opts=--all --no-banner --redact=100
"$(tool gitleaks)" dir . --config .gitleaks.toml --no-banner --redact=100
"$(tool go-licenses)" report --ignore github.com/ken-kai/discord-control-experiments ./... > reports/licenses.csv
"$(tool go-licenses)" check --ignore github.com/ken-kai/discord-control-experiments --allowed_licenses=Apache-2.0,BSD-2-Clause,BSD-3-Clause,ISC,MIT,MPL-2.0 ./...
"$(tool syft)" dir:. -o spdx-json=reports/sbom.spdx.json --exclude './.git/**' --exclude './.tools/**'

rm -rf .tools/repro-a .tools/repro-b
mkdir -p .tools/repro-a .tools/repro-b
build_flags=(-trimpath -buildvcs=false -ldflags "-s -w -buildid= -X github.com/ken-kai/discord-control-experiments/internal/buildinfo.Version=0.0.0-ci -X github.com/ken-kai/discord-control-experiments/internal/buildinfo.Commit=0000000000000000000000000000000000000000")
CGO_ENABLED=0 go build "${build_flags[@]}" -o .tools/repro-a/discord-control ./cmd/discord-control
CGO_ENABLED=0 go build "${build_flags[@]}" -o .tools/repro-b/discord-control ./cmd/discord-control
hash_a=$(sha256sum .tools/repro-a/discord-control | cut -d ' ' -f 1)
hash_b=$(sha256sum .tools/repro-b/discord-control | cut -d ' ' -f 1)
[[ "$hash_a" == "$hash_b" ]] || { echo 'reproducible build hashes differ' >&2; exit 1; }
printf 'verification passed; reproducible sha256=%s\n' "$hash_a"
