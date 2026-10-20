// internal/ui/remove_preview.go
//
// x in normal mode: remove a link preview from the user's own message
// (Slack's chat.deleteAttachment). The key opens a confirm prompt; the
// confirmed press comes back as RemovePreviewMsg, which re-checks the
// message and calls MessageService.RemoveAttachment. On success nothing
// is done here: the message_changed echo updates the message (and
// clears the pending state, see reduceNewMessage).
//
// Slack renumbers the attachments after a removal, so an id is only
// good against the list it was read from. Three guards follow from
// that: one removal per message at a time (previewRemovals), a re-check
// of the id and its URL when the prompt is confirmed, and the team the
// key was pressed in travels with the request.
package ui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/gammons/slk/internal/bubbles/confirmprompt"
	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui/messages"
)

// previewRemovalTimeout is how long a removal blocks another on the same
// message when no echo or reload says it is over.
const previewRemovalTimeout = 30 * time.Second

// previewLabelWidth is the most cells a preview's name takes in a prompt.
const previewLabelWidth = 60

// RemovePreviewMsg is a confirmed removal: the prompt button's answer.
// URL is what the preview showed when the prompt was built.
type RemovePreviewMsg struct {
	TeamID       string
	ChannelID    string
	ThreadTS     string
	TS           string
	AttachmentID int
	URL          string
}

// previewRemoval is one removal in flight. sig names the message's
// previews as they were, so a reload that changes them ends the wait.
type previewRemoval struct {
	started time.Time
	sig     string
}

func previewRemovalKey(channelID, ts string) string { return channelID + "/" + ts }

// previewSignature names a preview list: ids and URLs, in order.
func previewSignature(ps []messages.RemovablePreview) string {
	var b strings.Builder
	for _, p := range ps {
		fmt.Fprintf(&b, "%d=%s\n", p.AttachmentID, p.URL)
	}
	return b.String()
}

// previewRemovalPending reports whether a removal for the message is in
// flight. One that outlived the timeout, or whose message no longer has
// the previews it started with (it was reloaded), is forgotten.
func (a *App) previewRemovalPending(channelID, ts string, previews []messages.RemovablePreview) bool {
	k := previewRemovalKey(channelID, ts)
	r, ok := a.previewRemovals[k]
	if !ok {
		return false
	}
	if a.now().Sub(r.started) >= previewRemovalTimeout || r.sig != previewSignature(previews) {
		delete(a.previewRemovals, k)
		return false
	}
	return true
}

func (a *App) startPreviewRemoval(channelID, ts string, previews []messages.RemovablePreview) {
	if a.previewRemovals == nil {
		a.previewRemovals = map[string]previewRemoval{}
	}
	a.previewRemovals[previewRemovalKey(channelID, ts)] = previewRemoval{started: a.now(), sig: previewSignature(previews)}
}

// clearPreviewRemoval ends the wait for a message.
func (a *App) clearPreviewRemoval(channelID, ts string) {
	delete(a.previewRemovals, previewRemovalKey(channelID, ts))
}

// previewLabel is p's name for a prompt, on one line.
func previewLabel(p messages.RemovablePreview) string {
	return ansi.Truncate(strings.Join(strings.Fields(p.Label()), " "), previewLabelWidth, "…")
}

// removePreviewOfSelected is x: confirm, then remove, a link preview of
// the selected message. One preview gets the yes/no prompt, several a
// prompt with a button each.
func (a *App) removePreviewOfSelected() tea.Cmd {
	msg, ref, ok := a.selectedMessageInFocus()
	if !ok {
		return nil
	}
	if !a.isOwnMessage(msg) {
		return toastWithClear(a, "You can only remove previews from your own messages", 3*time.Second)
	}
	previews := messages.RemovablePreviews(msg)
	if len(previews) == 0 {
		return toastWithClear(a, "No link preview on this message", 3*time.Second)
	}
	if a.previewRemovalPending(ref.channelID, ref.ts, previews) {
		return toastWithClear(a, "Removing preview…", 3*time.Second)
	}
	remove := func(p messages.RemovablePreview) func() tea.Msg {
		req := RemovePreviewMsg{
			TeamID: a.activeTeamID, ChannelID: ref.channelID, ThreadTS: ref.threadTS, TS: ref.ts,
			AttachmentID: p.AttachmentID, URL: p.URL,
		}
		return func() tea.Msg { return req }
	}
	if len(previews) == 1 {
		a.openConfirmPrompt("Remove preview?", previewLabel(previews[0]), remove(previews[0]))
		return nil
	}
	choice := confirmprompt.Choice{Title: "Remove which preview?", Default: -1}
	for i, p := range previews {
		b := confirmprompt.Button{Label: previewLabel(p), Action: remove(p)}
		if i < 9 {
			b.Keys = key.NewBinding(key.WithKeys(strconv.Itoa(i + 1)))
		}
		choice.Buttons = append(choice.Buttons, b)
	}
	choice.Buttons = append(choice.Buttons, confirmprompt.Button{Label: "Cancel"})
	a.openConfirmChoice(choice)
	return nil
}

// removePreview runs a confirmed removal if the message still shows the
// preview the prompt named.
func (a *App) removePreview(m RemovePreviewMsg) tea.Cmd {
	cur, ok := a.findActionMessage(actionPromptRef{channelID: m.ChannelID, ts: m.TS, threadTS: m.ThreadTS})
	if !ok {
		return toastWithClear(a, "That preview changed, try again", 3*time.Second)
	}
	previews := messages.RemovablePreviews(cur)
	found := false
	for _, p := range previews {
		found = found || (p.AttachmentID == m.AttachmentID && p.URL == m.URL)
	}
	if !found {
		return toastWithClear(a, "That preview changed, try again", 3*time.Second)
	}
	if a.previewRemovalPending(m.ChannelID, m.TS, previews) {
		return toastWithClear(a, "Removing preview…", 3*time.Second)
	}
	a.startPreviewRemoval(m.ChannelID, m.TS, previews)
	svc := a.messageSvc
	return func() tea.Msg {
		return svc.RemoveAttachment(m.TeamID, ids.ChannelID(m.ChannelID), ids.MessageTS(m.TS), m.AttachmentID)
	}
}

var reduceRemovePreview reducerFunc = func(a *App, msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case RemovePreviewMsg:
		return a.removePreview(m), true

	case AttachmentRemovedMsg:
		if m.Err == nil {
			// The message_changed echo shows the result.
			return nil, true
		}
		if errors.Is(m.Err, context.DeadlineExceeded) {
			// Slack may still have acted. The removal stays pending until
			// the echo, a reload or the timeout: a resend could hit an id
			// Slack has renumbered.
			return toastWithClear(a, "Removing the preview timed out, check the message", 4*time.Second), true
		}
		a.clearPreviewRemoval(m.ChannelID, m.TS)
		return toastWithClear(a, "Could not remove the preview: "+m.Err.Error(), 4*time.Second), true
	}
	return nil, false
}
