pragma Singleton
import QtQuick

// Translation singleton following dankcalendar's pattern: resolves the
// locale from ui-settings.json or Qt.locale(), loads the matching JSON
// from ../translations/, and exposes tr(context, key). es + en ship from
// day one. TODO(anillo1): loading + fallback logic.
QtObject {
    function tr(context, key) {
        return key;
    }
}
