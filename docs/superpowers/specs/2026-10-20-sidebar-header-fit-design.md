# Sidebar section headers fit one line

Status: approved scope, 2026-10-20. Fixes the two header bugs left open
by `2026-10-20-sidebar-emoji-images-design.md`.

## Bugs

1. **Wrap.** `renderSectionHeaderLabel` takes no width and truncates
   nothing. `buildCache` renders the label through
   `styles.SectionHeader.Width(width-2)`, and lipgloss wraps overlong
   content, while the `renderRow` it lands in claims height 1. A long
   section name (or a shorter one plus the collapsed aggregates "•N" and
   the mention badge) emits several lines into a one-line slot and
   breaks the sidebar layout.
2. **Selected header loses bold.** In the selected variant the cursor
   glyph's reset is followed by `ReapplyBgAfterResets(selected, bgAnsi)`,
   and `bgAnsi` carries no bold, so everything after the cursor is drawn
   regular although `SectionHeader` is bold.

## Design

### Budget

`renderSectionHeaderLabel` gets the sidebar width. The label has
`width - 4` columns: `Width(width-2)` minus `SectionHeader`'s
`Padding(0, 1)`. Both variants share one budget, taken from the wider
one, the selected variant:

| Part | Charged |
|---|---|
| cursor `▌` (selected) / space (normal) | 2: `▌` is East Asian Ambiguous, hedged as the channel rows hedge it (`cursor(2)`) |
| triangle `▾` / `▸` and its space | 2 (both are narrow, not ambiguous) |
| emoji prefix, placed image | `1 + m.emojiCells()`: the footprint `PlaceShortcode` was given, plus the space after it. The placement string is never measured or cut |
| emoji prefix, glyph or `:code:` text | its measured width (the glyph prefix is the glyph plus two spaces, as today) |
| aggregates (`•N`, mention badge) | their measured width; already rendered, never cut |
| name | the rest |

"Measured" is `max(emoji.Width(s), lipgloss.Width(s))`. `emoji.Width`
knows the image footprint of emoji clusters in image mode; lipgloss is
what decides the wrap, and with a 1-cell image footprint `emoji.Width`
reports a wide glyph emoji as 1 while lipgloss (and the terminal) count
2. Taking the larger keeps both from overflowing.

### What gives way

When the measured label fits, it is built exactly as before, so a header
that fits is byte-identical in text mode. "Fits" includes the one-column
hedge for `▌`: a header exactly `width - 4` wide by lipgloss's count now
loses a column of its name. Otherwise, in order:

1. A `:code:` text fallback (an unknown emoji, or a custom one before the
   workspace's customs load) is dropped when the name does not fit whole
   next to it. It tells the user less than the name, so it goes before
   the name is cut. An image or a glyph stays.
2. The name is truncated grapheme by grapheme (never inside a cluster)
   and ends in "…", which takes one column of its budget. A budget of one
   column leaves just "…".
3. With no column left for the name, the name is dropped.
4. If the header still does not fit, the emoji prefix is dropped whole.
5. If it still does not fit, the aggregates are dropped.

The triangle always stays. At the minimum sidebar width (20) steps 4 and
5 are not reachable with today's figures (`•N` and a "99+" badge), but
nothing below that width is clamped, so they keep the one-line promise
for any width a caller passes.

No shared helper truncates by `emoji.Width` keeping graphemes whole
(`entrylist.SliceColumns` includes a split cluster, blockkit's
`truncateToWidth` is rune-based, `truncate.StringWithTail` measures with
runewidth), so the sidebar gets a small unexported one.

### Bold on the selected header

The selected variant's reapply payload becomes `bgAnsi + "\x1b[1m"`, as
channel rows pass `"\x1b[1m"` in theirs. Only the selected variant:
`ReapplyBgAfterResets` also injects after the resets inside the mention
badge's padding, so bold in the normal payload would make every
unselected collapsed header's badge digits bold. In the selected variant
that side effect matches the selected channel rows.

The image path's `placed + bgAnsi + " "` stays: a placement resets only
the foreground (`\x1b[39m`), so bold survives it.

## Tests

`internal/ui/sidebar`, scanning `View()`:

- a long section name at width 20 stays one line, the view keeps its line
  count, and the header ends in "…";
- a collapsed long header keeps `•N` and the mention badge, with the name
  truncated;
- a header with an image placement and a long name keeps the whole
  placement and truncates the name;
- narrower than the aggregates allow: the name and then the emoji give
  way, the header is still one line;
- a `:code:` fallback gives way to the name; a custom emoji section at
  width 20 shows its name before the customs load and image plus name
  after;
- the selected header re-applies bold after the cursor;
- headers that fit are unchanged (existing exact-string tests).
