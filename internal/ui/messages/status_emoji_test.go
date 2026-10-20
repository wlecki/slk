package messages

import (
	"bytes"
	goimage "image"
	"io"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	bubblesansi "github.com/gammons/slk/internal/bubbles/ansi"
	emojiutil "github.com/gammons/slk/internal/emoji"
	imgpkg "github.com/gammons/slk/internal/image"
	"github.com/gammons/slk/internal/ui/peerstatus"
	"github.com/gammons/slk/internal/ui/styles"
)

// statusEmojiImages turns image mode on for one test and points the
// kitty side channel at a buffer.
func statusEmojiImages(t *testing.T) *bytes.Buffer {
	t.Helper()
	emojiutil.SetImageMode(true, 2)
	t.Cleanup(func() { emojiutil.SetImageMode(false, 2) })
	var buf bytes.Buffer
	prev := imgpkg.KittyOutput
	imgpkg.KittyOutput = &buf
	t.Cleanup(func() { imgpkg.KittyOutput = prev })
	return &buf
}

// serveStatusEmoji makes the emoji a shortcode names warm in f and
// returns its placement: realistic, with the image ID in the foreground
// and the "\x1b[39m" a real placement ends with. Its upload writes
// upload to the kitty side channel.
func serveStatusEmoji(t *testing.T, f *fakePlaceFetcher, name string, customs map[string]string, id, upload string) string {
	t.Helper()
	url, ok := emojiutil.URLForShortcode(name, customs)
	if !ok {
		t.Fatalf("precondition: :%s: has no URL", name)
	}
	placement := "\x1b[38;2;0;0;" + id + "m\U0010EEEE̅̅\U0010EEEE̅̍\x1b[39m"
	f.setPrerendered(emojiutil.EmojiCacheKey(url), goimage.Pt(2, 1), imgpkg.Render{
		Cells: goimage.Pt(2, 1),
		Lines: []string{placement},
		OnFlush: func(w io.Writer) error {
			_, err := io.WriteString(w, upload)
			return err
		},
	})
	return placement
}

func statusPane(st peerstatus.Status, f *fakePlaceFetcher) *Model {
	m := New([]MessageItem{{TS: "1700000000.000100", UserID: "U1", UserName: "alice", Text: "hello", Timestamp: "10:00 AM"}}, "general")
	m.SetUserStatuses(map[string]peerstatus.Status{"U1": st})
	if f != nil {
		m.SetEmojiContext(EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}, Cells: 2})
	}
	return m
}

func TestAuthorStatus_ImageInTheHeaderAndUploadedFromView(t *testing.T) {
	kitty := statusEmojiImages(t)
	f := newFakePlaceFetcher()
	placement := serveStatusEmoji(t, f, "calendar", nil, "51", "<upload calendar>")

	m := statusPane(peerstatus.Status{Emoji: ":calendar:", Text: "Meeting"}, f)
	out := m.View(20, 80)
	if !strings.Contains(out, "alice") {
		t.Fatalf("author missing:\n%s", out)
	}
	if !strings.Contains(out, placement) {
		t.Errorf("status emoji not drawn as an image in the author header:\n%q", out)
	}
	if strings.Contains(out, "📆") {
		t.Errorf("header still draws the glyph:\n%q", out)
	}
	if got := strings.Count(kitty.String(), "<upload calendar>"); got != 1 {
		t.Errorf("status image uploaded %d times from View, want 1", got)
	}
}

// The selected row repaints the background for the selection tint; the
// image ID in the placement's foreground must come through untouched.
func TestAuthorStatus_SelectedRowKeepsTheImage(t *testing.T) {
	statusEmojiImages(t)
	f := newFakePlaceFetcher()
	placement := serveStatusEmoji(t, f, "calendar", nil, "52", "")

	m := statusPane(peerstatus.Status{Emoji: ":calendar:"}, f)
	m.SetFocused(true)
	_ = m.View(20, 80)
	var sel string
	for i, e := range m.list.Entries() {
		if e.Selectable {
			sel = strings.Join(m.selectedLines(i), "\n")
		}
	}
	if !strings.Contains(sel, "alice") {
		t.Fatalf("no selected message row found:\n%q", sel)
	}
	if !strings.Contains(sel, placement) {
		t.Errorf("selected row lost the placement:\n%q", sel)
	}
}

// A custom status emoji shows the generic glyph until the workspace's
// custom emoji arrive, then its artwork.
func TestAuthorStatus_CustomEmojiAfterSetEmojiCustoms(t *testing.T) {
	statusEmojiImages(t)
	customs := map[string]string{"company-logo": "https://emoji.slack-edge.com/T1/company-logo/abc.png"}
	f := newFakePlaceFetcher()
	placement := serveStatusEmoji(t, f, "company-logo", customs, "53", "")

	m := statusPane(peerstatus.Status{Emoji: ":company-logo:"}, f)
	if out := m.View(20, 80); !strings.Contains(out, peerstatus.FallbackGlyph) {
		t.Fatalf("before customs load the header should draw the fallback glyph:\n%q", out)
	}
	m.SetEmojiCustoms(customs)
	if out := m.View(20, 80); !strings.Contains(out, placement) {
		t.Errorf("custom status emoji not drawn as an image after SetEmojiCustoms:\n%q", out)
	}
}

