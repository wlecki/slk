package sidebar

import (
	"bytes"
	"context"
	"fmt"
	goimage "image"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/styles"
)

// fakeEmojiFetcher serves prerendered placements by emoji URL. Fetch
// runs on Place's background goroutine, hence the lock.
type fakeEmojiFetcher struct {
	mu        sync.Mutex
	prerender map[string]imgpkg.Render
	fetches   int
}

func newFakeEmojiFetcher() *fakeEmojiFetcher {
	return &fakeEmojiFetcher{prerender: map[string]imgpkg.Render{}}
}

func (f *fakeEmojiFetcher) Prerendered(key string, _ goimage.Point, _ imgpkg.Protocol) (imgpkg.Render, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.prerender[key]
	return r, ok
}

func (f *fakeEmojiFetcher) Fetch(context.Context, imgpkg.FetchRequest) (imgpkg.FetchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetches++
	return imgpkg.FetchResult{}, nil
}

// serve makes url warm and returns its placement: a realistic one, with
// the image ID in the foreground SGR and the "\x1b[39m" reset the real
// kitty renderer ends a placement with. onFlush may be nil.
func (f *fakeEmojiFetcher) serve(url string, id int, onFlush func(io.Writer) error) string {
	placement := fmt.Sprintf("\x1b[38;2;0;0;%dm\U0010EEEE̅̅\U0010EEEE̅̍\x1b[39m", id)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prerender[emoji.EmojiCacheKey(url)] = imgpkg.Render{
		Cells:   goimage.Pt(2, 1),
		Lines:   []string{placement},
		OnFlush: onFlush,
	}
	return placement
}

// withEmojiImages turns image mode on for one test.
func withEmojiImages(t *testing.T) {
	t.Helper()
	emoji.SetImageMode(true, 2)
	t.Cleanup(func() { emoji.SetImageMode(false, 2) })
}

// captureKittyOutput points imgpkg.KittyOutput at a buffer for one test.
func captureKittyOutput(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := imgpkg.KittyOutput
	imgpkg.KittyOutput = &buf
	t.Cleanup(func() { imgpkg.KittyOutput = prev })
	return &buf
}

func mustEmojiURL(t *testing.T, name string, customs map[string]string) string {
	t.Helper()
	url, ok := emoji.URLForShortcode(name, customs)
	if !ok {
		t.Fatalf("precondition: :%s: has no URL", name)
	}
	return url
}

// sectionModel is a Slack-mode sidebar with one section "Docs" carrying
// the given emoji shortcode, holding one channel.
func sectionModel(emojiCode string) Model {
	m := New([]ChannelItem{{ID: "C1", Name: "handbook", Type: "channel", Section: "S1"}})
	m.SetSectionsProvider(&fakeProvider{
		ready:    true,
		sections: []SectionMeta{{ID: "S1", Name: "Docs", Emoji: emojiCode, Type: "standard"}},
	})
	return m
}

func emojiCtx(f emoji.PlaceFetcher) messages.EmojiContext {
	return messages.EmojiContext{PlaceCtx: emoji.PlaceContext{Fetcher: f}, Cells: 2}
}

// sectionHeaderFg is what the header re-asserts after an image: the
// placement's "\x1b[39m" would otherwise leave the name in the
// terminal's default colour.
func sectionHeaderFg() string {
	return ansi.Style{}.ForegroundColor(styles.SectionHeader.GetForeground()).String()
}

// With image mode off, installing an emoji context changes nothing:
// the sidebar renders byte for byte what it rendered before. Same with
// image mode on but no fetcher wired.
func TestEmojiImages_OffIsByteIdentical(t *testing.T) {
	base := sectionModel("orange_book")
	want := base.View(10, 30)

	ff := newFakeEmojiFetcher()
	ff.serve(mustEmojiURL(t, "orange_book", nil), 42, nil)
	m := sectionModel("orange_book")
	m.SetEmojiContext(emojiCtx(ff))
	if got := m.View(10, 30); got != want {
		t.Errorf("image mode off changed the sidebar:\nwant %q\ngot  %q", want, got)
	}

	withEmojiImages(t)
	m = sectionModel("orange_book")
	m.SetEmojiContext(messages.EmojiContext{Cells: 2})
	if got := m.View(10, 30); got != want {
		t.Errorf("image mode on without a fetcher changed the sidebar:\nwant %q\ngot  %q", want, got)
	}
}

