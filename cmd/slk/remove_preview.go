package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gammons/slk/internal/ids"
	"github.com/gammons/slk/internal/ui"
)

// removeAttachmentTimeout bounds one chat.deleteAttachment call.
const removeAttachmentTimeout = 10 * time.Second

// attachmentRemover is the part of *slackclient.Client removing a link
// preview needs.
type attachmentRemover interface {
	DeleteAttachment(ctx context.Context, channelID, ts string, attachmentID int) error
}

// removeAttachment removes attachment attachmentID (a link preview) from
// a message through client, the client of workspace teamID, and reports
// the result for the UI. client is nil when that workspace is gone: the
// user may have switched or signed out since pressing the key.
func removeAttachment(ctx context.Context, client attachmentRemover, teamID string,
	channelID ids.ChannelID, ts ids.MessageTS, attachmentID int) ui.AttachmentRemovedMsg {
	chIDStr, tsStr := string(channelID), string(ts)
	if client == nil {
		return ui.AttachmentRemovedMsg{ChannelID: chIDStr, TS: tsStr, Err: fmt.Errorf("workspace %q is unavailable", teamID)}
	}
	ctx, cancel := context.WithTimeout(ctx, removeAttachmentTimeout)
	defer cancel()
	// One attempt, never WaitOutRateLimit: see DeleteAttachment.
	err := client.DeleteAttachment(ctx, chIDStr, tsStr, attachmentID)
	if err != nil {
		log.Printf("Warning: failed to remove preview %s/%s #%d: %v", chIDStr, tsStr, attachmentID, err)
	}
	return ui.AttachmentRemovedMsg{ChannelID: chIDStr, TS: tsStr, Err: err}
}
