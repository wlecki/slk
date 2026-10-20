package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/config"
	"github.com/gammons/slk/internal/core"
	slackclient "github.com/gammons/slk/internal/slack"
	"github.com/gammons/slk/internal/slack/edge"
	"github.com/gammons/slk/internal/ui"
	"github.com/gammons/slk/internal/ui/sidebar"
	"github.com/slack-go/slack"
)

func deletedIM(id, user string, deleted bool) slack.Channel {
	return slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{
		ID: id, IsIM: true, User: user, IsUserDeleted: deleted,
	}}}
}

func deactivatedMsgs(sent []tea.Msg) []ui.UserDeactivatedMsg {
	var out []ui.UserDeactivatedMsg
	for _, m := range sent {
		if d, ok := m.(ui.UserDeactivatedMsg); ok {
			out = append(out, d)
		}
	}
	return out
}

func TestBuildChannelItemCarriesDMUserIDToTheFinder(t *testing.T) {
	wctx := &WorkspaceContext{UserNames: newUserNameStore(map[string]string{"U1": "alice"})}
	item, finder := buildChannelItem(deletedIM("D1", "U1", true), wctx, config.Config{}, "T1")
	if item.DMUserID != "U1" || finder.DMUserID != "U1" {
		t.Fatalf("DMUserID: sidebar %q, finder %q; want U1 for both", item.DMUserID, finder.DMUserID)
	}
	// buildChannelItem stays pure: it reads the flag from nowhere.
	ch := slack.Channel{GroupConversation: slack.GroupConversation{Conversation: slack.Conversation{ID: "C1"}, Name: "general"}}
	if _, finder := buildChannelItem(ch, wctx, config.Config{}, "T1"); finder.DMUserID != "" {
		t.Fatalf("a channel got DMUserID %q", finder.DMUserID)
	}
}

func TestRecordUserDeletedReportsFreshObservations(t *testing.T) {
	for _, tc := range []struct {
		name       string
		row        bool // a cached row exists
		wasDeleted bool
		deleted    bool
		wantSent   bool
		wantCached bool
	}{
		{"deactivated, row", true, false, true, true, true},
		{"deactivated again", true, true, true, true, true},
		{"deactivated, no row", false, false, true, true, false},
		{"reactivated", true, true, false, true, false},
		{"active, was active", true, false, false, false, false},
		{"active, no row", false, false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			if tc.row {
				if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", Deleted: tc.wasDeleted}); err != nil {
					t.Fatal(err)
				}
			}
			sender := &captureSender{}
			recordUserDeleted(db, sender.Send, "U1", tc.deleted)
			msgs := deactivatedMsgs(sender.sent)
			if (len(msgs) == 1) != tc.wantSent || len(msgs) > 1 {
				t.Fatalf("sent %v, want sent = %v", msgs, tc.wantSent)
			}
			if tc.wantSent && (msgs[0].UserID != "U1" || msgs[0].Deactivated != tc.deleted) {
				t.Fatalf("sent %+v", msgs[0])
			}
			set, err := db.DeactivatedUsers()
			if err != nil || set["U1"] != tc.wantCached {
				t.Fatalf("cached deactivated = %v, %v; want U1 = %v", set, err, tc.wantCached)
			}
			if !tc.row {
				if _, err := db.GetUser("U1"); err == nil {
					t.Fatal("recordUserDeleted created a row")
				}
			}
		})
	}
}

func TestRecordUserDeletedToleratesNoDBOrSender(t *testing.T) {
	recordUserDeleted(nil, nil, "U1", true)
	recordUserDeleted(newTestDB(t), nil, "U1", true)
}

func TestOpeningADeactivatedDMMarksTheUserAndTellsTheUI(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", DisplayName: "Gone"}); err != nil {
		t.Fatal(err)
	}
	wctx := &WorkspaceContext{UserNames: newUserNameStore(map[string]string{"U1": "Gone"})}
	sender := &captureSender{}
	h := &rtmEventHandler{workspaceID: "T1", wsCtx: wctx, db: db, program: sender}

	h.OnConversationOpened(deletedIM("D1", "U1", true))

	set, _ := db.DeactivatedUsers()
	if !set["U1"] {
		t.Fatalf("the cache does not know U1 is deactivated: %v", set)
	}
	if !wctx.DeactivatedUsers(db)["U1"] {
		t.Fatal("the workspace does not report U1 deactivated")
	}
	if len(sender.sent) != 2 {
		t.Fatalf("sent %d messages, want the flag then the conversation: %+v", len(sender.sent), sender.sent)
	}
	if d, ok := sender.sent[0].(ui.UserDeactivatedMsg); !ok || d.UserID != "U1" || !d.Deactivated {
		t.Fatalf("first message = %+v, want UserDeactivatedMsg for U1", sender.sent[0])
	}
	if _, ok := sender.sent[1].(ui.ConversationOpenedMsg); !ok {
		t.Fatalf("second message = %+v", sender.sent[1])
	}
	if got := wctx.FinderItems[0].DMUserID; got != "U1" {
		t.Fatalf("finder DMUserID = %q", got)
	}
}

