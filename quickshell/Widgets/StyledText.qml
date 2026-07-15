import QtQuick
import qs.Common

Text {
    property bool isMonospace: false

    color: Theme.surfaceText
    font.pixelSize: Theme.fontSizeMedium
    font.family: isMonospace ? Theme.monoFontFamily : Theme.fontFamily
    font.weight: Theme.fontWeight
    // Rasterize with the native font engine (FreeType) instead of Qt's
    // default distance-field renderer, which synthesizes blurry bold
    // weights from the InterVariable glyph cache. Native rendering gives
    // crisp DemiBold/Bold text (sender names, subjects) at any weight.
    renderType: Text.NativeRendering
    wrapMode: Text.WordWrap
    elide: Text.ElideRight
    horizontalAlignment: Text.AlignLeft
    verticalAlignment: Text.AlignVCenter

    Behavior on opacity {
        NumberAnimation {
            duration: Theme.shortDuration
            easing.type: Theme.standardEasing
        }
    }
}
