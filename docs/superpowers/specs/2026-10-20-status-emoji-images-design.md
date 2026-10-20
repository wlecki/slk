# Peer status emoji as images outside the sidebar

Status: approved scope, 2026-10-20.

## Goal

The sidebar draws a DM peer's status emoji as an image in image mode
(`2026-10-20-sidebar-emoji-images-design.md`), custom emoji included.
The other surfaces that show the same status still draw a glyph, and a
custom status emoji there is the generic `💬` fallback. Draw it as an
image on those surfaces too, the same way the sidebar does.

## Scope

| Surface | Today | After |
|---|---|---|
| Message author header, both panes (`messages.AuthorStatusSuffix`, from `RenderMessage`) | glyph after the name | image (huddle `🎧`, status emoji including custom, text-only `💬`); glyph as fallback |
| Channel finder DM rows (`channelfinder.renderBox`) | glyph after the name | image; glyph as fallback |
| DM header topic (`peerstatus.Status.Summary`, drawn by `messages.Model`'s chrome) | `🎧 In a huddle · 🗓 text · 🔕 Do not disturb until 15:04` | the huddle and status emoji as images; glyph as fallback |
| DND glyph (`🔕`): finder prefix, DM header DND part | glyph | unchanged. It is slk's own marker, not an emoji the user chose, and `GlyphShortcode` does not name it |
| User profile dialog status line | glyph | unchanged (not asked for; follow-up if wanted) |

With image mode off, or with no fetcher, every surface's output is
byte-identical to today: each image path is tried first and, when it
does not place, the existing expression runs unchanged. The goldens
prove it.

## Shared rules (as in the sidebar)

- Place with `emoji.PlaceShortcode(ctx.PlaceCtx, st.GlyphShortcode(now),
  ctx.Customs, cells)`, where `cells` is the surface's emoji context
  cells (1 or 2, default 2).
- Measure with the image's cell count: a name budget is charged
  `1 + cells` for a placed image, `1 + GlyphWidth` for a glyph.
- Append the placement after any truncation, so truncation never sees
  it and cannot cut it.
- A placement ends with `\x1b[39m` (default foreground). Where text in
  the same style follows it, re-assert that style's foreground right
  after the placement. Where nothing follows, or what follows sets its
  own foreground, add nothing.
- Fire the uploads (flushes) during `View`, against
  `imgpkg.KittyOutput`. Each flush is idempotent.
- Each surface's cached output is dropped by the App's debounced
  `emojiInvalidateMsg` (`HandleEmojiImageReady`) and by
  `App.SetCustomEmoji` (`SetEmojiCustoms`), so a cold reservation
  becomes the image (or the glyph after a failed fetch), and a custom
  status emoji that arrives late gets its artwork.

## New helper

`bubbles/ansi.ForegroundSGR(c color.Color) string`: the SGR that sets
the foreground to `c`, for re-asserting a style's foreground after a
kitty placement. It replaces the inline
`ansi.Style{}.ForegroundColor(style.GetForeground()).String()` in
`messages.ReactionPillText` and the sidebar's section header, and the
DM header uses it. The sidebar's selected header label restores with
its background escape instead (a different pattern) and keeps it. This
lands as its own refactor commit before the features.

`peerstatus` gains `StatusShortcode(now)`, the status-only sibling of
`GlyphShortcode` (as `StatusGlyph` is of `Glyph`), and
`SummaryWith(now, layout, emoji)`: `Summary` with each emoji part
drawn by `emoji(shortcode, glyph)`. `Summary` is `SummaryWith` with the
glyph, so the join and format logic stays in one place.

## Message author headers

`AuthorStatusSuffix(statuses, userID, now)` keeps its glyph-only form
(callers and tests use it). `RenderMessage` calls a new
`AuthorStatus(rc, userID, now)`, which returns the suffix and the
upload: a placement inside the same background-only style the glyph
suffix uses (`" " + placement`), else exactly `AuthorStatusSuffix`.
The flush goes into `RenderedMessage.BodyFlushes`, which both panes
already fire for visible entries.

No foreground restore: after the suffix come background-only spaces,
then `styles.Timestamp`, which sets its own foreground. The selected
variant's background repaint (`RepaintBgToSelectionTint`) rewrites only
background tokens, never the placement's `38;2` image ID.

The header is not truncated (as today); `WordWrap` and lipgloss widths
keep a placement whole. `SelectedHeader` measures the image as `cells`
wide, so where a glyph was 1 cell the long timestamp can stop fitting
one column sooner.

Invalidation is already in place: both panes' `HandleEmojiImageReady`
and `SetEmojiCustoms` drop the entry cache holding `RenderMessage`
output.

Known, pre-existing, not fixed here: the thread pane renders its
parent every frame and discards the parent's flushes (v1, documented
at the call site). A status image only the parent shows uploads when
the same image is uploaded elsewhere, as the parent's body emoji do
today. Usually the channel pane shows the same message.

