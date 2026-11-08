package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// makeTransparent turns the transparent background on for a built App,
// through the same applyTheme a theme switch uses, and turns it off
// again after the test.
func makeTransparent(t *testing.T, a *App, theme string) {
	t.Helper()
	styles.SetTransparentBackground(true)
	t.Cleanup(func() {
		styles.SetTransparentBackground(false)
		styles.Apply("dark", config.Theme{})
	})
	a.applyTheme(theme)
}

// The scenarios and dialogs pinned again with the transparent background
// on, as transparent_<name>.
var (
	transparentGoldenScenarios = []string{"base", "thread_open", "thread_parent_selected", "drag_selection", "overlay_finder"}
	transparentGoldenDialogs   = []string{"dialog_confirm", "dialog_channel_finder_scroll", "dialog_search_results", "popup_mention", "popup_emoji", "popup_channel"}
)

func TestGolden_Transparent(t *testing.T) {
	scenarios := map[string]goldenScenario{}
	for _, sc := range goldenScenarios() {
		scenarios[sc.name] = sc
	}
	for _, name := range transparentGoldenScenarios {
		t.Run(name, func(t *testing.T) {
			sc, ok := scenarios[name]
			if !ok {
				t.Fatalf("no golden scenario %q", name)
			}
			a := sc.build(t)
			makeTransparent(t, a, "dark")
			compareGolden(t, "transparent_"+name, a.View().Content)
		})
	}

	dialogs := map[string]dialogGolden{}
	for _, d := range dialogGoldens() {
		dialogs[d.name] = d
	}
	for _, name := range transparentGoldenDialogs {
		t.Run(name, func(t *testing.T) {
			d, ok := dialogs[name]
			if !ok {
				t.Fatalf("no dialog golden %q", name)
			}
			opts := goldenFixtureOpts()
			if d.w != 0 {
				opts = append(opts, withSize(d.w, d.h))
			}
			a := newGoldenApp(t, opts...)
			makeTransparent(t, a, "dark")
			d.open(t, a)
			frame := a.View().Content
			if !d.shown(a) {
				t.Fatalf("%s: the dialog is not open", name)
			}
			compareGolden(t, "transparent_"+name, frame)
		})
	}
}

// transparentTestColors is a theme whose base colours are each unique, so
// a frame can be searched for any one of them.
var transparentTestColors = styles.ThemeColors{
	Primary: "#4A9EFF", Accent: "#50C878", Warning: "#E0A030", Error: "#E04040",
	Background:        "#101820",
	SidebarBackground: "#111921",
	RailBackground:    "#121A22",
	SurfaceDark:       "#131B23",
	Surface:           "#202830",
	Text:              "#E0E0E0", TextMuted: "#888888", Border: "#333333",
}

var sgrRe = regexp.MustCompile("\x1b\\[([0-9;]*)m")

// bgParams returns every truecolor background ("R;G;B") a frame sets,
// per line. A 48;2 inside some other colour group can't occur: the
// groups are walked in order.
func bgParams(frame string) [][]string {
	var out [][]string
	for _, line := range strings.Split(frame, "\n") {
		var bgs []string
		for _, m := range sgrRe.FindAllStringSubmatch(line, -1) {
			p := strings.Split(m[1], ";")
			for i := 0; i < len(p); i++ {
				if (p[i] == "38" || p[i] == "48" || p[i] == "58") && i+1 < len(p) {
					switch p[i+1] {
					case "2":
						if p[i] == "48" && i+4 < len(p) {
							bgs = append(bgs, strings.Join(p[i+2:i+5], ";"))
						}
						i += 4
					case "5":
						i += 2
					}
				}
			}
		}
		out = append(out, bgs)
	}
	return out
}

// TestTransparentFrameHasNoBaseFill renders whole frames with the
// transparent background on and looks for the base colours: none of the
// pane, sidebar and rail backgrounds may be painted anywhere, and the
// status bar (the last row) may not be painted with surface_dark. The
// composer keeps surface_dark, so that one is only checked on the status
// row.
func TestTransparentFrameHasNoBaseFill(t *testing.T) {
	styles.RegisterCustomTheme("Transparent Frame Test", transparentTestColors)
	banned := map[string]string{
		"16;24;32": "background",
		"17;25;33": "sidebar_background",
		"18;26;34": "rail_background",
	}
	const surfaceDark = "19;27;35"

	for _, sc := range goldenScenarios() {
		if sc.name == "overlay_finder" {
			continue // a dialog keeps the theme background on purpose
		}
		t.Run(sc.name, func(t *testing.T) {
			a := sc.build(t)
			makeTransparent(t, a, "Transparent Frame Test")
			lines := bgParams(a.View().Content)
			for i, bgs := range lines {
				for _, bg := range bgs {
					if name, ok := banned[bg]; ok {
						t.Fatalf("row %d paints %s (%s)", i, name, bg)
					}
				}
			}
			for _, bg := range lines[len(lines)-1] {
				if bg == surfaceDark {
					t.Fatalf("the status bar paints surface_dark (%s)", bg)
				}
			}
		})
	}
}

