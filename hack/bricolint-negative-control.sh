#!/usr/bin/env bash
# SPDX-License-Identifier: BSD-3-Clause
#
# Negative control for the bricolint hand-drawn-UI guard.
#
# A guard that never fires is worthless: this script proves bricolint actually
# BITES on this app. It runs go vet with the bricolint vettool three times:
#
#   1. clean tree                       -> MUST exit 0 (no hand-drawn UI)
#   2. a raw painter primitive injected -> MUST exit non-zero AND emit the
#      bricolint "hand-drawn UI" diagnostic (not a mere compile error)
#   3. injection removed                -> MUST exit 0 again
#
# The injection target is scene.go's draw() method, the one real site that holds
# a live *painter.PixelPainter (p := painter.NewPixelPainter(...)). A raw
# p.FillRect(...) there is exactly the class of hand-drawn chrome the guard
# forbids. The insert is an awk pass anchored on that constructor line, and a
# shell trap restores the pristine file no matter how the script exits.
#
# Env:
#   BRICOLINT  path to the bricolint binary (default: $(go env GOPATH)/bin/bricolint)
set -euo pipefail

BRICOLINT="${BRICOLINT:-$(go env GOPATH)/bin/bricolint}"
if [ ! -x "$BRICOLINT" ]; then
  echo "negative-control: bricolint not found/executable at: $BRICOLINT" >&2
  exit 1
fi

# Run from the module root (this script lives in hack/).
cd "$(dirname "$0")/.."

TARGET="scene.go"
ANCHOR='p := painter\.NewPixelPainter\(buf, s\.w, s\.h\)'
INJECT=$'\tp.FillRect(painter.Rect{X: 0, Y: 0, W: 1, H: 1}, s.theme.Background) // NEGATIVE-CONTROL: raw painter primitive, must be flagged'
DIAG='hand-drawn UI'

# Restore the target ONLY once a real backup has been taken (RESTORE=1) and it
# is non-empty. This stops a failure before the cp below from letting the trap
# copy an empty temp over $TARGET and wipe it.
BACKUP="$(mktemp)"
RESTORE=0
restore() { [ "$RESTORE" = 1 ] && [ -s "$BACKUP" ] && cp "$BACKUP" "$TARGET"; rm -f "$BACKUP"; return 0; }
trap restore EXIT
cp "$TARGET" "$BACKUP"; RESTORE=1

vet() { GOWORK=off go vet -vettool="$BRICOLINT" ./... ; }

# 1. Clean tree: the guard must be silent.
echo "negative-control [1/3]: clean tree must pass"
if ! vet; then
  echo "negative-control: FAIL — guard fired on the clean tree" >&2
  exit 1
fi

# 2. Inject a raw painter primitive right after the PixelPainter is created.
echo "negative-control [2/3]: injecting a raw painter primitive; guard must bite"
awk -v inject="$INJECT" '1; /'"$ANCHOR"'/{print inject}' "$BACKUP" > "$TARGET"
if ! grep -q 'p\.FillRect' "$TARGET"; then
  echo "negative-control: FAIL — injection anchor not found in $TARGET" >&2
  exit 1
fi
out="$(vet 2>&1)" && status=0 || status=$?
if [ "$status" -eq 0 ]; then
  echo "negative-control: FAIL — guard stayed silent on injected hand-drawn UI" >&2
  echo "$out" >&2
  exit 1
fi
if ! printf '%s\n' "$out" | grep -q "$DIAG"; then
  echo "negative-control: FAIL — non-zero exit but no bricolint diagnostic (compile error?)" >&2
  echo "$out" >&2
  exit 1
fi
echo "negative-control: guard bit as expected:"
printf '%s\n' "$out" | grep "$DIAG" || true

# 3. Remove the injection; the guard must fall silent again.
echo "negative-control [3/3]: removing injection; guard must pass again"
restore
trap - EXIT
if ! vet; then
  echo "negative-control: FAIL — guard still firing after the injection was removed" >&2
  exit 1
fi

echo "negative-control: OK — the guard bites on hand-drawn UI and passes clean code"
