const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../../quickshell/Common/BodyFormatter.js'), 'utf8');
const {format, safeUrl} = vm.runInNewContext(source.replace(/^\.pragma library\s*/, '') + '\n({format, safeUrl})');
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
