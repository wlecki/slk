# Emoji images in the sidebar

Status: approved scope, 2026-10-20.

## Goal

Render the sidebar's emoji as images, the way message bodies and
reactions do since the emoji-as-images work
(`2026-05-27-emoji-as-images-design.md`, which deferred "Channel list
emoji prefixes" and "User status / display-name emoji"). Workspace
custom emoji must show their real artwork.

## Scope

| Surface | Today | After |
|---|---|---|
| Section header emoji (`SectionMeta.Emoji`, e.g. ":orange_book: Docs") | glyph from the Slack codemap; custom emoji and skin tones as `:code:` text | image (standard, custom, aliases, skin tones); glyph, then `:code:`, as fallback |
| DM peer status emoji after the name (`item.Status.Glyph`) | glyph; a custom status emoji is the `💬` fallback | image of the status emoji (custom included); huddle `🎧` and text-only `💬` as images too; glyph as fallback |
| Channel names | n/a | unchanged: Slack refuses special characters in channel names (`invalid_name_specials` on `conversations.create` / `conversations.rename`), so a channel name cannot carry an emoji |
| Raw emoji in DM / group DM display names | glyph | unchanged (follow-up). Display names sit inside the truncated name budget, and `truncate.StringWithTail` would cut a placement in half; that needs token-aware truncation |
| Status emoji elsewhere (message author headers, channel finder, DM header) | glyph | unchanged (follow-up) |

Image mode is the existing process-global switch (`emoji.ImageModeActive()`:
kitty protocol and `emoji_images = "on"`). With image mode off, or with
no fetcher installed, the sidebar's output is byte-identical to today.

## Design

### Context

The sidebar takes the shared `messages.EmojiContext` (it already
imports `messages`): `SetEmojiContext(ctx)` and `SetEmojiCustoms(customs)`,
both invalidating the row cache. `App.SetEmojiContext` and
`App.SetCustomEmoji` forward to it, next to the other surfaces.

### Placing one shortcode

`emoji.PlaceShortcode(ctx, name, customs, cells)` is
`URLForShortcode` followed by `Place`: it returns the placement string,
the upload flush and whether it placed. It is new, small, and the sidebar
uses it in two places; other surfaces inline the same two calls today
and can move to it later (not in this change: refactor and feature
stay separate).

### Section headers

`renderSectionHeaderLabel` tries `PlaceShortcode` first. Warm path: the
kitty placeholder string. Cold path: `cells` spaces, then
`EmojiImageReadyMsg` re-renders. A negative-cached URL (failed fetch)
returns not-placed and the header falls back to the glyph, then to
`:code:`, exactly as today.

The placeholder ends with `\x1b[39m` (default foreground), which
`ReapplyBgAfterResets` does not catch, so the header name after the image
would be drawn in the terminal's default colour. After a placement each
label variant re-asserts the colours it had before the image, as
`messages.ReactionPillText` does for reaction pills: the normal label
`styles.SectionHeader`'s foreground; the selected label the sidebar
background and foreground that `ReapplyBgAfterResets` put back after
the cursor glyph's reset, so its name looks as it does in glyph mode. Nothing is inserted
between the placeholder's own `38;2;R;G;B` SGR and its runes: that
colour is the image ID.

The prefix is `placement + " "`. The glyph path keeps its existing
`Sprint(token) + " "` (two spaces, since `Sprint` appends one), so text
mode does not change.

Width: the header is not truncated (see Known issues). An image is
always `cells` wide (2 by default), where the glyph was 1 or 2.

### DM status emoji

`peerstatus.Status.GlyphShortcode(now)` names the emoji `Glyph(now)`
stands for: `headphones` in a huddle, the status shortcode (colons
trimmed) when there is one, `speech_balloon` for a text-only status,
"" when there is no glyph. The row tries `PlaceShortcode` with it; when
that does not place (custom emoji not loaded yet, failed fetch, image
mode off) the row uses `Glyph(now)` as today.

Measure, then place: the name budget is charged `1 + cells` for a
placed image (`1 + GlyphWidth` for a glyph, as today), the name is
truncated as plain text, and the placement is appended after. A
placement is never passed to the truncation, so it can never be cut.

The status emoji is followed only by the trailer (a space, then the
unread dot or mention badge, each with its own style), so the
placeholder's foreground reset needs no restoring there.

### Uploads

Placements made while `buildCache` runs collect their flushes; `View`
fires them against `imgpkg.KittyOutput` before returning, as the
pickers do. `buildCache` runs only from `View`, so every rebuild's
flushes are fired in the same call. Each flush is idempotent (its own
`fired` guard plus `Registry.MarkUploaded`), and uploading an image for
a row scrolled out of view is harmless. Closures are not stored in
`renderRow`.

### Invalidation

- `EmojiImageReadyMsg`: the App's debounced `emojiInvalidateMsg` arm
  calls `sidebar.HandleEmojiImageReady("")`, which drops the row cache
  and bumps `Version()` when the last build placed an image (warm or
  cold). Any surface's fetch ends there, and a sidebar that placed
  nothing keeps its rows. The App's wrapped sidebar panel cache
  (`renderCache.sidebar`) keys on `Version()`, so that is enough.
- Custom emoji arriving (`CustomEmojisLoadedMsg` via `App.SetCustomEmoji`):
  `SetEmojiCustoms`, same invalidation.
- Theme changes already call `sidebar.InvalidateCache()`.

### Terminals

- kitty and Ghostty resolve the placeholder cells natively.
- WezTerm runs kitty emulated placement: `finalizeSixelView` resolves
  placeholder cells anywhere in the final frame, sidebar included.
- Under a modal, `overlay.DimmedOverlay` blanks every placeholder cell
  of the dimmed background, sidebar included, so the image is hidden
  rather than drawn over the dim. Verified visually, not assumed.

## Known issues (not fixed here)

- A section header longer than the sidebar wraps to several lines in a
  one-line slot (documented on `renderSectionHeaderLabel`). The 2-cell
  image can reach that threshold one column sooner than a 1-cell glyph.
  Word wrap breaks at spaces, and a placement holds none, so the wrap
  cannot split the image; fixing the wrap (truncating) will need to
  measure with `emoji.Width` and keep the image whole.

## Tests

- `internal/emoji`: `PlaceShortcode` warm, cold, custom, unknown, image
  inputs missing.
- `internal/ui/peerstatus`: `GlyphShortcode` for huddle, status, custom,
  text-only, expired, none; each standard name maps to the same glyph
  as `Glyph`.
- `internal/ui/sidebar`: header with image mode on shows the placement
  (standard and custom via `SetEmojiCustoms`), restores the header
  foreground, fires the flush once; cold path reserves `cells` spaces;
  image mode off is byte-identical; DM status placement, budget and
  truncation, fallback when not placed; `HandleEmojiImageReady` bumps
  the version and rebuilds.
- `internal/ui`: the emoji invalidation tick and `SetCustomEmoji` reach
  the sidebar.

Visual check: the demo world (`slk --demo`) with a local, uncommitted
harness behind a build tag: kitty emulated placement on WezTerm, native
on Ghostty, a fake emoji fetcher, and a sections provider with one
standard and one custom emoji section (the demo has no Slack sections,
so its headers never carry an emoji).
