package slackclient

import (
	"context"
	"net/url"
	"strconv"

	"github.com/gammons/slk/internal/debuglog"
	"github.com/gammons/slk/internal/slackhttp"
)

// DeleteAttachment removes one legacy attachment (a link preview) from a
// message of the user's own, as Slack's web client does
// (chat.deleteAttachment, undocumented). attachmentID is the
// attachment's own id; Slack renumbers the rest afterwards, so an id is
// only good against the list it was read from. Slack then sends a
// message_changed over the WebSocket, so only the error comes back.
//
// No retry, a 429 included (postForm returns it as one error): a lost
// answer is ambiguous, and a resend can hit a renumbered id. Callers must
// not wrap this in WaitOutRateLimit.
func (c *Client) DeleteAttachment(ctx context.Context, channelID, ts string, attachmentID int) error {
	// The _x_reason emacs-slack sends; see the test for what is known.
	ctx = slackhttp.WithReason(ctx, "delete-single-attachment")
	form := url.Values{
		"channel":    {channelID},
		"ts":         {ts},
		"attachment": {strconv.Itoa(attachmentID)},
	}
	debuglog.General("chat.deleteAttachment: channel=%s ts=%s attachment=%d", channelID, ts, attachmentID)
	raw, err := c.postForm(ctx, "chat.deleteAttachment", form)
	if err == nil {
		err = parseOKResponse("chat.deleteAttachment", raw)
	}
	if err != nil {
		debuglog.General("chat.deleteAttachment: error=%v", err)
		return err
	}
	debuglog.General("chat.deleteAttachment: ok")
	return nil
}
