#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

targets=(
  "./internal/features/operations/api|9"
  "./internal/platform/shared|30"
  "./internal/features/services/api|4"
)

failed=0
echo "Feature coverage targets:"
for target in "${targets[@]}"; do
  pkg="${target%%|*}"
  min="${target##*|}"
  printf " - %s >= %s%%\n" "${pkg}" "${min}"
done
echo

for target in "${targets[@]}"; do
  pkg="${target%%|*}"
  min="${target##*|}"
  profile="$(mktemp)"

  go test -coverprofile="${profile}" "${pkg}" >/dev/null
  total="$(go tool cover -func="${profile}" | awk '/^total:/ {gsub("%","",$3); print $3}')"
  rm -f "${profile}"

  if [[ -z "${total}" ]]; then
    echo "No coverage result for ${pkg}"
    failed=1
    continue
  fi

  if awk -v total="${total}" -v min="${min}" 'BEGIN { exit !(total + 0 < min + 0) }'; then
    printf "FAIL %s coverage %.1f%% < %.1f%%\n" "${pkg}" "${total}" "${min}"
    failed=1
  else
    printf "PASS %s coverage %.1f%% >= %.1f%%\n" "${pkg}" "${total}" "${min}"
  fi
done

if [[ "${failed}" -ne 0 ]]; then
  exit 1
fi

echo
echo "Coverage targets satisfied."
