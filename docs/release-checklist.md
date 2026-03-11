# Release Checklist

## One-time setup

1. Create the tap repo:

   `https://github.com/moKshagna-p/homebrew-tap`

2. Add a GitHub token with write access to that tap repo as a secret in this repo:

   `TAP_GITHUB_TOKEN`

3. Confirm GitHub Actions is enabled for this repo.

## Per release

1. Commit and push the current branch:

   ```bash
   git add .
   git commit -m "Prepare release"
   git push origin v2
   ```

2. Create and push a version tag:

   ```bash
   git tag v0.1.0
   git push origin v0.1.0
   ```

3. Wait for the `release` workflow to finish:

   `.github/workflows/release.yml`

4. Verify the GitHub release contains these archives:

   - `letterboxd-tui_darwin_amd64.tar.gz`
   - `letterboxd-tui_darwin_arm64.tar.gz`
   - `letterboxd-tui_linux_amd64.tar.gz`
   - `letterboxd-tui_linux_arm64.tar.gz`
   - `letterboxd-tui_windows_amd64.zip`
   - `letterboxd-tui_windows_arm64.zip`

5. Verify the tap repo received the formula update for `letterboxd-tui`.

## User install commands

macOS:

```bash
brew install moKshagna-p/tap/letterboxd-tui
```

Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/moKshagna-p/letterboxd-TUI-Heatmap/v2/scripts/install.sh | sudo sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/moKshagna-p/letterboxd-TUI-Heatmap/v2/scripts/install.ps1 | iex
```

## Later

If you want `brew install letterboxd-tui` without the tap prefix, submit the project to `homebrew/core` after you have stable releases and adoption.
