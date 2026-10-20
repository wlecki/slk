# Hide deactivated users

Status: implemented.

## Problem

The Slack desktop client hides deactivated ("deleted") users: a DM with a
deactivated person leaves the sidebar, and they are not offered in people
pickers. slk shows them everywhere. Long-gone colleagues sit in the DM
section, in the channel finder, in the new-message picker and in `@`
autocomplete.

slk does not hide deactivated users anywhere today, and the flag never
reaches the UI:

- The `users` table has no `deleted` column. Every writer receives
  `deleted` from Slack (`slack.User.Deleted`, `edge.User.Deleted`,
  `boot.User.Deleted`) and drops it.
- `buildChannelItem` (`cmd/slk/channelitem.go`) ignores
  `slack.Channel.IsUserDeleted`, which `users.conversations`,
  `conversations.info` and WS `im_created` / `im_open` already decode.
- The only reader of the flag is the profile dialog's "deactivated" badge,
  from a fresh `users.info` (`internal/ui/userprofile/view.go`).

The 2026-05-25 new-message design already asked for the picker to exclude
`Deleted` users; that was never built because the flag was not there.

## Behaviour

New option, on by default to match the desktop client:

```toml
[general]
hide_deactivated_users = true   # false shows deactivated people again
```

It lives in `[general]`, not `[sidebar]`, because it covers pickers too.
`Load` decodes over `Default()`, so a plain `bool` defaulting to `true`
reads an explicit `false` correctly.

When on, a user whose account is deactivated is hidden from:

| Surface | Where |
|---|---|
| Sidebar DM and app rows | `sidebar.Model.rebuildFilter` |
| Channel finder (and `/open`, forwarding picker) | `channelfinder` `filter()` |
| `#` autocomplete in compose (it lists sidebar DM rows) | `App.SetChannels` |
| New-message picker (Ctrl+N) | `App.seedNewMessagePicker` |
| `@` mention autocomplete (both composers) | `App.SetUserNames` roster |

Exemptions:

- Sidebar only: the active conversation is never hidden (same rule as
  staleness), so opening one by permalink or history still works and the
  cursor does not vanish from under the user. If the selected row
  disappears for another reason, the existing cursor fallback applies.
- Nothing else is exempt. Starred or section-pinned DMs are hidden (unlike
  `IsStale`), and so are DMs with unreads: Slack hides both, and an unread
  exemption would need a refilter on every read-state change, which the
  sidebar does not do today (`Invalidate` does not rebuild the filter).
- The finder and the people pickers have no exemptions.

Out of scope:

- Group DMs with a deactivated member. Slack keeps them, and no
  per-member flag reaches `FormatMPDMName`.
- Message authors, reactions, threads and activity. Slack still shows
  deactivated authors on old messages.
- Pruning presence subscriptions for hidden DMs (`workspacePresenceIDs`).
  A possible follow-up.

## Design

One mechanism: a **global** set of deactivated user IDs, pushed to the App
the way `ExternalUsers` is, and consulted by every surface. Global, not
per workspace: Slack deactivates an account, not a membership, and the
users cache is keyed globally by user ID with `workspace_id` meaning only
"the workspace that first cached the row" (`UpsertUser`), so a
per-workspace query would miss users another workspace cached first. A
global set also means one live update reaches every workspace's view.

### Sources of the flag

1. **Conversation objects.** `IsUserDeleted` on IM objects from
   `users.conversations` (the main sidebar path), `conversations.info`
   (starred recovery, message discovery) and WS `im_created` / `im_open`.
   This covers exactly the DMs in the sidebar, with no extra requests.
   `buildChannelItem` stays pure; its callers collect the peers. The boot
   set is the cached set plus these peers, recomputed every connect.
   A conversation observation only ever adds: an absent or false flag
   does not undo a deletion (only a profile can, see below). The callers
   also run `MarkUserDeleted(id)`, an UPDATE that never creates a row, so
   later boots and the cache-only surfaces know it too; a peer with no row
   is still in the set for this run, from the conversation.
