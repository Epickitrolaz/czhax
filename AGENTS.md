# AGENTS.md — CZHax Development Guide

This guide tells coding agents how to build, style, and extend **CZHax**, a cybersecurity and utility TUI built in Go (`bubbletea` + `lipgloss`) for the **M5Stack Cardputer Zero** (Raspberry Pi CM0 Linux).

---

## 1. Target Hardware & Display Constraints

Never design for a desktop monitor first. Every screen must remain clean and legible on the physical handheld:

- **SoC / OS**: Raspberry Pi Compute Module 0 (ARM64 Linux, 512 MB RAM). Keep dependencies minimal and compile static binaries with `CGO_ENABLED=0`.
- **Display**: 1.9" ST7789V3 IPS, `320×170` pixels.
  - Depending on console font, terminal dimensions are **`40×10`**, **`45×10`**, or **`53×14`**.
  - **Every view MUST fit strictly inside `40×10`, `45×10`, and `53×14`** with zero line wrapping and exact row counts (`len(lines) == h`).
- **Keyboard**: 46-key TCA8418 matrix keyboard.
  - `Fn + F/X/Z/C` send `Up/Down/Left/Right` arrows.
  - `Fn + 4..8` send `F4..F8`.
  - Avoid requiring awkward modifier combos (`Ctrl+Alt+...`, `PgUp/PgDn`, `Home/End`) as the only way to do an action.

---

## 2. Project Structure

```text
cmd/czhax/main.go          # CLI entrypoint, flags (--cz, --cz45, --version, --update) & `czhax update`
internal/
  boot/boot.go             # Start-on-boot toggle (systemd tty1 service or ~/.bash_profile hook)
  config/config.go         # Persistent JSON config (~/.config/czhax/config.json)
  console/console.go       # Kernel printk console log suppression during TUI / sleep
  power/power.go           # Power management (Sleep mode + evdev/GPIO/key wake, Reboot, Shutdown)
  sysinfo/sysinfo.go       # Linux /proc & /sys telemetry + battery time-to-empty estimation
  theme/theme.go           # High Contrast palette (4-role color hierarchy)
  updater/updater.go       # Self-updater (GitHub Releases API + SHA256 verification + atomic replace)
  ui/
    layout.go              # TopViews registry, viewport/sidebar sizing, FitLine, SplitLinesPadded, MiniBar
    shell.go               # Top horizontal tab bar, left vertical sidebar, pane divider
    footer.go              # Context-sensitive bottom keyhint bar
    status.go              # Status -> Device view
    system.go              # System -> Tools view (Update, Terminal Shell, Start on Boot)
    help.go                # Help overlay
    app.go                 # Root Bubble Tea model (FocusTabs <-> FocusContent)
    app_test.go            # Mandatory bounds & navigation tests across 40x10, 45x10, 53x14, 100x30
install.sh                 # Universal one-line curl installer/updater
.github/workflows/build.yml # CI tests + cross-builds + tag-triggered GitHub Releases
```

---

## 3. Layout & Navigation Contract

CZHax follows a **WiFi-Pineapple-inspired two-axis layout**:

1. **Left Vertical Sidebar**: Lists top-level views defined in `TopViews` (`internal/ui/layout.go`).
   - Uses `Name` when `sideW >= 8` (e.g., `Status`), and 3-character `Short3` when `sideW < 8` (e.g., `Sts`).
   - `Name` labels are capped at **6 visible characters** via `SidebarLabel` (`SidebarLabelMax`); longer names are truncated with `…`.
2. **Top Horizontal Bar**: Lists horizontal sub-tabs for the active top-level view (`TopViews[activeView].Tabs`), with compact status (`Bat%`, `W`/`N`) right-aligned.
3. **Right Content Pane**: Renders the active `(ViewID, TabIndex)` inside `(mainW, bodyH)`.

### Focus & Arrow Rules (Strict)

Maintain two explicit focus states in `AppModel`:

