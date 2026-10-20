#!/bin/sh
set -eu
cd "$(git rev-parse --show-toplevel)"
command -v gitleaks >/dev/null || { echo 'Install Gitleaks: brew install gitleaks' >&2; exit 1; }
existing=$(git config --get core.hooksPath || true)
if [ -n "$existing" ] && [ "$existing" != '.githooks' ]; then
  echo "Existing hooks at $existing; merge them before installing Shibu guards." >&2
  exit 1
fi
chmod +x .githooks/pre-commit .githooks/pre-push
git config core.hooksPath .githooks
git config remote.pushDefault origin
git remote set-url --push upstream DISABLED
echo 'Installed repository-local secret guards and fork-only push policy.'
