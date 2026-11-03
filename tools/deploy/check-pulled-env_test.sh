#!/usr/bin/env bash
# Tests for check-pulled-env.sh. Usage: tools/deploy/check-pulled-env_test.sh
# Every fixture value contains $secret; no output of any case may contain it.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$here/check-pulled-env.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
pass=0 failn=0
secret=sk_live_DO_NOT_PRINT_7f3a9c

check() { # check <description> <command...>: passes when the command succeeds
  local desc=$1; shift
  if "$@"; then echo "ok   $desc"; pass=$((pass + 1)); else echo "FAIL $desc"; failn=$((failn + 1)); fi
}

expect() { # expect <rc> <name> <path...>: runs the script, keeps its output in $work/<name>.out
  local want=$1 name=$2 rc=0; shift 2
  "$script" "$@" > "$work/$name.out" 2>&1 || rc=$?
  if [[ $rc -eq $want ]]; then
    echo "ok   rc=$want $name"; pass=$((pass + 1))
  else
    echo "FAIL rc=$want $name (rc=$rc)"; failn=$((failn + 1))
    # Fixture values are test data, but redact anyway so a broken script cannot model a leak here.
    sed "s/$secret/<redacted>/g" "$work/$name.out"
  fi
  if grep -qF "$secret" "$work/$name.out"; then
    echo "FAIL $name printed a value"; failn=$((failn + 1))
  else
    pass=$((pass + 1))
  fi
}

has() { grep -qF -- "$2" "$work/$1.out"; } # has <name> <text>
lacks() { ! grep -qF -- "$2" "$work/$1.out"; }

fixture() { mkdir -p "$(dirname "$work/$1")"; cat > "$work/$1"; } # fixture <path> < content

# Only VERCEL_ variables, in the shape `vercel pull` writes.
fixture vercel-only/.env.preview.local << EOF
# Created by Vercel CLI
VERCEL_ENV="preview"
VERCEL_OIDC_TOKEN="eyJ.$secret.sig"
VERCEL_GIT_COMMIT_SHA=""
VERCEL_URL=""
EOF
expect 0 vercel-only "$work/vercel-only/.env.preview.local"
check "vercel-only: reports no extra system vars" has vercel-only "allowed system vars present (non-VERCEL_): none"

# Allowlisted exact names pass and are named on a pass.
fixture system/.env.preview.local << EOF
VERCEL="1"
CI=1
VERCEL_OIDC_TOKEN="$secret"
EOF
expect 0 system "$work/system/.env.preview.local"
check "system: names VERCEL and CI" has system "allowed system vars present (non-VERCEL_): VERCEL, CI"

# An app variable fails and its NAME (only) is reported.
fixture app/.env.preview.local << EOF
VERCEL_ENV="preview"
DATABASE_URL="postgres://u:$secret@db/x"
EOF
expect 1 app "$work/app/.env.preview.local"
check "app: names DATABASE_URL with its line" has app "line 2: unexpected variable name DATABASE_URL"
check "app: says the design must be revisited" has app "env handoff design must be revisited"
check "app: does not flag the VERCEL_ line" lacks app "VERCEL_ENV"

# Prefix lookalikes are not system variables.
fixture lookalike/.env << EOF
VERCELX="$secret"
NEXT_PUBLIC_VERCEL_URL="$secret"
CI_TOKEN="$secret"
EOF
expect 1 lookalike "$work/lookalike/.env"
check "lookalike: VERCELX flagged" has lookalike "unexpected variable name VERCELX"
check "lookalike: NEXT_PUBLIC_VERCEL_URL flagged" has lookalike "unexpected variable name NEXT_PUBLIC_VERCEL_URL"
check "lookalike: CI_TOKEN flagged" has lookalike "unexpected variable name CI_TOKEN"

# Quoted multi-line values whose continuation lines look like assignments are not false positives.
fixture multiline/.env << EOF
VERCEL_A="first
DATABASE_URL=$secret
  # not a comment, still the value
