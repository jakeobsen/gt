#!/usr/bin/env bash
# Sign a macOS binary with a Developer ID Application certificate and have
# Apple notarize it.
#
# GoReleaser runs this on the universal binary (see .goreleaser.yaml). The
# release workflow sets KEYCHAIN_PATH to a temporary keychain that holds the
# signing certificate and a notarytool credentials profile, so this script
# never sees a password or Apple ID.
#
# Without KEYCHAIN_PATH (local snapshot builds) it leaves the binary unsigned.
set -euo pipefail
set +x # never trace: commands here run with signing material available

bin=${1:?usage: sign-and-notarize.sh <binary>}
profile=${NOTARY_PROFILE:-gt-notary}

if [[ -z "${KEYCHAIN_PATH:-}" ]]; then
  echo "sign-and-notarize: KEYCHAIN_PATH is not set; leaving $bin unsigned"
  exit 0
fi

# Sign only with the identity in KEYCHAIN_PATH. codesign's --keychain does not
# stop it falling back to the user's other keychains, so look the identity up
# in that keychain alone and sign with its exact SHA-1 hash.
if [[ ! -f "$KEYCHAIN_PATH" ]]; then
  echo "sign-and-notarize: keychain $KEYCHAIN_PATH does not exist" >&2
  exit 1
fi
identity=$(security find-identity -v -p codesigning "$KEYCHAIN_PATH" |
  awk '/"Developer ID Application: / { print $2; exit }')
if [[ -z "$identity" ]]; then
  echo "sign-and-notarize: no Developer ID Application identity in $KEYCHAIN_PATH" >&2
  exit 1
fi

echo "sign-and-notarize: signing $bin"
codesign --force \
  --sign "$identity" \
  --keychain "$KEYCHAIN_PATH" \
  --options runtime \
  --timestamp \
  "$bin"
codesign --verify --strict --verbose=2 "$bin"

# notarytool accepts zip, dmg or pkg; a bare binary goes in a zip.
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
zip="$work/$(basename "$bin").zip"
ditto -c -k --keepParent "$bin" "$zip"

notary=(--keychain-profile "$profile" --keychain "$KEYCHAIN_PATH")

echo "sign-and-notarize: submitting to Apple and waiting for the result"
result=$(xcrun notarytool submit "$zip" "${notary[@]}" --wait --timeout 20m --output-format json)
status=$(printf '%s' "$result" | plutil -extract status raw -o - -)
id=$(printf '%s' "$result" | plutil -extract id raw -o - -)

if [[ "$status" != "Accepted" ]]; then
  echo "sign-and-notarize: notarization finished with status \"$status\" (submission $id)" >&2
  xcrun notarytool log "$id" "${notary[@]}" >&2 || true
  exit 1
fi
echo "sign-and-notarize: notarized (submission $id)"