2. **Users cache.** New column `users.deleted INTEGER NOT NULL DEFAULT 0`.
   Every full-profile writer sets it and it is overwritten on conflict like
   `is_bot`: both `resolveUser` branches, `resolveOneContext`,
   `applyEdgeUser` (`UpsertUserFromEdge`, both SQL branches),
   `hydrateFirstSight` (insert only), bootstrap revalidation
   (`UpdateUserFromEdge`, both branches). The bot-ID writer
   (`user_resolver.go`, `GetBotInfo`) is a partial record for a `B…` ID
   and writes `false`, which is correct for a bot ID. Profile observations
   that bypass those writers (`peer_status.go`'s users.info fallback, the
   avatar backfill, the profile dialog's fetch) go through one helper,
   `recordUserDeleted(teamID, userID, deleted)`.
3. **Migration and coverage.** The one-time `UPDATE users SET version = 0`
   the status, huddle and home-team columns used. Conditional revalidation
   then refetches, in batches, the users bootstrap asks about: the open
   DMs' peers and the authors of the opened channel. That is every
   sidebar DM, which is the point. It is **not** every cached user: the
   `@` roster (seeded from the whole cache) learns that someone is
   deactivated only when they are next resolved. A whole-cache sweep would
   cost about `ceil(N/80)` users/info calls per workspace for tens of
   thousands of rows, so it is not done; this gap is documented.
4. `user_change` carries only the self user in practice and is not used.
   The `userBoot` fallback (`boot.IM`) carries no `is_user_deleted` in the
   captures and is not modelled; the cache covers that path.

### Engine to UI

- `(*cache.DB).DeactivatedUsers() map[string]bool`, caller-owned.
- `WorkspaceReadyMsg` and `WorkspaceSwitchedMsg` gain `DeactivatedUsers`
  (cache set plus that workspace's conversation peers).
  `App.SetDeactivatedUsers(map)` merges it into the App's set and pushes
  it to the sidebar, channel finder and composers.
- Live change: `recordUserDeleted` and the profile writers send
  `UserDeactivatedMsg{UserID, Deactivated}` whenever a fresh observation
  is `true`, and when it is `false` for a user the cache had as deleted
  (reactivation). The reducer updates the set and refilters. Sending on
  every `true` observation is idempotent and cheap (a resolved profile is
  rare), and avoids relying on the shared cache to know what each UI has
  seen.
- The option reaches the App through `uiSettings` and
  `App.SetHideDeactivatedUsers(bool)`, like `SetSidebarStaleThreshold`.

### Filtering

- Sidebar: `ChannelItem.DMUserID` is already there. `rebuildFilter` skips a
  `dm` / `app` row whose `DMUserID` is in the set, before the stale check,
  unless it is the active channel. Setters capture `currentCursorKey()`,
  refilter and `rebuildNavWithCursor`, like `SetStaleThreshold`.
- Channel finder: `core.ChannelFinderItem` gains `DMUserID`, set in
  `buildChannelItem`, `refreshDMPeerFromCache` (which builds finder items
  field by field) and preserved by `upsertFinderItem`. `filter()` skips set
  members. `/open` reads the raw items and matches only channels, so it is
  unaffected.
- `#` autocomplete: `App.SetChannels` skips the same DM rows.
- New-message picker: seeded on open (`seedNewMessagePicker`), so it skips
  set members there and needs no live refilter.
- Mention roster: `SetUserNames` skips set members; a live change re-runs
  it, as `UserResolvedMsg` already does. The full name map is kept for
  rendering; only the suggestion list is filtered.

## Tests

- cache: migration adds the column and resets versions once; every writer
  persists and overwrites `deleted`; `MarkUserDeleted` never creates a
  row; `DeactivatedUsers` returns exactly the deleted users.
- config: default `true`, explicit `false` read back.
- `buildChannelItem` carries `DMUserID` on finder items; an IM with
  `IsUserDeleted` marks the user in the cache.
- sidebar: a deactivated DM is hidden; shown again when the option is off;
  the active one stays; starred and unread ones are hidden; the cursor
  stays on the same item across the refilter.
- channel finder, new-message picker, mention roster: deactivated users
  are absent when on, present when off.
- reducer: `UserDeactivatedMsg` hides the row live and a reactivation
  shows it again, in whichever workspace is on screen.

## Docs

`wiki/Configuration.md` gains the key in the full example, and a
`[sidebar]` block it never had (`hide_inactive_after_days`, `width`).
`wiki/Features.md` gets a bullet. AGENTS.md lists
`cache.DB.DeactivatedUsers` and `MarkUserDeleted`.

## Implementation notes

Where the code differs from the design above:

- `cache.DB.ClearUserDeleted(id)` exists next to `MarkUserDeleted`. A profile
  observation that bypasses the full-profile writers can show a user active
  again, and `recordUserDeleted` has to clear the flag then.
- `recordUserDeleted(db, send, userID, deleted)` takes no team ID: the message
  names no workspace. Writers that persist the flag themselves do not call it.
  They read `knownDeleted(db, id)` before the write and call
  `announceUserDeleted(send, id, was, now)` after it, so a reactivation is
  reported only for a user known as deleted. `recordUserDeleted` is the same
  two steps around `MarkUserDeleted` / `ClearUserDeleted`.
- A user known deactivated only from this run, because the cache has no row
  to hold the flag, is kept in one process-wide set, `rememberedPeers`. It
  holds IM peers Slack flagged on a conversation and users a bypassing
  profile showed deactivated. It is global, not per workspace, because the
  account is deactivated everywhere and a reactivation has to clear it
  everywhere. `knownDeleted` counts it, `announceUserDeleted` forgets a user
  shown active again, and `deactivatedUsers(db)` is the cache set plus the
  remembered users the cache has no row for. Once a row exists the cache
  decides, so a profile showing the user active again is not undone by a
  remembered flag on the next workspace switch.
- One rule decides whether a row is a hidden deactivated DM, for the sidebar,
  the finder and the `#` autocomplete alike:
  `core.IsHiddenPeerConversation(type, dmUserID, hidden)`, a dm or app row
  with a hidden peer.
- `App.SetDeactivatedUsers` only adds. A reactivation arrives as
  `UserDeactivatedMsg{Deactivated: false}`. The App replaces its set whole on
  every change and never writes a published map, because the sidebar and
  finder keep the map they are given. `App.hiddenUsers()` returns nil when the
  option is off, so the surfaces know nothing about the option.
- `App.channelPickItems` keeps the last `SetChannels` list so the `#`
  autocomplete can be rebuilt when the set changes. `compose.Model.ChannelPicks`
  is a test accessor, like `MentionUsers`.
- `NewApp` starts with the option on, like `config.Default()`.
- The `@` roster marks deactivated users (`mentionpicker.User.Deactivated`)
  instead of dropping them: `compose.rebuildMentionUsers` leaves them out
  of the suggestions, while `SetUsers` keeps them for the send-time
  `@Name` to `<@ID>` translation, so a name typed by hand still becomes a
  mention.
- `/open` is unaffected, as the Filtering section says, so the wiki does not
  list it among the hidden surfaces.