func TestAuthorStatus(t *testing.T) {
	now := time.Unix(1700000000, 0)
	glyphSuffix := func(st peerstatus.Status) string {
		return AuthorStatusSuffix(map[string]peerstatus.Status{"U1": st}, "U1", now)
	}
	bg := lipgloss.NewStyle().Background(styles.Background)

	t.Run("image mode off is AuthorStatusSuffix", func(t *testing.T) {
		f := newFakePlaceFetcher()
		serveStatusEmoji(t, f, "calendar", nil, "54", "")
		for _, st := range []peerstatus.Status{{Emoji: ":calendar:"}, {Huddle: peerstatus.HuddleActive}, {Text: "Lunch"}, {}} {
			got, flush := AuthorStatus(map[string]peerstatus.Status{"U1": st}, "U1", now, EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}, Cells: 2})
			if got != glyphSuffix(st) || flush != nil {
				t.Errorf("%+v: AuthorStatus = %q (flush %v), want %q", st, got, flush != nil, glyphSuffix(st))
			}
		}
	})

	t.Run("no fetcher is AuthorStatusSuffix", func(t *testing.T) {
		statusEmojiImages(t)
		st := peerstatus.Status{Emoji: ":calendar:"}
		if got, _ := AuthorStatus(map[string]peerstatus.Status{"U1": st}, "U1", now, EmojiContext{}); got != glyphSuffix(st) {
			t.Errorf("AuthorStatus = %q, want %q", got, glyphSuffix(st))
		}
	})

	t.Run("huddle", func(t *testing.T) {
		statusEmojiImages(t)
		f := newFakePlaceFetcher()
		placement := serveStatusEmoji(t, f, "headphones", nil, "55", "")
		got, flush := AuthorStatus(map[string]peerstatus.Status{"U1": {Huddle: peerstatus.HuddleActive}}, "U1", now, EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}, Cells: 2})
		if want := bg.Render(" " + placement); got != want || flush == nil {
			t.Errorf("AuthorStatus = %q (flush %v), want %q", got, flush != nil, want)
		}
	})

	t.Run("cold image reserves its cells", func(t *testing.T) {
		statusEmojiImages(t)
		f := newFakePlaceFetcher() // nothing warm: Place reserves blanks and fetches
		got, flush := AuthorStatus(map[string]peerstatus.Status{"U1": {Emoji: ":hourglass_flowing_sand:"}}, "U1", now, EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}, Cells: 2})
		if want := bg.Render("   "); got != want || flush != nil {
			t.Errorf("AuthorStatus = %q (flush %v), want %q", got, flush != nil, want)
		}
		if w := lipgloss.Width(got); w != 1+2 {
			t.Errorf("cold suffix is %d cells, want 3", w)
		}
	})

	t.Run("no status", func(t *testing.T) {
		statusEmojiImages(t)
		f := newFakePlaceFetcher()
		if got, flush := AuthorStatus(nil, "U1", now, EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}}); got != "" || flush != nil {
			t.Errorf("AuthorStatus = %q (flush %v), want empty", got, flush != nil)
		}
	})
}

func dmHeaderPane(f *fakePlaceFetcher) *Model {
	m := New(nil, "alice")
	m.SetChannelType("dm")
	if f != nil {
		m.SetEmojiContext(EmojiContext{PlaceCtx: emojiutil.PlaceContext{Fetcher: f}, Cells: 2})
	}
	return m
}

var dmTopicNow = time.Unix(1700000000, 0)

func TestDMTopic_EmojiAsImagesWithTheTopicColourRestored(t *testing.T) {
	kitty := statusEmojiImages(t)
	f := newFakePlaceFetcher()
	huddle := serveStatusEmoji(t, f, "headphones", nil, "81", "<upload headphones>")
	cal := serveStatusEmoji(t, f, "calendar", nil, "82", "<upload calendar>")

	m := dmHeaderPane(f)
	m.SetDMTopic(peerstatus.Status{Emoji: ":calendar:", Text: "Meeting", Huddle: peerstatus.HuddleActive}, dmTopicNow, "15:04")
	out := m.View(10, 80)
	restore := bubblesansi.ForegroundSGR(styles.Timestamp.GetForeground())
	if !strings.Contains(out, huddle+restore+" In a huddle") {
		t.Errorf("huddle not drawn as an image with the topic colour restored after it:\n%q", out)
	}
	if !strings.Contains(out, cal+restore+" Meeting") {
		t.Errorf("status not drawn as an image with the topic colour restored after it:\n%q", out)
	}
	if strings.Contains(out, peerstatus.HuddleGlyph) || strings.Contains(out, "📆") {
		t.Errorf("header still draws a glyph:\n%q", out)
	}
	for _, u := range []string{"<upload headphones>", "<upload calendar>"} {
		if n := strings.Count(kitty.String(), u); n != 1 {
			t.Errorf("%s fired %d times from View, want 1", u, n)
		}
	}
	// Every chrome line is exactly the pane width (ui.borderedTopPane
	// assembles the border by concatenation).
	for i, l := range strings.Split(m.chromeCache, "\n") {
		if w := lipgloss.Width(l); w != 80 {
			t.Errorf("chrome line %d is %d cells, want 80", i, w)
		}
	}
}

