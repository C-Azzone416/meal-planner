#!/usr/bin/env bash
# Tests for materialize-output.sh. Usage: tools/deploy/materialize-output_test.sh
# Fixtures mirror the real failure: a function whose .vc-config.json maps `bootstrap` to a binary in a
# directory outside the project root, reached through `../../../../../tmp/<id>/bootstrap`.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$here/materialize-output.sh"
work=$(cd "$(mktemp -d)" && pwd -P)
trap 'rm -rf "$work"' EXIT
pass=0 failn=0

check() { # check <description> <command...>: passes when the command succeeds
  local desc=$1; shift
  if "$@" > /dev/null; then echo "ok   $desc"; pass=$((pass + 1)); else echo "FAIL $desc"; failn=$((failn + 1)); fi
}

has() { grep -qF -- "$2" "$work/$1.out"; } # has <name> <text>

expect() { # expect <rc> <name> <script args...>: keeps the output in $work/<name>.out
  local want=$1 name=$2 rc=0; shift 2
  "$script" "$@" > "$work/$name.out" 2>&1 || rc=$?
  if [[ $rc -eq $want ]]; then
    echo "ok   rc=$want $name"; pass=$((pass + 1))
  else
    echo "FAIL rc=$want $name (rc=$rc)"; cat "$work/$name.out"; failn=$((failn + 1))
  fi
}

# new_root <name>: a project root five levels below $work, so `../../../../../tmp/..` lands in $work/tmp.
new_root() {
  local r="$work/$1/a/b/c/d/root"
  mkdir -p "$r/.vercel/output/static" "$work/$1/tmp"
  echo '{"version":3}' > "$r/.vercel/output/config.json"
  echo "$r"
}
root_of() { echo "$work/$1/a/b/c/d/root"; }
out_of() { echo "$work/$1/a/b/c/d/root/.vercel/output"; }

# write_func <root> <func path under functions> <filePathMap json>
write_func() {
  local d="$1/.vercel/output/functions/$2.func"
  mkdir -p "$d"
  jq -n --argjson m "$3" '{handler: "bootstrap", runtime: "provided.al2023", runtimeLanguage: "go", architecture: "x86_64",
    environment: {}, supportsWrapper: true} + (if $m == null then {} else {filePathMap: $m} end)' > "$d/.vc-config.json"
}

# binary <path> [mode]: a stand-in compiled binary
binary() { mkdir -p "$(dirname "$1")"; printf '#!/bin/sh\necho %s\n' "$1" > "$1"; chmod "${2:-755}" "$1"; }

fp_empty() { # fp_empty <config>: filePathMap absent, null or {}
  jq -e '(.filePathMap // {}) | length == 0' "$1" > /dev/null
}

# 1. The real failure shape: one function, one entry, source outside the root.
new_root real > /dev/null; r=$(root_of real)
binary "$work/real/tmp/77f3c1/bootstrap"
write_func "$r" api/health '{"bootstrap":"../../../../../tmp/77f3c1/bootstrap"}'
expect 0 real "$r"
f="$r/.vercel/output/functions/api/health.func"
check "real: binary copied into the func dir" test -f "$f/bootstrap"
check "real: copy has the executable bit" test -x "$f/bootstrap"
check "real: copy has the source content" cmp -s "$f/bootstrap" "$work/real/tmp/77f3c1/bootstrap"
check "real: filePathMap removed" fp_empty "$f/.vc-config.json"
check "real: other config fields kept" jq -e '.handler == "bootstrap" and .runtime == "provided.al2023" and .supportsWrapper == true' "$f/.vc-config.json"
check "real: source left in place" test -f "$work/real/tmp/77f3c1/bootstrap"
check "real: reports the copy" has real "materialized 1 file(s) into 1 function(s)"
before=$(cat "$f/.vc-config.json")
expect 0 real-again "$r"
check "real: idempotent, config unchanged" test "$(cat "$f/.vc-config.json")" = "$before"
check "real: idempotent, reports nothing to do" has real-again "materialized 0 file(s) into 0 function(s)"
# shellcheck disable=SC2016 # expansion happens in the inner shell
check "real: --check accepts the result" bash -c '"$0" --check "$1" > /dev/null 2>&1' "$script" "$work/real/a/b/c/d/root"

