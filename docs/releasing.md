# Releasing

A release is a pushed `v*` tag. The [release workflow](../.github/workflows/release.yml) then:

1. checks that every release secret is set,
2. runs the tests,
3. runs [GoReleaser](https://goreleaser.com) with [`.goreleaser.yaml`](../.goreleaser.yaml), which
   - builds `gt` for Apple silicon and Intel and joins them into one universal binary,
   - signs it with a Developer ID Application certificate (hardened runtime, secure timestamp),
   - submits it to Apple for notarization and waits for the result — a rejection stops the release before anything is published,
   - publishes a GitHub release with the archive and checksums,
   - writes the `gt` cask to [jakeobsen/homebrew-tap](https://github.com/jakeobsen/homebrew-tap),
4. verifies the signature on the binary it built.

## Cutting a release

```bash
git tag v0.1.0
git push origin v0.1.0
```

Versions follow [semantic versioning](https://semver.org). `gt --version` reports the tag without the `v`.

## One-time setup: repository secrets

The workflow needs six secrets in the gt repository (Settings → Secrets and variables → Actions). It stops with an error naming any that are missing.

| Secret | What it is |
|---|---|
| `MACOS_SIGN_P12` | Developer ID Application certificate and its private key, as a base64-encoded `.p12` |
| `MACOS_SIGN_PASSWORD` | The password the `.p12` was exported with |
| `MACOS_NOTARY_KEY` | App Store Connect API key, as a base64-encoded `.p8` |
| `MACOS_NOTARY_KEY_ID` | That key's Key ID |
| `MACOS_NOTARY_ISSUER_ID` | The Issuer ID shown above the key list in App Store Connect |
| `HOMEBREW_TAP_DEPLOY_KEY` | Private half of an SSH deploy key with write access to the tap |

### Signing certificate

1. In Keychain Access, find the **Developer ID Application** certificate under *My Certificates*. (Create one at developer.apple.com → Certificates if you don't have it; it needs the Account Holder role.)
2. Right-click → *Export*, save as `.p12`, and set a strong password.
3. Store both, then delete the export:

   ```bash
   base64 -i DeveloperID.p12 | gh secret set MACOS_SIGN_P12 --repo jakeobsen/gt
   gh secret set MACOS_SIGN_PASSWORD --repo jakeobsen/gt     # paste the password when asked
   rm DeveloperID.p12
   ```

### Notarization key

1. In App Store Connect → *Users and Access* → *Integrations* → *App Store Connect API* → *Team Keys*, generate a key with the **Developer** role.
2. Download the `.p8` (Apple only lets you download it once) and note its Key ID and the Issuer ID.
3. Store them:

   ```bash
   base64 -i AuthKey_XXXXXXXXXX.p8 | gh secret set MACOS_NOTARY_KEY --repo jakeobsen/gt
   gh secret set MACOS_NOTARY_KEY_ID --repo jakeobsen/gt
   gh secret set MACOS_NOTARY_ISSUER_ID --repo jakeobsen/gt
   ```

   Keep the `.p8` somewhere safe or delete it; a new key can always be generated.

### Tap deploy key

GoReleaser pushes the cask to the tap over SSH with a key that can write to that one repository and nothing else:

```bash
ssh-keygen -t ed25519 -N "" -C "gt release" -f gt_tap_key
gh repo deploy-key add gt_tap_key.pub --repo jakeobsen/homebrew-tap --allow-write --title "gt release"
gh secret set HOMEBREW_TAP_DEPLOY_KEY --repo jakeobsen/gt < gt_tap_key
rm gt_tap_key gt_tap_key.pub
```

## Trying a release locally

A snapshot release builds everything except signing and publishing, into `dist/`:

```bash
HOMEBREW_TAP_DEPLOY_KEY=unused goreleaser release --snapshot --clean
```

## Checking a published release

```bash
brew install jakeobsen/tap/gt
codesign --verify --strict --verbose=2 "$(which gt)"
codesign --display --verbose=2 "$(which gt)"   # Authority=Developer ID Application, flags=…runtime
```
