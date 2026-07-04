pragma Singleton
pragma ComponentBehavior: Bound

import QtQuick
import Quickshell
import Quickshell.Io
import qs.Services

// Translation singleton (pattern from dankcalendar, trimmed): source
// strings are English; ../translations/<lang>.json provides overrides,
// shaped {"context": {"term": "translation"}}. es ships from day one.
Singleton {
    id: root

    readonly property var log: Log.scoped("I18n")

    readonly property string _rawLocale: Qt.locale().name
    readonly property string _lang: _rawLocale.split(/[_-]/)[0]
    readonly property var _rtlLanguages: ["ar", "he", "iw", "fa", "ur", "ps", "sd", "dv", "yi", "ku"]
    readonly property bool isRtl: _rtlLanguages.includes(_lang)
    property string resolvedLocale: "en"

    property var translations: ({})
    property bool translationsLoaded: false

    property url _selectedPath: ""

    Component.onCompleted: {
        const override = Quickshell.env("DMAIL_LANG");
        const lang = (override && override !== "") ? override.split(/[_-]/)[0] : _lang;
        if (lang && lang !== "en") {
            resolvedLocale = lang;
            _selectedPath = Qt.resolvedUrl("../translations/" + lang + ".json");
        }
    }

    FileView {
        id: translationLoader
        path: root._selectedPath
        printErrors: false

        onLoaded: {
            try {
                root.translations = JSON.parse(text());
                root.translationsLoaded = true;
                root.log.info(`loaded '${root.resolvedLocale}' translations`);
            } catch (e) {
                root.log.warn(`bad translations for '${root.resolvedLocale}', using English`);
                root.translationsLoaded = false;
            }
        }

        onLoadFailed: {
            root.translationsLoaded = false;
        }
    }

    function tr(term, context) {
        if (!translationsLoaded || !translations)
            return term;
        const ctx = context || term;
        if (translations[ctx] && translations[ctx][term])
            return translations[ctx][term];
        for (const c in translations) {
            if (translations[c] && translations[c][term])
                return translations[c][term];
        }
        return term;
    }
}
