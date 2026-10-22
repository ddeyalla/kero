#!/bin/sh
# check.sh runs the checks a commit needs, with the bundled Go toolchain.
#
#   sh scripts/check.sh                    every check: tests, vet, scripts, diff
#   sh scripts/check.sh ./internal/daemon  only the tests of these packages
#
# Run the full set before a commit and once at integration. The focused form
# is for the edit-test loop, and keeps parallel agents from each running the
# whole suite on one shared cache.
#
# The tests never reach the daily app: the variables that point a client or a
# daemon at it are cleared first.
set -eu

cd "$(git rev-parse --show-toplevel)"
. scripts/dev-env.sh

unset KERO_HOME KERO_CONFIG KERO_SOCKET KERO_PANE KERO_SESSION

# System tools first: a developer zsh breaks two shell tests.
PATH="/usr/bin:/bin:/usr/sbin:/sbin:$PATH"
export PATH
go="$GOROOT/bin/go"

if [ "$#" -gt 0 ]; then
	CGO_ENABLED=0 "$go" test "$@"
	exit
fi

CGO_ENABLED=0 "$go" test ./...
"$go" tool mygo vet .
python3 -m unittest discover -s scripts -p 'test_*.py'
git diff --check
