#!/usr/bin/env bash
set -euo pipefail

HOOK=".git/hooks/pre-commit"

echo "==> Installing pre-commit hook..."

cat > "$HOOK" << 'HOOKEOF'
#!/usr/bin/env bash
set -euo pipefail

echo "==> Running pre-commit checks..."

# go fmt — check for unformatted files
UNFMT=$(gofmt -l . 2>&1 | grep -v vendor || true)
if [ -n "$UNFMT" ]; then
    echo "FAIL: go fmt — unformatted files:"
    echo "$UNFMT"
    exit 1
fi

# go vet
if ! go vet ./... 2>&1; then
    echo "FAIL: go vet"
    exit 1
fi

# go build
if ! go build ./... 2>&1; then
    echo "FAIL: go build"
    exit 1
fi

echo "==> All checks passed."
HOOKEOF

chmod +x "$HOOK"
echo "==> Done. Hook installed at $HOOK"
