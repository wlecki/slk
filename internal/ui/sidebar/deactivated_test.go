package sidebar

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/core"
)

func deactivatedFixture() []ChannelItem {
	return []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel"},
		{ID: "D1", Name: "Alice", Type: "dm", DMUserID: "U1"},
		{ID: "D2", Name: "Gone", Type: "dm", DMUserID: "U2"},
		{ID: "A1", Name: "Gonebot", Type: "app", DMUserID: "U3"},
		{ID: "G1", Name: "gone, alice", Type: "group_dm"},
	}
}

func visibleIDs(m *Model) []string {
	var ids []string
	for _, it := range m.VisibleItems() {
		ids = append(ids, it.ID)
	}
	// Sections order the rows; only membership matters here.
	sort.Strings(ids)
	return ids
}

func TestDeactivatedDMsAreHidden(t *testing.T) {
	m := New(deactivatedFixture())
	m.SetDeactivatedUsers(map[string]bool{"U2": true, "U3": true})
	// DM and app rows go; channels and group DMs (no single peer) stay.
	if got, want := visibleIDs(&m), []string{"C1", "D1", "G1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("visible = %v, want %v", got, want)
	}
	// Nil means show everyone again, which is how the option is turned off.
	m.SetDeactivatedUsers(nil)
	if got := visibleIDs(&m); len(got) != 5 {
		t.Fatalf("visible after clearing = %v, want all 5", got)
	}
}

func TestDeactivatedActiveConversationStays(t *testing.T) {
	m := New(deactivatedFixture())
	m.SetActiveChannelID("D2")
	m.SetDeactivatedUsers(map[string]bool{"U2": true})
	got := map[string]bool{}
	for _, id := range visibleIDs(&m) {
		got[id] = true
	}
	if !got["D2"] {
		t.Fatal("the active conversation was hidden")
	}
	m.SetActiveChannelID("C1")
	if got, want := visibleIDs(&m), []string{"A1", "C1", "D1", "G1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after leaving the deactivated DM visible = %v, want %v", got, want)
	}
}

// Starred, section-pinned and unread DMs get no exemption: Slack hides
// them too.
func TestDeactivatedHidesStarredAndUnreadDMs(t *testing.T) {
	items := deactivatedFixture()
	items[2].Section = "ST"
	m := New(items)
	m.SetReadStateReader(func() map[string]core.ReadState {
		return map[string]core.ReadState{"D2": {HasUnread: true, MentionCount: 2}}
	})
	m.SetSectionsProvider(&fakeProvider{ready: true, sections: []SectionMeta{{ID: "ST", Type: "stars"}}})
	m.SetDeactivatedUsers(map[string]bool{"U2": true})
	for _, id := range visibleIDs(&m) {
		if id == "D2" {
			t.Fatal("a starred DM with unreads stayed visible")
		}
	}
}

func TestDeactivatedKeepsCursorOnTheSameItem(t *testing.T) {
	for _, selected := range []string{"D1", "G1"} {
		m := New(deactivatedFixture())
		m.SelectByID(selected)
		m.SetDeactivatedUsers(map[string]bool{"U2": true, "U3": true})
		if m.SelectedID() != selected {
			t.Fatalf("selected %s after refilter, want %s", m.SelectedID(), selected)
		}
		m.SetDeactivatedUsers(nil)
		if m.SelectedID() != selected {
			t.Fatalf("selected %s after showing everyone, want %s", m.SelectedID(), selected)
		}
	}
	// A cursor on the row that disappears falls back like any other
	// removed row.
	m := New(deactivatedFixture())
	m.SelectByID("D2")
	m.SetDeactivatedUsers(map[string]bool{"U2": true})
	if m.SelectedID() == "D2" {
		t.Fatal("cursor stayed on a hidden row")
	}
}

// In Slack-sections mode (the default) the rendered sidebar leaves out a
// deactivated DM in the Direct Messages section, not just VisibleItems.
func TestDeactivatedDMIsNotRenderedInSlackSections(t *testing.T) {
	items := []ChannelItem{
		{ID: "C1", Name: "general", Type: "channel", Section: "CH"},
		{ID: "D1", Name: "Alice", Type: "dm", DMUserID: "U1", Section: "DMS"},
		{ID: "D2", Name: "Ghosted", Type: "dm", DMUserID: "U2", Section: "DMS"},
	}
	m := New(items)
	m.SetSectionsProvider(&fakeProvider{ready: true, sections: []SectionMeta{
		{ID: "CH", Name: "Channels", Type: "channels"},
		{ID: "DMS", Name: "Direct Messages", Type: "direct_messages"},
	}})
	if view := ansi.Strip(m.View(30, 40)); !strings.Contains(view, "Ghosted") {
		t.Fatalf("fixture: the DM is not rendered before hiding:\n%s", view)
	}
	m.SetDeactivatedUsers(map[string]bool{"U2": true})
	view := ansi.Strip(m.View(30, 40))
	if strings.Contains(view, "Ghosted") {
		t.Fatalf("deactivated DM still rendered in Slack-sections mode:\n%s", view)
	}
	if !strings.Contains(view, "Alice") {
		t.Fatalf("the other DM disappeared too:\n%s", view)
	}
}
