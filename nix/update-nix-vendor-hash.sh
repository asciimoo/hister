#!/usr/bin/env bash

set -euo pipefail

PACKAGE_FILE="nix/package.nix"
FAKE_HASH="sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

CURRENT_HASH=$(grep -oP 'vendorHash = "\K[^"]+' "$PACKAGE_FILE")

# Force nix to compute the correct vendorHash instead of reusing cache.
sed -i.bak "s|vendorHash = \"$CURRENT_HASH\"|vendorHash = \"$FAKE_HASH\"|" "$PACKAGE_FILE"
rm -f "$PACKAGE_FILE.bak"

echo "::group::Build to determine vendorHash"
OUTPUT=$(nix build .#hister 2>&1 || true)
echo "$OUTPUT"
echo "::endgroup::"

if echo "$OUTPUT" | grep -q "hash mismatch in fixed-output derivation"; then
  GOT_HASH=$(echo "$OUTPUT" | grep "got:" | sed 's/.*got: *//')
  echo "::notice::New vendorHash: $GOT_HASH"
  sed -i.bak "s|vendorHash = \"$FAKE_HASH\"|vendorHash = \"$GOT_HASH\"|" "$PACKAGE_FILE"
  rm -f "$PACKAGE_FILE.bak"
else
  echo "::error::Expected a fixed-output hash mismatch but none was reported; restoring original hash"
  sed -i.bak "s|vendorHash = \"$FAKE_HASH\"|vendorHash = \"$CURRENT_HASH\"|" "$PACKAGE_FILE"
  rm -f "$PACKAGE_FILE.bak"
  exit 1
fi

echo "::group::Verifying final build"
nix build .#hister 2>&1 | tail -5
echo "::endgroup::"
echo "::notice::Build successful! vendorHash updated to $GOT_HASH"
