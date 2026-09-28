const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = fs.readFileSync(path.join(__dirname, '../../quickshell/Common/ThreadSearch.js'), 'utf8');
const api = vm.runInNewContext(source.replace(/^\.pragma library\s*/, '') + '\n({create, syncModel})');
const clone = value => JSON.parse(JSON.stringify(value));

function rig() {
    const calls = [], changes = [], states = [], errors = [];
    const controller = api.create((params, callback) => calls.push({params: clone(params), callback}),
        rows => changes.push(clone(rows)), (loading, more) => states.push({loading, more}), e => errors.push(e), 2);
    return {controller, calls, changes, states, errors};
}
const row = id => ({id, subject: `Thread ${id}`, participants: ['Ada <ada@example.org>']});

test('typing invalidates a response before the debounce dispatches the new query', () => {
    const r = rig(); r.controller.refresh({query: 'old'});
    r.controller.setFilters({query: 'new'});
    r.calls[0].callback({result: [row(1)]});
    assert.deepEqual(r.changes.at(-1), []);
    assert.equal(r.states.at(-1).loading, true);
    assert.equal(r.calls.length, 1);
    r.controller.refresh({query: 'new'});
    r.calls[1].callback({result: [row(2)]});
    assert.deepEqual(r.changes.at(-1), [row(2)]);
    assert.equal(r.states.at(-1).loading, false);
});

test('rapid queries and event refreshes coalesce into one latest request', () => {
    const r = rig(); r.controller.refresh({query: 'a'});
    r.controller.refresh({query: 'ab'});
    r.controller.refresh({query: 'abc'});
    r.controller.refresh({query: 'abc'});
    assert.equal(r.calls.length, 1);
    r.calls[0].callback({result: [row(1)]});
    assert.equal(r.calls.length, 2);
    assert.equal(r.calls[1].params.query, 'abc');
    r.calls[1].callback({result: [row(3)]});
    assert.deepEqual(r.changes.at(-1), [row(3)]);
});

test('same-query sync events do not starve usable results', () => {
    const r = rig(); r.controller.refresh({query: 'mail'});
    r.controller.refresh({query: 'mail'});
    r.calls[0].callback({result: [row(1)]});
    assert.deepEqual(r.changes.at(-1), [row(1)]);
    assert.equal(r.calls.length, 2);
    r.calls[1].callback({result: [row(2)]});
    assert.deepEqual(r.changes.at(-1), [row(2)]);
});

test('lookahead and offset expose more results without duplicates', () => {
    const r = rig(); r.controller.refresh({query: 'mail', account: 'one'});
    assert.deepEqual(r.calls[0].params, {query:'mail', account:'one', limit:3, offset:0});
    r.calls[0].callback({result: [row(3), row(2), row(1)]});
    assert.deepEqual(r.changes.at(-1), [row(3), row(2)]);
    assert.equal(r.states.at(-1).more, true);
    r.controller.loadMore(); r.controller.loadMore();
    assert.equal(r.calls.length, 2); assert.equal(r.calls[1].params.offset, 2);
    r.calls[1].callback({result: [row(2), row(1)]});
    assert.deepEqual(r.changes.at(-1), [row(3), row(2), row(1)]);
    assert.equal(r.states.at(-1).more, false);
    r.controller.loadMore(); assert.equal(r.calls.length, 2);
});

test('account changes reject an in-flight page from the previous account', () => {
    const r = rig(); r.controller.refresh({account:'one'});
    r.calls[0].callback({result: [row(3), row(2), row(1)]});
    r.controller.loadMore(); r.controller.refresh({account:'two'});
    r.calls[1].callback({result: [row(1)]});
    assert.deepEqual(r.changes.at(-1), []);
    assert.equal(r.calls[2].params.offset, 0);
    assert.equal(r.calls[2].params.account, 'two');
    r.calls[2].callback({result: [row(5)]});
    assert.deepEqual(r.changes.at(-1), [row(5)]);
});

test('disconnect resets the queue and ignores callbacks from the old connection', () => {
    const r = rig(); r.controller.refresh({query:'mail'});
    r.controller.reset(); r.controller.refresh({query:'mail'});
    r.calls[0].callback({result:[row(1)]});
    assert.deepEqual(r.changes.at(-1), []);
    assert.equal(r.states.at(-1).loading, true);
    r.calls[1].callback({result:[row(2)]});
    assert.deepEqual(r.changes.at(-1), [row(2)]);
});

test('errors clear loading and allow a retry', () => {
    const r = rig(); r.controller.refresh({query:'mail'});
    r.calls[0].callback({error:'database busy'});
    assert.deepEqual(r.errors, ['database busy']);
    assert.equal(r.states.at(-1).loading, false);
    r.controller.refresh({query:'mail'});
    assert.equal(r.calls.length, 2);
});

test('unchanged rows retain delegates; changed, moved and removed rows reconcile', () => {
    const rows = [], ops = [];
    const model = {
        get count() { return rows.length; }, get: i => rows[i],
        insert(i, value) { ops.push('insert'); rows.splice(i, 0, clone(value)); },
        move(from, to) { ops.push('move'); rows.splice(to, 0, rows.splice(from, 1)[0]); },
        setProperty(i, key, value) { ops.push('set'); rows[i][key] = value; },
        remove(i, n) { ops.push('remove'); rows.splice(i, n); }
    };
    api.syncModel(model, [row(1), row(2)]);
    ops.length = 0; api.syncModel(model, [row(1), row(2)]);
    assert.deepEqual(ops, []);
    api.syncModel(model, [{...row(2), unread:true}, row(1), row(3)]);
    assert.deepEqual(ops, ['move', 'set', 'insert']);
    assert.deepEqual(rows.map(x => JSON.parse(x.rowJson)), [{...row(2),unread:true},row(1),row(3)]);
    api.syncModel(model, [row(3)]);
    assert.deepEqual(rows.map(x => x.rowId), [3]);
});
