package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gammons/slk/internal/ui"
)

type fakeRemover struct {
	channel, ts string
	id          int
	deadline    time.Duration
	hasDeadline bool
	calls       int
	err         error
}

func (f *fakeRemover) DeleteAttachment(ctx context.Context, channelID, ts string, attachmentID int) error {
	f.calls++
	f.channel, f.ts, f.id = channelID, ts, attachmentID
	var d time.Time
	d, f.hasDeadline = ctx.Deadline()
	f.deadline = time.Until(d)
	return f.err
}

func TestRemoveAttachmentMapsFieldsAndBoundsTheCall(t *testing.T) {
	f := &fakeRemover{}
	got := removeAttachment(context.Background(), f, "T1", "C1", "1.000100", 2)
	if f.calls != 1 || f.channel != "C1" || f.ts != "1.000100" || f.id != 2 {
		t.Errorf("call = %+v, want one for C1 1.000100 #2", f)
	}
	if !f.hasDeadline || f.deadline <= 0 || f.deadline > removeAttachmentTimeout {
		t.Errorf("deadline in %v (set %v), want within %v", f.deadline, f.hasDeadline, removeAttachmentTimeout)
	}
	if want := (ui.AttachmentRemovedMsg{ChannelID: "C1", TS: "1.000100"}); got != want {
		t.Errorf("msg = %+v, want %+v", got, want)
	}
}

func TestRemoveAttachmentWithoutAClientSendsNothing(t *testing.T) {
	got := removeAttachment(context.Background(), nil, "T9", "C1", "1.0", 1)
	if got.ChannelID != "C1" || got.TS != "1.0" || got.Err == nil {
		t.Errorf("msg = %+v, want an error for C1 1.0", got)
	}
}

func TestRemoveAttachmentReturnsTheError(t *testing.T) {
	boom := errors.New("boom")
	got := removeAttachment(context.Background(), &fakeRemover{err: boom}, "T1", "C1", "1.0", 1)
	if !errors.Is(got.Err, boom) {
		t.Errorf("err = %v, want boom", got.Err)
	}
}
