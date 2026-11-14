const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../../quickshell/Common/BodyFormatter.js'), 'utf8');
const {format, safeUrl, unwrapUrl, extractLinks, sanitizeDraftHtml} = vm.runInNewContext(source.replace(/^\.pragma library\s*/, '') + '\n({format, safeUrl, unwrapUrl, extractLinks, sanitizeDraftHtml})');
test('distilled headings, paragraphs, lists, quotes, code and tables retain structure', () => {
 const html = format('# Heading\r\n\r\nFirst line\r\nsecond line\n\n- one\n- two\n\n2. second\n3. third\n\n> quoted\n>> nested\n\n```js\n<a> & code\n```\n\n| A | B |\n| --- | --- |\n| **bold** | value |');
 for (const term of ['<b>Heading</b>', 'First line<br>second line', '<ul><li>one</li><li>two</li></ul>', '<ol start="2">', '<blockquote', '&lt;a&gt; &amp; code', '<table', '<th>A</th>', '<td><b>bold</b></td>']) assert.ok(html.includes(term), term);
 assert.equal((html.match(/<blockquote(?:\s|>)/g)||[]).length, 2);
});
test('raw HTML and hostile links never become executable content', () => {
 const html = format('<script>alert(1)</script> <img src="https://tracker.test/x">\n[x](javascript:alert) [x](data:text/html,test) [x](file:///tmp/x)\nhttps://safe.test/\"onclick=evil\n\x010\x01');
 assert.ok(!/<(?:script|img)\b/.test(html));
 assert.ok(!/href="(?:javascript|data|file):/.test(html));
 assert.ok(html.includes('&lt;script&gt;'));
 assert.ok(!html.includes('undefined'));
 for (const url of ['javascript:alert(1)', 'file:///tmp/a', 'https://x.test/"bad', 'https://x.test/\nfoo', 'https:\\evil.test']) assert.equal(safeUrl(url),false,url);
});
test('links escape attributes, shorten only their labels and never fetch images', () => {
 const long = 'https://example.org/' + 'a'.repeat(100);
 const html = format('[**Visit**](https://example.org/?a=1&b=2)\n![portrait](https://tracker.test/a) [![Button](https://tracker.test/b)](https://example.org/button)\n' + long + '.\n`https://example.org/code`', {linkColor:'#123456'});
 assert.ok(html.includes('href="https://example.org/?a=1&amp;b=2"'));
 assert.ok(html.includes('<b>Visit</b>'));
 assert.ok(html.includes('href="'+long+'"'));
 assert.ok(!html.includes('href="'+long+'."'));
 assert.ok(!html.includes('tracker.test'));
 assert.ok(!html.includes('<img'));
 assert.ok(html.includes('<code>https://example.org/code</code>'));
 assert.equal(safeUrl('mailto:ada@example.org?subject=Hello'),true);
});
test('malformed fences and very deep quotations remain bounded and escaped', () => {
 assert.ok(format('```\n<img src=x>').includes('&lt;img src=x&gt;'));
 assert.ok((format('>'.repeat(5000)+' text').match(/<blockquote(?:\s|>)/g)||[]).length <= 6);
 assert.equal(format(''), '');
});

test('inline code in a link label restores generated tokens safely', () => {
 const html = format('[`details`](https://example.org)');
 assert.ok(html.includes('<code>details</code>'));
 assert.ok(!html.includes('\x01'));
});

test('gmail plain-text links, google redirects, wrapped lists and nested sublists render cleanly', () => {
 const sample = [
  'Les pido un ajuste en *plataformas.udgplus.udg.mx',
  '<https://www.google.com/url?q=http://plataformas.udgplus.udg.mx&source=gmail&ust=1792185885752000&sa=E>*',
  '',
  '   1. *Recomendada:* en *Administración del sitio →',
  '   Definir roles*, marcar la casilla.',
  '   2. O aplicarlo directamente:',
  '      - Planeación didáctica (id',
  '      124)',
  '      - Evaluación (id 128)'
 ].join('\r\n');
 const html = format(sample);
 assert.equal(unwrapUrl('https://www.google.com/url?q=http://plataformas.udgplus.udg.mx&source=gmail'), 'http://plataformas.udgplus.udg.mx');
 assert.ok(html.includes('href="http://plataformas.udgplus.udg.mx"'));
 assert.ok(html.includes('<b>Recomendada:</b>'));
 assert.ok(html.includes('<b>Administración del sitio → Definir roles</b>'));
 assert.ok(html.includes('<ul><li>Planeación didáctica (id 124)</li><li>Evaluación (id 128)</li></ul>'));
 const links = extractLinks(sample);
 assert.equal(links.length, 1);
 assert.equal(links[0].url, 'http://plataformas.udgplus.udg.mx');
});

test('markdown images and linked images with titles, parens, brackets, multiline blocks and data URIs never leak ! or ()', () => {
 const sample = [
  '![Banner UDG](https://udg.mx/assets/banner_(2026).png "Banner oficial")',
  '![](https://tracker.example.org/open.gif?id=999 "pixel")',
  '![\\[Aviso\\] Logo](https://cdn.example.org/logo.png)',
  '![Firma](data:image/png;base64,' + 'A'.repeat(6000) + ')',
  '[',
  '',
  '![Ver convocatoria](https://ci3.googleusercontent.com/meips/proxy_(1).png "Abrir")',
  '',
  '](https://udg.mx/convocatoria_(2026) "Convocatoria")',
  '[ ![](https://cdn.example.org/icon.png) Abrir documento ](<https://udg.mx/docs/Circular 04.pdf>)',
  '[![](https://cdn.example.org/fb.png)](https://facebook.com/udg)'
 ].join('\n\n');
 const html = format(sample, {linkLabel: 'Enlace'});
 assert.ok(!html.includes('!['), html);
 assert.ok(!html.includes(']('), html);
 assert.ok(!html.includes('tracker.example.org'), html);
 assert.ok(!html.includes('googleusercontent.com'), html);
 assert.ok(!html.includes('data:image'), html);
 assert.ok(html.includes('Banner UDG'), html);
 assert.ok(html.includes('[Aviso] Logo'), html);
 assert.ok(html.includes('href="https://udg.mx/convocatoria_(2026)"'), html);
 assert.ok(html.includes('>Ver convocatoria<'), html);
 assert.ok(html.includes('href="https://udg.mx/docs/Circular%2004.pdf"'), html);
 assert.ok(html.includes('>Abrir documento<'), html);
 assert.ok(html.includes('href="https://facebook.com/udg"'), html);
 assert.ok(html.includes('>Enlace<'), html);
});

test('markdownItalics and sanitizeDraftHtml support rich draft authoring safely', () => {
 const md = format('**bold** *italic* _under_ ~~strike~~ ***both***', {markdownItalics: true});
 assert.ok(md.includes('<b>bold</b>'), md);
 assert.ok(md.includes('<i>italic</i>'), md);
 assert.ok(md.includes('<i>under</i>'), md);
 assert.ok(md.includes('<s>strike</s>'), md);
 assert.ok(md.includes('<b><i>both</i></b>'), md);
 const clean = sanitizeDraftHtml('<h2>Title</h2><script>evil()</script><img src="https://tracker.test/p"><a href="javascript:bad" onclick="x()">x</a><a href="https://example.org">ok</a>');
 assert.ok(clean.includes('<h2>Title</h2>'));
 assert.ok(!clean.includes('<script'));
 assert.ok(!clean.includes('<img'));
 assert.ok(!clean.includes('javascript:'));
 assert.ok(!clean.includes('onclick'));
 assert.ok(clean.includes('href="https://example.org"'));
});
