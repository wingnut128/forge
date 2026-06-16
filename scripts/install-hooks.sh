#!/usr/bin/env bash
set -euo pipefail

echo "==> Installing pre-commit hook..."
git config core.hooksPath .githooks
echo "==> Done. Hook installed at .githooks/pre-commit"
