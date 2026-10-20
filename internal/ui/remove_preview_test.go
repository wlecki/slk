package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/gammons/slk/internal/core"
	"github.com/gammons/slk/internal/core/blocks"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

type removeCall struct {
	team, channel, ts string
	id                int
}

func card(id int, title, url string) blocks.LegacyAttachment {
	return blocks.LegacyAttachment{ID: id, Title: title, FromURL: url}
}

func ownMessage(ts string, atts ...blocks.LegacyAttachment) messages.MessageItem {
	return messages.MessageItem{TS: ts, UserID: "U1", UserName: "me", Text: "links", LegacyAttachments: atts}
}

// previewApp is an App in T1 on C1 whose first message is selected and
// focused, with the user U1. Removals are recorded and answered with err.
func previewApp(t *testing.T, err error, msgs ...messages.MessageItem) (*App, *[]removeCall) {
	t.Helper()
	a := newTestApp(t, withSize(120, 40), withActiveChannel("C1"), withMessages(msgs...))
	a.currentUserID = "U1"
	a.activeTeamID = "T1"
	a.focusedPanel = PanelMessages
	a.messagepane.SelectByIndex(0)
	calls := &[]removeCall{}
	a.setAttachmentRemoverForTest(func(team string, ch ids.ChannelID, ts ids.MessageTS, id int) core.Msg {
		*calls = append(*calls, removeCall{team, string(ch), string(ts), id})
		return AttachmentRemovedMsg{ChannelID: string(ch), TS: string(ts), Err: err}
	})
	return a, calls
}

// pressKeyFast is pressKey, but it feeds every answer back through Update
// and does not wait for toast clear ticks: a
// command that has not answered within 100ms is a timer and is dropped,
// which leaves the toast on the status bar to be read.
func pressKeyFast(t *testing.T, a *App, k tea.KeyMsg) []tea.Msg {
	t.Helper()
	var run func(cmd tea.Cmd) []tea.Msg
	run = func(cmd tea.Cmd) []tea.Msg {
		if cmd == nil {
			return nil
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- cmd() }()
		select {
		case m := <-done:
			if cmds, ok := cmdListOf(m); ok {
				var out []tea.Msg
				for _, c := range cmds {
					out = append(out, run(c)...)
				}
				return out
			}
			return []tea.Msg{m}
		case <-time.After(100 * time.Millisecond):
			return nil
		}
	}
	_, cmd := a.Update(k)
	var out []tea.Msg
	queue := run(cmd)
	for len(queue) > 0 {
		m := queue[0]
		queue = queue[1:]
		if m == nil {
			continue
		}
		out = append(out, m)
		_, next := a.Update(m)
		queue = append(queue, run(next)...)
	}
	return out
}

// toastShows reports whether the status bar currently shows text.
func toastShows(a *App, text string) bool {
	return strings.Contains(statusbarText(a), text)
}

func TestRemovePreview_OnePreviewConfirmSends(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("1.0", card(1, "Example", "https://example.com")))
	pressKeyFast(t, a, keyPress('x'))
	if a.mode != ModeConfirm || !dialogShows(a, "Remove preview?") || !dialogShows(a, "Example") {
		t.Fatalf("mode = %v; want the prompt naming the preview", a.mode)
	}
	if len(*calls) != 0 {
		t.Fatal("the prompt sent before it was confirmed")
	}
	if toast := toastText(pressKeyFast(t, a, keyPress('y'))); toast != "" {
		t.Errorf("a successful removal toasted %q", toast)
	}
	want := []removeCall{{"T1", "C1", "1.0", 1}}
	if len(*calls) != 1 || (*calls)[0] != want[0] {
		t.Errorf("calls = %+v, want %+v", *calls, want)
	}
	if a.mode != ModeNormal {
		t.Errorf("mode = %v after confirming", a.mode)
	}
}

func TestRemovePreview_CancelSendsNothing(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("1.0", card(1, "Example", "https://example.com")))
	pressKeyFast(t, a, keyPress('x'))
	pressKeyFast(t, a, keyPress('n'))
	if len(*calls) != 0 || a.mode != ModeNormal {
		t.Errorf("calls = %+v, mode = %v; cancel must send nothing", *calls, a.mode)
	}
}

func TestRemovePreview_UsesURLWhenThereIsNoTitle(t *testing.T) {
	a, _ := previewApp(t, nil, ownMessage("1.0", card(1, "", "https://example.com/a")))
	pressKeyFast(t, a, keyPress('x'))
	if !dialogShows(a, "https://example.com/a") {
		t.Errorf("prompt does not name the URL:\n%s", a.confirmPrompt.View())
	}
}