## Channel finder

`SetEmojiContext` / `SetEmojiCustoms` on `channelfinder.Model`,
forwarded from `App.SetEmojiContext` / `App.SetCustomEmoji`. The
finder has no row cache and `renderBox` runs every frame, so there is
nothing to invalidate; the emoji tick is an `Update`, which redraws.
`View` and `renderBox` are value receivers, so `renderBox` fires each
row's flush inline instead of collecting them.

The joined row already budgets the name with the glyph's cells and
appends the glyph after truncation. With an image, the budget is
charged `1 + cells` and the placement is appended after the line's
width backstop (which would otherwise run `truncate.StringWithTail`
over it), in the name's own style so the image cells keep the row
background. The emoji is the last thing on the name, so no foreground
restore is needed.

## DM header

The topic is drawn by the pane, at `View` time, so the placement and
its upload must happen there, not in `presence.go`. The App hands the
pane the status instead of only a string:

- `messages.Model.SetDMTopic(st, now, layout)` sets the topic text to
  `st.Summary(now, layout)` (so the text form, and everything that
  reads it, is what it is today) and keeps `st`, `now` and `layout`.
  `SetChannel` and `SetChannelTopic` clear the kept status.
- `presence.pushDMStatus` and the two `SetChannel(name,
  dmTopicFor(...))` call sites use it for DMs.
- The chrome builds the topic with `st.SummaryWith(now, layout, place)`
  when a status is kept and image mode can place; otherwise it uses the
  text as today. The chrome cache is keyed on the kept status as well
  as the text, since two custom status emoji with the same text render
  the same glyph text.
- `place` returns the placement plus `ForegroundSGR` of
  `styles.Timestamp`'s foreground, since status text follows it in that
  style. A part is `placement + " " + text`, never passed through
  `TrimSpace`, which would eat a cold reservation (`"  "`) and shift
  the line when the image warms.
- `HandleEmojiImageReady` and `SetEmojiCustoms` also drop the chrome
  cache (today they drop only the entry cache). Flushes from a chrome
  build fire in that `View`.

## Tests

- `bubbles/ansi`: `ForegroundSGR` equals the expression it replaces.
- `peerstatus`: `StatusShortcode` (status, custom, text-only, expired,
  huddle ignored); `SummaryWith` with the glyph equals `Summary`; with a
  marker emoji the parts and separators are kept.
- `messages`: author header in image mode shows the placement (custom
  via customs), fires its flush from the pane's `View`, cold path
  reserves `cells` spaces, falls back to the glyph when not placed,
  image mode off is byte-identical; DM header topic shows the
  placement with the timestamp foreground restored, cold reservation is
  kept, the chrome rebuilds on `HandleEmojiImageReady` and
  `SetEmojiCustoms` and on a status change with the same text.
- `thread`: the reply header shows the placement and fires its flush.
- `channelfinder`: row in image mode shows the placement after a
  truncated name, never cut, row width unchanged; fallback; flush fired.
- `internal/ui`: `SetEmojiContext` / `SetCustomEmoji` reach the finder;
  a DM status pushed through `pushDMStatus` reaches the header as an
  image; the emoji tick rebuilds the header.
- Goldens unchanged.

Visual check: the demo world with a local, uncommitted harness (kitty
emulated placement on WezTerm, a fake emoji fetcher, DM rows seeded
with a huddle, a standard and a custom status): author headers in both
panes, the selected header with its long timestamp, the thread parent,
the finder (selected row too) and the DM header, cold to warm, and
image mode off.
