# Wes Anderson-Style UI Enhancement Plan

## Overview
Transform the TUI's visual output to have the symmetrical, centered, pastel aesthetic of Wes Anderson films. All changes are in `internal/app/app.go`.

## Changes

### 1. Color Palette (lines 1067-1077)
Replace current ANSI colors with Wes Anderson pastels:
- `uiAccent`: `(232,180,120)` warm gold
- `uiWarm`: `(204,102,102)` dusty red
- `uiRose`: `(199,134,157)` muted mauve-pink
- `uiCool`: `(130,180,205)` faded powder blue
- `uiPanel`: `(90,100,115)` slate-blue (more visible borders)
- `uiDim`: `(146,155,165)` soft gray
- Add `uiMint`: `(130,190,160)` sage green
- Add `uiFaint`: ANSI dim attribute

### 2. New Helper Functions
- `letterSpace(s string) string` - "HELLO" -> "H E L L O"
- `centerText(s string, width int) string` - pad both sides
- `dashedSep(width int) string` - "- - - - -" style
- `dotLeader(label, value string, width int) string` - "Label .... Value"

### 3. Banners - Double-line ornamental boxes
- Use `╔═╗║╚╝` characters
- Letterspaced titles
- Centered subtitles with `──` decorative dashes
- Breathing room (empty lines inside)

### 4. Cards - Rounded corners, fully bordered
- Use `╭─╮│╰╯` characters
- Both-side borders (closing `│` on right)
- Letterspaced centered title
- Centered subtitle with `──` dashes
- Dashed separator `─ ─ ─` instead of `···`
- Content padded and right-bordered

### 5. Stats Card - Dot leaders
- Metric names with dot leaders to values
- Centered within card
- Breathing room

### 6. Heatmap - Centered with title and legend
- Letterspaced title above
- Subtitle with decorative dashes
- Entire grid centered
- Legend bar below: "Less ■ ■ ■ ■ ■ More   Total: N films"

### 7. Heatmap Colors - Wes Anderson pastels
- 0: `(60,68,80)` muted slate
- 1: `(160,140,100)` aged parchment
- 2: `(190,140,110)` warm camel
- 3: `(204,120,100)` dusty terracotta
- 4+: `(220,160,130)` warm peach

### 8. Menu - Cleaner spacing
- Breathing room, centered labels, aligned columns