func TestEmojiImages_SectionHeaderPlacesStandardEmoji(t *testing.T) {
	withEmojiImages(t)
	ff := newFakeEmojiFetcher()
	placement := ff.serve(mustEmojiURL(t, "orange_book", nil), 42, nil)

	m := sectionModel("orange_book")
	m.SetEmojiContext(emojiCtx(ff))
	header := rowFor(t, m.View(10, 30), "Docs")

	if !strings.Contains(header, placement+sectionHeaderFg()+" Docs") {
		t.Errorf("header does not draw the image, restore the header colour, then the name:\n%q", header)
	}
	if glyph := emoji.CodeMap()[":orange_book:"]; strings.Contains(header, glyph) {
		t.Errorf("header still draws the glyph %q next to the image:\n%q", glyph, header)
	}
}

// With the cursor on the header, the label after the cursor glyph is in
// the selected colours (ReapplyBgAfterResets re-applies them after the
// glyph's reset). The image must hand the name back those colours, not
// the unselected header's, so the name looks as it does in glyph mode.
func TestEmojiImages_SelectedHeaderKeepsSelectedColours(t *testing.T) {
	selectedHeader := func(m *Model) string {
		t.Helper()
		m.SetFocused(true)
		for {
			if _, ok := m.IsSectionHeaderSelected(); ok {
				break
			}
			m.MoveDown()
		}
		return rowFor(t, m.View(10, 30), "Docs")
	}
	selectedAttrs := messages.SidebarBgANSI() + messages.SidebarFgANSI()

	glyph := sectionModel("orange_book")
	if row := selectedHeader(&glyph); !strings.Contains(row, selectedAttrs+"\x1b[1m▾ "+emoji.CodeMap()[":orange_book:"]) {
		t.Fatalf("precondition: in glyph mode the selected header is drawn in %q:\n%q", selectedAttrs, row)
	}

	withEmojiImages(t)
	ff := newFakeEmojiFetcher()
	placement := ff.serve(mustEmojiURL(t, "orange_book", nil), 42, nil)
	m := sectionModel("orange_book")
	m.SetEmojiContext(emojiCtx(ff))
	if row := selectedHeader(&m); !strings.Contains(row, placement+selectedAttrs+" Docs") {
		t.Errorf("selected header does not restore the selected colours after the image:\n%q", row)
	}
}

// Every surface's emoji fetch ends in HandleEmojiImageReady. A sidebar
// with no image placed has nothing to swap and keeps its cached rows.
func TestEmojiImages_ImageReadyIgnoredWithoutPlacements(t *testing.T) {
	withEmojiImages(t)
	m := sectionModel("")
	m.SetEmojiContext(emojiCtx(newFakeEmojiFetcher()))
	m.View(10, 30)
	v := m.Version()
	m.HandleEmojiImageReady("https://example.com/x.png")
	if m.Version() != v {
		t.Error("HandleEmojiImageReady rebuilt a sidebar that holds no emoji image")
	}
}

// Custom emoji arrive after startup (CustomEmojisLoadedMsg). Until then
// the header shows the text form; SetEmojiCustoms brings the artwork.
func TestEmojiImages_SectionHeaderCustomEmojiAfterCustomsLoad(t *testing.T) {
	withEmojiImages(t)
	customs := map[string]string{"partyparrot": "https://emoji.slack-edge.com/T1/partyparrot/abc.gif"}
	ff := newFakeEmojiFetcher()
	placement := ff.serve(customs["partyparrot"], 43, nil)

	m := sectionModel("partyparrot")
	m.SetEmojiContext(emojiCtx(ff))
	if header := rowFor(t, m.View(10, 30), "Docs"); !strings.Contains(header, ":partyparrot: ") {
		t.Fatalf("before customs load the header should show the text form:\n%q", header)
	}

	v := m.Version()
	m.SetEmojiCustoms(customs)
	if m.Version() == v {
		t.Error("SetEmojiCustoms did not bump Version; the App's sidebar cache would keep the old frame")
	}
	header := rowFor(t, m.View(10, 30), "Docs")
	if !strings.Contains(header, placement) {
		t.Errorf("custom emoji not drawn as an image after SetEmojiCustoms:\n%q", header)
	}
	if strings.Contains(header, ":partyparrot:") {
		t.Errorf("header still shows the text form next to the image:\n%q", header)
	}
}

