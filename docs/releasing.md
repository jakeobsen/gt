# Releasing

A release is a pushed `v*` tag. The [release workflow](../.github/workflows/release.yml) then:

1. checks that every release secret is set,
2. runs the tests,
3. loads the signing certificate and the notarization credentials into a temporary keychain,
4. runs [GoReleaser](https://goreleaser.com) with [`.goreleaser.yaml`](../.goreleaser.yaml), which
   - builds `gt` for Apple silicon and Intel and joins them into one universal binary,
   - runs [`scripts/sign-and-notarize.sh`](../scripts/sign-and-notarize.sh) on it: `codesign` with the Developer ID Application certificate (hardened runtime, secure timestamp), then `notarytool` with the stored credentials profile, waiting for Apple's verdict — a rejection stops the release before anything is published,
   - publishes a GitHub release with the archive and checksums,
   - writes the `gt` cask to [jakeobsen/homebrew-tap](https://github.com/jakeobsen/homebrew-tap),
5. verifies the signature on the binary it built, and removes the temporary keychain.

## Cutting a release

```bash
git tag v0.1.0
git push origin v0.1.0
```

Versions follow [semantic versioning](https://semver.org). `gt --version` reports the tag without the `v`.

## Repository secrets

The workflow needs six secrets in the gt repository (Settings → Secrets and variables → Actions). It stops with an error naming any that are missing.

| Secret | What it is |
|---|---|
| `APPLE_CERTIFICATE_BASE64` | The Developer ID Application certificate with its private key, exported from Keychain Access as a `.p12` and base64-encoded |
| `APPLE_CERTIFICATE_PASSWORD` | The password the `.p12` was exported with |
| `APPLE_ID` | The Apple ID that submits to the notary service |
| `APPLE_TEAM_ID` | The developer team ID (Apple Developer → Membership details) |
| `APPLE_APP_SPECIFIC_PASSWORD` | An app-specific password for that Apple ID (account.apple.com → Sign-In and Security) |
| `HOMEBREW_TAP_DEPLOY_KEY` | Private half of an SSH key added to the tap as a deploy key with write access |

### How the secrets are kept out of logs and tools

The repository is public, and so are its workflow logs.

- Only one step, *Load signing and notarization credentials*, receives the Apple secrets. It hands them straight to Apple's tools (`security`, `notarytool store-credentials`), which store them in a temporary keychain. The decoded certificate file is deleted as soon as it is imported, and the keychain is deleted when the job ends.
- GoReleaser and the signing script never see the Apple values: the script signs with the identity in that keychain (never falling back to other keychains) and notarizes with the stored `notarytool` profile.
- `HOMEBREW_TAP_DEPLOY_KEY` goes only to the GoReleaser step, which needs it to push the cask.
- Nothing prints a secret, and shell tracing is off. GitHub also masks secret values in logs, and the step masks the keychain's random password.
- Pull requests from forks never receive secrets, and the release workflow only runs for pushed tags.

## Trying a release locally

A snapshot release builds everything except signing and publishing, into `dist/`. Without a signing keychain the script leaves the binary unsigned:

```bash
HOMEBREW_TAP_DEPLOY_KEY=unused goreleaser release --snapshot --clean
```

## Checking a published release

```bash
brew install jakeobsen/tap/gt
codesign --verify --strict --verbose=2 "$(which gt)"
codesign --display --verbose=2 "$(which gt)"   # Authority=Developer ID Application, flags=…runtime
```
