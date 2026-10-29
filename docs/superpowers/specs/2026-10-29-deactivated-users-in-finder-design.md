# Find deactivated users in the channel finder

Status: implemented.

Follow-up to `2026-10-20-hide-deactivated-users-design.md`.

## Problem

With `general.hide_deactivated_users` on (the default), a DM with a
deactivated person is gone from every surface, the channel finder
(Ctrl+T / Ctrl+P; Ctrl+K in slk is "navigate forward") included. There is no way to look up an old
conversation with someone who left, which is the main reason to keep it.

What slk does today, from the code and tests:

- The finder holds conversations only. Its rows come from
  `users.conversations` (`buildChannelItem`) plus `channels/search` for
  public channels the user has not joined. There is no people search: a
  person with no DM is not in the finder at all, deactivated or not.
- A DM row whose peer is deactivated is dropped in `filter()` through
  `core.IsHiddenPeerConversation`, before matching. Typing the person's
  name finds nothing (`TestDeactivatedUsersAreAbsentFromTheFinder`).
  Forwarding mode drops it too.
- The DM is still opened by a permalink, a mark or history. The sidebar
  then shows it while it is the active conversation (the existing
  exemption), history loads as for any DM, and the composer is enabled.
  A send goes to Slack. The repo has no capture or error code for a post
  to a deactivated peer, so what Slack answers is not known here; the
  expectation (from the desktop client, which shows no composer) is a
  refusal shown as the generic send-failure toast. To confirm with the
  test binary.

## Behaviour

Like the Slack desktop quick switcher: the person stays out of sight
until you look for them on purpose. This only reaches conversations slk
already has (an existing DM from `users.conversations`): the finder has
no people search, so a former colleague you never had a DM with is not
offered.

- **Empty query (or only spaces): hidden.** The recent list is a browse surface like the
  sidebar, and the point of the option is to not see people who left.
  Showing them there only because you visited them once would bring them
  back into daily view.
- **With a query: offered and marked.** A deactivated DM matches by name
  like any row. It is drawn in the dim grey used for not-joined channels,
  with no presence or status glyph, and with a `deactivated` tag after
  the name. It sorts after every active match (including not-joined
  channels), so it never pushes a live conversation down. The cost is
  that an exact name match can sit below a weak fuzzy match on a
  channel; that is on purpose.
- **Forwarding: not offered.** A message cannot be sent there. With the
  option off the finder gets no set, so forwarding offers the DM as it
  did before this change, and a forward there fails at Slack.
- **Opening it** works as any DM: history loads. The sidebar shows the
  row while it is active (existing rule) and drops it when you leave.
  The sidebar cursor does not move onto it: `openChannel` selects the
  row before `SetActiveChannelID` makes it visible. Stale rows behave
  the same today; it is left for a separate fix.
- **Composer disabled.** The desktop client does not offer a composer
  for a deactivated account (see above: the API refusal itself is not
  captured). slk disables the channel composer for a DM whose peer is
  deactivated, the same way it does for read-only conversations: the
  composer area reads "You can't send messages to a deactivated
  account.", `i` shows it as a toast, and a thread reply there is refused
  with the same toast on Enter (the thread composer still takes text, as
  for read-only conversations). Editing an old message is not blocked,
  as it is not for read-only conversations. This is slk's choice from
  the deactivated set, not a verified API answer, and it does not depend
  on the option: hiding is a display preference, the account is
  deactivated either way. A live `UserDeactivatedMsg` updates it for the
  conversation on screen and takes the user out of insert mode, like a
  failed probe does; the draft is kept.
- **Option off:** unchanged. The finder gets a nil set and shows the DM
  as a normal row, unmarked.

Other surfaces (sidebar, `#` autocomplete, new-message picker, `@`
roster) are unchanged.

## Design

- `channelfinder.filter()`: the `IsHiddenPeerConversation` check moves
  after the query check. With an empty query or in forwarding mode the
  row is skipped as today; with a query it goes into matching. The sort
  gets one new top key, deactivated last, ahead of `Joined`.
  `channels/search` returns channels only (no `DMUserID`), so the server
  search cannot bring back an unmarked DM. No new set: the finder keeps
  the map the App already publishes (`SetDeactivatedUsers`).
- `channelfinder.renderBox`: a deactivated row takes the dim branch
  (same `nonJoinedColor`), with a dim `○` instead of the hardcoded `#`,
  no presence, DND or status glyph (a status the sidebar pushed with
  `SetStatus` is ignored), and the tag. The tag is taken out of the name
  budget, so a long name truncates before the tag is lost.
- One predicate, `(*App).sendBlockedReason(channelID) string`, returns
  the reason a conversation takes no messages, or "": read-only mark,
  then deactivated DM peer, then the app-home probe. The peer
  (`(*App).peerDeactivated`) comes from the finder's rows
  (`findChannelByID`), which hold the conversation just picked there,
  else from `sidebar.Items()` (a DM opened with Ctrl+N reaches the
  sidebar first), and is looked up in `a.deactivatedUsers`. It replaces the `readOnlyChannels || unsendableChannels`
  pairs in `retargetActiveChannel`, `ChannelMessagingCapabilityMsg` and the
  thread reply check, and `composeBlockedText` reads it.
  `publishDeactivatedUsers` recomputes `composeDisabled` for the active
  channel from it (so a reactivation keeps a read-only mark), and the
  `UserDeactivatedMsg` reducer returns `dropInsertIfComposeDisabled()`.
  `ConversationOpenedMsg` recomputes it too: `addConversation` in
  `cmd/slk` reports a deactivated peer before the row that names it.

No I/O, no new message, nothing in `cmd/slk`.

## Tests

- finder: a deactivated DM is absent on an empty query, present and last
  on a matching query, absent in forwarding mode; the rendered row has
  the tag; with a nil set it is a normal row.
- finder: a query of only spaces does not offer it; a long name keeps
  the tag.
- App: opening a deactivated DM disables the composer with the reason,
  also with the option off; `i`, Enter and a thread reply (thread in
  another conversation than the channel pane) are refused; a live
  deactivation of the DM on screen disables it and leaves insert mode; a
  reactivation enables it but keeps a read-only mark; a row that names
  the peer after the deactivation arrived disables it.
