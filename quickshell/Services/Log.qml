pragma Singleton
pragma ComponentBehavior: Bound

import QtQuick
import Quickshell

// Minimal scoped logger (pattern from dankcalendar, trimmed).
// Level via DMAIL_LOG_LEVEL: debug|info|warn|error (default info).
Singleton {
    id: root

    readonly property int level: {
        switch ((Quickshell.env("DMAIL_LOG_LEVEL") || "").toLowerCase()) {
        case "debug":
            return 0;
        case "warn":
        case "warning":
            return 2;
        case "error":
            return 3;
        default:
            return 1;
        }
    }

    function scoped(module) {
        return {
            debug: function (...args) {
                if (root.level <= 0)
                    console.log(`[${module}]`, ...args);
            },
            info: function (...args) {
                if (root.level <= 1)
                    console.log(`[${module}]`, ...args);
            },
            warn: function (...args) {
                if (root.level <= 2)
                    console.warn(`[${module}]`, ...args);
            },
            error: function (...args) {
                console.error(`[${module}]`, ...args);
            }
        };
    }
}
