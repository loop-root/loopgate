# Haven macOS build and release helpers

v1 ships **HTTP** over the local Loopgate Unix socket; these scripts only package the **Haven** desktop app.

## Production frontend + Wails binary

From the repo root (requires [Wails](https://wails.io/) v2 and Go):

```bash
./scripts/haven/build-macos-app.sh
```

Artifacts land under `cmd/haven/build/bin/` (see Wails output).

## Signed `.dmg` and notarization (operator steps)

Apple Developer ID Application certificate, `notarytool`, and a provisioning profile are environment-specific. Typical flow:

1. Build a release binary with `wails build` (optionally `-platform darwin/universal`).
2. Code sign the app bundle and its embedded binaries (`codesign --deep --force --options runtime`).
3. Submit for notarization (`notarytool submit` …), staple the ticket.
4. Package a drag-to-Applications `.dmg` (e.g. `create-dmg` or `hdiutil`).

Track exact commands in your internal runbook; do not commit signing identities or secrets.

## Homebrew Cask

After a public `.dmg` URL exists, add or update a Cask formula pointing at that artifact and run `brew install --cask haven` in CI or locally to verify.
