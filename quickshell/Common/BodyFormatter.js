.pragma library

// Render only markup we generate from the provider's distilled text. Never
// load message HTML, remote images, CSS or executable URLs into the reader.
function escapeHtml(value) {
    return String(value).replace(/&/g, "&amp;").replace(/</g, "&lt;")
        .replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&#39;");
}

function safeUrl(value) {
    return /^(https?:\/\/[^\s/?#]+|mailto:[^\s@]+@[^\s@]+)[^\s]*$/i.test(value)
        && !/[\x00-\x20\x7f<>"'\\]/.test(value);
}

function inline(text, color, linkLabel) {
    const stash = [];
    const put = html => "\x01" + (stash.push(html) - 1) + "\x01";
    const emphasis = value => escapeHtml(value)
        .replace(/\*\*([^*\n]+)\*\*/g, "<b>$1</b>")
        .replace(/(^|[\s(])\*([^*\n]+)\*(?=$|[\s.,!?:;)])/g, "$1<i>$2</i>");
    const anchor = (url, label) => safeUrl(url)
        ? '<a href="' + escapeHtml(url) + '"><font color="' + color + '">' + emphasis(label) + '</font></a>'
        : emphasis(label);
    let s = text.replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/g, "");
    s = s.replace(/`([^`\n]+)`/g, (m, code) => put('<code>' + escapeHtml(code) + '</code>'));
    s = s.replace(/\[!\[([^\]]*)\]\(([^\s()]+)\)\]\(([^\s()]+)\)/g,
        (m, alt, image, url) => put(anchor(url, alt || linkLabel)));
    s = s.replace(/!\[([^\]]*)\]\(([^\s()]+)\)/g, (m, alt) => put(escapeHtml(alt)));
    s = s.replace(/\[([^\]]+)\]\(([^\s()]+)\)/g, (m, label, url) => put(anchor(url, label)));
    s = s.replace(/(?:https?:\/\/|mailto:)[^\s<>"'\x01]+/gi, url => {
        const clean = url.replace(/[.,;!?:)\]}]+$/, "");
        const tail = url.substring(clean.length);
        const label = clean.length > 72 ? clean.substring(0, 68) + "…" : clean;
        return put(anchor(clean, label)) + tail;
    });
    s = emphasis(s).replace(/\\([\\*_{}\[\]()#+.!~-])/g, "$1");
    for (let i = stash.length - 1; i >= 0; i--)
        s = s.split("\x01" + i + "\x01").join(stash[i]);
    return s;
}

function format(text, options) {
    options = options || {};
    const color = /^#[0-9a-f]{3,8}$/i.test(String(options.linkColor)) ? String(options.linkColor) : "#517cbb";
    const quoteColor = /^#[0-9a-f]{3,8}$/i.test(String(options.quoteColor)) ? String(options.quoteColor) : color;
    const render = value => inline(value, color, options.linkLabel || "Link");
    const lines = String(text || "").replace(/\r\n?/g, "\n").split("\n");
    const cells = line => line.trim().replace(/^\||\|$/g, "").split("|").map(value => value.trim());
    const tableSeparator = line => /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(line);
    let html = "";
    let i = 0;
    while (i < lines.length) {
        const line = lines[i];
        if (!line.trim()) { i++; continue; }
        if (/^\s*```/.test(line)) {
            const code = [];
            i++;
            while (i < lines.length && !/^\s*```/.test(lines[i])) code.push(lines[i++]);
            if (i < lines.length) i++;
            html += '<pre style="margin-top:8px; margin-bottom:8px">' + escapeHtml(code.join("\n")) + '</pre>';
            continue;
        }
        if (/^\s*>/.test(line)) {
            const quoted = [];
            while (i < lines.length && /^\s*>/.test(lines[i])) quoted.push(lines[i++].replace(/^\s*> ?/, ""));
            // Bound nesting, even for hostile or malformed message text.
            const depth = options.depth || 0;
            html += '<blockquote style="margin-left:16px; margin-top:8px; margin-bottom:8px"><font color="' + quoteColor + '">' + (depth < 5
                ? format(quoted.join("\n"), {linkColor: color, quoteColor: quoteColor, linkLabel: options.linkLabel, depth: depth + 1})
                : quoted.map(render).join('<br>')) + '</font></blockquote>';
            continue;
        }
        const heading = line.match(/^\s*#{1,6}\s+(.*)$/);
        if (heading) {
            html += '<p style="margin-top:12px; margin-bottom:6px"><b>' + render(heading[1]) + '</b></p>';
            i++; continue;
        }
        if (/^\s*(?:-{3,}|\*{3,}|_{3,})\s*$/.test(line)) { html += '<hr>'; i++; continue; }
        if (i + 1 < lines.length && line.includes('|') && tableSeparator(lines[i + 1])) {
            const header = cells(line);
            html += '<table border="1" cellspacing="0" cellpadding="6" width="100%"><tr>' + header.map(c => '<th>' + render(c) + '</th>').join('') + '</tr>';
            i += 2;
            while (i < lines.length && lines[i].trim() && lines[i].includes('|')) {
                const row = cells(lines[i++]);
                html += '<tr>' + header.map((c, n) => '<td>' + render(row[n] || '') + '</td>').join('') + '</tr>';
            }
            html += '</table>'; continue;
        }
        const list = line.match(/^\s*(?:([-+*])|(\d+)[.)])\s+(.+)$/);
        if (list) {
            const ordered = !!list[2];
            const tag = ordered ? 'ol' : 'ul';
            html += '<' + tag + (ordered ? ' start="' + Number(list[2]) + '"' : '') + '>';
            while (i < lines.length) {
                const item = lines[i].match(/^\s*(?:([-+*])|(\d+)[.)])\s+(.+)$/);
                if (!item || !!item[2] !== ordered) break;
                html += '<li>' + render(item[3]) + '</li>'; i++;
            }
            html += '</' + tag + '>'; continue;
        }
        const paragraph = [render(line)];
        i++;
        while (i < lines.length && lines[i].trim() && !/^\s*(?:>|```|#{1,6}\s|[-+*]\s|\d+[.)]\s|[-*_]{3,}\s*$)/.test(lines[i])
               && !(i + 1 < lines.length && tableSeparator(lines[i + 1]))) paragraph.push(render(lines[i++]));
        html += '<p style="margin-top:0px; margin-bottom:10px; line-height:125%">' + paragraph.join('<br>') + '</p>';
    }
    return html;
}
