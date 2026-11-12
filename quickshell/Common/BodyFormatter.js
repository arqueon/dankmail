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

function unwrapUrl(value) {
    const s = String(value || "").trim();
    const m = s.match(/^https?:\/\/(?:www\.)?google\.[a-z.]+\/url\?([^\s#]+)$/i);
    if (m) {
        const parts = m[1].split("&");
        for (let i = 0; i < parts.length; i++) {
            const kv = parts[i].split("=");
            if ((kv[0] === "q" || kv[0] === "url") && kv.length > 1) {
                try {
                    const decoded = decodeURIComponent(kv.slice(1).join("=").replace(/\+/g, "%20"));
                    if (safeUrl(decoded))
                        return decoded;
                } catch (e) {}
            }
        }
    }
    return s;
}

function readBracketed(str, openIdx, maxLen) {
    if (str[openIdx] !== "[")
        return null;
    const limit = Math.min(str.length, openIdx + (maxLen || 2000));
    let depth = 0;
    for (let i = openIdx; i < limit; i++) {
        const ch = str[i];
        if (ch === "\\") {
            i++;
            continue;
        }
        if (ch === "[") {
            depth++;
        } else if (ch === "]") {
            depth--;
            if (depth === 0)
                return { content: str.slice(openIdx + 1, i), endIdx: i };
        }
    }
    return null;
}

function readParenDest(str, openIdx, isImage) {
    if (str[openIdx] !== "(")
        return null;
    const limit = str.length;
    let i = openIdx + 1;
    while (i < limit && /[ \t\n]/.test(str[i]))
        i++;
    if (i >= limit)
        return null;
    if (str[i] === ")")
        return { url: "", endIdx: i };

    let rawUrl = "";
    if (str[i] === "<") {
        const closeAngle = str.indexOf(">", i + 1);
        if (closeAngle === -1 || closeAngle >= limit)
            return null;
        rawUrl = str.slice(i + 1, closeAngle).trim().replace(/\s+/g, "%20");
        i = closeAngle + 1;
        while (i < limit && str[i] !== ")") {
            if (str[i] === "\\")
                i++;
            i++;
        }
        if (i >= limit || str[i] !== ")")
            return null;
        return { url: rawUrl, endIdx: i };
    }

    // Scan up to matching ')' tracking nested parentheses and quotes.
    let depth = 1;
    let inQuote = "";
    const contentStart = i;
    while (i < limit && depth > 0) {
        const ch = str[i];
        if (ch === "\\") {
            i += 2;
            continue;
        }
        if (inQuote !== "") {
            if (ch === inQuote)
                inQuote = "";
            i++;
            continue;
        }
        if (ch === '"' || ch === "'") {
            // Treat as a title quote only when preceded by whitespace.
            if (i > contentStart && /\s/.test(str[i - 1])) {
                inQuote = ch;
                i++;
                continue;
            }
        }
        if (ch === "(") {
            depth++;
        } else if (ch === ")") {
            depth--;
            if (depth === 0)
                break;
        }
        i++;
    }
    if (depth !== 0 || i >= limit)
        return null;

    const endIdx = i;
    let inner = str.slice(contentStart, endIdx).trim();
    // Strip optional trailing Markdown title: url "title" or url 'title'.
    inner = inner.replace(/\s+(?:"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*')\s*$/, "").trim();
    if (isImage)
        return { url: inner, endIdx: endIdx };

    // Collapse line wraps inside URLs and percent-encode any residual spaces.
    inner = inner.replace(/\s*\n\s*/g, "").replace(/ /g, "%20");
    return { url: inner, endIdx: endIdx };
}

function stripNestedImages(label) {
    let out = "";
    let i = 0;
    while (i < label.length) {
        if (label[i] === "!" && label[i + 1] === "[") {
            const br = readBracketed(label, i + 1, 1500);
            if (br) {
                let after = br.endIdx + 1;
                if (label[after] === " " && label[after + 1] === "(")
                    after++;
                if (label[after] === "(") {
                    const dest = readParenDest(label, after, true);
                    if (dest) {
                        const alt = br.content.replace(/\\([\[\]])/g, "$1").trim();
                        if (alt !== "" && !/^(?:spacer|pixel|1x1|blank|image\d*\.[a-z0-9]+)$/i.test(alt))
                            out += alt;
                        i = dest.endIdx + 1;
                        continue;
                    }
                }
            }
        }
        out += label[i++];
    }
    return out;
}

function collapseMultilineMarkdownLinks(text) {
    // Preserve fenced code blocks untouched while flattening multi-line
    // ![alt](url) and [label](url) tokens so block splitting never breaks them.
    const parts = text.split(/(^\s*```[\s\S]*?^\s*```)/m);
    for (let p = 0; p < parts.length; p += 2) {
        const seg = parts[p];
        let out = "";
        let i = 0;
        while (i < seg.length) {
            const isImg = seg[i] === "!" && seg[i + 1] === "[";
            const brIdx = isImg ? i + 1 : (seg[i] === "[" ? i : -1);
            if (brIdx !== -1) {
                const br = readBracketed(seg, brIdx, 1500);
                if (br && !br.content.includes("```")) {
                    let after = br.endIdx + 1;
                    while (after < seg.length && (seg[after] === " " || seg[after] === "\t"))
                        after++;
                    if (seg[after] === "(" && (after === br.endIdx + 1 || isImg || /^\(\s*<?(?:https?:|mailto:)/i.test(seg.slice(after, after + 24)))) {
                        const dest = readParenDest(seg, after, isImg);
                        if (dest && (br.content.includes("\n") || seg.slice(after, dest.endIdx + 1).includes("\n") || after > br.endIdx + 1)) {
                            let cleanContent = br.content
                                .replace(/^\s*#{1,6}\s+/gm, "**")
                                .replace(/\s*\n+\s*/g, " ")
                                .trim();
                            // Close any '**' opened by heading-inside-link normalization.
                            if ((cleanContent.match(/\*\*/g) || []).length % 2 === 1)
                                cleanContent += "**";
                            const cleanDest = seg.slice(after + 1, dest.endIdx).replace(/\s*\n+\s*/g, " ").trim();
                            out += (isImg ? "![" : "[") + cleanContent + "](" + cleanDest + ")";
                            i = dest.endIdx + 1;
                            continue;
                        }
                    }
                }
            }
            out += seg[i++];
        }
        parts[p] = out;
    }
    return parts.join("");
}

function normalizePlainEmail(text) {
    let s = String(text || "").replace(/\r\n?/g, "\n");
    // Join soft-wrapped Gmail plain-text "<https://...>" link lines to their label line (including inside > quotes).
    s = s.replace(/([^\n\s>])\n(?:[ \t]*>[ \t]*)?[ \t]*(<https?:\/\/[^\s<>]+>)/gi, "$1 $2");
    // Collapse "https://example.org <https://example.org>" duplicate URL pairs.
    s = s.replace(/(https?:\/\/[^\s<>*]+)[ \t]+<(https?:\/\/[^\s<>]+)>/gi,
        (m, u1, u2) => "[" + u1.replace(/[.,;!?:)\]}]+$/, "") + "](" + unwrapUrl(u2) + ")");
    // Convert "domain.tld <https://...>" into a proper markdown link.
    s = s.replace(/([a-zA-Z0-9][\w.-]*\.[a-zA-Z]{2,}(?:\/[^\s<>*]*)?)[ \t]+<(https?:\/\/[^\s<>]+)>/g,
        (m, label, url) => "[" + label + "](" + unwrapUrl(url) + ")");
    // Unwrap standalone angle-bracket URLs "<https://...>" or "<mailto:...>".
    s = s.replace(/<(https?:\/\/[^\s<>]+|mailto:[^\s<>]+)>/gi,
        (m, url) => unwrapUrl(url));
    // Convert angle-bracket email addresses "<user@domain>" into mailto links.
    s = s.replace(/<([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})>/g,
        "[$1](mailto:$1)");
    return collapseMultilineMarkdownLinks(s);
}

function replaceMarkdownImagesAndLinks(s, anchor, put, linkLabel) {
    let out = "";
    let i = 0;
    while (i < s.length) {
        // Standalone Markdown image: ![alt](url "optional title")
        if (s[i] === "!" && s[i + 1] === "[") {
            const br = readBracketed(s, i + 1, 1500);
            if (br) {
                let after = br.endIdx + 1;
                if (s[after] === " " && s[after + 1] === "(")
                    after++;
                if (s[after] === "(") {
                    const dest = readParenDest(s, after, true);
                    if (dest) {
                        const alt = br.content.replace(/\\([\[\]])/g, "$1").trim();
                        if (alt !== "" && !/^(?:spacer|pixel|1x1|blank|image\d*\.[a-z0-9]+)$/i.test(alt))
                            out += put(escapeHtml(alt));
                        i = dest.endIdx + 1;
                        continue;
                    }
                }
                // Reference-style image ![alt][ref] -> keep alt text only.
                if (s[after] === "[") {
                    const ref = readBracketed(s, after, 200);
                    if (ref) {
                        const alt = br.content.replace(/\\([\[\]])/g, "$1").trim();
                        if (alt !== "")
                            out += put(escapeHtml(alt));
                        i = ref.endIdx + 1;
                        continue;
                    }
                }
            }
        }
        // Markdown link (including linked images [![alt](img)](url)): [label](url "title")
        if (s[i] === "[") {
            const br = readBracketed(s, i, 2000);
            if (br) {
                let after = br.endIdx + 1;
                if (s[after] === " " && /^\(\s*<?(?:https?:|mailto:)/i.test(s.slice(after, after + 24)))
                    after++;
                if (s[after] === "(") {
                    const dest = readParenDest(s, after, false);
                    if (dest) {
                        const cleanedLabel = stripNestedImages(br.content)
                            .replace(/\\([\[\]])/g, "$1")
                            .replace(/\s+/g, " ")
                            .trim();
                        const finalLabel = cleanedLabel !== "" ? cleanedLabel : (dest.url !== "" ? linkLabel : "");
                        if (finalLabel !== "")
                            out += put(anchor(dest.url, finalLabel));
                        i = dest.endIdx + 1;
                        continue;
                    }
                }
            }
        }
        out += s[i++];
    }
    return out;
}

function inline(text, color, linkLabel) {
    const stash = [];
    const put = html => "\x01" + (stash.push(html) - 1) + "\x01";
    const emphasis = value => escapeHtml(value)
        .replace(/\*\*([^*\n]+)\*\*/g, "<b>$1</b>")
        .replace(/(^|[\s(«"'])\*([^*\n]+?)\s*\*(?=$|[\s.,!?:;)»"'])/g, "$1<b>$2</b>")
        .replace(/(^|[\s(«"'])\*([^\s*][^*]*?)\s*\*(?=$|[\s.,!?:;)»"'])/g, "$1<b>$2</b>")
        .replace(/(^|[\s(«"'])\*([^\s*][^*]*?)\s+\*(?=\S)/g, "$1<b>$2</b> ");
    const anchor = (rawUrl, label) => {
        const url = unwrapUrl(rawUrl);
        return safeUrl(url)
            ? '<a href="' + escapeHtml(url) + '" style="color:' + color + '; text-decoration:underline;"><font color="' + color + '">' + emphasis(label) + '</font></a>'
            : emphasis(label);
    };
    let s = text.replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/g, "");
    s = s.replace(/`([^`\n]+)`/g, (m, code) => put('<code>' + escapeHtml(code) + '</code>'));
    s = s.replace(/!\[([^\]]*)\]\(\s*data:[^)\s]*\)?/gi, (m, alt) => alt.trim() ? put(escapeHtml(alt.trim())) : "");
    s = s.replace(/\[image:\s*([^\]]+)\]/gi, (m, alt) => put('<code>[' + escapeHtml(alt.trim()) + ']</code>'));
    s = replaceMarkdownImagesAndLinks(s, anchor, put, linkLabel);
    s = s.replace(/(?:https?:\/\/|mailto:)[^\s<>"'\x01]+/gi, rawUrl => {
        const cleanRaw = rawUrl.replace(/[.,;!?:)\]}*]+$/, "");
        const tail = rawUrl.substring(cleanRaw.length);
        const clean = unwrapUrl(cleanRaw);
        const label = clean.length > 72 ? clean.substring(0, 68) + "…" : clean;
        return put(anchor(clean, label)) + tail;
    });
    // Bare domain names (e.g. plataformas.udgplus.udg.mx) outside stashed links/code.
    s = s.replace(/(^|[\s(«"'\x01>])((?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+(?:udg\.mx|gob\.mx|edu\.mx|org\.mx|com\.mx|mx|org|com|net|edu|gov|io|dev|app|ai)(?:\/[^\s<>"'\x01)*\]».,;!?]*)?)/g,
        (m, pre, domain) => pre + put(anchor("https://" + domain, domain)));
    s = emphasis(s).replace(/\\([\\*_{}\[\]()#+.!~-])/g, "$1").replace(/\n/g, "<br>");
    for (let i = stash.length - 1; i >= 0; i--)
        s = s.split("\x01" + i + "\x01").join(stash[i]);
    return s;
}

function format(text, options) {
    options = options || {};
    const color = /^#[0-9a-f]{3,8}$/i.test(String(options.linkColor)) ? String(options.linkColor) : "#517cbb";
    const quoteColor = /^#[0-9a-f]{3,8}$/i.test(String(options.quoteColor)) ? String(options.quoteColor) : color;
    const render = value => inline(value, color, options.linkLabel || "Link");
    const lines = normalizePlainEmail(text).split("\n");
    const cells = line => line.trim().replace(/^\||\|$/g, "").split("|").map(value => value.trim());
    const tableSeparator = line => /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(line);
    const listMatch = line => line.match(/^(\s*)(?:([-+*])|(\d+)[.)])\s+(.+)$/);
    const isBlockStart = (idx) => {
        const l = lines[idx];
        return /^\s*(?:>|```|#{1,6}\s|[-+*]\s|\d+[.)]\s|[-*_]{3,}\s*$)/.test(l)
            || (idx + 1 < lines.length && l.includes('|') && tableSeparator(lines[idx + 1]));
    };
    const parseList = () => {
        const first = listMatch(lines[i]);
        if (!first) return "";
        const ordered = !!first[3];
        const baseIndent = first[1].length;
        const tag = ordered ? 'ol' : 'ul';
        let out = '<' + tag + (ordered ? ' start="' + Number(first[3]) + '"' : '') + '>';
        while (i < lines.length) {
            const item = listMatch(lines[i]);
            if (!item || !!item[3] !== ordered || item[1].length < baseIndent) break;
            const itemParts = [item[4].trim()];
            i++;
            let nestedHtml = "";
            while (i < lines.length && lines[i].trim()) {
                if (/^\s*(?:>|```|#{1,6}\s|[-*_]{3,}\s*$)/.test(lines[i])
                    || (i + 1 < lines.length && lines[i].includes('|') && tableSeparator(lines[i + 1]))) {
                    break;
                }
                const nextList = listMatch(lines[i]);
                if (nextList) {
                    if (nextList[1].length > baseIndent) {
                        nestedHtml += parseList();
                        continue;
                    }
                    break;
                }
                itemParts.push(lines[i++].trim());
            }
            out += '<li>' + render(itemParts.join(" ")) + nestedHtml + '</li>';
        }
        out += '</' + tag + '>';
        return out;
    };
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
        if (listMatch(line)) {
            html += parseList();
            continue;
        }
        const paragraph = [line];
        i++;
        while (i < lines.length && lines[i].trim() && !isBlockStart(i)) {
            paragraph.push(lines[i++]);
        }
        html += '<p style="margin-top:0px; margin-bottom:10px; line-height:132%">' + render(paragraph.join("\n")) + '</p>';
    }
    return html;
}

function extractLinks(text) {
    const html = format(text || "");
    const seen = {};
    const out = [];
    const re = /<a href="([^"]+)"[^>]*>(?:<font[^>]*>)?([\s\S]*?)(?:<\/font>)?<\/a>/gi;
    let m;
    while ((m = re.exec(html)) !== null) {
        const rawUrl = m[1]
            .replace(/&amp;/g, "&")
            .replace(/&lt;/g, "<")
            .replace(/&gt;/g, ">")
            .replace(/&quot;/g, '"')
            .replace(/&#39;/g, "'");
        const url = unwrapUrl(rawUrl);
        if (!safeUrl(url) || seen[url]) continue;
        seen[url] = true;
        const label = m[2].replace(/<[^>]+>/g, "").replace(/&amp;/g, "&").trim() || url;
        out.push({ url: url, label: label });
    }
    return out;
}
