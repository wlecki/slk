# Transparent background

## Goal

`[appearance] transparent_background = true` (default `false`) makes slk's
base surfaces use the terminal's default background, so the terminal's own
opacity and blur show through. It works with every theme, built-in and
custom, with the theme switcher and with the `SIGUSR1` theme reload.

## Why the default background

WezTerm (`window_background_opacity`), Ghostty (`background-opacity`) and
Alacritty (`opacity`) only make a cell see-through when it has the default
background. Any explicit colour, palette 0 included, is drawn solid. So in
transparent mode slk has to emit no background (`ESC[49m`, or no bg SGR at
all) wherever it paints a base surface today.

## What goes transparent, what stays solid

Transparent (base surfaces):

- messages and thread panes: `styles.Background`, `messages.BgANSI()` and
  everything that re-asserts it after resets
- sidebar: `styles.SidebarBackground`, `messages.SidebarBgANSI()`
- workspace rail: `styles.RailBackground`
- status bar (today `SurfaceDark`): a new `styles.StatusBarBackground`
- every fill painted with those colours: padding, gutters, spacer rows,
  scrollbar tracks, date dividers, the invisible left border of message rows

Solid (unchanged):

- the selected message tint, the sidebar selection (when the theme sets
  `sidebar_selected_bg`), mention and unread badges, search highlights,
  reaction pills (`Surface`)
- the composer (`SurfaceDark`, `ComposeInsertBG`)
- modals and popups (`bubbles/chrome` box and popup, the confirm prompt,
  channel finder, user profile, new message picker, reactions view, search
  results)
- image frames and halfblock padding (`SurfaceDark` in `imgrender`,
  `blockkit/image.go`, `blockkit/styles.go`), code blocks, the bootstrap
  splash

## Design

### One switch in `styles`

`styles.SetTransparentBackground(on bool)` sets package state. `Apply`
reads it, so the theme switcher and the `SIGUSR1` reload keep it with no
signature change. `cmd/slk/main.go` (and the demo) call it from
`cfg.Appearance.TransparentBackground` before the first `Apply`, the same
way `messages.SetDateTokenTimeLayout` is seeded.

`Apply` does all its maths on the real colours (selection fg default,
search highlight fg default, `ComposeInsertBG`), as today. At the end, when
the switch is on, it keeps the real colours aside and sets the paint
colours to `lipgloss.NoColor{}`:

| Paint colour (what code fills with) | Real colour (for maths and solid surfaces) |
|---|---|
| `styles.Background` | `styles.ThemeBackground()` |
| `styles.SidebarBackground` | `styles.ThemeSidebarBackground()` |
| `styles.RailBackground` | (no maths user, none added) |
| `styles.StatusBarBackground` (new; `SurfaceDark` when off) | `styles.SurfaceDark` |

The ~100 `.Background(styles.Background)` call sites need no edit: lipgloss
emits no bg SGR for `NoColor`, and its padding spaces get the default
background, so there are no holes that look different from their
neighbours.

The `Theme*()` accessors return the stored real colour when the switch is
on, and the paint variable itself when it is off. So with the switch off
they are exactly today's value, even in tests that assign
`styles.Background` directly.

Sites that use a base colour as a colour, not a fill, move to the real one:

- `SelectionTintColor` (mix with `Background`) and `MutedMentionBadgeStyle`
  (mix with `SidebarBackground`)
- modal fills: `ChromeStyles` (`Box`, `Input` and the rows, `BaseANSI`),
  `confirmPromptStyles`, `channelfinder`, `userprofile`,
  `newmessagepicker` (fill and the pill foreground), `reactionsview`,
  `searchresults`
- `messages.SolidBgANSI()` (new): the escape of `ThemeBackground()`, for
  modal rows that re-assert their background after resets

`mixColors` on `NoColor{}` does not panic, it blends toward black, so a
missed site would show as a dark tint, not a crash. A test pins the derived
colours (selection tint, compose insert bg, muted badge) as equal with the
switch on and off.

### ANSI escapes

`bgANSIFor(NoColor{})` today falls through to `RGBA()` and returns
`ESC[48;2;0;0;0m`, solid black. It gets a `NoColor` case returning
`ESC[49m` (and `fgANSIFor` returns `ESC[39m`). So `BgANSI()` and
`SidebarBgANSI()` return `ESC[49m` in transparent mode. Re-asserting it
after resets costs a few bytes and nothing else.

### Selected row tint

`RepaintBgToSelectionTint` swaps the base bg token for the tint inside a
rendered message. In transparent mode the base spans carry no bg token at
all (lipgloss emits none for `NoColor`), so a swap is not enough:

1. the bg tokenizer also treats `49` as a bg param, so `ESC[49m` from
   `ReapplyAfterResets` is swapped for the tint, and
2. in transparent mode the tint is re-asserted after every reset
   (`bubbles/ansi.ReapplyAfterResets(s, tint)`), and put at the start.

Neither path is reachable with the switch off: `49` only matches when the
base escape is `ESC[49m`, and step 2 is gated on the switch.

### Invisible left border

Message rows (`wrap_rows.go`, the thread parent, `activityview`,
`threadsview`) carry an invisible `▌` border, drawn fg = bg. With a
`NoColor` foreground the glyph would show in the terminal text colour. A
helper, `styles.InvisibleLeftBorder()`, returns the `▌` border when off and
a `" "` border when on; all four sites use it, so the column keeps its width
and looks like its neighbours.

### Dimmed overlay

`DimmedOverlay` darkens each cell's fg and its bg when set. Transparent
cells have no bg, so under a modal the backdrop stays see-through and only
its text dims. That is enough to show the screen is inactive, and it avoids
the whole window turning opaque each time a modal opens. No change to
`overlay`.

### Images

- Kitty native: placeholder cells carry only a fg (the image ID), so they
  sit on the default background like their neighbours. Transparent pixels
  of an image show the terminal background, which is what the user wants.
- Kitty emulated (WezTerm) and sixel: the painter erases with
  `EraseSGR = messages.BgANSI()`, which becomes `ESC[49m`, so ECH resets
  cells to the default background, the same as their neighbours.
  `stampFrameGuards` hashes rows as before; the frame is still the same for
  the same input.
- Halfblock fallbacks and image frames keep their `SurfaceDark` fill.

### Config

`Appearance.TransparentBackground bool` with TOML key
`transparent_background`, default `false`. Documented in
`wiki/Configuration.md` next to `theme`, with a note that the terminal's own
opacity setting does the actual transparency.

## Testing

- Option off: every existing golden passes unchanged (byte-identical).
- Option on: a transparent golden set next to the existing ones
  (`transparent_*`), with `pinRenderGlobals` resetting the switch.
- A full-frame test in transparent mode, with a theme whose base colours
  are unique: none of `Background`, `SidebarBackground`, `RailBackground`
  appears as a bg SGR anywhere, and `SurfaceDark` does not appear on the
  status bar row.
- Unit tests: `bgANSIFor(NoColor)`, the `49` tokenizer case, the transparent
  selected variant (tint reaches every cell after a reset), the derived
  colours being equal on and off, the invisible border glyph, the switch
  surviving `Apply` (theme switch) and `RegisterCustomThemes` + `Apply`
  (reload).
- Config: default false, explicit true reads back.

## Out of scope

- Per-theme transparency
- Partial transparency of solid surfaces (terminals can't blend a cell's
  explicit bg with the window)