func TestRemovePreview_SeveralPreviewsOffersEach(t *testing.T) {
	msg := ownMessage("1.0",
		card(1, "First", "https://a.example"),
		blocks.LegacyAttachment{ID: 2, Title: "A bot card"}, // not a preview
		card(3, "Third", "https://c.example"),
	)
	a, calls := previewApp(t, nil, msg)
	pressKeyFast(t, a, keyPress('x'))
	for _, want := range []string{"[1] First", "[2] Third", "[Cancel]"} {
		if !dialogShows(a, want) {
			t.Errorf("prompt missing %q:\n%s", want, a.confirmPrompt.View())
		}
	}
	if dialogShows(a, "A bot card") {
		t.Error("a card without a URL was offered")
	}
	pressKeyFast(t, a, keyPress('2'))
	if len(*calls) != 1 || (*calls)[0].id != 3 {
		t.Errorf("calls = %+v, want one for attachment 3", *calls)
	}
}

func TestRemovePreview_SeveralPreviewsCancelButton(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("1.0", card(1, "First", "https://a.example"), card(2, "Second", "https://b.example")))
	pressKeyFast(t, a, keyPress('x'))
	clickConfirmText(t, a, "Cancel")
	if len(*calls) != 0 || a.confirmPrompt.IsVisible() {
		t.Errorf("calls = %+v, visible = %v; Cancel must close without sending", *calls, a.confirmPrompt.IsVisible())
	}
}

func TestRemovePreview_NoPreviewToasts(t *testing.T) {
	for name, msg := range map[string]messages.MessageItem{
		"no attachments": ownMessage("1.0"),
		"a bot card":     ownMessage("1.0", blocks.LegacyAttachment{ID: 1, Title: "Deploy ok"}),
	} {
		t.Run(name, func(t *testing.T) {
			a, calls := previewApp(t, nil, msg)
			if got := toastText(pressKeyFast(t, a, keyPress('x'))); got != "" {
				// toastWithClear sets the status bar directly.
				t.Errorf("unexpected ToastMsg %q", got)
			}
			if a.confirmPrompt.IsVisible() || len(*calls) != 0 {
				t.Error("prompted or sent with no preview")
			}
			if !toastShows(a, "No link preview on this message") {
				t.Errorf("toast does not show %q", "No link preview on this message")
			}
		})
	}
}

func TestRemovePreview_NotOwnMessage(t *testing.T) {
	msg := ownMessage("1.0", card(1, "Example", "https://example.com"))
	msg.UserID = "U2"
	a, calls := previewApp(t, nil, msg)
	pressKeyFast(t, a, keyPress('x'))
	if a.confirmPrompt.IsVisible() || len(*calls) != 0 {
		t.Error("prompted for someone else's message")
	}
	if !toastShows(a, "You can only remove previews from your own messages") {
		t.Errorf("toast does not show %q", "You can only remove previews from your own messages")
	}
}

func TestRemovePreview_RefusedOnEphemeral(t *testing.T) {
	msg := ownMessage("1.0", card(1, "Example", "https://example.com"))
	msg.Ephemeral = true
	a, calls := previewApp(t, nil, msg)
	toast := toastText(pressKeyFast(t, a, keyPress('x')))
	if toast != "Only visible to you" || a.confirmPrompt.IsVisible() || len(*calls) != 0 {
		t.Errorf("toast = %q, prompt = %v, calls = %+v; want the ephemeral refusal", toast, a.confirmPrompt.IsVisible(), *calls)
	}
}

// While a removal is in flight, a second x on that message does nothing:
// Slack renumbers the attachments, so a second id could name another one.
func TestRemovePreview_SecondPressWhilePendingIsRefused(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("1.0", card(1, "One", "https://a.example"), card(2, "Two", "https://b.example")))
	pressKeyFast(t, a, keyPress('x'))
	pressKeyFast(t, a, keyPress('1'))
	pressKeyFast(t, a, keyPress('x'))
	if a.confirmPrompt.IsVisible() || len(*calls) != 1 {
		t.Errorf("prompt = %v, calls = %d; the second x must do nothing", a.confirmPrompt.IsVisible(), len(*calls))
	}
	if !toastShows(a, "Removing preview…") {
		t.Errorf("toast does not show %q", "Removing preview…")
	}
}

// The echo that carries the new attachment list ends the wait.
func TestRemovePreview_EchoEndsPending(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("1.0", card(1, "One", "https://a.example"), card(2, "Two", "https://b.example")))
	pressKeyFast(t, a, keyPress('x'))
	pressKeyFast(t, a, keyPress('1'))

	a.Update(NewMessageMsg{ChannelID: "C1", Changed: true, Message: ownMessage("1.0", card(1, "Two", "https://b.example"))})
	pressKeyFast(t, a, keyPress('x'))
	if !dialogShows(a, "Remove preview?") {
		t.Fatal("x after the echo should prompt again")
	}
	pressKeyFast(t, a, keyPress('y'))
	// Ids were renumbered by Slack: the remaining card is now 1.
	if len(*calls) != 2 || (*calls)[1].id != 1 {
		t.Errorf("calls = %+v, want a second call for the renumbered id 1", *calls)
	}
}

