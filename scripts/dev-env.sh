# dev-env.sh sets up the current shell to build Shibu Code with the bundled
# Go toolchain. Source it from the repository root or from any worktree:
#
#   . scripts/dev-env.sh
#
# Every worktree shares the main checkout's .toolchain: one Go, one module
# cache and one build cache. A new worktree then builds warm, and parallel
# agents do not each download modules or fill a cache of their own.

_shibu_common=$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || {
	echo "dev-env.sh: not inside a Shibu checkout" >&2
	return 1 2>/dev/null || exit 1
}
SHIBU_TOOLCHAIN="$(dirname "$_shibu_common")/.toolchain"
unset _shibu_common
if [ ! -x "$SHIBU_TOOLCHAIN/go/bin/go" ]; then
	echo "dev-env.sh: no Go toolchain at $SHIBU_TOOLCHAIN/go (see docs/build.md)" >&2
	return 1 2>/dev/null || exit 1
fi

export SHIBU_TOOLCHAIN
export GOROOT="$SHIBU_TOOLCHAIN/go"
export GOPATH="$SHIBU_TOOLCHAIN/gopath"
export GOCACHE="$SHIBU_TOOLCHAIN/gocache"
export GOTOOLCHAIN=local
case ":$PATH:" in
*":$GOROOT/bin:"*) ;;
*) export PATH="$GOROOT/bin:$PATH" ;;
esac
