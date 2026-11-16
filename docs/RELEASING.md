# Releasing

## Unsigned release (the default)

1. Update the version in a tag: `git tag v0.2.0 && git push origin v0.2.0`.
2. The **Release** workflow builds the universal app on `macos-latest`, packs `Headroom.dmg` (ad-hoc signed) and attaches it with a SHA-256 checksum to a GitHub release for the tag.

People opening an unsigned app the first time need to approve it once: see "Install" in the README.

To build the same thing locally: `make dmg` → `build/out/Headroom.dmg`.

## Optional: Developer ID signing and notarization

Signing removes the first-launch warning. It needs a paid Apple Developer account. **Nothing about your identity goes in this repository:** no certificates, team IDs, Apple IDs or passwords. The build reads the identity from the environment, and the certificate and credentials live in your login keychain.

### One-time setup on your Mac

1. In Xcode › Settings › Accounts, create a **Developer ID Application** certificate. It goes into your login keychain.
2. Store notarization credentials in the keychain under a profile name of your choice, using an app-specific password from appleid.apple.com:

   ```bash
   xcrun notarytool store-credentials headroom-notary
   ```

   The tool prompts for your Apple ID, team and password, and keeps them in the keychain. Don't pass them on the command line, so they stay out of your shell history.

### Each release

```bash
export SIGN_IDENTITY="Developer ID Application"   # matched against your keychain
make dmg
xcrun notarytool submit build/out/Headroom.dmg --keychain-profile headroom-notary --wait
xcrun stapler staple build/out/Headroom.dmg
spctl -a -t open --context context:primary-signature -v build/out/Headroom.dmg
```

`SIGN_IDENTITY` is only read from the environment; the Makefile defaults to ad-hoc (`-`). Upload the stapled DMG to the release in place of the unsigned one.

### Signing in CI (not set up)

If you later sign in GitHub Actions, keep the certificate and passwords in **encrypted repository secrets**, import them into a temporary keychain in the job, delete that keychain at the end, and run the job only on tags from protected branches. Never echo secrets or write them to files in the workspace.
