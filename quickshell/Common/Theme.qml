pragma Singleton
pragma ComponentBehavior: Bound

import QtQuick
import Quickshell
import Quickshell.Io

// Dux Shell (dms-theme-sync) dynamic Material 3 theme & typography singleton:
// watches ~/.cache/dms/dms-colors.json and ~/.cache/dms/dms-typography.json
// (with fallback to ~/.cache/DankMaterialShell/*), follows the active
// "mode": "dark"|"light" (overridable via DMAIL_THEME_MODE), exposes full
// on_*_container roles, and enforces WCAG contrast on filled/tonal surfaces.
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

    readonly property string xdgCacheDir: {
        const xdg = Quickshell.env("XDG_CACHE_HOME");
        if (xdg && xdg !== "")
            return xdg;
        return Quickshell.env("HOME") + "/.cache";
    }

    property var matugenColors: ({})
    property bool colorsLoaded: false
    property bool primaryColorsMissing: false
    property var dmsTypography: ({})
    property bool primaryTypoMissing: false

    readonly property bool isLightMode: {
        const envMode = Quickshell.env("DMAIL_THEME_MODE");
        if (envMode === "light")
            return true;
        if (envMode === "dark")
            return false;
        if (colorsLoaded && matugenColors && typeof matugenColors.mode === "string")
            return matugenColors.mode.toLowerCase() === "light";
        return false;
    }

    readonly property var stockDark: ({
            "primary": "#a6c8ff",
            "primaryText": "#00315f",
            "primaryContainer": "#1f4876",
            "onPrimaryContainer": "#d4e3ff",
            "secondary": "#bcc7dc",
            "secondaryContainer": "#3c4758",
            "onSecondaryContainer": "#d8e3f8",
            "tertiary": "#dabde2",
            "tertiaryContainer": "#553e5d",
            "onTertiaryContainer": "#f7d8ff",
            "surface": "#111318",
            "surfaceText": "#e2e2e9",
            "surfaceVariant": "#44474e",
            "surfaceVariantText": "#c4c6d0",
            "surfaceTint": "#a6c8ff",
            "background": "#111318",
            "backgroundText": "#e2e2e9",
            "outline": "#8e9099",
            "outlineVariant": "#44474e",
            "surfaceContainerLowest": "#0c0e13",
            "surfaceContainerLow": "#191c20",
            "surfaceContainer": "#1d2024",
            "surfaceContainerHigh": "#282a2f",
            "surfaceContainerHighest": "#33353a",
            "inverseSurface": "#e2e2e9",
            "inverseOnSurface": "#2e3036",
            "error": "#ffb4ab",
            "errorText": "#690005",
            "errorContainer": "#93000a",
            "onErrorContainer": "#ffdad6",
            "warning": "#ffcc80",
            "info": "#a6c8ff",
            "success": "#a6d495"
        })

    readonly property var stockLight: ({
            "primary": "#3b608f",
            "primaryText": "#ffffff",
            "primaryContainer": "#d4e3ff",
            "onPrimaryContainer": "#001c3a",
            "secondary": "#545f71",
            "secondaryContainer": "#d8e3f8",
            "onSecondaryContainer": "#111c2b",
            "tertiary": "#6d5676",
            "tertiaryContainer": "#f7d8ff",
            "onTertiaryContainer": "#271430",
            "surface": "#f9f9ff",
            "surfaceText": "#191c20",
            "surfaceVariant": "#e0e2ec",
            "surfaceVariantText": "#44474e",
            "surfaceTint": "#3b608f",
            "background": "#f9f9ff",
            "backgroundText": "#191c20",
            "outline": "#74777f",
            "outlineVariant": "#c4c6d0",
            "surfaceContainerLowest": "#ffffff",
            "surfaceContainerLow": "#f3f3fa",
            "surfaceContainer": "#ededf4",
            "surfaceContainerHigh": "#e7e8ee",
            "surfaceContainerHighest": "#e2e2e9",
            "inverseSurface": "#2e3036",
            "inverseOnSurface": "#f0f0f7",
            "error": "#ba1a1a",
            "errorText": "#ffffff",
            "errorContainer": "#ffdad6",
            "onErrorContainer": "#410002",
            "warning": "#e65100",
            "info": "#3b608f",
            "success": "#2e7d32"
        })

    readonly property color contrastDark: "#111318"
    readonly property color contrastLight: "#f8f9ff"

    function srgbChannelToLinear(v) {
        return v <= 0.03928 ? (v / 12.92) : Math.pow((v + 0.055) / 1.055, 2.4);
    }

    function relativeLuminance(c) {
        const col = Qt.color(c);
        return 0.2126 * srgbChannelToLinear(col.r)
             + 0.7152 * srgbChannelToLinear(col.g)
             + 0.0722 * srgbChannelToLinear(col.b);
    }

    function contrastRatio(c1, c2) {
        const l1 = relativeLuminance(c1);
        const l2 = relativeLuminance(c2);
        const hi = Math.max(l1, l2);
        const lo = Math.min(l1, l2);
        return (hi + 0.05) / (lo + 0.05);
    }

    function bestTextOn(bg) {
        const darkRatio = contrastRatio(contrastDark, bg);
        const lightRatio = contrastRatio(contrastLight, bg);
        return darkRatio >= lightRatio ? contrastDark : contrastLight;
    }

    // Guarantees readable foreground text (>= 4.2:1) even when a custom
    // palette sets vibrant containers close in luminance to primary.
    function ensureContrast(fg, bg) {
        if (contrastRatio(fg, bg) >= 4.2)
            return Qt.color(fg);
        return bestTextOn(bg);
    }

    function getMatugenColor(path, fallback) {
        const colorMode = isLightMode ? "light" : "dark";
        const cur = matugenColors && matugenColors.colors && matugenColors.colors[colorMode];
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
            "onPrimaryContainer": getMatugenColor("on_primary_container", stock.onPrimaryContainer),
            "secondary": getMatugenColor("secondary", stock.secondary),
            "secondaryContainer": getMatugenColor("secondary_container", stock.secondaryContainer),
            "onSecondaryContainer": getMatugenColor("on_secondary_container", stock.onSecondaryContainer),
            "tertiary": getMatugenColor("tertiary", stock.tertiary),
            "tertiaryContainer": getMatugenColor("tertiary_container", stock.tertiaryContainer),
            "onTertiaryContainer": getMatugenColor("on_tertiary_container", stock.onTertiaryContainer),
            "surface": getMatugenColor("surface", stock.surface),
            "surfaceText": getMatugenColor("on_surface", stock.surfaceText),
            "surfaceVariant": getMatugenColor("surface_variant", stock.surfaceVariant),
            "surfaceVariantText": getMatugenColor("on_surface_variant", stock.surfaceVariantText),
            "surfaceTint": getMatugenColor("surface_tint", stock.surfaceTint),
            "background": getMatugenColor("background", stock.background),
            "backgroundText": getMatugenColor("on_background", stock.backgroundText),
            "outline": getMatugenColor("outline", stock.outline),
            "outlineVariant": getMatugenColor("outline_variant", stock.outlineVariant),
            "surfaceContainerLowest": getMatugenColor("surface_container_lowest", stock.surfaceContainerLowest),
            "surfaceContainerLow": getMatugenColor("surface_container_low", stock.surfaceContainerLow),
            "surfaceContainer": getMatugenColor("surface_container", stock.surfaceContainer),
            "surfaceContainerHigh": getMatugenColor("surface_container_high", stock.surfaceContainerHigh),
            "surfaceContainerHighest": getMatugenColor("surface_container_highest", stock.surfaceContainerHighest),
            "inverseSurface": getMatugenColor("inverse_surface", stock.inverseSurface),
            "inverseOnSurface": getMatugenColor("inverse_on_surface", stock.inverseOnSurface),
            "error": getMatugenColor("error", stock.error),
            "errorText": getMatugenColor("on_error", stock.errorText),
            "errorContainer": getMatugenColor("error_container", stock.errorContainer),
            "onErrorContainer": getMatugenColor("on_error_container", stock.onErrorContainer),
            "warning": stock.warning,
            "info": stock.info,
            "success": stock.success
        };
    }

    property color primary: currentThemeData.primary
    property color primaryText: ensureContrast(currentThemeData.primaryText, primary)
    property color primaryContainer: currentThemeData.primaryContainer
    property color onPrimaryContainer: ensureContrast(currentThemeData.onPrimaryContainer, primaryContainer)
    property color secondary: currentThemeData.secondary
    property color secondaryContainer: currentThemeData.secondaryContainer
    property color onSecondaryContainer: ensureContrast(currentThemeData.onSecondaryContainer, secondaryContainer)
    property color tertiary: currentThemeData.tertiary
    property color tertiaryContainer: currentThemeData.tertiaryContainer
    property color onTertiaryContainer: ensureContrast(currentThemeData.onTertiaryContainer, tertiaryContainer)
    property color surface: currentThemeData.surface
    property color surfaceText: currentThemeData.surfaceText
    property color surfaceVariant: currentThemeData.surfaceVariant
    property color surfaceVariantText: currentThemeData.surfaceVariantText
    property color surfaceTint: currentThemeData.surfaceTint
    property color background: currentThemeData.background
    property color backgroundText: currentThemeData.backgroundText
    property color outline: currentThemeData.outline
    property color outlineVariant: currentThemeData.outlineVariant
    property color surfaceContainerLowest: currentThemeData.surfaceContainerLowest
    property color surfaceContainerLow: currentThemeData.surfaceContainerLow
    property color surfaceContainer: currentThemeData.surfaceContainer
    property color surfaceContainerHigh: currentThemeData.surfaceContainerHigh
    property color surfaceContainerHighest: currentThemeData.surfaceContainerHighest
    property color inverseSurface: currentThemeData.inverseSurface
    property color inverseOnSurface: ensureContrast(currentThemeData.inverseOnSurface, inverseSurface)

    property color onSurface: surfaceText
    property color onSurfaceVariant: surfaceVariantText
    property color onPrimary: primaryText

    property color error: currentThemeData.error
    property color errorText: ensureContrast(currentThemeData.errorText, error)
    property color onError: errorText
    property color errorContainer: currentThemeData.errorContainer
    property color onErrorContainer: ensureContrast(currentThemeData.onErrorContainer, errorContainer)
    property color warning: currentThemeData.warning
    property color info: currentThemeData.info
    property color success: currentThemeData.success

    property color primaryHover: Qt.rgba(primary.r, primary.g, primary.b, 0.12)
    property color primaryHoverLight: Qt.rgba(primary.r, primary.g, primary.b, 0.08)
    property color primaryPressed: Qt.rgba(primary.r, primary.g, primary.b, 0.16)
    property color primarySelected: Qt.rgba(primary.r, primary.g, primary.b, 0.3)
    property color primaryBackground: Qt.rgba(primary.r, primary.g, primary.b, 0.06)

    property color surfaceHover: Qt.rgba(surfaceVariant.r, surfaceVariant.g, surfaceVariant.b, 0.08)
    property color surfacePressed: Qt.rgba(surfaceVariant.r, surfaceVariant.g, surfaceVariant.b, 0.12)
    property color surfaceSelected: Qt.rgba(surfaceVariant.r, surfaceVariant.g, surfaceVariant.b, 0.15)

    property color surfaceTextAlpha: Qt.rgba(surfaceText.r, surfaceText.g, surfaceText.b, 0.35)
    property color surfaceTextMedium: Qt.rgba(surfaceText.r, surfaceText.g, surfaceText.b, 0.72)

    property color outlineButton: Qt.rgba(outline.r, outline.g, outline.b, 0.5)
    property color outlineLight: Qt.rgba(outline.r, outline.g, outline.b, 0.08)
    property color outlineMedium: Qt.rgba(outline.r, outline.g, outline.b, 0.16)
    property color outlineStrong: Qt.rgba(outline.r, outline.g, outline.b, 0.24)

    property color errorHover: Qt.rgba(error.r, error.g, error.b, 0.12)
    property color errorPressed: Qt.rgba(error.r, error.g, error.b, 0.16)

    property color shadowMedium: Qt.rgba(0, 0, 0, 0.08)
    property color shadowStrong: Qt.rgba(0, 0, 0, 0.3)

    property real spacingXS: 4
    property real spacingS: 8
    property real spacingM: 12
    property real spacingL: 16
    property real spacingXL: 24

    property real fontScale: (dmsTypography && typeof dmsTypography.fontScale === "number" && dmsTypography.fontScale > 0) ? dmsTypography.fontScale : 1.0
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

    property string fontFamily: (dmsTypography && dmsTypography.fontFamily) ? dmsTypography.fontFamily : defaultFontFamily
    property string monoFontFamily: (dmsTypography && dmsTypography.monoFontFamily) ? dmsTypography.monoFontFamily : defaultMonoFontFamily
    property int fontWeight: (dmsTypography && typeof dmsTypography.fontWeight === "number") ? dmsTypography.fontWeight : Font.Normal

    property int shorterDuration: 100
    property int shortDuration: 200
    property int mediumDuration: 400
    property int longDuration: 600
    property int standardEasing: Easing.OutCubic
    property int emphasizedEasing: Easing.OutQuart

    function withAlpha(c, a) {
        return Qt.rgba(c.r, c.g, c.b, a);
    }

    function applyColorsText(text) {
        if (!text)
            return false;
        try {
            root.matugenColors = JSON.parse(text);
            root.colorsLoaded = true;
            return true;
        } catch (e) {
            return false;
        }
    }

    function applyTypographyText(text) {
        if (!text)
            return false;
        try {
            root.dmsTypography = JSON.parse(text);
            return true;
        } catch (e) {
            return false;
        }
    }

    // Dux Shell 2.1 canonical cache path (~/.cache/dms/dms-colors.json),
    // falling back to legacy ~/.cache/DankMaterialShell/dms-colors.json.
    FileView {
        id: dmsColorsPrimary
        path: root.xdgCacheDir + "/dms/dms-colors.json"
        blockLoading: false
        watchChanges: true
        printErrors: false

        onLoaded: {
            root.primaryColorsMissing = false;
            root.applyColorsText(dmsColorsPrimary.text());
        }
        onFileChanged: dmsColorsPrimary.reload()
        onLoadFailed: root.primaryColorsMissing = true
    }

    FileView {
        id: dmsColorsLegacy
        path: root.primaryColorsMissing ? (root.xdgCacheDir + "/DankMaterialShell/dms-colors.json") : ""
        blockLoading: false
        watchChanges: true
        printErrors: false

        onLoaded: {
            if (root.primaryColorsMissing)
                root.applyColorsText(dmsColorsLegacy.text());
        }
        onFileChanged: dmsColorsLegacy.reload()
        onLoadFailed: {
            if (root.primaryColorsMissing)
                root.colorsLoaded = false;
        }
    }

    FileView {
        id: dmsTypoPrimary
        path: root.xdgCacheDir + "/dms/dms-typography.json"
        blockLoading: false
        watchChanges: true
        printErrors: false

        onLoaded: {
            root.primaryTypoMissing = false;
            root.applyTypographyText(dmsTypoPrimary.text());
        }
        onFileChanged: dmsTypoPrimary.reload()
        onLoadFailed: root.primaryTypoMissing = true
    }

    FileView {
        id: dmsTypoLegacy
        path: root.primaryTypoMissing ? (root.xdgCacheDir + "/DankMaterialShell/dms-typography.json") : ""
        blockLoading: false
        watchChanges: true
        printErrors: false

        onLoaded: {
            if (root.primaryTypoMissing)
                root.applyTypographyText(dmsTypoLegacy.text());
        }
        onFileChanged: dmsTypoLegacy.reload()
    }
}
