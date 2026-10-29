import assert from 'node:assert/strict';
import { existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { DatabaseSync } from 'node:sqlite';
import test from 'node:test';
import { runCompanionDataCLI } from '../companion-data.mjs';
function fixture(t) {
  const root=realpathSync(mkdtempSync(join(tmpdir(),'nexus-setup-')));
  t.after(()=>rmSync(root,{recursive:true,force:true}));
  return {root,databasePath:join(root,'.config/nexus/logs/observability.sqlite')};
}
function call(args,databasePath) {
  let output='';
  const code=runCompanionDataCLI(args,{databasePath,out:{write(text){output+=text;}}});
  assert.equal(output.split('\n').length,2);
  return {code,reply:JSON.parse(output)};
}
test('unconfirmed/malformed setup and status leave missing directories absent',t=>{
  const f=fixture(t);
  for(const args of [['initialize'],['initialize','--confirm','extra'],['initialize','--unknown']]) assert.equal(call(args,f.databasePath).code,2);
  assert.equal(call(['status'],f.databasePath).code,1);
  assert.equal(existsSync(join(f.root,'.config')),false);
});
test('confirmed setup applies owned migrations with private files, disabled collection and no grants',t=>{
  const f=fixture(t);
  assert.deepEqual(call(['initialize','--confirm'],f.databasePath),{code:0,reply:{schemaVersion:1,ok:true,action:'initialize',retentionDays:14,storedSpans:0}});
  assert.equal(lstatSync(f.databasePath).mode&0o777,0o600);
  assert.equal(lstatSync(join(f.root,'.config/nexus/logs')).mode&0o777,0o700);
  const db=new DatabaseSync(f.databasePath,{readOnly:true});
  try {
    const settings=db.prepare('SELECT collection_enabled,collection_started_at FROM companion_settings WHERE id=1').get();
    assert.equal(settings.collection_enabled,0);assert.equal(settings.collection_started_at,null);
    for(const table of ['companion_tool_consents','tool_activity','tasks','sessions']) assert.equal(db.prepare(`SELECT COUNT(*) n FROM ${table}`).get().n,0);
    assert.equal(db.prepare('SELECT COUNT(*) n FROM schema_migrations').get().n,5);
  } finally {db.close();}
  const bytes=readFileSync(f.databasePath);
  assert.equal(call(['initialize','--confirm'],f.databasePath).reply.error,'companion_store_exists');
  assert.deepEqual(readFileSync(f.databasePath),bytes);
});
test('existing malformed data, dangling links and journal remnants are never overwritten',t=>{
  const f=fixture(t);mkdirSync(join(f.root,'.config/nexus/logs'),{recursive:true});
  writeFileSync(f.databasePath,'private-existing-data');
  assert.equal(call(['initialize','--confirm'],f.databasePath).reply.error,'companion_store_exists');
  assert.equal(readFileSync(f.databasePath,'utf8'),'private-existing-data');
  rmSync(f.databasePath);
  symlinkSync(join(f.root,'missing-target'),f.databasePath);
  assert.equal(call(['initialize','--confirm'],f.databasePath).reply.error,'companion_store_exists');
  assert.equal(existsSync(join(f.root,'missing-target')),false);
  rmSync(f.databasePath);
  writeFileSync(f.databasePath+'-wal','existing-journal');
  assert.equal(call(['initialize','--confirm'],f.databasePath).reply.error,'companion_store_exists');
  assert.equal(readFileSync(f.databasePath+'-wal','utf8'),'existing-journal');
  assert.equal(existsSync(f.databasePath),false);
});
test('redirected parent directory and unsupported context cannot create a store',t=>{
  const f=fixture(t);const outside=join(f.root,'outside');mkdirSync(outside);
  symlinkSync(outside,join(f.root,'.config'));
  assert.equal(call(['initialize','--confirm'],f.databasePath).reply.error,'companion_setup_unavailable');
  assert.equal(existsSync(join(outside,'nexus')),false);
  const child=spawnSync(process.execPath,[join(import.meta.dirname,'../companion-data.mjs'),'initialize','--confirm'],{env:{...process.env,HOME:f.root,FLATPAK_ID:'com.example.Companion'},encoding:'utf8',timeout:10000});
  assert.equal(child.status,1);
  assert.equal(JSON.parse(child.stdout).error,'companion_setup_unavailable');
  assert.equal(existsSync(join(outside,'nexus')),false);
});
