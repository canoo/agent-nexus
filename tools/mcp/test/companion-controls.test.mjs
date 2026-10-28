import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtempSync, readFileSync, rmSync, existsSync, symlinkSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import { createObservabilityStore } from '../lib/observability-store.mjs';
import { runCompanionControlsCLI } from '../companion-controls.mjs';
function fixture(t) {
 const root=mkdtempSync(join(tmpdir(),'nexus-controls-'));t.after(()=>rmSync(root,{recursive:true,force:true}));
 const databasePath=join(root,'logs','observability.sqlite');
 const store=createObservabilityStore({databasePath});store.migrate();
 function call(args,code=0,path=databasePath) {let text='';assert.equal(runCompanionControlsCLI(args,{databasePath:path,out:{write:s=>{text+=s;}}}),code,text);return JSON.parse(text);}
 function sql(fn) {const db=new DatabaseSync(databasePath);try{return fn(db);}finally{db.close();}}
 const grant=(adapter='browser-chrome',tool='chatgpt')=>call(['consent','grant','--adapter',adapter,'--tool',tool,'--acknowledge-policy','1']);
 return {root,databasePath,store,call,sql,grant};
}
test('status is fixed, read-only, non-pruning and excludes unknown consent rows',t=>{
 const f=fixture(t);f.sql(db=>db.exec("INSERT INTO companion_tool_consents VALUES('private-path','private-tool',1,1,'private-time')"));
 const before=readFileSync(f.databasePath);const status=f.call(['status']);
 assert.equal(status.collectionEnabled,false);assert.equal(status.consents.length,10);assert.equal(status.storedSpans,0);assert.ok(!JSON.stringify(status).includes('private'));assert.deepEqual(readFileSync(f.databasePath),before);
});
test('invalid/unacknowledged commands never create a store or grant policy',t=>{
 const f=fixture(t);const missing=join(f.root,'missing.sqlite');
 for(const args of [[],['resume','extra'],['disable'],['consent','grant','--adapter','browser-chrome','--tool','chatgpt'],['consent','grant','--adapter','browser-chrome','--tool','chatgpt','--acknowledge-policy','2'],['consent','revoke','--adapter','browser-firefox','--tool','chatgpt']]) assert.equal(f.call(args,2,missing).error,'companion_command_invalid');
 assert.equal(existsSync(missing),false);assert.equal(f.call(['status'],1,missing).error,'companion_store_unavailable');assert.equal(existsSync(missing),false);
});
test('resume requires current supported browser consent and failures roll back',t=>{
 const f=fixture(t);assert.equal(f.call(['resume'],1).error,'companion_consent_required');
 f.sql(db=>db.exec("INSERT INTO companion_tool_consents VALUES('browser-chrome','chatgpt',1,2,'2026-01-01'); INSERT INTO companion_tool_consents VALUES('desktop-foreground-app','chatgpt',1,1,'2026-01-01')"));
 assert.equal(f.call(['resume'],1).error,'companion_consent_required');assert.equal(f.call(['status']).collectionEnabled,false);
});
test('per-tool grants do not enable collection; revoke stays adapter/tool-specific',t=>{
 const f=fixture(t);assert.equal(f.grant().collectionEnabled,false);f.grant('browser-edge','claude');
 const s=f.call(['consent','revoke','--adapter','browser-chrome','--tool','chatgpt']);
 assert.equal(s.consents.find(c=>c.adapterId==='browser-chrome'&&c.toolId==='chatgpt').enabled,false);
 assert.equal(s.consents.find(c=>c.adapterId==='browser-edge'&&c.toolId==='claude').enabled,true);
});
test('CLI pause/resume/regrant boundaries are enforced by actual activity ingestion',async t=>{
 const f=fixture(t);f.grant();f.call(['resume']);
 const event=()=>({tool_id:'chatgpt',surface:'browser',started_at:new Date(Date.now()-1).toISOString(),ended_at:new Date().toISOString(),detector:'selected-browser-tab',confidence:'surface-active',browser_family:'chrome',platform:'linux',schema_version:1,consent_policy_version:1});
 const old=event();await new Promise(r=>setTimeout(r,5));f.call(['pause']);
 assert.equal(f.store.recordToolActivity(old).sqlite.error,'companion_collection_disabled');
 assert.equal(f.call(['status']).consents[0].enabled,true);f.call(['resume']);
 assert.equal(f.store.recordToolActivity(old).sqlite.error,'companion_activity_crosses_boundary');
 await new Promise(r=>setTimeout(r,5));
 assert.deepEqual(f.store.recordToolActivity(event()).sqlite,{ok:true});
 await new Promise(r=>setTimeout(r,5));f.call(['consent','revoke','--adapter','browser-chrome','--tool','chatgpt']);
 assert.equal(f.store.recordToolActivity(event()).sqlite.error,'companion_tool_consent_missing');f.grant();
 assert.equal(f.store.recordToolActivity(old).sqlite.error,'companion_activity_crosses_boundary');
 assert.equal(f.call(['status']).storedSpans,1);
});
test('confirmed disable revokes all grants, preserves activity and permits no implicit restart',t=>{
 const f=fixture(t);f.grant();f.grant('browser-edge','claude');f.call(['resume']);
 f.sql(db=>db.exec("INSERT INTO tool_activity(id,tool_id,surface,started_at,ended_at,detector,confidence,browser_family,platform,schema_version,consent_policy_version) VALUES('kept','chatgpt','browser','2026-10-01T00:00:00Z','2026-10-01T00:00:01Z','selected-browser-tab','surface-active','chrome','linux',1,1)"));
 const result=f.call(['disable','--confirm']);
 assert.equal(result.collectionEnabled,false);assert.ok(result.consents.every(c=>!c.enabled));assert.equal(result.storedSpans,1);assert.equal(f.call(['resume'],1).error,'companion_consent_required');
});
test('old/malformed stores and symlink database files fail without migration or replacement',t=>{
 const f=fixture(t);const link=join(f.root,'redirect.sqlite');symlinkSync(f.databasePath,link);assert.equal(f.call(['pause'],1,link).error,'companion_store_unavailable');
 f.sql(db=>db.exec('DELETE FROM schema_migrations WHERE version=5'));const before=readFileSync(f.databasePath);
 for(const action of ['status','pause','resume']) assert.equal(f.call([action],1).error,'companion_controls_unavailable');
 assert.deepEqual(readFileSync(f.databasePath),before);
});
test('mutation failure rolls back state and returns only a fixed safe error',t=>{
 const f=fixture(t);f.grant();f.call(['resume']);f.sql(db=>db.exec("CREATE TRIGGER reject_disable BEFORE UPDATE ON companion_tool_consents BEGIN SELECT RAISE(ABORT,'private SQL/path'); END"));
 const result=f.call(['disable','--confirm'],1);assert.equal(result.error,'companion_controls_unavailable');const after=f.call(['status']);assert.equal(after.collectionEnabled,true);assert.equal(after.consents[0].enabled,true);
});


test('invalid enabled boundaries fail status while pause repairs policy without deleting history',t=>{
 const f=fixture(t);f.grant();
 for(const boundary of [null,'private boundary','2026-02-31T00:00:00Z']) {
  f.sql(db=>db.prepare('UPDATE companion_settings SET collection_enabled=1,collection_started_at=?').run(boundary));
  assert.equal(f.call(['status'],1).error,'companion_controls_unavailable');
  assert.equal(f.call(['pause']).collectionEnabled,false);
 }
 f.sql(db=>db.exec('ALTER TABLE companion_settings DROP COLUMN collection_started_at'));
 assert.equal(f.call(['status'],1).error,'companion_controls_unavailable');
});
