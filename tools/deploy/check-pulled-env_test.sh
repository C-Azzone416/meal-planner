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

# Usage and I/O errors exit 2.
expect 2 missing "$work/does-not-exist"
expect 2 no-args
fixture empty/.env < /dev/null
expect 0 empty "$work/empty/.env"

echo "passed=$pass failed=$failn"
[[ $failn -eq 0 ]]
