# film-heatmap

Local-first film logging and GitHub-style daily heatmap.

## Quick start
```bash
cd /Users/mokshagna/Desktop/Projects/film-heatmap
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
- First run (`heatmap`): if no logs exist, it prompts you to import Letterboxd export (ZIP or diary CSV).
- After first import and terminal restart: run `film-heatmap` and it automatically shows your heatmap.

## Commands
```bash
film-heatmap add --title "Inception" --date 2026-02-20 --rating 4.5
film-heatmap list --from 2026-01-01 --to 2026-12-31
film-heatmap heatmap --year 2026
film-heatmap stats --year 2026
film-heatmap ui
film-heatmap import letterboxd --file /path/to/letterboxd-export.zip
film-heatmap import csv --file /path/to/logs.csv
film-heatmap export csv --file /path/to/out.csv --year 2026
```

## Interactive UI commands
Inside `film-heatmap` or `film-heatmap ui`:
```bash
watched
ratings
reviews
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