// Nothing to place: an unknown shortcode keeps today's text fallback.
func TestEmojiImages_SectionHeaderUnknownEmojiKeepsText(t *testing.T) {
	withEmojiImages(t)
	m := sectionModel("no_such_emoji_anywhere")
	m.SetEmojiContext(emojiCtx(newFakeEmojiFetcher()))
	if header := rowFor(t, m.View(10, 40), "Docs"); !strings.Contains(header, ":no_such_emoji_anywhere: Docs") {
		t.Errorf("unknown emoji should render as text:\n%q", header)
	}
}

// Cold path: the image is not fetched yet, so the header reserves the
// image's cells. When the fetch lands, HandleEmojiImageReady rebuilds
// the cached rows, which then hold the image.
func TestEmojiImages_ColdHeaderRedrawsOnImageReady(t *testing.T) {
	withEmojiImages(t)
	url := mustEmojiURL(t, "ledger", nil)
	ff := newFakeEmojiFetcher()

	m := sectionModel("ledger")
	m.SetEmojiContext(emojiCtx(ff))
	header := rowFor(t, m.View(10, 30), "Docs")
	if !strings.Contains(header, "▾   "+sectionHeaderFg()+" Docs") {
		t.Fatalf("cold header should reserve two cells before the name:\n%q", header)
	}

	placement := ff.serve(url, 44, nil)
	if header := rowFor(t, m.View(10, 30), "Docs"); strings.Contains(header, placement) {
		t.Fatal("precondition: the cached rows still hold the reservation until told otherwise")
	}
	v := m.Version()
	m.HandleEmojiImageReady(url)
	if m.Version() == v {
		t.Error("HandleEmojiImageReady did not bump Version; the App's sidebar cache would keep the old frame")
	}
	if header := rowFor(t, m.View(10, 30), "Docs"); !strings.Contains(header, placement) {
		t.Errorf("header not redrawn with the image after HandleEmojiImageReady:\n%q", header)
	}
}

// The upload a placement needs reaches the terminal from View, once.
func TestEmojiImages_ViewFiresUploadsOnce(t *testing.T) {
	withEmojiImages(t)
	out := captureKittyOutput(t)
	uploads := 0
	ff := newFakeEmojiFetcher()
	ff.serve(mustEmojiURL(t, "orange_book", nil), 42, func(w io.Writer) error {
		uploads++
		_, err := io.WriteString(w, "<upload>")
		return err
	})

	m := sectionModel("orange_book")
	m.SetEmojiContext(emojiCtx(ff))
	m.View(10, 30)
	if uploads != 1 || out.String() != "<upload>" {
		t.Fatalf("after the first View: %d uploads, KittyOutput %q; want 1 upload written to KittyOutput", uploads, out.String())
	}
	m.View(10, 30)
	if uploads != 1 {
		t.Errorf("a View from the cached rows ran the upload again (%d uploads)", uploads)
	}
}

func dmModel(status peerstatus.Status) Model {
	m := New([]ChannelItem{{ID: "D1", Name: "alice", Type: "dm", DMUserID: "U1", Status: status}})
	return m
}