// A cold image keeps its blank cells: nothing trims them, so the line
// does not shift when the image arrives.
func TestDMTopic_ColdReservationKept(t *testing.T) {
	statusEmojiImages(t)
	f := newFakePlaceFetcher() // nothing warm
	m := dmHeaderPane(f)
	m.SetDMTopic(peerstatus.Status{Emoji: ":hourglass_flowing_sand:"}, dmTopicNow, "15:04")
	_ = m.View(10, 80)
	topicLine := strings.Split(m.chromeCache, "\n")[1]
	if got := ansi.Strip(topicLine); !strings.HasPrefix(got, "  ") || strings.TrimSpace(got) != "" {
		t.Errorf("cold topic = %q, want a 2-cell blank reservation", got)
	}
}

// The header is rebuilt when an emoji fetch lands, when custom emoji
// arrive, and when the status changes even though its text does not
// (two custom emoji draw the same fallback glyph).
func TestDMTopic_HeaderRebuilds(t *testing.T) {
	statusEmojiImages(t)
	customs := map[string]string{
		"logo-a": "https://emoji.slack-edge.com/T1/logo-a/a.png",
		"logo-b": "https://emoji.slack-edge.com/T1/logo-b/b.png",
	}
	f := newFakePlaceFetcher()
	a := serveStatusEmoji(t, f, "logo-a", customs, "83", "")
	b := serveStatusEmoji(t, f, "logo-b", customs, "84", "")

	m := dmHeaderPane(f)
	m.SetDMTopic(peerstatus.Status{Emoji: ":logo-a:", Text: "Busy"}, dmTopicNow, "15:04")
	if out := m.View(10, 80); !strings.Contains(out, peerstatus.FallbackGlyph+" Busy") {
		t.Fatalf("before customs load the header should draw the fallback glyph:\n%q", out)
	}
	m.SetEmojiCustoms(customs)
	if out := m.View(10, 80); !strings.Contains(out, a) {
		t.Errorf("SetEmojiCustoms did not rebuild the header with the image:\n%q", out)
	}

	m.SetDMTopic(peerstatus.Status{Emoji: ":logo-b:", Text: "Busy"}, dmTopicNow, "15:04")
	if out := m.View(10, 80); !strings.Contains(out, b) {
		t.Errorf("a new status with the same text did not rebuild the header:\n%q", out)
	}

	// A fetch that lands (here: a different placement now warm for the
	// same URL) reaches the header through HandleEmojiImageReady.
	b2 := serveStatusEmoji(t, f, "logo-b", customs, "85", "")
	_ = m.View(10, 80)
	m.HandleEmojiImageReady("")
	if out := m.View(10, 80); !strings.Contains(out, b2) {
		t.Errorf("HandleEmojiImageReady did not rebuild the header:\n%q", out)
	}
}

// With image mode off, or for a plain topic, the header is what
// SetChannelTopic draws.
func TestDMTopic_ImageModeOffIsThePlainTopic(t *testing.T) {
	st := peerstatus.Status{Emoji: ":calendar:", Text: "Meeting", Huddle: peerstatus.HuddleActive, DND: true}
	f := newFakePlaceFetcher()
	serveStatusEmoji(t, f, "calendar", nil, "86", "")

	plain := dmHeaderPane(nil)
	plain.SetChannelTopic(st.Summary(dmTopicNow, "15:04"))
	want := plain.View(10, 80)

	m := dmHeaderPane(f)
	m.SetDMTopic(st, dmTopicNow, "15:04")
	if got := m.View(10, 80); got != want {
		t.Errorf("image mode off changed the DM header:\n got %q\nwant %q", got, want)
	}

	// SetChannelTopic drops the kept status: a plain topic never draws images.
	statusEmojiImages(t)
	m.SetChannelTopic("Release day")
	if got := m.View(10, 80); strings.Contains(got, "\U0010EEEE") {
		t.Errorf("plain topic drew an image:\n%q", got)
	}
}
