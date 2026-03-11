# letterboxd-tui

Local-first film logging and GitHub-style daily heatmap.

## Install

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

## Quick start
```bash
cd path_of_the_file
make test
make install-local
export PATH="$PWD/bin:$PATH"
```

After that, you can run:
```bash
letterboxd-tui
```

`letterboxd-tui` opens the interactive TUI by default.
On first launch, it asks for your Letterboxd username and password, validates them, saves them locally, syncs your data, and then opens the TUI.
On later launches, it skips the credential prompt and opens the TUI directly.

## Behavior you asked for
- Auto-sync uses authenticated Letterboxd scraping (no repeated manual export download flow).
- `auth login` asks for your Letterboxd username/password one time and validates them.
- `refresh` or `import letterboxd --auto` scrapes diary data, compares to last fetch hash, and imports only if changed.
- `lbstats` scrapes your Letterboxd account on demand and lets you choose which feature to view (`watched`, `reviews`, `watchlist`, `lists`, `tags`, `heatmap`, or all).
- On first `lbstats` run, the app asks for username/password once, validates credentials, and stores them locally.
- On later `lbstats` runs, it scrapes again and only refreshes cached feature data if content changed.
- TUI data commands trigger sync checks automatically when auto-sync is enabled.
- Watchlist sync uses public watchlist page scrape fallback.
- If auto-sync is not configured, the app falls back to manual import (ZIP or diary CSV).
- After import, `letterboxd-tui` opens the interactive CLI studio.

## Commands
```bash
letterboxd-tui add --title "Inception" --date 2026-02-20 --rating 4.5
letterboxd-tui list --from 2026-01-01 --to 2026-12-31
letterboxd-tui heatmap --year 2026
letterboxd-tui stats --year 2026
letterboxd-tui ui
letterboxd-tui help backend
letterboxd-tui help features
letterboxd-tui auth login
letterboxd-tui auth status
letterboxd-tui auth logout
letterboxd-tui refresh
letterboxd-tui lbstats
letterboxd-tui lbstats --view watched
letterboxd-tui lbstats --view heatmap
letterboxd-tui import letterboxd --auto
letterboxd-tui import letterboxd --file /path/to/letterboxd-export.zip
letterboxd-tui import csv --file /path/to/logs.csv
letterboxd-tui export csv --file /path/to/out.csv --year 2026
letterboxd-tui dedupe
```

## Account commands
```bash
# Logout current app session
letterboxd-tui auth logout

# Login once with credentials
letterboxd-tui auth login

# Refresh latest data now
letterboxd-tui refresh
```

## Interactive UI commands
Inside `letterboxd-tui` or `letterboxd-tui ui`:
```bash
watched
ratings
reviews
lbstats
watchlist
watchlist add "Chungking Express" --notes "Weekend"
watchlist rm <item-id>
lists
lists create "Neo-Noir Essentials"
lists add <list-id> "Heat" --notes "Rewatch soon"
lists view <list-id>
heatmap
stats
```

Default DB path: `letterboxd-tui.db` in current directory.
Override with:
```bash
export LETTERBOXD_TUI_DB=/absolute/path/to.db
```

## Auto-sync config
```bash
export LETTERBOXD_TUI_AUTO_SYNC=true
export LETTERBOXD_TUI_CONFIG=/absolute/path/to/config.json
```

Legacy `FILM_HEATMAP_*` environment variables still work for compatibility.

## Distribution

See [docs/distribution.md](docs/distribution.md) for the Homebrew and cross-platform packaging plan.
See [docs/release-checklist.md](docs/release-checklist.md) for the exact steps to publish a release.
