pragma Singleton
import QtQuick

// Bridge to the dmail daemon, mirroring dankcalendar's DankCalService:
//  - one unix-socket connection (Common/DankSocket) for IPC mutations
//    (ops.*, dnd.*, ui.*) with the Capabilities handshake;
//  - one subscription connection for daemon events (sync.updated,
//    unread.changed, op.failed, snooze.woke);
//  - HTTP reads against the localhost API (address via env DMAIL_API_ADDR)
//    for thread lists and previews.
// Exposes: accounts, threads model, unread counters, and signals the
// Modules re-render on.
QtObject {
    id: service

    // TODO(anillo1): socket wiring, models, optimistic-update helpers.
    property var accounts: []
    property int unreadTotal: 0
}