last"
VERCEL_B='one
API_KEY=$secret'
VERCEL_C=\`x
TOKEN=$secret\`
VERCEL_D="escaped \" quote
DATABASE_URL=$secret \\" still inside" # trailing comment
VERCEL_E="a=b=c $secret"
EOF
expect 0 multiline "$work/multiline/.env"
check "multiline: 5 variables" has multiline "5 variable(s)"

# A multi-line value hides nothing: an app var after it is still found, on its own line number.
fixture multiline-then-app/.env << EOF
VERCEL_A="x
$secret"
STRIPE_KEY=$secret
EOF
expect 1 multiline-then-app "$work/multiline-then-app/.env"
check "multiline-then-app: STRIPE_KEY at line 3" has multiline-then-app "line 3: unexpected variable name STRIPE_KEY"

# export prefix and CRLF line endings.
printf 'export VERCEL_ENV="preview"\r\n  export   VERCEL_URL=%s\r\n\r\n# comment\r\nexport CI=1\r\n' "$secret" > "$work/export-crlf.env"
expect 0 export-crlf "$work/export-crlf.env"
check "export-crlf: 3 variables" has export-crlf "3 variable(s)"
printf 'export VERCEL_ENV="preview"\r\nexport SESSION_SECRET="%s"\r\n' "$secret" > "$work/export-crlf-bad.env"
expect 1 export-crlf-bad "$work/export-crlf-bad.env"
check "export-crlf-bad: SESSION_SECRET flagged" has export-crlf-bad "line 2: unexpected variable name SESSION_SECRET"

# No trailing newline on the last line is still checked.
printf 'VERCEL_ENV=preview\nAPP_SECRET=%s' "$secret" > "$work/no-eol.env"
expect 1 no-eol "$work/no-eol.env"
check "no-eol: APP_SECRET flagged" has no-eol "line 2: unexpected variable name APP_SECRET"

# Garbage fails closed and is never echoed.
fixture garbage/.env << EOF
VERCEL_ENV="preview"
this is not $secret an assignment
-----BEGIN $secret-----
EOF
expect 1 garbage "$work/garbage/.env"
check "garbage: line 2 unparseable" has garbage "line 2: unparseable"
check "garbage: line 3 unparseable" has garbage "line 3: unparseable"
check "garbage: line text not echoed" lacks garbage "not an assignment"

# Unterminated quote, junk after a closing quote, and invalid names fail closed.
fixture unterminated/.env << EOF
VERCEL_OK=1
VERCEL_A="$secret
VERCEL_B=2
EOF
expect 1 unterminated "$work/unterminated/.env"
check "unterminated: reported at its start line" has unterminated "line 2: unparseable"
fixture junk-after-quote/.env << EOF
VERCEL_A="$secret"junk
EOF
expect 1 junk-after-quote "$work/junk-after-quote/.env"
fixture bad-name/.env << EOF
VERCEL_A.B=$secret
EOF
expect 1 bad-name "$work/bad-name/.env"

# A directory is searched for .env* files (like .vercel); other files are ignored.
mkdir -p "$work/dir/.vercel"
printf 'VERCEL_ENV=preview\n' > "$work/dir/.vercel/.env.preview.local"
printf '{"projectId":"%s"}\n' "$secret" > "$work/dir/.vercel/project.json"
expect 0 dir "$work/dir/.vercel"
check "dir: checked the env file" has dir ".env.preview.local: 1 variable(s)"
printf 'DATABASE_URL=%s\n' "$secret" > "$work/dir/.vercel/.env.production.local"
expect 1 dir-bad "$work/dir/.vercel"
check "dir-bad: names DATABASE_URL" has dir-bad "unexpected variable name DATABASE_URL"
check "dir-bad: still checks the good file" has dir-bad ".env.preview.local: 1 variable(s)"

# F1: a stray base64 or non-uppercase line is unparseable; its text is never printed as a "name".
b64=QUJDREVGR0hJSktMTU5PUA
fixture b64/.env << EOF
VERCEL_ENV="preview"
$secret==
$b64==
lower_case=1
Mixed_Case="x"
EOF
expect 1 b64 "$work/b64/.env"
for l in 2 3 4 5; do check "b64: line $l unparseable" has b64 "line $l: unparseable"; done
check "b64: uppercase base64 not printed" lacks b64 "$b64"
check "b64: no name printed" lacks b64 "unexpected variable name"

# F2: a lone CR is a line break to dotenv, so it must not hide a second assignment.
printf 'VERCEL_ENV=preview\rDATABASE_URL=%s\n' "$secret" > "$work/lone-cr.env"
expect 1 lone-cr "$work/lone-cr.env"
check "lone-cr: line 1 unparseable" has lone-cr "line 1: unparseable"
printf 'VERCEL_A="x\rDATABASE_URL=%s"\r\n' "$secret" > "$work/lone-cr-quoted.env"
expect 1 lone-cr-quoted "$work/lone-cr-quoted.env"

# NUL bytes fail closed.
printf 'VERCEL_ENV=preview\0\nVERCEL_URL=%s\n' "$secret" > "$work/nul.env"
expect 1 nul "$work/nul.env"
check "nul: reported" has nul "contains NUL bytes"

# F3: a .env* entry that is a symlink or not a regular file fails closed, as does a symlinked argument.
mkdir -p "$work/links/.vercel" "$work/links/.vercel/.env.d"
printf 'DATABASE_URL=%s\n' "$secret" > "$work/links/target"
ln -s ../target "$work/links/.vercel/.env.preview.local"
expect 1 symlink-entry "$work/links/.vercel"
check "symlink-entry: reported" has symlink-entry ".vercel/.env.preview.local is a symlink or not a regular file"
check "symlink-entry: .env.d directory reported" has symlink-entry ".vercel/.env.d is a symlink or not a regular file"
expect 1 symlink-arg "$work/links/.vercel/.env.preview.local"
ln -s .vercel "$work/links/vercel-link"
expect 1 symlink-dir-arg "$work/links/vercel-link"

# F4: a file that cannot be opened is an I/O error (exit 2), never an empty pass. Skipped when running as
# root, where chmod 000 does not stop reads.
printf 'DATABASE_URL=%s\n' "$secret" > "$work/unreadable.env"
mkdir -p "$work/unreadable-dir/.vercel"
printf 'DATABASE_URL=%s\n' "$secret" > "$work/unreadable-dir/.vercel/.env.preview.local"
chmod 000 "$work/unreadable.env" "$work/unreadable-dir/.vercel/.env.preview.local"
if [[ -r $work/unreadable.env ]]; then
  echo "skip unreadable (running as root)"
else
  expect 2 unreadable "$work/unreadable.env"
  check "unreadable: no pass line" lacks unreadable "variable(s)"
  expect 2 unreadable-dir "$work/unreadable-dir/.vercel"
fi
chmod 600 "$work/unreadable.env" "$work/unreadable-dir/.vercel/.env.preview.local"

# Vercel build settings (NX_DAEMON, TURBO_*): exact names with constrained values.
# The exact shape `vercel pull` writes for preview: comment, the five sorted, then VERCEL_*.
fixture build-settings/.env.preview.local << EOF
# Created by Vercel CLI
NX_DAEMON="false"
TURBO_CACHE="remote:rw"
TURBO_DOWNLOAD_LOCAL_ENABLED="1"
TURBO_REMOTE_ONLY="true"
TURBO_RUN_SUMMARY="false"
VERCEL="1"
VERCEL_ENV="preview"
VERCEL_OIDC_TOKEN="eyJ.$secret.sig"
EOF
expect 0 build-settings "$work/build-settings/.env.preview.local"
check "build-settings: 8 variables" has build-settings "8 variable(s)"
check "build-settings: names the allowed ones" has build-settings "present (non-VERCEL_): NX_DAEMON, TURBO_CACHE, TURBO_DOWNLOAD_LOCAL_ENABLED, TURBO_REMOTE_ONLY, TURBO_RUN_SUMMARY, VERCEL"

# Every allowed value, unquoted, double-quoted and single-quoted.
for v in true false 1 0; do
  for q in '' '"' "'"; do
    printf 'TURBO_DOWNLOAD_LOCAL_ENABLED=%s\nTURBO_REMOTE_ONLY=%s\nTURBO_RUN_SUMMARY=%s\n' "$q$v$q" "$q$v$q" "$q$v$q" > "$work/bool.env"
    expect 0 "bool-$v-q${#q}" "$work/bool.env"
  done
done
for v in remote:rw local:rw,remote:rw local:r,remote:w local:rw remote:r; do
  printf 'TURBO_CACHE=%s\nNX_DAEMON=false\n' "$v" > "$work/cache.env"
  expect 0 "cache-$v" "$work/cache.env"
done

# Each name with a wrong value fails by name and line, and nothing of the value is printed.
for n in NX_DAEMON TURBO_CACHE TURBO_DOWNLOAD_LOCAL_ENABLED TURBO_REMOTE_ONLY TURBO_RUN_SUMMARY; do
  printf 'VERCEL_ENV=preview\n%s="%s"\n' "$n" "$secret" > "$work/wrong-$n.env"
  expect 1 "wrong-$n" "$work/wrong-$n.env"
  check "wrong-$n: names it at line 2" has "wrong-$n" "line 2: $n has an unexpected value"
  check "wrong-$n: no value prefix" lacks "wrong-$n" "sk_live"
  check "wrong-$n: not reported as an unexpected name" lacks "wrong-$n" "unexpected variable name"
done

# Near misses on each constraint, quoted or not, all fail closed.
bad_value() { # bad_value <case> <NAME> <raw text after =>: the line NAME=<raw> must fail with the value message
  printf 'VERCEL_ENV=preview\n%s=%s\n' "$2" "$3" > "$work/$1.env"
  expect 1 "$1" "$work/$1.env"
  check "$1: unexpected value at line 2" has "$1" "line 2: $2 has an unexpected value"
}
bad_value nx-true NX_DAEMON true
bad_value nx-upper NX_DAEMON '"False"'
bad_value nx-empty NX_DAEMON ''
bad_value nx-empty-quoted NX_DAEMON '""'
bad_value nx-space NX_DAEMON '" false"'
bad_value nx-comment NX_DAEMON 'false # note'
bad_value bool-yes TURBO_REMOTE_ONLY yes
bad_value bool-2 TURBO_RUN_SUMMARY 2
bad_value bool-upper TURBO_DOWNLOAD_LOCAL_ENABLED TRUE
bad_value bool-empty TURBO_REMOTE_ONLY ''
bad_value bool-list TURBO_RUN_SUMMARY 'true,false'
bad_value cache-empty TURBO_CACHE '""'
bad_value cache-nopair TURBO_CACHE remote
bad_value cache-trailing-comma TURBO_CACHE 'remote:rw,'
bad_value cache-url TURBO_CACHE 'https://example.com/x'
bad_value cache-upper TURBO_CACHE 'Remote:RW'
# TURBO_CACHE is capped at 32 characters: 32 passes, 33 fails.
printf 'TURBO_CACHE=%s\n' "aaaaaaaaaaaaaaa:bbbbbbbbbbbbbbbb" > "$work/cache-32.env"
expect 0 cache-32 "$work/cache-32.env"
bad_value cache-33 TURBO_CACHE "aaaaaaaaaaaaaaa:bbbbbbbbbbbbbbbbb"

# A multi-line value never matches, even if its first line looks right.
fixture multiline-setting/.env << EOF
NX_DAEMON="false
"
EOF
expect 1 multiline-setting "$work/multiline-setting/.env"
check "multiline-setting: unexpected value at its start line" has multiline-setting "line 1: NX_DAEMON has an unexpected value"

# An app var mixed in with valid build settings still fails, by name.
fixture build-settings-app/.env.preview.local << EOF
# Created by Vercel CLI
NX_DAEMON="false"
TURBO_CACHE="remote:rw"
DATABASE_URL="postgres://u:$secret@db/x"
TURBO_RUN_SUMMARY="false"
VERCEL_ENV="preview"
EOF
expect 1 build-settings-app "$work/build-settings-app/.env.preview.local"
check "build-settings-app: names DATABASE_URL" has build-settings-app "line 4: unexpected variable name DATABASE_URL"
check "build-settings-app: valid settings not flagged" lacks build-settings-app "has an unexpected value"

# Lookalikes of the new names are not allowlisted: no prefix families.
fixture build-settings-lookalike/.env << EOF
NX_DAEMON_X="false"
TURBO_TOKEN="$secret"
TURBO_TEAM="$secret"
NX_CLOUD_ACCESS_TOKEN="$secret"
TURBO_CACHE_DIR="x"
EOF
expect 1 build-settings-lookalike "$work/build-settings-lookalike/.env"
for n in NX_DAEMON_X TURBO_TOKEN TURBO_TEAM NX_CLOUD_ACCESS_TOKEN TURBO_CACHE_DIR; do
  check "build-settings-lookalike: $n flagged" has build-settings-lookalike "unexpected variable name $n"
done

# The fail-closed paths still apply next to the new names: lone CR and NUL fail, CRLF passes.
printf 'NX_DAEMON=false\rDATABASE_URL=%s\n' "$secret" > "$work/setting-lone-cr.env"
expect 1 setting-lone-cr "$work/setting-lone-cr.env"
printf 'NX_DAEMON=false\0\n' > "$work/setting-nul.env"
expect 1 setting-nul "$work/setting-nul.env"
printf 'NX_DAEMON="false"\r\nTURBO_REMOTE_ONLY=true\r\n' > "$work/setting-crlf.env"
expect 0 setting-crlf "$work/setting-crlf.env"

# Usage and I/O errors exit 2.
expect 2 missing "$work/does-not-exist"
expect 2 no-args
fixture empty/.env < /dev/null
expect 0 empty "$work/empty/.env"

echo "passed=$pass failed=$failn"
[[ $failn -eq 0 ]]
