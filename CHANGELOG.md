# Changelog

All notable changes to **DankMail** (`dmail` daemon, Quickshell UI, and the `dankmailUnread` DankMaterialShell companion) are documented in this file.

## [0.3.12] - 2026-10-15

### Added
- **Drafts section & draft editing**:
  - Added a **Drafts** view in both the main `TriageWindow` filter bar and the `dankmailUnread` DankMaterialShell bar popout.
  - Added local draft persistence (`drafts.json`) synchronized with Gmail `users.drafts` (`ListDrafts`, `GetDraft`, `CreateDraft`, `UpdateDraft`, `DeleteDraft`) over IPC (`drafts.list`, `drafts.save`, `drafts.delete`, `ui.editDraft`).
  - Clicking a draft in the bar popout or clicking **Edit draft** in the triage window opens `ComposeModal` pre-populated with its recipients (`To`, `Cc`, `Bcc`), subject, body, and attachments.
- **Rich composer, URL links, `Cc`/`Bcc`, and file attachments**:
  - Added `Cc / Bcc` recipient fields with contact autocomplete, a formatting and URL link insertion toolbar (`**bold**`, `*italic*`, `[label](url)`, bulleted/numbered lists, quotes, code, horizontal rules), and a live rich-text **Preview** toggle in `ComposeModal`.
  - Added a native DankMail-styled multi-file attachment picker (`FilePickerModal.qml` backed by `fs.list` IPC) in both `ComposeModal` and inline quick reply, avoiding unstyled `QtQuick.Dialogs` fallbacks under `QT_QUICK_CONTROLS_STYLE=Material`.
  - Updated `mailmime` to build `multipart/mixed` base64 attachments and `multipart/alternative` (`text/plain` + `text/html`) when messages contain Markdown links or formatting.
- **Thread message selector & copyable sender/recipient addresses**:
  - Added a horizontal message selector bar in the preview pane for multi-message threads so any message in a conversation can be inspected directly.
  - Added RFC 2047 MIME word decoding (`=?utf-8?q?...?=`, `=?utf-8?b?...?=`) and interactive address chips for sender (`From`) and recipients (`To`, `Cc`) with one-click clipboard copy and a **Copy all emails** action.

### Fixed
- **Reader formatting & link activation**:
  - Unwrapped Gmail redirect URLs (`https://www.google.com/url?q=...`), merged soft-wrapped `label <https://...>` plain-text links, linkified bare domains (such as `plataformas.udgplus.udg.mx`) and `<email@domain>` addresses, and improved multi-line `*...*` emphasis and nested list parsing in `BodyFormatter.js`.
  - Fixed link hover cursor detection and click handling on `TextEdit` (`selectByMouse: true`) using `TapHandler` + `linkAt` and daemon-side `ui.openUrl` (`xdg-open`).
  - Fixed chip width calculations (`Layout.preferredWidth` / `contentWidth` with `Text.NoWrap`) in `RowLayout` headers and suppressed false-positive BCC warnings on forwarded mail when recipients are present.

## [0.3.11] - 2026-10-02

### Added
- Resumable mailbox sync checkpoints across quota pauses and restarts.
- **Sent** mailbox filter with on-demand pagination (`threads.fetchSent`) for Gmail and Microsoft Graph.
- **Not spam** rescue action (`ops.unspam`) for single threads and cross-account bulk selections.

### Fixed
- Companion socket reconnection recovery and translation installation in `dms-plugin`.

## [0.3.10] - 2026-09-30

### Fixed
- Paginated Gmail archive history (`threads.fetchArchived`) so **Archived** loads unstarred archived conversations beyond the local cache.

## [0.3.9] - 2026-09-29

### Added
- **Archived** quick filter in the triage window.
