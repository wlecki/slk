# Plan: `[appearance] transparent_background`

## Context

The user runs slk in WezTerm with 0.75 opacity and blur, using the Noctalia palette. Terminals only make a cell see-through when its background is the *default* one, and slk paints an explicit theme colour on every cell. So slk always looks opaque. We'll add a global option, default off, that makes the base surfaces (the panes, sidebar, rail and status bar) use the default background. Accents, the composer and modals stay solid. With the option off, output must be byte-identical. The change should be clean enough to upstream later.

Spec (already written): `docs/superpowers/specs/2026-11-08-transparent-background-design.md`. After approval, a copy of this plan goes to `docs/superpowers/plans/2026-11-08-transparent-background.md`.

## Approach: one switch in `styles`, paint colour vs real colour

`styles.Background`, `SidebarBackground`, `RailBackground` and a new `StatusBarBackground` become *paint* colours: `lipgloss.NoColor{}` when transparent, or the theme colour as today. The real colours stay available through accessors, for maths and solid surfaces. I checked that lipgloss emits no bg SGR for `NoColor`, and that its padding is plain spaces. So the ~100 `.Background(styles.Background)` sites need no edit.

## Steps

1. **Config** (`internal/config/config.go`): add `TransparentBackground bool \`toml:"transparent_background"\`` to `Appearance`. It defaults to false. Add a test that reads it back. Document it in `wiki/Configuration.md` next to `theme`, with a note that the terminal's opacity setting does the real work.

2. **styles** (`internal/ui/styles/styles.go`, `tint.go`):
   - Package state `transparent`, with `SetTransparentBackground(on)` and `TransparentBackground() bool`.
   - `Apply` does all its maths on the real colours, as today. At the end it stores `themeBackground` and `themeSidebarBackground`. When on, it sets `Background`, `SidebarBackground` and `RailBackground` to `NoColor{}`. It always sets `StatusBarBackground` (`SurfaceDark`, or `NoColor` when on). Then it calls `buildStyles` (`StatusBar` and `StatusbarSyncing` use `StatusBarBackground`).
   - `ThemeBackground()` and `ThemeSidebarBackground()`: the stored colour when on, otherwise the paint var itself. This keeps the off path identical even in tests that assign `styles.Background` directly.
   - `SelectionTintColor` and `MutedMentionBadgeStyle` mix with the `Theme*()` accessors.
   - `InvisibleLeftBorder() lipgloss.Border`: `{Left: "▌"}` when off, `{Left: " "}` when on.

3. **ANSI** (`internal/ui/messages/render.go`):
   - `bgANSIFor` / `fgANSIFor`: a `lipgloss.NoColor` case returning `ESC[49m` / `ESC[39m`. Today it falls through to black.
   - `isBasicBgParam` accepts `"49"`.
   - `RepaintBgToSelectionTint`: when transparent, swap `49` for the tint, re-assert the tint after resets (`bubblesansi.ReapplyAfterResets`) and prefix it.
   - New `SolidBgANSI()` (`ThemeBackground()`), cached like `BgANSI`.

4. **Sites that need the real colour** (the full list from the grep audit):
   - Modals: `messages/chrome_styles.go` (`bg` → `ThemeBackground()`, `BaseANSI` → `SolidBgANSI()+FgANSI()`), `ui/confirm.go`, `channelfinder/model.go:634`, `userprofile/view.go` (5x), `newmessagepicker/view.go` (fill and pill fg), `reactionsview/model.go:180`, `searchresults/model.go:359,406`.
   - Status bar: `statusbar/model.go` `SurfaceDark` → `StatusBarBackground` (~12), and `view_status.go:31`.
   - Invisible borders through `InvisibleLeftBorder()`: `messages/wrap_rows.go:33`, `thread/model.go:1168`, `activityview/model.go:69`, `threadsview/model.go:61`.
   - Before editing, I'll re-check each remaining `styles.Background` and `BgANSI()` site from the audit list against "fill vs colour". So far every other site is a fill.

5. **Wiring**: `cmd/slk/main.go` calls `styles.SetTransparentBackground(cfg.Appearance.TransparentBackground)` before `styles.Apply` at line 247. `cmd/slk/demo.go` does the same if it has a cfg. The switcher (`ui/theme.go`) and the SIGUSR1 reload both go through `Apply`, so they keep the flag for free.

6. **Unchanged on purpose**: `overlay.DimmedOverlay` (a transparent backdrop stays see-through and only its text dims, so the window doesn't go opaque each time a modal opens). Also `SurfaceDark` for the composer, popups, image frames, halfblock padding and bootstrap. And `EraseSGR` (it becomes `ESC[49m`, which is right for ECH next to transparent cells).

7. **AGENTS.md**: add rows for `SetTransparentBackground` / `ThemeBackground()` / `ThemeSidebarBackground()` / `StatusBarBackground`, `SolidBgANSI()` and `InvisibleLeftBorder()`, with the rule "fill with the paint colour, do maths and modals with `Theme*()`".

## Tests (plain `testing.T`, white-box)

- `styles`: the derived colours (selection tints, `ComposeInsertBG`, muted badge, selection/search fg) are equal on and off. Paint vars are `NoColor` when on. The flag survives a second `Apply` and `RegisterCustomThemes`+`Apply`. Plus `InvisibleLeftBorder`.
- `messages`: `bgANSIFor(NoColor)`. The `49` tokenizer case. In the transparent selected variant, every reset is followed by the tint. `ChromeStyles` stays solid when on.
- `ui` goldens: `pinRenderGlobals` resets the flag. Add new `transparent_*` goldens (main view, thread open, a dialog) via `newGoldenApp`, and bless them with `-update`. Existing goldens must stay byte-identical (run them before blessing anything).
- `ui` full-frame check in transparent mode, with a theme whose Background, Sidebar and Rail colours are unique: none of them appears as a bg SGR in the frame, and `SurfaceDark` doesn't appear on the status bar row.
- `config`: default false, explicit true.

## Verification

- `go build ./...`, `go vet ./...`, `gofmt -l .` (empty), `golangci-lint run`, `go test ./... -race` (in the background).
- A Fable reviewer subagent (`model: fable`) reviews the diff. I fix its findings and keep notes in the shared notes folder.
- `go build -o bin/slk ./cmd/slk`. If useful, a headless or `agent-test` WezTerm check with `--demo`, closed afterwards.
- Then I hand the user the binary path and test steps: set `transparent_background = true`, theme Noctalia, and check the panes/sidebar/rail/status bar, the selection, badges, composer, modals, the theme switcher, `kill -USR1`, and images.
- Commits are title-only conventional (e.g. `feat(theme): add transparent_background option`), on this branch only: no push, no PR.
