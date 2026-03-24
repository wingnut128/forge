#!/usr/bin/env bash
set -euo pipefail

echo "==> Checking for pre-commit..."
if ! command -v pre-commit &>/dev/null; then
    echo "    pre-commit not found. Installing via pip..."
    pip install --quiet pre-commit
fi

echo "==> Installing pre-commit hooks..."
pre-commit install

echo "==> Done. Hooks will run on every commit."
