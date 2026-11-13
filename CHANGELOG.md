# Changelog

All notable changes to **DankMail** (`dmail` daemon, Quickshell UI, and the `dankmailUnread` DankMaterialShell companion) are documented in this file.

## [0.4.1] - 2026-11-12

### Fixed
- **Dux Shell 2.0+ `DCommon` widget migration (`#14`)**:
  - Migrated `dms-plugin/DankmailWidget.qml` and `dms-plugin/DankmailSettings.qml` from the deprecated `qs.Widgets` `Dank*` shims (`DankActionButton`, `DankFlickable`, `DankIcon`) to `DActionButton`, `DFlickable`, and `DIcon` from `qs.DCommon.Widgets`.
  - Updated `dms-plugin/plugin.json` (`requires_dms: ">=2.0.0"`) and `dms-plugin/README.md` so DMS 1.x installations are not offered an incompatible companion update.

## [0.4.0] - 2026-11-12

### Added
- **One-click OAuth (`dankcalendar` parity), CLI account setup & account filter tooltips**:
  - Added Step 0 one-click sign-in (`Connect Google Account` / `Connect Microsoft Account`) with PKCE S256 in `AccountAddModal`, reusable OAuth client credential resolution (system keyring + `0600` local fallback + existing accounts + env + `-ldflags -X`), and a native `Import client_secret.json` file picker button.
  - Added `dmail account setup google|microsoft` and `dmail account add google|microsoft|imap` CLI commands, plus hover tooltips (`DankTooltip`) showing each account's email address and unread count on the header account filter dots.
- **Full IMAP + SMTP provider**:
  - Added `core/internal/provider/imap` (`go-imap/v2` + `net/smtp` STARTTLS/TLS, with loopback self-signed TLS tolerance for Proton Mail Bridge) supporting sync, archive/sent pagination, flag/folder triage, replies, compose, and attachments, automatically un-parking previously stored IMAP accounts.
- **OS-native attachment opening & Save as**:
  - Clicking an attachment chip downloads the part on demand into `~/.cache/dankmail/attachments/` (`0600`) and opens it via `xdg-open` (`attachments.open`), with a secondary **Save as…** folder picker button (`attachments.saveAs`) and webmail fallback on error.
- **Snoozed mailbox view, weekend preset & Move to Inbox**:
  - Added a **Snoozed** filter chip ordered by wake time (`SnoozedUntil ASC`), wake-time badges on snoozed threads, **Unsnooze** actions (`ops.unsnooze`), and a **This weekend** (Saturday 09:00) snooze preset, wrapping inline row snooze presets within the thread column margins so options never clip at the window edge.
  - Added **Move to Inbox** (`ops.unarchive`) across the preview toolbar, thread row hover actions, bulk selection bar, and the `dankmailUnread` bar popout for archived and other non-inbox threads, flushing pending undoable actions immediately on filter switches or window close.
- **HTML-to-Markdown message distillation & full body resync**:
  - Added `core/internal/mailtext` (`html-to-markdown/v2`) across Gmail, Microsoft Graph, and IMAP so HTML messages retain readable link labels, headings, lists, and multi-column data tables while unwrapping layout tables, sanitizing `<img>` / linked-banner nodes, and stripping hidden preheaders and tracking pixels, plus a per-account full resync button in `SettingsModal`.
  - Replaced regex-only Markdown link/image parsing in `BodyFormatter.js` with balanced token scanners so cached and incoming links/images with optional titles, parentheses or spaces in URLs, multi-line block wrappers, and `data:` URIs never leak raw `!` or `(...)` syntax.
- **Selectable & copyable `Subject`**:
  - Replaced static subject labels in thread and draft previews with read-only mouse-selectable `TextEdit` controls and one-click clipboard copy buttons.

### Fixed
- **Dux Shell (`dms-theme-sync`) theme roles, typography, and WCAG contrast**:
  - Updated `Theme.qml` to watch `~/.cache/dms/dms-colors.json` and `~/.cache/dms/dms-typography.json` (with `~/.cache/DankMaterialShell/` fallback), honor `"mode": "dark"|"light"`, load full Material 3 `on_*` / `secondary_container` / `tertiary_container` / `inverse_surface` roles, and enforce WCAG contrast safeguards.
  - Replaced all 30 `primaryContainer`+`primary` low-contrast buttons/chips across `quickshell/` and `dms-plugin/` with `Theme.primary`+`Theme.primaryText` (filled actions) or `Theme.primaryContainer`+`Theme.onPrimaryContainer` (tonal chips), and removed hardcoded hex colors in `TriageWindow.qml`.

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
