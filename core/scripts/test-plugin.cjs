const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const plugin = path.join(__dirname, '../../dms-plugin');
const source = fs.readFileSync(path.join(plugin, 'DankmailWidget.qml'), 'utf8');
function rig() {
    const root = {_statusReqId: 1, _threadsReqId: 2, _syncReqId: 3, threads: [{id: 9}], accounts: [{}], syncing: true, unread: 4, dnd: true, requestError: ''};
    const context = vm.createContext({root, syncGuard: {stop() {}}, I18n: {trFor: (_id, text) => text}});
    for (const name of ['clearConnectionState', 'handleResponse']) {
        const fn = source.match(new RegExp('    function ' + name + '\\([^]*?\\n    }'));
        assert.ok(fn, name);
        root[name] = vm.runInContext('(' + fn[0].trim() + ')', context);
    }
    return root;
}
test('disconnect discards stale mail and request IDs before reconnecting', () => {
    const r = rig(); r.clearConnectionState();
    assert.equal(r.threads.length, 0); assert.equal(r.accounts.length, 0);
    assert.equal(r.unread, 0); assert.equal(r.dnd, false); assert.equal(r.syncing, false);
    r.handleResponse({id: 2, result: [{id: 10}]});
    assert.equal(r.threads.length, 0);
});
test('an IPC error preserves the list and explains the failure', () => {
    const r = rig(); r.handleResponse({id: 2, error: {message: 'private diagnostic'}});
    assert.equal(r.threads[0].id, 9); assert.ok(r.requestError.includes('request failed'));
    assert.ok(!r.requestError.includes('private diagnostic'));
    r.handleResponse({id: 3, error: {message: 'sync failed'}});
    assert.equal(r.syncing, false);
});
test('only the latest mailbox request may update the list', () => {
    const r = rig(); r._threadsReqId = 5;
    r.handleResponse({id: 2, result: [{id: 10}]}); assert.equal(r.threads[0].id, 9);
    r.handleResponse({id: 5, result: [{id: 11}]}); assert.equal(r.threads[0].id, 11);
    r.handleResponse({id: 5, result: 'bad payload'}); assert.equal(r.threads[0].id, 11);
});
test('each shipped plugin translation covers its QML strings', () => {
    const qml = fs.readdirSync(plugin).filter(f => f.endsWith('.qml')).map(f => fs.readFileSync(path.join(plugin, f), 'utf8')).join('\n');
    const terms = new Set([...qml.matchAll(/I18n\.trFor\("dankmailUnread", "([^"\n]*)"\)/g)].map(m => m[1]));
    for (const locale of fs.readdirSync(path.join(plugin, 'translations')).filter(f => f.endsWith('.json'))) {
        const table = JSON.parse(fs.readFileSync(path.join(plugin, 'translations', locale)));
        for (const term of terms) assert.equal(typeof table[term]?.[term], 'string', `${locale}: ${term}`);
        for (const term of Object.keys(table)) assert.ok(terms.has(term), `unused: ${term}`);
    }
});
test('the local installer carries plugin translations into a clean install', () => {
    const os = require('node:os');
    const {execFileSync} = require('node:child_process');
    const config = fs.mkdtempSync(path.join(os.tmpdir(), 'dankmail-plugin-install-'));
    try {
        execFileSync('make', ['install-dms-plugin', `USER_CONFIG_HOME=${config}`], {cwd: path.join(plugin, '..'), env: {...process.env, SUDO_USER: ''}});
        const installed = path.join(config, 'DankMaterialShell/plugins/dankmailUnread');
        for (const locale of fs.readdirSync(path.join(plugin, 'translations'))) {
            assert.equal(fs.readFileSync(path.join(installed, 'translations', locale), 'utf8'), fs.readFileSync(path.join(plugin, 'translations', locale), 'utf8'));
        }
    } finally {
        fs.rmSync(config, {recursive: true, force: true});
    }
});
