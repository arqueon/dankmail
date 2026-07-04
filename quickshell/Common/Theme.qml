pragma Singleton
pragma ComponentBehavior: Bound

import QtQuick
import Quickshell
import Quickshell.Io

// Simplified from dankcalendar's Theme: follows DankMaterialShell dynamic
// colors (dms-colors.json, watched) with a stock Material dark/light
// fallback. Mode: DMAIL_THEME_MODE=light|dark, default dark.
Singleton {
    id: root

    FontLoader {
        id: interFont
        source: Qt.resolvedUrl("../assets/fonts/inter/InterVariable.ttf")
    }

    FontLoader {
        id: firaCodeFont
        source: Qt.resolvedUrl("../assets/fonts/nerd-fonts/FiraCodeNerdFont-Regular.ttf")
    }

    readonly property string defaultFontFamily: interFont.name || "Inter Variable"
    readonly property string defaultMonoFontFamily: firaCodeFont.name || "Fira Code"

    readonly property bool isLightMode: Quickshell.env("DMAIL_THEME_MODE") === "light"

    readonly property string xdgCacheDir: {
        const xdg = Quickshell.env("XDG_CACHE_HOME");
        if (xdg && xdg !== "")
            return xdg;
        return Quickshell.env("HOME") + "/.cache";
    }

    property var matugenColors: ({})
    property bool colorsLoaded: false

    readonly property var stockDark: ({
            "primary": "#a6c8ff",
            "primaryText": "#00315f",
            "primaryContainer": "#1f4876",
            "secondary": "#bcc7dc",
            "surface": "#111318",
            "surfaceText": "#e2e2e9",
            "surfaceVariant": "#44474e",
            "surfaceVariantText": "#c4c6d0",
            "surfaceTint": "#a6c8ff",
            "background": "#111318",
            "backgroundText": "#e2e2e9",
            "outline": "#8e9099",
            "surfaceContainer": "#1d2024",
            "surfaceContainerHigh": "#282a2f",
            "surfaceContainerHighest": "#33353a",
            "error": "#ffb4ab",
            "warning": "#ffcc80",
            "info": "#a6c8ff",
            "success": "#a6d495"
        })

    readonly property var stockLight: ({
            "primary": "#3b608f",
            "primaryText": "#ffffff",
            "primaryContainer": "#d4e3ff",
            "secondary": "#545f71",
            "surface": "#f9f9ff",
            "surfaceText": "#191c20",
            "surfaceVariant": "#e0e2ec",
            "surfaceVariantText": "#44474e",
            "surfaceTint": "#3b608f",
            "background": "#f9f9ff",
            "backgroundText": "#191c20",
            "outline": "#74777f",
            "surfaceContainer": "#ededf4",
            "surfaceContainerHigh": "#e7e8ee",
            "surfaceContainerHighest": "#e2e2e9",
            "error": "#ba1a1a",
            "warning": "#e65100",
            "info": "#3b608f",
            "success": "#2e7d32"
        })

    function getMatugenColor(path, fallback) {
        const colorMode = isLightMode ? "light" : "dark";
        let cur = matugenColors && matugenColors.colors && matugenColors.colors[colorMode];
        if (!cur)
            return fallback;
        return cur[path] || fallback;
    }

    readonly property var currentThemeData: {
        const stock = isLightMode ? stockLight : stockDark;
        if (!colorsLoaded)
            return stock;
        return {
            "primary": getMatugenColor("primary", stock.primary),
            "primaryText": getMatugenColor("on_primary", stock.primaryText),
            "primaryContainer": getMatugenColor("primary_container", stock.primaryContainer),
            "secondary": getMatugenColor("secondary", stock.secondary),
            "surface": getMatugenColor("surface", stock.surface),
            "surfaceText": getMatugenColor("on_surface", stock.surfaceText),
            "surfaceVariant": getMatugenColor("surface_variant", stock.surfaceVariant),
            "surfaceVariantText": getMatugenColor("on_surface_variant", stock.surfaceVariantText),
            "surfaceTint": getMatugenColor("surface_tint", stock.surfaceTint),
            "background": getMatugenColor("background", stock.background),
            "backgroundText": getMatugenColor("on_background", stock.backgroundText),
            "outline": getMatugenColor("outline", stock.outline),
            "surfaceContainer": getMatugenColor("surface_container", stock.surfaceContainer),
            "surfaceContainerHigh": getMatugenColor("surface_container_high", stock.surfaceContainerHigh),
            "surfaceContainerHighest": getMatugenColor("surface_container_highest", stock.surfaceContainerHighest),
            "error": stock.error,
            "warning": stock.warning,
            "info": stock.info,
            "success": stock.success
        };
    }

    property color primary: currentThemeData.primary
    property color primaryText: currentThemeData.primaryText
    property color primaryContainer: currentThemeData.primaryContainer
    property color secondary: currentThemeData.secondary
    property color surface: currentThemeData.surface
    property color surfaceText: currentThemeData.surfaceText
    property color surfaceVariant: currentThemeData.surfaceVariant
    property color surfaceVariantText: currentThemeData.surfaceVariantText
    property color surfaceTint: currentThemeData.surfaceTint
    property color background: currentThemeData.background
    property color backgroundText: currentThemeData.backgroundText
    property color outline: currentThemeData.outline
    property color outlineVariant: Qt.rgba(outline.r, outline.g, outline.b, 0.6)
    property color surfaceContainer: currentThemeData.surfaceContainer
    property color surfaceContainerHigh: currentThemeData.surfaceContainerHigh
    property color surfaceContainerHighest: currentThemeData.surfaceContainerHighest

    property color onSurface: surfaceText
    property color onSurfaceVariant: surfaceVariantText
    property color onPrimary: primaryText

    property color error: currentThemeData.error
    property color warning: currentThemeData.warning
    property color info: currentThemeData.info
    property color success: currentThemeData.success

    property color primaryHover: Qt.rgba(primary.r, primary.g, primary.b, 0.12)
    property color primaryHoverLight: Qt.rgba(primary.r, primary.g, primary.b, 0.08)
    property color primaryPressed: Qt.rgba(primary.r, primary.g, primary.b, 0.16)
    property color primarySelected: Qt.rgba(primary.r, primary.g, primary.b, 0.3)
    property color primaryBackground: Qt.rgba(primary.r, primary.g, primary.b, 0.04)

    property color surfaceHover: Qt.rgba(surfaceVariant.r, surfaceVariant.g, surfaceVariant.b, 0.08)
    property color surfacePressed: Qt.rgba(surfaceVariant.r, surfaceVariant.g, surfaceVariant.b, 0.12)
    property color surfaceSelected: Qt.rgba(surfaceVariant.r, surfaceVariant.g, surfaceVariant.b, 0.15)

    property color surfaceTextAlpha: Qt.rgba(surfaceText.r, surfaceText.g, surfaceText.b, 0.3)
    property color surfaceTextMedium: Qt.rgba(surfaceText.r, surfaceText.g, surfaceText.b, 0.7)

    property color outlineButton: Qt.rgba(outline.r, outline.g, outline.b, 0.5)
    property color outlineLight: Qt.rgba(outline.r, outline.g, outline.b, 0.05)
    property color outlineMedium: Qt.rgba(outline.r, outline.g, outline.b, 0.12)
    property color outlineStrong: Qt.rgba(outline.r, outline.g, outline.b, 0.18)

    property color errorHover: Qt.rgba(error.r, error.g, error.b, 0.12)
    property color errorPressed: Qt.rgba(error.r, error.g, error.b, 0.16)

    property color shadowMedium: Qt.rgba(0, 0, 0, 0.08)
    property color shadowStrong: Qt.rgba(0, 0, 0, 0.3)

    property real spacingXS: 4
    property real spacingS: 8
    property real spacingM: 12
    property real spacingL: 16
    property real spacingXL: 24

    property real fontScale: 1.0
    property real fontSizeSmall: Math.round(fontScale * 12)
    property real fontSizeMedium: Math.round(fontScale * 14)
    property real fontSizeLarge: Math.round(fontScale * 16)
    property real fontSizeXLarge: Math.round(fontScale * 20)

    property real iconSize: 24
    property real iconSizeSmall: 16
    property real iconSizeLarge: 32

    property real cornerRadius: 12
    property real cornerRadiusSmall: 8
    property real cornerRadiusLarge: 16

    property string fontFamily: defaultFontFamily
    property string monoFontFamily: defaultMonoFontFamily
    property int fontWeight: Font.Normal

    property int shorterDuration: 100
    property int shortDuration: 200
    property int mediumDuration: 400
    property int longDuration: 600
    property int standardEasing: Easing.OutCubic
    property int emphasizedEasing: Easing.OutQuart

    function withAlpha(c, a) {
        return Qt.rgba(c.r, c.g, c.b, a);
    }

    FileView {
        id: dmsColorsView
        path: root.xdgCacheDir + "/DankMaterialShell/dms-colors.json"
        blockLoading: false
        watchChanges: true
        printErrors: false

        onLoaded: {
            try {
                const text = dmsColorsView.text();
                if (!text)
                    return;
                root.matugenColors = JSON.parse(text);
                root.colorsLoaded = true;
            } catch (e) {
                root.colorsLoaded = false;
            }
        }

        onFileChanged: dmsColorsView.reload()
        onLoadFailed: root.colorsLoaded = false
    }
}
