#!/usr/bin/env bash
# No database access. Source presence is not evidence of an applied migration.
# Until a reviewed, environment-specific readiness gate is wired, fail closed.
set -euo pipefail
root=${1:-.}
[[ -d "$root" ]] || { echo '::error::migration readiness: source root is missing' >&2; exit 2; }
found=0
for relative in db/migrations migrations hosted/migrations; do
  directory="$root/$relative"
  [[ ! -L "$directory" ]] || { echo "::error::migration readiness: $relative is a symlink" >&2; exit 2; }
  [[ -e "$directory" ]] || continue
  [[ -d "$directory" ]] || { echo "::error::migration readiness: $relative is not a directory" >&2; exit 2; }
  # Capture find's exit status; do not silently treat an unreadable tree as empty.
  entries=$(find "$directory" -mindepth 1 ! -type d -print) || { echo "::error::migration readiness: cannot inspect $relative" >&2; exit 2; }
  if [[ -n "$entries" ]]; then
    echo "::error::migration readiness: migration sources exist under $relative"
    found=1
  fi
done
if [[ "$found" == 1 ]]; then
  echo '::error::Migration readiness is unverified for this target. An owner must inventory the target database, apply/reconcile the reviewed migrations and verify schema/privileges separately. No migration ran here. A reviewed target-specific readiness gate is required before deployment; do not bypass this guard.'
  exit 1
fi
echo 'No migration sources found in the known directories; no database readiness claim or migration was performed.'
