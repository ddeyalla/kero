#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
if [ -x .toolchain/go/bin/go ]; then
  export GOPATH="$PWD/.toolchain/gopath"
  export GOCACHE="$PWD/.toolchain/gocache"
  export PATH="$PWD/.toolchain/go/bin:$PATH"
fi

# The host architecture avoids accidentally running an Intel build on Rosetta.
go tool mygo build -platform "$(go env GOOS)/$(go env GOARCH)" -sign - -skip-dmg -skip-notarize
