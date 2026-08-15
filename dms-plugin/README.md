# Dankmail for DankMaterialShell

This directory contains the **DankMaterialShell companion for
[Dankmail](https://github.com/arqueon/dankmail)**. It is part of the same
project as the `dmail` daemon and the Dankmail desktop interface; it is not a
separate mail client or a second implementation.

The plugin adds Dankmail to DankBar with live unread status, truthful
per-account counters, a recent-inbox popout, triage actions, compose and sync
controls, do-not-disturb status, and a shortcut into Dankmail's existing quick
reply flow. Mail access, synchronization, account state, and mutations remain
owned by the `dmail` daemon.

## Requirements

- DankMaterialShell with plugin support.
- Dankmail installed and configured, including the `dmail` daemon.
- At least one mail account configured in Dankmail.

## Installation

Install **Dankmail Unread** from the DankMaterialShell plugin browser. The
registry entry points to this `dms-plugin/` directory in the main Dankmail
repository, so plugin and daemon development stay together.

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
