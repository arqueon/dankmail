import QtQuick
import qs.Common
import qs.Widgets

StyledRect {
    id: tooltip

    property Item target: null
    property string text: ""
    property string side: "bottom"

    readonly property real maxWidth: parent ? Math.max(160, parent.width - Theme.spacingM * 2) : 360

    enabled: false
    visible: opacity > 0 && text !== ""
    opacity: (target !== null && text !== "") ? 1 : 0
    z: 1000

    width: Math.min(maxWidth, label.implicitWidth + Theme.spacingM * 2)
    height: label.implicitHeight + Theme.spacingXS * 2 + 2
    radius: 6
    color: Theme.inverseSurface
    border.width: 1
    border.color: Theme.outline

    function show(item, tipText, preferredSide) {
        hideTimer.stop();
        target = item;
        text = tipText || "";
        side = preferredSide || "bottom";
        reposition();
    }

    function hide(item) {
        if (item && target !== item)
            return;
        hideTimer.restart();
    }

    function reposition() {
        if (!target || !parent)
            return;
        const pt = target.mapToItem(parent, target.width / 2, side === "top" ? 0 : target.height);
        const rawX = pt.x - width / 2;
        const minX = Theme.spacingS;
        const maxX = Math.max(minX, parent.width - width - Theme.spacingS);
        x = Math.round(Math.max(minX, Math.min(maxX, rawX)));
        if (side === "top") {
            y = Math.max(Theme.spacingXS, Math.round(pt.y - height - 6));
        } else {
            const rawY = Math.round(pt.y + 6);
            y = (rawY + height > parent.height - Theme.spacingXS)
                ? Math.max(Theme.spacingXS, Math.round(pt.y - height - 6))
                : rawY;
        }
    }

    onWidthChanged: reposition()
    onHeightChanged: reposition()

    Timer {
        id: hideTimer
        interval: 60
        repeat: false
        onTriggered: {
            tooltip.target = null;
            tooltip.text = "";
        }
    }

    Behavior on opacity {
        NumberAnimation {
            duration: 100
            easing.type: Easing.OutCubic
        }
    }

    StyledText {
        id: label
        anchors.centerIn: parent
        width: Math.min(implicitWidth, tooltip.maxWidth - Theme.spacingM * 2)
        text: tooltip.text
        color: Theme.inverseOnSurface
        font.pixelSize: Theme.fontSizeSmall
        font.weight: Font.Medium
        elide: Text.ElideMiddle
        maximumLineCount: 1
    }
}