func TestRemovePreview_FailureToastsAndClearsPending(t *testing.T) {
	a, calls := previewApp(t, errors.New("cant_delete_message"), ownMessage("1.0", card(1, "One", "https://a.example")))
	pressKeyFast(t, a, keyPress('x'))
	pressKeyFast(t, a, keyPress('y'))
	if !toastShows(a, "Could not remove the preview: cant_delete_message") {
		t.Errorf("toast does not show %q", "Could not remove the preview: cant_delete_message")
	}
	pressKeyFast(t, a, keyPress('x'))
	if !dialogShows(a, "Remove preview?") {
		t.Error("a failed removal must not stay pending")
	}
	if len(*calls) != 1 {
		t.Errorf("calls = %d, want 1", len(*calls))
	}
}

// A call that timed out may still have happened: it stays pending until
// the echo or the timeout, and the toast says to check the message.
func TestRemovePreview_TimeoutStaysPendingUntilThirtySeconds(t *testing.T) {
	a, _ := previewApp(t, context.DeadlineExceeded, ownMessage("1.0", card(1, "One", "https://a.example")))
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	pressKeyFast(t, a, keyPress('x'))
	pressKeyFast(t, a, keyPress('y'))
	if !toastShows(a, "Removing the preview timed out, check the message") {
		t.Errorf("toast does not show %q", "Removing the preview timed out, check the message")
	}

	now = now.Add(29 * time.Second)
	pressKeyFast(t, a, keyPress('x'))
	if a.confirmPrompt.IsVisible() || !toastShows(a, "Removing preview…") {
		t.Error("still pending at 29s")
	}
	now = now.Add(2 * time.Second)
	pressKeyFast(t, a, keyPress('x'))
	if !dialogShows(a, "Remove preview?") {
		t.Error("pending must end after 30s")
	}
}

// A reload that replaces the message's attachments ends the wait too.
func TestRemovePreview_ReloadEndsPending(t *testing.T) {
	a, _ := previewApp(t, context.DeadlineExceeded, ownMessage("1.0", card(1, "One", "https://a.example"), card(2, "Two", "https://b.example")))
	pressKeyFast(t, a, keyPress('x'))
	pressKeyFast(t, a, keyPress('1'))

	a.messagepane.SetMessages([]messages.MessageItem{ownMessage("1.0", card(1, "Two", "https://b.example"))})
	a.messagepane.SelectByIndex(0)
	pressKeyFast(t, a, keyPress('x'))
	if !dialogShows(a, "Remove preview?") {
		t.Error("a reload that changed the previews must end the wait")
	}
}

// The prompt was built against one attachment list; if the message
// changed underneath it, confirming must not send the old id.
func TestRemovePreview_ConfirmAfterTheMessageChangedIsRefused(t *testing.T) {
	for name, changed := range map[string]messages.MessageItem{
		"preview gone":       ownMessage("1.0"),
		"id now has another": ownMessage("1.0", card(1, "Other", "https://other.example")),
		"renumbered":         ownMessage("1.0", card(1, "Second", "https://b.example")),
	} {
		t.Run(name, func(t *testing.T) {
			a, calls := previewApp(t, nil, ownMessage("1.0", card(1, "First", "https://a.example"), card(2, "Second", "https://b.example")))
			pressKeyFast(t, a, keyPress('x'))
			a.Update(NewMessageMsg{ChannelID: "C1", Changed: true, Message: changed})
			pressKeyFast(t, a, keyPress('1')) // "First": id 1, https://a.example
			if len(*calls) != 0 {
				t.Errorf("calls = %+v; a stale confirm must not send", *calls)
			}
			if !toastShows(a, "That preview changed, try again") {
				t.Errorf("toast does not show %q", "That preview changed, try again")
			}
		})
	}
}

// The request names the workspace the key was pressed in, not the one
// that is active when the prompt is confirmed.
func TestRemovePreview_RequestCarriesTheTeamOfThePress(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("1.0", card(1, "One", "https://a.example")))
	pressKeyFast(t, a, keyPress('x'))
	a.activeTeamID = "T2"
	pressKeyFast(t, a, keyPress('y'))
	if len(*calls) != 1 || (*calls)[0].team != "T1" {
		t.Errorf("calls = %+v, want team T1", *calls)
	}
}

func TestRemovePreview_ThreadReply(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("P1"))
	a.threadPanel.SetThread(ownMessage("P1"), []messages.MessageItem{ownMessage("R1", card(1, "One", "https://a.example"))}, "C1", "P1")
	a.threadVisible = true
	a.focusedPanel = PanelThread
	pressKeyFast(t, a, keyPress('x'))
	pressKeyFast(t, a, keyPress('y'))
	if len(*calls) != 1 || (*calls)[0] != (removeCall{"T1", "C1", "R1", 1}) {
		t.Errorf("calls = %+v, want one for reply R1", *calls)
	}
}

func TestRemovePreview_IDIsThePositionWhenSlackSentNone(t *testing.T) {
	a, calls := previewApp(t, nil, ownMessage("1.0",
		blocks.LegacyAttachment{Title: "bot"},
		blocks.LegacyAttachment{Title: "Second", FromURL: "https://b.example"},
	))
	pressKeyFast(t, a, keyPress('x'))
	pressKeyFast(t, a, keyPress('y'))
	if len(*calls) != 1 || (*calls)[0].id != 2 {
		t.Errorf("calls = %+v, want attachment 2 (its position)", *calls)
	}
}