- **`FocusTabs` (default at startup)**:
  - `↑` / `↓` (and `k` / `j` when `NavArrowsVim`): cycle **vertical** views in the left sidebar.
  - `←` / `→` (and `h` / `l` when `NavArrowsVim`): cycle **horizontal** tabs in the top bar.
  - `Enter`: enters `FocusContent` for the active view + tab.
- **`FocusContent` (inside a tab)**:
  - All arrow keys (`↑`, `↓`, `←`, `→`) belong to **navigating/interacting with that tab's content**. Do **not** let `←` exit back to `FocusTabs`—use `Esc` for exiting!
  - `Esc`: closes any inner modal/input/preview inside the tab first; otherwise returns focus to `FocusTabs`.

---

## 4. Visual Styling & Palette Rules

- **Palette**: Always use `theme.HighContrast` from `internal/theme/theme.go`. Do not hardcode ANSI colors or introduce rainbow/per-button coloring.
- **4-Role Color Hierarchy**:
  1. `pal.Accent` / `pal.SelectBg` + `pal.SelectFg` (`#FFFFFF` / `#000000`): Active focus, active tab, active sidebar item, brand pill, and selected row.
  2. `pal.Secondary` (`#E5E7EB`): Field labels (`Host`, `IP`, `RAM`, etc.) and footer keycaps.
  3. `pal.Text` (`#FFFFFF`), `pal.Muted` (`#9CA3AF`), `pal.Border` (`#6B7280`): Primary values, secondary descriptions, and calm dividers (`│`, `─`).
  4. `pal.Success` (`#86EFAC`), `pal.Warning` (`#FEF08A`), `pal.Danger` (`#FCA5A5`): Reserved strictly for semantic status (`UP`/`RDY`, caution/pending, `DN`/alert, battery & thermal thresholds).
- **Formatting Discipline**:
  - Put **one primary fact/item per line** in content panes—avoid cramming multiple columns + tags onto 34-char panes.
  - Always pass output lines through `SplitLinesPadded(lines, w, h)` (which wraps `FitLine`) so lines never overflow `w` visible cells or alter `h`.
  - Keep `showDivider := h >= 12` so `10`-row screens (`40×10`, `45×10`) preserve vertical content space.

---

## 5. How to Add a New Feature or Tab

1. **Create a feature backend package** under `internal/<feature>/` (e.g., `internal/wifi/`, `internal/netscan/`, `internal/crypto/`):
   - Keep platform/Linux calls isolated behind clean Go functions and interfaces so unit tests run anywhere without root or hardware.
   - **Never block `Update()` or `View()`**: run scans, subprocesses, or network probes asynchronously in a `tea.Cmd` returning a typed result message.
2. **Create a UI view** under `internal/ui/<feature>.go`:
   - Implement `RenderLines(w, h int, pal theme.Palette, ..., focused bool) []string` returning `SplitLinesPadded(..., w, h)`.
   - When `focused == true`, render an inverse selection bar (`pal.SelectFg` on `pal.SelectBg`) on the active row; when `focused == false`, render passively without an active inverse block so focus is obvious.
3. **Register the view or horizontal tab** in `internal/ui/layout.go`:
   - Add a `ViewID` constant and entry in `TopViews` (give every view a 3-char `Short3` name and short `Tabs` labels that fit inside `34` cols!).
   - Or add a new horizontal tab to an existing `ViewDef.Tabs` slice.
4. **Wire routing in `internal/ui/app.go`**:
   - Handle `Update()` async messages from your `tea.Cmd`.
   - Handle `FocusContent` keys in `handleContentKey` (keeping `Esc` returning to `FocusTabs`).
   - Dispatch `RenderLines` in `View()` and return context hints in `currentHints()`.
5. **Write tests & verify**:
   - Add bounds tests at `40×10`, `45×10`, `53×14`, and `100×30` in `internal/ui/app_test.go` using `assertFitsBounds`.
   - Add navigation & backend unit tests.
   - Run before committing:
     ```sh
     go vet ./...
     go test -count=1 ./...
     make build-all
     ```
