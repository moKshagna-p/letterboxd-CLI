# Distribution

## Install commands

macOS:

```bash
brew install moKshagna-p/tap/letterboxd-tui
```

Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/moKshagna-p/letterboxd-TUI-Heatmap/main/scripts/install.sh | sudo sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/moKshagna-p/letterboxd-TUI-Heatmap/main/scripts/install.ps1 | iex
```

`brew install letterboxd-tui` with no tap prefix is only possible after acceptance into `homebrew/core`.

## What is already prepared in this repo

- `cmd/letterboxd-tui` is the canonical shipped command.
- `.goreleaser.yaml` builds release archives for macOS, Linux, and Windows.
- `.github/workflows/release.yml` publishes GitHub release artifacts whenever you push a `v*` tag.
- `scripts/install.sh` installs the latest Linux release binary into `/usr/local/bin`.
- `scripts/install.ps1` installs the latest Windows release binary into the user's local programs directory and updates `PATH`.

## Standalone binary

The app now uses an embedded Go SQLite driver, so release binaries do not require `sqlite3` to be installed separately.

## To get to Homebrew

1. Add a `LICENSE` file. `homebrew/core` expects an open-source license.
2. Cut a stable tag such as `v0.1.0`.
3. Publish release artifacts from this repo.
4. Either:
   - create `moKshagna-p/homebrew-tap` first for immediate installs and add a `TAP_GITHUB_TOKEN` secret with access to that repo, or
   - submit a formula to `homebrew/core` if the project meets their acceptance rules.

## Recommended path

For the smoothest multi-platform install story:

1. Ship the tap-based Homebrew install first.
2. Keep the Linux shell installer as the simple non-brew fallback.
3. Add Winget later if you want a more native Windows install command than the PowerShell one-liner.
