package main

import (
	tea "charm.land/bubbletea/v2"
	"github.com/gammons/slk/internal/cache"
	"github.com/gammons/slk/internal/ui"
	"github.com/slack-go/slack"
)

// Deactivated users reach the UI from two kinds of source. A conversation
// object (users.conversations, conversations.info, im_created, im_open)
// carries is_user_deleted on an IM: it can only add a user, and it creates
// no cache row. A profile (users.info, edge users/info, bootstrap
// revalidation) is authoritative in both directions and is written to
// users.deleted by the full-profile writers.

// cachedUserDeleted reports the cached flag. Read it before a profile
// writer overwrites it: a reactivation is reported only for a user the
// cache had as deleted. No row, or no cache, is "not deleted".
func cachedUserDeleted(db *cache.DB, userID string) bool {
	if db == nil {
		return false
	}
	u, err := db.GetUser(userID)
	return err == nil && u.Deleted
}

// announceUserDeleted tells the UI about a fresh profile observation. A
// deactivated user is sent every time (idempotent, and the UI may not have
// seen the cache's earlier answer); an active one only when the cache had
// them as deleted, which is a reactivation. The message names no
// workspace: the account is deactivated everywhere.
func announceUserDeleted(send func(tea.Msg), userID string, was, now bool) {
	if send == nil || userID == "" || (!now && !was) {
		return
	}
	send(ui.UserDeactivatedMsg{UserID: userID, Deactivated: now})
}

// recordUserDeleted persists and reports a profile observation that
// bypasses the full-profile writers (the peer-status users.info fallback,
// the avatar backfill, the profile dialog's fetch), which would otherwise
// drop the flag. It never creates a cache row.
func recordUserDeleted(db *cache.DB, send func(tea.Msg), userID string, deleted bool) {
	was := cachedUserDeleted(db, userID)
	if db != nil {
		if deleted {
			_ = db.MarkUserDeleted(userID)
		} else if was {
			_ = db.ClearUserDeleted(userID)
		}
	}
	announceUserDeleted(send, userID, was, deleted)
}

// recordDeactivatedPeer notes the peer of an IM Slack flagged
// is_user_deleted and reports whether it was one. It remembers the peer for
// this run, because a peer with no cache row cannot be marked there, and
// marks the row when there is one. It only ever adds: an IM without the flag
// says nothing, and only a profile can clear a deletion. buildChannelItem
// stays pure, so each place that builds conversations calls this.
func (w *WorkspaceContext) recordDeactivatedPeer(ch slack.Channel, db *cache.DB) bool {
	if w == nil || !ch.IsIM || !ch.IsUserDeleted || ch.User == "" {
		return false
	}
	w.deletedPeers.Store(ch.User, struct{}{})
	if db != nil {
		_ = db.MarkUserDeleted(ch.User)
	}
	return true
}

// DeactivatedUsers is the set sent with WorkspaceReadyMsg and
// WorkspaceSwitchedMsg: every user the cache has as deactivated plus this
// workspace's IM peers Slack flagged. The caller owns the map.
//
// A flagged peer counts only while the cache has no row for it. Once a row
// exists the cache is the answer: recordDeactivatedPeer marked it when the
// row was there, and a later profile overwrites it, so a reactivation is not
// undone by the remembered conversation flag on the next workspace switch.
func (w *WorkspaceContext) DeactivatedUsers(db *cache.DB) map[string]bool {
	set := map[string]bool{}
	if db != nil {
		if cached, err := db.DeactivatedUsers(); err == nil {
			set = cached
		}
	}
	if w != nil {
		w.deletedPeers.Range(func(key, _ any) bool {
			id := key.(string)
			if db != nil {
				if _, err := db.GetUser(id); err == nil {
					return true
				}
			}
			set[id] = true
			return true
		})
	}
	return set
}
