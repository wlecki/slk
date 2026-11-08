package threadsview

import (
	"strings"
	"testing"

	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/ui/messages"
	"github.com/gammons/slk/internal/ui/styles"
)

// The selected card's tint must survive every reset in its header and
// preview, or the terminal shows through the selected row.
func TestTransparentSelectedCardKeepsTint(t *testing.T) {
	styles.SetTransparentBackground(true)
	styles.Apply("dark", config.Theme{})
	t.Cleanup(func() {
		styles.SetTransparentBackground(false)
		styles.Apply("dark", config.Theme{})
	})
	m := New(map[string]string{"U1": "alice"}, "U9")
	m.focused = true
	lines := m.renderCard(core.ThreadSummary{
		ChannelName: "general", ChannelType: "private", ParentUserID: "U1",
		ParentText: "hello *bold* world", ParentTS: "1700000000.000100",
		ReplyCount: 2, Unread: true,
	}, 60, true)
	tint := messages.SelectionTintBgANSI(true)
	for i, l := range lines[:2] {
		if strings.Contains(l, "\x1b[49m") {
			t.Errorf("line %d sets the default background: %q", i, l)
		}
		// Up to the last reset: the trailing one ends the row.
		body := l[:strings.LastIndex(l, "\x1b[m")]
		for rest := body; ; {
			j := strings.Index(rest, "\x1b[m")
			if j < 0 {
				break
			}
			rest = rest[j+len("\x1b[m"):]
			if rest != "" && !strings.HasPrefix(rest, tint) && !strings.HasPrefix(rest, "\x1b[m") {
				t.Errorf("line %d: a reset is not followed by the tint: ...%q", i, rest)
				break
			}
		}
	}
}