// And the other way round: with the option off the same theme does paint
// those colours, so the test above can fail.
func TestTransparentFrameCheckSeesOpaqueFill(t *testing.T) {
	styles.RegisterCustomTheme("Transparent Frame Test", transparentTestColors)
	a := newGoldenApp(t, goldenFixtureOpts()...)
	a.applyTheme("Transparent Frame Test")
	lines := bgParams(a.View().Content)
	seen := map[string]bool{}
	for _, bgs := range lines {
		for _, bg := range bgs {
			seen[bg] = true
		}
	}
	for _, want := range []string{"16;24;32", "17;25;33", "18;26;34"} {
		if !seen[want] {
			t.Errorf("opaque frame never paints %s; the transparent check would pass vacuously", want)
		}
	}
	statusSeen := false
	for _, bg := range lines[len(lines)-1] {
		statusSeen = statusSeen || bg == "19;27;35"
	}
	if !statusSeen {
		t.Error("opaque status bar does not paint surface_dark")
	}
}

// The selected thread parent keeps its tint across the whole row: no
// reset in its rows may fall back to the default background.
func TestTransparentSelectedThreadParentHasNoHoles(t *testing.T) {
	sc := goldenScenarioNamed(t, "thread_parent_selected")
	a := sc.build(t)
	makeTransparent(t, a, "dark")
	frame := a.View().Content
	tint := messages.SelectionTintBgANSI(a.threadPanel.Focused())
	tinted := 0
	for i, line := range strings.Split(frame, "\n") {
		first := strings.Index(line, tint)
		last := strings.LastIndex(line, tint)
		if first < 0 {
			continue
		}
		tinted++
		// The pane re-asserts BgANSI after every reset; harmless when
		// the tint comes straight after it.
		span := strings.ReplaceAll(line[first:last+len(tint)], "\x1b[49m"+tint, tint)
		if hole := strings.Index(span, "\x1b[49m"); hole >= 0 {
			t.Errorf("row %d: the tint falls back to the default background: ...%q", i, span[hole:])
		}
	}
	if tinted == 0 {
		t.Fatal("no tinted row: the parent is not selected")
	}
}

var cellTokRe = regexp.MustCompile("\x1b\\[([0-9;]*)m|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)|(?s:(.))")

// defaultBgCells counts the visible characters in s drawn on the default
// background, walking the SGR state (s starts with a bg set).
func defaultBgCells(s string) int {
	set, n := true, 0
	for _, m := range cellTokRe.FindAllStringSubmatchIndex(s, -1) {
		if m[4] >= 0 { // a visible character
			if !set {
				n++
			}
			continue
		}
		if m[2] < 0 {
			continue // OSC
		}
		p := strings.Split(s[m[2]:m[3]], ";")
		for i := 0; i < len(p); i++ {
			switch p[i] {
			case "", "0", "49":
				set = false
			case "48":
				set = true
				fallthrough
			case "38", "58":
				if i+1 < len(p) && p[i+1] == "2" {
					i += 4
				} else {
					i += 2
				}
			}
		}
	}
	return n
}

// A composer popup is solid: between its first and last surface_dark
// cell on a row, nothing is drawn on the default background.
func TestTransparentPopupsHaveNoHoles(t *testing.T) {
	dialogs := map[string]dialogGolden{}
	for _, d := range dialogGoldens() {
		dialogs[d.name] = d
	}
	for _, name := range []string{"popup_mention", "popup_emoji", "popup_channel"} {
		t.Run(name, func(t *testing.T) {
			d := dialogs[name]
			a := newGoldenApp(t, goldenFixtureOpts()...)
			makeTransparent(t, a, "dark")
			d.open(t, a)
			surface := "\x1b[48;2;15;15;35m" // dark's surface_dark
			rows := 0
			for i, line := range strings.Split(a.View().Content, "\n") {
				first, last := strings.Index(line, surface), strings.LastIndex(line, surface)
				if first < 0 {
					continue
				}
				rows++
				if n := defaultBgCells(line[first:last]); n > 0 {
					t.Errorf("row %d: %d popup cells show the terminal background", i, n)
				}
			}
			if rows == 0 {
				t.Fatal("no popup row found")
			}
		})
	}
}

// The startup splash fills the screen around its box with
// SplashBackground, so with the transparent background on it paints no
// background at all, and with it off it paints SurfaceDark.
func TestTransparentSplashHasNoFill(t *testing.T) {
	pinRenderGlobals(t)
	b := newWorkspaceBootstrap()
	b.SetWorkspaces([]string{"Acme"})

	off := b.Render(40, 10, "*")
	if !strings.Contains(off, "48;") {
		t.Fatal("with the option off the splash should paint a background")
	}

	a := newGoldenApp(t)
	makeTransparent(t, a, "dark")
	on := b.Render(40, 10, "*")
	if strings.Contains(on, "48;") {
		t.Errorf("transparent splash still paints a background:\n%q", on)
	}
	if !strings.Contains(on, "Connecting to Acme") {
		t.Error("splash lost its text")
	}
}