// A conversation only ever adds. An IM that does not carry the flag says
// nothing about the user, so it must not clear a deletion a profile reported.
func TestAConversationWithoutTheFlagDoesNotUndoADeletion(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", Deleted: true}); err != nil {
		t.Fatal(err)
	}
	wctx := &WorkspaceContext{UserNames: newUserNameStore(nil)}
	sender := &captureSender{}
	h := &rtmEventHandler{workspaceID: "T1", wsCtx: wctx, db: db, program: sender}
	h.OnConversationOpened(deletedIM("D1", "U1", false))
	if set, _ := db.DeactivatedUsers(); !set["U1"] {
		t.Fatal("an IM without the flag cleared the cache")
	}
	if len(deactivatedMsgs(sender.sent)) != 0 {
		t.Fatalf("an unflagged IM sent %+v", sender.sent)
	}
}

func TestADeactivatedPeerWithNoRowIsStillReported(t *testing.T) {
	db := newTestDB(t)
	wctx := &WorkspaceContext{UserNames: newUserNameStore(nil)}
	if !wctx.recordDeactivatedPeer(deletedIM("D1", "U9", true), db) {
		t.Fatal("a flagged IM was not recorded")
	}
	if wctx.recordDeactivatedPeer(deletedIM("D2", "U8", false), db) {
		t.Fatal("an unflagged IM was recorded")
	}
	set := wctx.DeactivatedUsers(db)
	if !set["U9"] || set["U8"] || len(set) != 1 {
		t.Fatalf("DeactivatedUsers = %v, want only U9", set)
	}
	if cached, _ := db.DeactivatedUsers(); len(cached) != 0 {
		t.Fatalf("a row-less peer reached the cache: %v", cached)
	}
}

// Once a profile has given the peer a row, the cache decides: a reactivation
// must not be undone by the remembered conversation flag when the workspace
// is switched back to.
func TestAProfileOverridesARememberedDeactivatedPeer(t *testing.T) {
	db := newTestDB(t)
	wctx := &WorkspaceContext{}
	wctx.recordDeactivatedPeer(deletedIM("D1", "U1", true), db)
	if !wctx.DeactivatedUsers(db)["U1"] {
		t.Fatal("a row-less flagged peer must be in the set")
	}
	if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", Deleted: false}); err != nil {
		t.Fatal(err)
	}
	if wctx.DeactivatedUsers(db)["U1"] {
		t.Fatal("the remembered flag overrode a profile that shows the user active")
	}
	if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", Deleted: true}); err != nil {
		t.Fatal(err)
	}
	if !wctx.DeactivatedUsers(db)["U1"] {
		t.Fatal("a profile that shows the user deactivated must keep them in the set")
	}
}

func TestWorkspaceDeactivatedUsersUnionsTheCacheAndPeersAndIsCallerOwned(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", Deleted: true}); err != nil {
		t.Fatal(err)
	}
	wctx := &WorkspaceContext{}
	wctx.recordDeactivatedPeer(deletedIM("D2", "U2", true), db)
	set := wctx.DeactivatedUsers(db)
	if !set["U1"] || !set["U2"] || len(set) != 2 {
		t.Fatalf("set = %v", set)
	}
	delete(set, "U1")
	if !wctx.DeactivatedUsers(db)["U1"] {
		t.Fatal("the set was not caller-owned")
	}
	if got := (*WorkspaceContext)(nil).DeactivatedUsers(db); got == nil {
		t.Fatal("a nil workspace must still yield a usable set")
	}
}

func TestDMPeerRepairKeepsDMUserIDOnFinderItems(t *testing.T) {
	db := newTestDB(t)
	if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1", DisplayName: "Alice"}); err != nil {
		t.Fatal(err)
	}
	wctx := &WorkspaceContext{
		UserNames: newUserNameStore(map[string]string{"U1": "Alice"}),
		Channels:  []sidebar.ChannelItem{{ID: "D1", Name: "U1", Type: "dm", DMUserID: "U1"}},
		// A missing finder entry is recovered by the repair.
	}
	h := &rtmEventHandler{workspaceID: "T1", wsCtx: wctx, db: db, program: &captureSender{}}
	h.refreshDMPeerFromCache("U1")
	if len(wctx.FinderItems) != 1 || wctx.FinderItems[0].DMUserID != "U1" {
		t.Fatalf("the recovered finder row lost the peer: %+v", wctx.FinderItems)
	}
}

