#!/bin/sh
set -eu
root="$(git rev-parse --show-toplevel)"
git -C "$root" config core.hooksPath .githooks
echo "Git hooks enabled from .githooks"
