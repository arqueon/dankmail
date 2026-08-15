#!/usr/bin/env bash
# check-i18n: guard the language policy from both sides.
#
#   1. Fail if translated text shows up in Go/QML string literals outside
#      the translation layers. English is the source language for every
#      user-visible string; translations live ONLY in core/internal/i18n
#      (daemon/CLI) and quickshell/translations/<lang>.json (UI). This
#      guards against regressions like issue #1 (hard-coded Spanish
#      notification buttons).
#   2. Fail if a shipped quickshell/translations/<lang>.json has drifted
#      from the I18n.tr() call sites, in either direction: an untranslated
#      term silently renders English, and a translation with no call site
#      is dead weight left behind by a UI rewrite.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"

# Accented characters catch most Spanish and Portuguese; the word lists
# catch the accent-free strings that slipped through before ("Borrar",
# "Posponer", "Adiar").
accents='[áéíóúâêôàãõçñÁÉÍÓÚÂÊÔÀÃÕÇÑ¿¡]'
es_words='[Cc]uenta|[Cc]orreo|[Bb]orrar|[Pp]osponer|[Pp]ospuesto|[Aa]rchivar|[Bb]andeja|[Pp]esta[ñn]a|[Nn]avegador|[Ee]jecuta|[Rr]ecargado|[Aa]rrancarlo'
pt_words='[Cc]onta|[Cc]orreio|[Aa]rquivar|[Ll]ixeira|[Aa]diar|[Cc]aixa|[Cc]onversa|[Ee]xecute|[Rr]ecarregado|[Ee]nviando'
words="\\b(${es_words}|${pt_words})\\b"

fail=0
while IFS= read -r hit; do
    if [ "$fail" -eq 0 ]; then
        echo "Translated text in source string literals (move it to the i18n layer):" >&2
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
    echo "English is the source string; add the translation as an override in" >&2
    echo "core/internal/i18n/i18n.go or quickshell/translations/<lang>.json." >&2
    exit 1
fi

# en.json stays empty on purpose: English IS the source string.
python3 - "$root" <<'PY' || exit 1
import json, pathlib, re, sys

root = pathlib.Path(sys.argv[1])
call = re.compile(r'I18n\.tr\(\s*"((?:\\.|[^"\\])*)"')
terms = {m.group(1) for f in (root / "quickshell").rglob("*.qml") for m in call.finditer(f.read_text())}

failed = False
for path in sorted((root / "quickshell" / "translations").glob("*.json")):
    if path.stem == "en":
        continue
    translated = {t for context in json.loads(path.read_text()).values() for t in context}
    for term in sorted(terms - translated):
        print(f"{path.name}: no translation for {term!r}", file=sys.stderr)
        failed = True
    for term in sorted(translated - terms):
        print(f"{path.name}: translates {term!r}, which no I18n.tr() call uses", file=sys.stderr)
        failed = True

if failed:
    print("\nEvery I18n.tr() term needs an entry in each shipped locale.", file=sys.stderr)
    sys.exit(1)
PY

echo "check-i18n: OK"