func TestUpsertFinderItemKeepsDMUserID(t *testing.T) {
	wctx := &WorkspaceContext{FinderItems: []core.ChannelFinderItem{{ID: "D1", Name: "x", Type: "dm", DMUserID: "U1", Joined: true}}}
	h := &rtmEventHandler{wsCtx: wctx}
	got, _ := h.upsertFinderItem(core.ChannelFinderItem{ID: "D1", Name: "Alice", Type: "dm", Joined: true})
	if got.DMUserID != "U1" || wctx.FinderItems[0].DMUserID != "U1" || wctx.FinderItems[0].Name != "Alice" {
		t.Fatalf("an update that omitted DMUserID erased it: %+v", wctx.FinderItems[0])
	}
}

func TestProfileWritersPersistAndReportDeactivation(t *testing.T) {
	for _, mode := range []string{"per-user", "edge batch"} {
		t.Run(mode, func(t *testing.T) {
			db := newTestDB(t)
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U1","name":"gone","deleted":true,"team_id":"T1","profile":{"display_name":"Gone"}}}`))
			}))
			defer srv.Close()
			var batcher userBatcher
			if mode == "edge batch" {
				rec := edgeUserRecord("U1", "gone", "Gone", "", "T1", 1, false)
				rec.Deleted = true
				batcher = &fakeBatcher{res: []edge.User{rec}}
			}
			sender := &captureSender{}
			r := newUserResolver("T1", newTestClient(t, srv), db, nil, sender.Send, batcher, nil)
			if mode == "per-user" {
				r.resolveOne("U1")
			} else {
				r.ResolveNow([]string{"U1"})
			}
			if set, _ := db.DeactivatedUsers(); !set["U1"] {
				t.Fatalf("the resolved profile's flag was not cached: %v", set)
			}
			msgs := deactivatedMsgs(sender.sent)
			if len(msgs) != 1 || msgs[0].UserID != "U1" || !msgs[0].Deactivated {
				t.Fatalf("sent %+v", msgs)
			}

			// A later profile that shows the user active again clears
			// the flag and tells the UI.
			rec := edgeUserRecord("U1", "gone", "Gone", "", "T1", 2, false)
			r2 := newUserResolver("T1", nil, db, nil, sender.Send, &fakeBatcher{res: []edge.User{rec}}, nil)
			r2.ResolveNow([]string{"U1"})
			if set, _ := db.DeactivatedUsers(); set["U1"] {
				t.Fatalf("a reactivation left the flag set: %v", set)
			}
			msgs = deactivatedMsgs(sender.sent)
			if last := msgs[len(msgs)-1]; last.UserID != "U1" || last.Deactivated {
				t.Fatalf("last message %+v, want U1 reactivated", last)
			}
		})
	}
}

func deletedProfile(id string) *slackclient.UserProfile {
	return &slackclient.UserProfile{User: &slack.User{ID: id, Name: "gone", Deleted: true}}
}

// The two profile observations that bypass the writers still record the
// flag: the peer-status users.info fallback, and the avatar backfill even
// when users.info has no image to backfill.
func TestBypassingProfileObservationsRecordTheFlag(t *testing.T) {
	t.Run("peer status fallback", func(t *testing.T) {
		db := newTestDB(t)
		if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1"}); err != nil {
			t.Fatal(err)
		}
		sender := &captureSender{}
		p := newPeerStatusRefresher("T1", "USELF", nil, nil, nil,
			func(string) (*slackclient.UserProfile, error) { return deletedProfile("U1"), nil }, db, sender.Send)
		p.fallbackResolve("U1", time.Now())
		if set, _ := db.DeactivatedUsers(); !set["U1"] {
			t.Fatalf("the flag was not cached: %v", set)
		}
		if msgs := deactivatedMsgs(sender.sent); len(msgs) != 1 || !msgs[0].Deactivated {
			t.Fatalf("sent %+v", sender.sent)
		}
	})
	t.Run("avatar backfill", func(t *testing.T) {
		db := newTestDB(t)
		if err := db.UpsertUser(cache.User{ID: "U1", WorkspaceID: "T1"}); err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"U1","name":"gone","deleted":true,"profile":{}}}`))
		}))
		defer srv.Close()
		sender := &captureSender{}
		r := newUserResolver("T1", newTestClient(t, srv), db, nil, sender.Send, nil, nil)
		r.backfillAvatar("U1")
		if set, _ := db.DeactivatedUsers(); !set["U1"] {
			t.Fatalf("the flag was not cached: %v", set)
		}
		if msgs := deactivatedMsgs(sender.sent); len(msgs) != 1 {
			t.Fatalf("sent %+v", sender.sent)
		}
	})
}