# 2. Non-executable source keeps its mode.
new_root plain > /dev/null; r=$(root_of plain)
binary "$work/plain/tmp/data.bin" 644
write_func "$r" api/x '{"data.bin":"../../../../../tmp/data.bin"}'
expect 0 plain "$r"
check "plain: mode 644 preserved (not made executable)" test ! -x "$r/.vercel/output/functions/api/x.func/data.bin"

# 3. Several functions, several entries (nested key, source inside the root too).
new_root multi > /dev/null; r=$(root_of multi)
binary "$work/multi/tmp/one/bootstrap"; binary "$work/multi/tmp/two/bootstrap"; binary "$work/multi/tmp/lib/helper.so" 644
binary "$r/bin/inside" 755
write_func "$r" api/one '{"bootstrap":"../../../../../tmp/one/bootstrap","lib/helper.so":"../../../../../tmp/lib/helper.so"}'
write_func "$r" api/nested/two '{"bootstrap":"../../../../../tmp/two/bootstrap","tools/inside":"bin/inside"}'
write_func "$r" api/plain 'null'
expect 0 multi "$r"
o=$(out_of multi)
check "multi: first func binary" test -x "$o/functions/api/one.func/bootstrap"
check "multi: first func nested key" test -f "$o/functions/api/one.func/lib/helper.so"
check "multi: second func binary" test -x "$o/functions/api/nested/two.func/bootstrap"
check "multi: second func in-root source" test -x "$o/functions/api/nested/two.func/tools/inside"
check "multi: distinct sources kept distinct" test "$(cat "$o/functions/api/one.func/bootstrap")" != "$(cat "$o/functions/api/nested/two.func/bootstrap")"
check "multi: map-less func untouched" jq -e 'has("filePathMap") | not' "$o/functions/api/plain.func/.vc-config.json"
# shellcheck disable=SC2016 # expansion happens in the inner shell
check "multi: all maps removed" bash -c '! grep -rq filePathMap "$0"' "$o/functions"
check "multi: reports 3 files in 2 functions" has multi "materialized 4 file(s) into 2 function(s)"

