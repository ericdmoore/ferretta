#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export CGO_ENABLED=0
export GOTOOLCHAIN="go$(cat .go-version)"

if [ "$(go version | awk '{print $3}')" != "$GOTOOLCHAIN" ]; then
  echo "Compiler must match .go-version" >&2
  exit 1
fi
# Use gofmt from that exact toolchain, even if PATH contains another version.
formatter="$(go env GOROOT)/bin/gofmt"
unformatted=$(git ls-files --cached --others --exclude-standard '*.go' | xargs "$formatter" -l)
if [ -n "$unformatted" ]; then
  echo "Run make fmt; unformatted files:" >&2
  echo "$unformatted" >&2
  exit 1
fi
go vet ./...
sh scripts/test-coverage.sh
go test -count=1 -coverprofile=.coverage.out ./...

# Compare exact statement ratios rather than rounded go tool cover output.
measured=$(awk 'NR > 1 { total += $2; if ($3 > 0) covered += $2 } END { if (total == 0) exit 1; print covered+0, total }' .coverage.out)
baseline=$(cat .coverage-baseline)
base_ref=${COVERAGE_BASE_REF:-HEAD}
previous=$(git show "$base_ref:.coverage-baseline" 2>/dev/null || true)
if [ -z "$previous" ]; then
  # Absence is valid only for the first implementation, not an invalid ref.
  git rev-parse --verify "$base_ref^{commit}" >/dev/null
  if git cat-file -e "$base_ref:.coverage-baseline" 2>/dev/null; then
    echo "Cannot read previous coverage baseline" >&2
    exit 1
  fi
  previous=$baseline
fi
awk -v measured="$measured" -v baseline="$baseline" -v previous="$previous" \
  -v update="${UPDATE_COVERAGE:-0}" -f scripts/coverage.awk
if [ "${UPDATE_COVERAGE:-0}" = 1 ]; then
  printf '%s\n' "$measured" > .coverage-baseline
fi

for platform in darwin linux; do
  for architecture in amd64 arm64; do
    GOOS="$platform" GOARCH="$architecture" go build -o /dev/null ./cmd/ferretta
  done
done
git diff --check
