#!/usr/bin/env bash
# Tests for safe-extract.sh. Usage: tools/deploy/safe-extract_test.sh
# Builds malicious archives with python3's tarfile (portable across GNU and BSD tar) and asserts rejection.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script="$here/safe-extract.sh"
work=$(mktemp -d)
trap 'chmod -R u+rwx "$work" 2> /dev/null; rm -rf "$work"' EXIT
pass=0 failn=0

mktar() { # mktar <out.tar> <kind>
  python3 -I - "$1" "$2" << 'PY'
import io, sys, tarfile
out, kind = sys.argv[1:3]
t = tarfile.open(out, "w", format=tarfile.GNU_FORMAT)
def add(name, data=b"x", type=tarfile.REGTYPE, linkname=""):
    i = tarfile.TarInfo(name); i.type = type; i.linkname = linkname; i.mode = 0o755 if type == tarfile.DIRTYPE else 0o644
    if type == tarfile.REGTYPE:
        i.size = len(data); t.addfile(i, io.BytesIO(data))
    else:
        t.addfile(i)
if kind == "clean":
    add(".vercel/output/", type=tarfile.DIRTYPE); add(".vercel/output/config.json", b"{}"); add(".vercel/project.json", b"{}")
elif kind == "symlink":
    add(".vercel/output", type=tarfile.SYMTYPE, linkname="/tmp"); add(".vercel/output/evil", b"pwn")
elif kind == "hardlink":
    add(".vercel/project.json", type=tarfile.LNKTYPE, linkname="/etc/passwd")
elif kind == "dotdot":
    add(".vercel/../../escape", b"pwn")
elif kind == "dotdot_end":
    add(".vercel/output/..", type=tarfile.DIRTYPE)
elif kind == "dotdot_name":
    add(".vercel/output/", type=tarfile.DIRTYPE); add(".vercel/output/chunk..js", b"x"); add(".vercel/output/a..b/c", b"x")
elif kind == "absolute":
    add("/tmp/safe-extract-abs", b"pwn")
elif kind == "fifo":
    add(".vercel/project.json", type=tarfile.FIFOTYPE)
elif kind == "outside_allow":
    add("other/file", b"x")
t.close()
PY
}

check() { # check <description> <command...>: passes when the command succeeds
  local desc=$1; shift
  if "$@"; then echo "ok   $desc"; pass=$((pass + 1)); else echo "FAIL $desc"; failn=$((failn + 1)); fi
}

expect() { # expect <accept|reject> <name> <tar> [allow_re]
  local want=$1 name=$2 tarfile=$3 re=${4:-} dest="$work/dest-$2" rc=0
  SAFE_EXTRACT_ALLOW_RE=$re "$script" "$tarfile" "$dest" > "$work/out" 2>&1 || rc=$?
  if [[ $want == accept && $rc -eq 0 ]] || [[ $want == reject && $rc -ne 0 ]]; then
    echo "ok   $want $name"; pass=$((pass + 1))
  else
    echo "FAIL $want $name (rc=$rc)"; cat "$work/out"; failn=$((failn + 1))
  fi
}

allow='^\.vercel/(output(/|$)|project\.json$)'
mktar "$work/clean.tar" clean;   expect accept clean "$work/clean.tar" "$allow"
check "clean tar content extracted" test -f "$work/dest-clean/.vercel/project.json"
for kind in symlink hardlink dotdot dotdot_end absolute fifo; do
  mktar "$work/$kind.tar" "$kind"; expect reject "$kind" "$work/$kind.tar"
done
# `..` is rejected only as a whole path component; names that merely contain two dots are fine.
mktar "$work/dotdot_name.tar" dotdot_name; expect accept dots-inside-name "$work/dotdot_name.tar" "$allow"
check "chunk..js extracted" test -f "$work/dest-dots-inside-name/.vercel/output/chunk..js"
mktar "$work/outside.tar" outside_allow; expect reject outside-allowlist "$work/outside.tar" "$allow"
check "nothing escaped" test ! -e /tmp/safe-extract-abs

# Non-empty destination is refused.
mkdir "$work/nonempty"; touch "$work/nonempty/f"
# shellcheck disable=SC2016 # expansion happens in the inner shell
check "reject non-empty dest" bash -c '! "$0" "$1" "$2" > /dev/null 2>&1' "$script" "$work/clean.tar" "$work/nonempty"

echo "passed=$pass failed=$failn"
[[ $failn -eq 0 ]]
