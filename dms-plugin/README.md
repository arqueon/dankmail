# Dankmail for DankMaterialShell

This directory contains the **DankMaterialShell companion for
[Dankmail](https://github.com/arqueon/dankmail)**. It is part of the same
project as the `dmail` daemon and the Dankmail desktop interface; it is not a
separate mail client or a second implementation.

The plugin adds Dankmail to DankBar with live unread status, truthful
per-account counters, a popout with up to 20 recent Inbox or Starred threads, triage actions, compose and sync
controls, do-not-disturb status, and a shortcut into Dankmail's existing quick
reply flow. Mail access, synchronization, account state, and mutations remain
owned by the `dmail` daemon.

## Requirements

- DankMaterialShell 1.6.0 or later.
- Dankmail installed and configured, including the `dmail` daemon.
- At least one mail account configured in Dankmail.

## Installation

Install **Dankmail Unread** from the DankMaterialShell plugin browser. The
registry entry points to this `dms-plugin/` directory in the main Dankmail
repository, so plugin and daemon development stay together.

On Arch Linux, you can instead install the AUR package:

```sh
paru -S dms-shell-plugin-dankmail
```

This installs the companion in DMS's system plugin directory. An existing
user-installed copy takes precedence; move that copy out of
`~/.config/DankMaterialShell/plugins/` if you want to use the packaged version.
Enable **Dankmail Unread** in DMS Settings → Plugins and add it to the bar.

For a local checkout, link this directory as the plugin source:

```sh
ln -s /path/to/dankmail/dms-plugin \
  ~/.config/DankMaterialShell/plugins/dankmailUnread
```

Then enable `dankmailUnread` in DMS and make sure the user service is running:

```sh
systemctl --user enable --now dmail
```

See the [main Dankmail README](https://github.com/arqueon/dankmail#readme) for
account setup, packaging, and daemon documentation.

## Source of truth

The canonical plugin source is this directory. The former standalone
`dms-dankmail` repository is retained only as historical packaging and source
evidence; new changes should be made here alongside the daemon protocol they
consume.

## Controls and connection status

Left click opens the popout, middle click shows or hides the app, and right
click requests synchronization. Hover over an action button for its label.
The badge counts unread Inbox mail even when the popout shows Starred.
English is the source language; the plugin includes Spanish translations
selected by DMS. The desktop application's language setting is separate.

If the daemon disconnects, the plugin clears its old mail rows and retries.
A failed request shows an error instead of claiming that the mailbox is empty;
open Dankmail to inspect the account and retry. The event subscription reconnects
independently if only that connection fails. A safety poll runs every minute.

The popout is a compact view of locally synchronized mail. Use the application
for search, Archived and Sent pagination, spam recovery, account diagnostics,
and multi-account selection. These operations use the same daemon and cache.

Colors, text sizing, action buttons and corner radii follow DMS, including its
Expressive theme. Unread mail uses the primary accent; an idle icon uses the
bar's configured icon color. Expressive can change button shapes and contrast.

Both `make install-dms-plugin` and the packaged plugin install `translations/`.
When updating a local symlink, check that it points to the checkout containing
the update, then run `dms ipc plugin-scan reload dankmailUnread`.
