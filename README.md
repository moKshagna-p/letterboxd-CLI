# 🎥 Letterboxd TUI & CLI

A local-first film logging tool with a beautiful terminal interface and GitHub-style daily heatmap. Sync your entire Letterboxd profile—diary, watchlist, and custom lists—directly to your terminal.

![Heatmap Preview](https://github.com/moKshagna-p/letterboxd-TUI-Heatmap/raw/v2/docs/assets/heatmap-preview.png) *(Placeholder for your preview image)*

## ✨ What's New in v0.2.0

- **🚀 Resilient Scraper:** Completely rebuilt to handle Letterboxd's latest HTML changes.
- **📁 Detailed List Sync:** Now scrapes and stores individual films within your custom collections.
- **📜 TUI Scrolling:** Navigate through thousands of films effortlessly with full scrolling support and visual indicators.
- **✍️ Enhanced Reviews:** View your film notes and commentary directly in the TUI (Shortcut: `2`).
- **🔄 Functional Refresh:** Trigger a full Letterboxd sync anytime by pressing `r` in the UI.

---

## 🚀 How to Setup & Login

### 1. Installation

**macOS (Recommended):**
```bash
brew install moKshagna-p/tap/letterboxd-tui
```

**Linux:**
```bash
curl -fsSL https://raw.githubusercontent.com/moKshagna-p/letterboxd-TUI-Heatmap/v2/scripts/install.sh | sudo sh
```

**Windows (PowerShell):**
```powershell
irm https://raw.githubusercontent.com/moKshagna-p/letterboxd-TUI-Heatmap/v2/scripts/install.ps1 | iex
```

### 2. Authentication (One-time Setup)

To access your private data (watchlist, lists, reviews), you need to login:

1.  Open your terminal.
2.  Run the login command:
    ```bash
    letterboxd-tui auth login
    ```
3.  Enter your **Letterboxd Username** and **Password** when prompted.
4.  The app will validate your credentials and save them securely in your local configuration.

### 3. Sync Your Data

Fetch your entire history from Letterboxd:
```bash
letterboxd-tui refresh
```
*Note: The first sync might take a minute if you have a very large diary.*

### 4. Launch the UI

Start exploring your film library:
```bash
letterboxd-tui ui
```

---

## 🎮 TUI Shortcuts

Once inside the UI, use these keys to navigate:

- **`1`**: **Watched** - Your recently logged films (look for `✎` for reviews).
- **`2`**: **Reviews** - Read your notes and commentary. Press `Enter` on a film to read the full note.
- **`3`**: **Heatmap** - Your visual activity map.
- **`4`**: **Stats** - Your annual overview and streaks.
- **`5`**: **Watchlist** - Your planned watches.
- **`6`**: **Collections** - Your Letterboxd lists. Highlight one and press `Enter` to see its films.
- **`r`**: **Refresh** - Trigger a live sync from Letterboxd.
- **`↑ / ↓`** or **`j / k`**: Scroll through lists.
- **`q`** or **`Esc`**: Go back or quit.
- **`?`**: Show help message.

---

## 🛠 Advanced CLI Commands

You can also use `letterboxd-tui` as a powerful CLI tool:

```bash
# Add a film manually
letterboxd-tui add --title "Inception" --date 2026-02-20 --rating 4.5

# Search your local logs
letterboxd-tui list --from 2026-01-01 --title "Alien"

# Check auth status
letterboxd-tui auth status

# Import from an official Letterboxd .zip export
letterboxd-tui import letterboxd --file ~/Downloads/letterboxd-user-export.zip

# Remove exact duplicate logs
letterboxd-tui dedupe
```

---

## ⚙️ Configuration

- **Database:** Defaults to `letterboxd-tui.db` in your current directory.
- **Environment Variables:**
  - `LETTERBOXD_TUI_DB`: Set an absolute path to your database file.
  - `LETTERBOXD_TUI_AUTO_SYNC`: Set to `true` to sync automatically on launch.

---

## 📄 License
MIT License. See [LICENSE](LICENSE) for details.
