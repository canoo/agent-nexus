import assert from 'node:assert/strict';
import {existsSync,mkdtempSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import test from 'node:test';
import {DatabaseSync} from 'node:sqlite';
import {createObservabilityStore} from '../lib/observability-store.mjs';
import {startCompanionRetentionMaintenance} from '../../../apps/companion-native-host/lib/host.mjs';
const NOW=Date.parse('2026-10-07T12:00:00.345Z');
function fixture(t) {
 const dir=mkdtempSync(join(tmpdir(),'nexus-retention-'));t.after(()=>rmSync(dir,{recursive:true,force:true}));
 const databasePath=join(dir,'logs','observability.sqlite');
 const store=createObservabilityStore({databasePath,jsonlPath:join(dir,'legacy.jsonl'),now:()=>NOW});
 store.migrate();return {store,databasePath};
}
function db(path,run){const conn=new DatabaseSync(path);try{return run(conn);}finally{conn.close();}}
function seed(conn,id,end,start=new Date(Date.parse(end)-1000).toISOString()) {
 conn.prepare(`INSERT INTO tool_activity (id,tool_id,surface,started_at,ended_at,detector,confidence,browser_family,platform,schema_version,consent_policy_version)
 VALUES (?,'chatgpt','browser',?,?,'selected-browser-tab','surface-active','chrome','linux',1,1)`).run(id,start,end);
}
function event(end=new Date(NOW).toISOString()){return {tool_id:'chatgpt',surface:'browser',started_at:new Date(Date.parse(end)-1000).toISOString(),ended_at:end,detector:'selected-browser-tab',confidence:'surface-active',browser_family:'chrome',platform:'linux',schema_version:1,consent_policy_version:1};}
function enable(conn){conn.exec("UPDATE companion_settings SET collection_enabled=1; INSERT INTO companion_tool_consents VALUES ('browser-chrome','chatgpt',1,1,'before');");}
function ids(path){return db(path,c=>c.prepare('SELECT id FROM tool_activity ORDER BY id').all().map(r=>r.id));}
test('UTC end cutoff is exact for whole-second and millisecond rows',t=>{
 const {store,databasePath}=fixture(t),cutoff=NOW-14*86400000;
 db(databasePath,c=>{seed(c,'expired-ms',new Date(cutoff-1).toISOString());seed(c,'expired-whole','2026-09-23T12:00:00Z','2026-09-23T12:00:00Z');seed(c,'boundary',new Date(cutoff).toISOString());seed(c,'recent',new Date(cutoff+1).toISOString());});
 assert.deepEqual(store.pruneToolActivity(),{deleted:2,retentionDays:14});assert.deepEqual(ids(databasePath),['boundary','recent']);assert.equal(store.pruneToolActivity().deleted,0);
});
test('cleanup commits even when disabled collection rejects incoming span',t=>{
 const {store,databasePath}=fixture(t);db(databasePath,c=>seed(c,'expired','2026-09-01T00:00:00Z'));
 assert.equal(store.recordToolActivity(event()).sqlite.error,'companion_collection_disabled');assert.deepEqual(ids(databasePath),[]);
});
test('zero retention purges raw history and suppresses new spans without changing collection',t=>{
 const {store,databasePath}=fixture(t);db(databasePath,c=>{enable(c);seed(c,'recent',new Date(NOW).toISOString());});
 assert.deepEqual(store.setCompanionRetentionDays(0),{deleted:1,retentionDays:0});assert.equal(store.recordToolActivity(event()).sqlite.error,'companion_retention_disabled');assert.deepEqual(ids(databasePath),[]);
 assert.equal(db(databasePath,c=>c.prepare('SELECT collection_enabled FROM companion_settings').get().collection_enabled),1);
});
test('shrinking policy prunes atomically and expired incoming spans stay rejected',t=>{
 const {store,databasePath}=fixture(t);db(databasePath,c=>{enable(c);seed(c,'old','2026-10-01T00:00:00Z');seed(c,'new','2026-10-07T00:00:00Z');});
 assert.deepEqual(store.setCompanionRetentionDays(1),{deleted:1,retentionDays:1});assert.deepEqual(ids(databasePath),['new']);
 assert.equal(store.recordToolActivity(event('2026-10-01T00:00:00Z')).sqlite.error,'companion_activity_expired');assert.equal(store.recordToolActivity(event()).sqlite.ok,true);
});
test('invalid policy inputs and clocks leave policy and history untouched',t=>{
 const {store,databasePath}=fixture(t);db(databasePath,c=>seed(c,'keep','2026-09-01T00:00:00Z'));
 for(const days of [-1,366,1.5,'14',NaN,Infinity,null])assert.throws(()=>store.setCompanionRetentionDays(days),/companion_retention_invalid/);
 for(const value of [NaN,Infinity,-1,1.5,253402300800000])assert.throws(()=>createObservabilityStore({databasePath,now:()=>value}).pruneToolActivity(),/companion_clock_invalid/);
 assert.deepEqual(ids(databasePath),['keep']);assert.equal(db(databasePath,c=>c.prepare('SELECT raw_span_retention_days FROM companion_settings').get().raw_span_retention_days),14);
});
test('maintenance never creates a missing database',t=>{
 const dir=mkdtempSync(join(tmpdir(),'nexus-retention-missing-'));t.after(()=>rmSync(dir,{recursive:true,force:true}));const databasePath=join(dir,'missing','db.sqlite');const store=createObservabilityStore({databasePath,now:()=>NOW});
 for(const op of [()=>store.pruneToolActivity(),()=>store.setCompanionRetentionDays(7),()=>store.clearToolActivity()])assert.throws(op,/companion_store_unavailable/);
 assert.equal(existsSync(join(dir,'missing')),false);
});
test('delete failure rolls back changed policy; missing settings cannot delete history',t=>{
 const {store,databasePath}=fixture(t);db(databasePath,c=>{seed(c,'keep','2026-09-01T00:00:00Z');c.exec("CREATE TRIGGER refuse_cleanup BEFORE DELETE ON tool_activity BEGIN SELECT RAISE(ABORT,'private detail'); END;");});
 assert.throws(()=>store.setCompanionRetentionDays(1),/^Error: companion_data_unavailable$/);assert.equal(db(databasePath,c=>c.prepare('SELECT raw_span_retention_days FROM companion_settings').get().raw_span_retention_days),14);
 db(databasePath,c=>c.exec('DROP TRIGGER refuse_cleanup; DELETE FROM companion_settings;'));assert.throws(()=>store.pruneToolActivity(),/companion_retention_invalid/);assert.deepEqual(ids(databasePath),['keep']);
});
test('prune/clear preserve tasks, sessions, receipts, metadata, policy and consent',t=>{
 const {store,databasePath}=fixture(t);store.importLegacyMcpJsonl({inputPath:join(import.meta.dirname,'fixtures','mcp-tasks.jsonl')});store.ensureLegacyJsonlImported();
 const tables=['tasks','sessions','routing_decisions','legacy_import_receipts','store_meta','companion_settings','companion_tool_consents'];
 db(databasePath,c=>{enable(c);seed(c,'expired','2026-09-01T00:00:00Z');seed(c,'recent',new Date(NOW).toISOString());});
 const snapshot=()=>db(databasePath,c=>Object.fromEntries(tables.map(table=>[table,JSON.stringify(c.prepare(`SELECT * FROM ${table}`).all())]))),before=snapshot();
 assert.equal(store.pruneToolActivity().deleted,1);assert.equal(store.clearToolActivity().deleted,1);assert.deepEqual(snapshot(),before);
});
test('host maintenance is immediate, periodic, silent and does not keep process alive',()=>{
 let runs=0,tick,unreferenced=false,cancelled=false;const timer={unref(){unreferenced=true;}};
 const stop=startCompanionRetentionMaintenance({pruneToolActivity(){runs++;throw new Error('private');}},{schedule(callback,interval){assert.equal(interval,1800000);tick=callback;return timer;},cancel(value){assert.equal(value,timer);cancelled=true;}});
 assert.equal(runs,1);tick();assert.equal(runs,2);assert.equal(unreferenced,true);stop();assert.equal(cancelled,true);assert.doesNotThrow(()=>startCompanionRetentionMaintenance({})());
});
