# film-heatmap

Local-first film logging and GitHub-style daily heatmap.

## Quick start
```bash
cd path_of_the_file
make test
make install-local
export PATH="$PWD/bin:$PATH"
```

After that, you can run:
```bash
heatmap
film-heatmap
```

`film-heatmap` now opens an interactive CLI studio where you can type section commands.

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
- After import, `film-heatmap` opens the interactive CLI studio.

## Commands
```bash
film-heatmap add --title "Inception" --date 2026-02-20 --rating 4.5
film-heatmap list --from 2026-01-01 --to 2026-12-31
film-heatmap heatmap --year 2026
film-heatmap stats --year 2026
film-heatmap ui
film-heatmap help backend
film-heatmap help features
film-heatmap auth login
film-heatmap auth status
film-heatmap auth logout
film-heatmap refresh
film-heatmap lbstats
film-heatmap lbstats --view watched
film-heatmap lbstats --view heatmap
film-heatmap import letterboxd --auto
film-heatmap import letterboxd --file /path/to/letterboxd-export.zip
film-heatmap import csv --file /path/to/logs.csv
film-heatmap export csv --file /path/to/out.csv --year 2026
film-heatmap dedupe
```

## Account commands
```bash
# Logout current app session
film-heatmap auth logout

# Login once with credentials
film-heatmap auth login

# Refresh latest data now
film-heatmap refresh
```

## Interactive UI commands
Inside `film-heatmap` or `film-heatmap ui`:
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

Default DB path: `film-heatmap.db` in current directory.
Override with:
```bash
export FILM_HEATMAP_DB=/absolute/path/to.db
```

## Auto-sync config
```bash
export FILM_HEATMAP_AUTO_SYNC=true
export FILM_HEATMAP_CONFIG=/absolute/path/to/config.json
```
