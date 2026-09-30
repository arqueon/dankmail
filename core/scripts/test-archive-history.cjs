const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname,'../../quickshell/Common/ArchiveHistory.js'),'utf8');
const api = vm.runInNewContext(source.replace(/^\.pragma library\s*/, '')+'\n({create})');
const clone = x => JSON.parse(JSON.stringify(x));
function rig() {
 const calls=[], states=[]; let refreshes=0;
 const c=api.create((p,cb)=>calls.push({p:clone(p),cb}),(...s)=>states.push(s),()=>refreshes++);
 return {c,calls,states,get refreshes(){return refreshes}};
}
test('entering archive fetches the first page, then uses opaque continuation tokens',()=>{
 const r=rig();r.c.reset('one',true);r.c.load();
 assert.equal(r.calls.length,1);assert.deepEqual(r.calls[0].p,{account:'one'});
 r.calls[0].cb({result:{supported:1,next:{one:'page-2'},ingested:100}});
 assert.equal(r.refreshes,1);assert.equal(r.states.at(-1)[1],true);
 r.c.load();assert.deepEqual(r.calls[1].p,{account:'one',next:{one:'page-2'}});
 r.calls[1].cb({result:{supported:1,next:{}}});r.c.load();assert.equal(r.calls.length,2);
 assert.equal(r.states.at(-1)[1],false);
});
test('scope changes and disconnects ignore stale responses',()=>{
 const r=rig();r.c.reset('one',true);r.c.reset('two',true);
 r.calls[0].cb({result:{supported:1,next:{one:'old'}}});assert.equal(r.refreshes,0);
 r.calls[1].cb({result:{supported:1,next:{two:'new'}}});r.c.load();
 assert.deepEqual(r.calls[2].p,{account:'two',next:{two:'new'}});
 r.c.reset('',false);r.calls[2].cb({error:'late'});assert.deepEqual(r.states.at(-1),[false,false,'',false]);
});
test('first-page failures are retryable; partial failures preserve only failed cursors',()=>{
 const r=rig();r.c.reset('',true);r.calls[0].cb({error:'offline'});
 assert.deepEqual(r.states.at(-1),[false,true,'offline',false]);r.c.load();assert.deepEqual(r.calls[1].p,{});
 r.calls[1].cb({result:{supported:2,next:{failed:''},warning:'quota'}});r.c.load();
 assert.deepEqual(r.calls[2].p,{next:{failed:''}});
});
test('unsupported providers stop remote pagination and identify cached-only views',()=>{
 const r=rig();r.c.reset('one',true);r.calls[0].cb({result:{supported:0,next:{}}});
 assert.deepEqual(r.states.at(-1),[false,false,'',true]);r.c.load();assert.equal(r.calls.length,1);
});
