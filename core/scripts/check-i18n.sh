#!/usr/bin/env bash
# check-i18n: fail if Spanish text shows up in Go/QML string literals
# outside the translation layers. English is the source language for
# every user-visible string; Spanish lives ONLY in core/internal/i18n
# (daemon/CLI) and quickshell/translations/es.json (UI). This guards
# against regressions like issue #1 (hard-coded Spanish notification
# buttons).
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"

# Accented characters catch most Spanish; the word list catches the
# accent-free strings that slipped through before ("Borrar", "Posponer").
accents='[áéíóúñÁÉÍÓÚÑ¿¡]'
words='\b([Cc]uenta|[Cc]orreo|[Bb]orrar|[Pp]osponer|[Pp]ospuesto|[Aa]rchivar|[Bb]andeja|[Pp]esta[ñn]a|[Nn]avegador|[Ee]jecuta|[Rr]ecargado|[Aa]rrancarlo)\b'

fail=0
while IFS= read -r hit; do
    if [ "$fail" -eq 0 ]; then
        echo "Spanish in source string literals (move it to the i18n layer):" >&2
        fail=1
    fi
    echo "  $hit" >&2
done < <(
    grep -rn --include='*.go' --include='*.qml' --exclude='*_test.go' \
        -P "\"[^\"]*(${accents}|${words})[^\"]*\"" \
        "$root/core" "$root/quickshell" 2>/dev/null |
        grep -v '/internal/i18n/' |
        grep -v '/quickshell/translations/' |
        grep -vP '^[^:]+:\d+:\s*//' || true
)

if [ "$fail" -ne 0 ]; then
    echo >&2
    echo "English is the source string; add the Spanish text as an override in" >&2
    echo "core/internal/i18n/i18n.go or quickshell/translations/es.json." >&2
    exit 1
fi
echo "check-i18n: OK"
