import assert from 'node:assert/strict';
import {existsSync,mkdtempSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {spawnSync} from 'node:child_process';
import {DatabaseSync} from 'node:sqlite';
import test from 'node:test';
import {runCompanionDataCLI} from '../companion-data.mjs';
import {createObservabilityStore} from '../lib/observability-store.mjs';
function call(args,options={}) {
 let text=''; const code=runCompanionDataCLI(args,{...options,out:{write(value){text+=value;}}});
 assert.equal(text.split('\n').length,2);return {code,reply:JSON.parse(text)};
}
function fixture(t){const root=mkdtempSync(join(tmpdir(),'companion-data-'));t.after(()=>rmSync(root,{recursive:true,force:true}));const databasePath=join(root,'observability.sqlite');const store=createObservabilityStore({databasePath});store.migrate();return {databasePath,store};}
test('invalid arguments and unconfirmed clear never touch the store or create files',t=>{
 const root=mkdtempSync(join(tmpdir(),'data-missing-'));t.after(()=>rmSync(root,{recursive:true,force:true}));
 const databasePath=join(root,'missing','db');const store=new Proxy({}, {get(){throw new Error('must not invoke');}});
 for(const args of [[],['clear'],['clear','--confirm','extra'],['status','extra'],['retention','--days','-1'],['retention','--days','366'],['retention','--days','1.5'],['retention','--days','1e2'],['retention','--days',''],['unknown']]) {
  assert.deepEqual(call(args,{databasePath,store}),{code:2,reply:{schemaVersion:1,ok:false,error:'companion_command_invalid'}});
 }
 assert.equal(existsSync(join(root,'missing')),false);
});
test('status is read-only and maintenance summaries reflect real migrated data',t=>{
 const f=fixture(t),db=new DatabaseSync(f.databasePath);t.after(()=>db.close());
 db.exec("INSERT INTO tool_activity (id,tool_id,surface,started_at,ended_at,detector,confidence,browser_family,platform,schema_version,consent_policy_version) VALUES ('old','chatgpt','browser','2000-01-01T00:00:00Z','2000-01-01T00:00:01Z','selected-browser-tab','surface-active','chrome','linux',1,1);");
 assert.deepEqual(call(['status'],f),{code:0,reply:{schemaVersion:1,ok:true,action:'status',retentionDays:14,storedSpans:1}});
 assert.equal(db.prepare('SELECT COUNT(*) n FROM tool_activity').get().n,1);
 assert.deepEqual(call(['prune'],f),{code:0,reply:{schemaVersion:1,ok:true,action:'prune',deleted:1,retentionDays:14}});
 assert.deepEqual(call(['retention','--days','0'],f),{code:0,reply:{schemaVersion:1,ok:true,action:'retention',deleted:0,retentionDays:0}});
 assert.deepEqual(call(['clear','--confirm'],f),{code:0,reply:{schemaVersion:1,ok:true,action:'clear',deleted:0,retentionDays:0}});
});
test('safe store errors survive while private messages and invalid summaries do not',()=>{
 for(const message of ['companion_store_unavailable','companion_retention_invalid','companion_clock_invalid','private SQL/path']) {
  const {code,reply}=call(['prune'],{store:{pruneToolActivity(){throw new Error(message);}}});
  assert.equal(code,1);assert.equal(reply.error,message.startsWith('companion_')?message:'companion_data_unavailable');
 }
 for(const result of [{deleted:-1,retentionDays:14},{deleted:1,retentionDays:366},{deleted:NaN,retentionDays:14}])assert.equal(call(['prune'],{store:{pruneToolActivity(){return result;}}}).reply.error,'companion_data_unavailable');
 let writes=0;assert.equal(runCompanionDataCLI(['prune'],{store:{pruneToolActivity(){return {deleted:0,retentionDays:14};}},out:{write(){writes++;throw new Error('broken pipe');}}}),1);assert.equal(writes,1);
});
test('standalone helper never creates a missing store and malformed schema fails closed',t=>{
 const root=mkdtempSync(join(tmpdir(),'data-cli-'));t.after(()=>rmSync(root,{recursive:true,force:true}));
 const proc=spawnSync(process.execPath,[join(import.meta.dirname,'..','companion-data.mjs'),'status'],{env:{...process.env,HOME:root},encoding:'utf8'});
 assert.equal(proc.status,1);assert.equal(JSON.parse(proc.stdout).error,'companion_store_unavailable');assert.equal(existsSync(join(root,'.config')),false);
 const path=join(root,'wrong.sqlite'),db=new DatabaseSync(path);db.close();
 assert.equal(call(['status'],{databasePath:path}).reply.error,'companion_data_unavailable');
});