func TestEmojiImages_DMStatusEmojiIsPlaced(t *testing.T) {
	withEmojiImages(t)
	customs := map[string]string{"company-ooo": "https://emoji.slack-edge.com/T1/company-ooo/abc.png"}
	cases := []struct {
		name   string
		status peerstatus.Status
		url    string
		glyph  string // what the row shows when the image is not placed
	}{
		{"standard status", peerstatus.Status{Emoji: ":calendar:"}, mustEmojiURL(t, "calendar", nil), emoji.CodeMap()[":calendar:"]},
		{"custom status", peerstatus.Status{Emoji: ":company-ooo:"}, customs["company-ooo"], peerstatus.FallbackGlyph},
		{"huddle", peerstatus.Status{Huddle: peerstatus.HuddleActive}, mustEmojiURL(t, "headphones", nil), peerstatus.HuddleGlyph},
		{"text only", peerstatus.Status{Text: "Heads down"}, mustEmojiURL(t, "speech_balloon", nil), peerstatus.FallbackGlyph},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ff := newFakeEmojiFetcher()
			placement := ff.serve(c.url, 50+i, nil)
			m := dmModel(c.status)
			m.SetEmojiContext(messages.EmojiContext{PlaceCtx: emoji.PlaceContext{Fetcher: ff}, Cells: 2, Customs: customs})
			row := rowFor(t, m.View(10, 30), "alice")
			if !strings.Contains(row, "alice "+placement) {
				t.Errorf("status emoji not placed after the name:\n%q", row)
			}
			if strings.Contains(row, c.glyph) {
				t.Errorf("row shows the glyph %q as well as the image:\n%q", c.glyph, row)
			}
		})
	}
}

// When the image can't be placed (here: the custom emoji map has not
// loaded), the row shows today's glyph.
func TestEmojiImages_DMStatusFallsBackToGlyph(t *testing.T) {
	withEmojiImages(t)
	m := dmModel(peerstatus.Status{Emoji: ":company-ooo:"})
	m.SetEmojiContext(emojiCtx(newFakeEmojiFetcher()))
	row := ansiRe.ReplaceAllString(rowFor(t, m.View(10, 30), "alice"), "")
	if !strings.Contains(row, "alice "+peerstatus.FallbackGlyph) {
		t.Errorf("unplaced custom status should show the fallback glyph:\n%q", row)
	}
}

// The image is charged to the name budget and appended after the name
// is truncated, so a long name truncates and the image stays whole on
// a one-line row. The image is as wide as the glyph it replaces (2
// cells), so the name truncates exactly as it does in glyph mode.
func TestEmojiImages_DMStatusImageNeverCutByTruncation(t *testing.T) {
	long := "abcdefghijklmnopqrstuvwxyz"
	status := func(m *Model) { m.UpdateStatusByUser("U1", ":calendar:", "", time.Time{}) }
	truncatedName := func(row string) string {
		plain := ansiRe.ReplaceAllString(row, "")
		return plain[:strings.Index(plain, "…")]
	}

	glyphMode := New([]ChannelItem{{ID: "D1", Name: long, Type: "dm", DMUserID: "U1"}})
	status(&glyphMode)
	wantName := truncatedName(rowFor(t, glyphMode.View(10, 24), long[:5]))

	withEmojiImages(t)
	ff := newFakeEmojiFetcher()
	placement := ff.serve(mustEmojiURL(t, "calendar", nil), 60, nil)
	m := New([]ChannelItem{{ID: "D1", Name: long, Type: "dm", DMUserID: "U1"}})
	m.SetEmojiContext(emojiCtx(ff))
	before := m.View(10, 24)
	status(&m)
	after := m.View(10, 24)

	if strings.Count(after, "\n") != strings.Count(before, "\n") {
		t.Fatalf("status image changed the sidebar's line count:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	row := rowFor(t, after, long[:5])
	if !strings.Contains(row, "… "+placement) {
		t.Errorf("row should truncate the name, then draw the whole image:\n%q", row)
	}
	if got := truncatedName(row); got != wantName {
		t.Errorf("name truncated to %q next to the image, want %q as in glyph mode", got, wantName)
	}
	if got, want := lipgloss.Width(row), lipgloss.Width(rowFor(t, glyphMode.View(10, 24), long[:5])); got != want {
		t.Errorf("row is %d cells wide with the image, %d with the glyph", got, want)
	}
}
