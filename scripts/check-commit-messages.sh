#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

RANGE="${1:-HEAD~20..HEAD}"
REGEX='^(feat|fix|refactor|chore|docs|test|perf|ci|build|revert)(\([a-z0-9._/-]+\))?: .+'

if ! git rev-parse --verify "${RANGE%%..*}" >/dev/null 2>&1; then
  echo "Commit check skipped: invalid base in range '${RANGE}'."
  exit 0
fi

if [[ -z "$(git log --format='%s' "${RANGE}")" ]]; then
  echo "Commit check skipped: no commits in range '${RANGE}'."
  exit 0
fi

failed=0
while IFS= read -r subject; do
  if [[ ! "${subject}" =~ ${REGEX} ]]; then
    echo "Invalid commit subject: ${subject}"
    failed=1
  fi
done < <(git log --format='%s' "${RANGE}")

if [[ "${failed}" -ne 0 ]]; then
  echo
  echo "Expected format: type(scope): message"
  echo "Allowed types: feat, fix, refactor, chore, docs, test, perf, ci, build, revert"
  exit 1
fi

echo "Commit message check passed for range '${RANGE}'."
