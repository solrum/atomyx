#!/usr/bin/env bash
# Builds atomyx-sim-hid as a properly-signed .app bundle so
# SimulatorKit private APIs are accessible via the hardened runtime.
#
# Outputs:
#   atomyx-sim-hid.app/          ← signed bundle
#   atomyx-sim-hid                ← symlink into the bundle executable
#                                   for backward compatibility with the
#                                   Rust path resolver.
#
# Environment overrides:
#   ATOMYX_SIGN_IDENTITY   Codesign identity (e.g. "Developer ID
#                          Application: Hieu Nguyen (956PUTVBFG)").
#                          Defaults to the first Developer ID found.

set -euo pipefail

cd "$(dirname "$0")"

SOURCE=main.swift
BIN=atomyx-sim-hid
APP=${BIN}.app
CONTENTS=${APP}/Contents
MACOS=${CONTENTS}/MacOS
PLIST=Info.plist
ENT=entitlements.plist

IDENTITY=${ATOMYX_SIGN_IDENTITY:-}
if [ -z "${IDENTITY}" ]; then
    IDENTITY=$(security find-identity -v -p codesigning | awk -F'"' '/"Developer ID Application/ {print $2; exit}')
fi
if [ -z "${IDENTITY}" ]; then
    echo "error: no 'Developer ID Application' identity found in keychain" >&2
    echo "       set ATOMYX_SIGN_IDENTITY to an explicit identity" >&2
    exit 1
fi

echo "→ compiling ${SOURCE}"
swiftc -O -o "${BIN}.bin" "${SOURCE}"

echo "→ building bundle ${APP}"
rm -rf "${APP}"
mkdir -p "${MACOS}"
cp "${PLIST}" "${CONTENTS}/Info.plist"
mv "${BIN}.bin" "${MACOS}/${BIN}"
chmod +x "${MACOS}/${BIN}"

echo "→ codesigning with '${IDENTITY}'"
codesign --force \
  --options runtime \
  --entitlements "${ENT}" \
  --sign "${IDENTITY}" \
  --timestamp=none \
  "${APP}"

echo "→ verifying signature"
codesign -dv --verbose=2 "${APP}" 2>&1 | grep -E "Authority|TeamIdentifier|Identifier|Runtime"

# Keep a sibling `atomyx-sim-hid` path pointing at the bundle
# executable so the Rust path resolver finds a runnable binary
# without any code change.
rm -f "${BIN}"
ln -s "${MACOS}/${BIN}" "${BIN}"

echo "✔ ${APP} ready"