# 4. Anomalies fail closed and name the problem.
# anomaly <name> <expected text> <filePathMap json> [setup command...]
anomaly() {
  local name=$1 text=$2 map=$3; shift 3
  new_root "$name" > /dev/null; local r; r=$(root_of "$name")
  binary "$work/$name/tmp/bootstrap"
  mkdir -p "$work/$name/tmp/adir"
  write_func "$r" api/health "$map"
  if [[ $# -gt 0 ]]; then "$@" "$r" "$name"; fi
  expect 1 "$name" "$r"
  check "$name: says $text" has "$name" "$text"
}
S='"../../../../../tmp/bootstrap"'
anomaly missing-source "does not exist" '{"bootstrap":"../../../../../tmp/nope"}'
mk_symlink_source() { ln -s bootstrap "$work/$2/tmp/link"; }
anomaly symlink-source "is a symlink" '{"bootstrap":"../../../../../tmp/link"}' mk_symlink_source
anomaly dir-source "is not a regular file" '{"bootstrap":"../../../../../tmp/adir"}'
anomaly dotdot-key "'..' component" "{\"../evil\":$S}"
anomaly dotdot-mid-key "'..' component" "{\"a/../../evil\":$S}"
anomaly absolute-key "absolute filePathMap key" "{\"/etc/evil\":$S}"
anomaly escaping-key "'..' component" "{\"a/b/../../../../evil\":$S}"
anomaly dot-key "'.' or '..' component" "{\"./x\":$S}"
anomaly empty-component-key "empty" "{\"a//x\":$S}"
anomaly trailing-slash-key "trailing slash" "{\"a/\":$S}"
anomaly config-key "overwrite .vc-config.json" "{\".vc-config.json\":$S}"
anomaly non-string-value "non-empty strings" '{"bootstrap":7}'
anomaly empty-value "non-empty strings" '{"bootstrap":""}'
anomaly control-char-key "control characters" "{\"a\\nb\":$S}"
anomaly map-not-object "non-empty strings" '["bootstrap"]'
mk_existing_dest() { echo old > "$1/.vercel/output/functions/api/health.func/bootstrap"; }
anomaly dest-exists "already exists" "{\"bootstrap\":$S}" mk_existing_dest
check "escaping-key: nothing written outside the func dir" test ! -e "$(out_of escaping-key)/evil" -a ! -e "$work/escaping-key/a/b/c/d/root/evil"
check "dotdot-key: nothing written next to the func dir" test ! -e "$(out_of dotdot-key)/functions/api/evil"
check "absolute-key: nothing written to /etc" test ! -e /etc/evil
check "missing-source: config left unchanged on failure" jq -e '.filePathMap.bootstrap == "../../../../../tmp/nope"' "$(out_of missing-source)/functions/api/health.func/.vc-config.json"

# 5. Malformed JSON, and a non-object config.
new_root malformed > /dev/null; r=$(root_of malformed)
mkdir -p "$r/.vercel/output/functions/api/bad.func"; echo '{"handler": ' > "$r/.vercel/output/functions/api/bad.func/.vc-config.json"
expect 1 malformed "$r"
check "malformed: names the bad config" has malformed "not a JSON object (malformed JSON?)"
new_root array > /dev/null; r=$(root_of array)
mkdir -p "$r/.vercel/output/functions/api/bad.func"; echo '[]' > "$r/.vercel/output/functions/api/bad.func/.vc-config.json"
expect 1 array "$r"

# 6. Symlinks under the output are rejected, before and after.
new_root linked > /dev/null; r=$(root_of linked)
binary "$work/linked/tmp/bootstrap"; write_func "$r" api/health "{\"bootstrap\":$S}"
ln -s health.func "$r/.vercel/output/functions/api/alias.func"
expect 1 linked "$r"
check "linked: names the symlink" has linked "symlink under .vercel/output before materializing"
check "linked: nothing was materialized" test ! -e "$r/.vercel/output/functions/api/health.func/bootstrap"

# 7. No functions dir (static only), and a missing output dir.
new_root static > /dev/null; r=$(root_of static)
echo '<html></html>' > "$r/.vercel/output/static/index.html"
expect 0 static "$r"
check "static: reports nothing to do" has static "materialized 0 file(s) into 0 function(s)"
check "static: static file untouched" test -f "$r/.vercel/output/static/index.html"
expect 1 no-output "$work/static/tmp"
check "no-output: says vercel build may not have run" has no-output "did vercel build run?"
expect 1 no-root "$work/does-not-exist"

# 8. Empty or null maps are fine and are left alone.
new_root emptymap > /dev/null; r=$(root_of emptymap)
write_func "$r" api/a '{}'; write_func "$r" api/b 'null'
expect 0 emptymap "$r"

# 9. Check mode (the deploy-side defence in depth).
new_root dirty > /dev/null; r=$(root_of dirty)
write_func "$r" api/health "{\"bootstrap\":$S}"
expect 1 check-dirty --check "$work/dirty/a/b/c/d/root"
check "check-dirty: says not self-contained" has check-dirty "build output is not self-contained"
check "check-dirty: names the config" has check-dirty "health.func/.vc-config.json"
expect 0 check-clean --check "$work/real/a/b/c/d/root"
expect 0 check-static --check "$work/static/a/b/c/d/root"
expect 0 check-emptymap --check "$work/emptymap/a/b/c/d/root"
new_root check-nested > /dev/null; r=$(root_of check-nested)
write_func "$r" api/ok 'null'; mkdir -p "$r/.vercel/output/svc/functions/deep/x.func"
echo '{"filePathMap":{"b":"../x"}}' > "$r/.vercel/output/svc/functions/deep/x.func/.vc-config.json"
expect 1 check-nested --check "$work/check-nested/a/b/c/d/root"
check "check-nested: finds a config at any depth" has check-nested "svc/functions/deep/x.func/.vc-config.json"
expect 1 check-malformed --check "$work/malformed/a/b/c/d/root"
expect 1 check-noout --check "$work/static/tmp"

echo "passed=$pass failed=$failn"
[[ $failn -eq 0 ]]
