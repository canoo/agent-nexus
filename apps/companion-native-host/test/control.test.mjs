import test from 'node:test';
import assert from 'node:assert/strict';
import { handleControl } from '../lib/control.mjs';
const request=(operation,params={},confirm=true)=>({kind:'nexus-control',schemaVersion:1,operation,params,confirm});
test('Chrome companion controls use fixed arguments and JSON parser contract',()=>{
 for(const operation of ['companion.status','companion.pause','companion.resume','companion.disable','companion.consent']){
 let called=false;
 const result=handleControl(request(operation,operation==='companion.consent'?{tool:'claude',enabled:true}:{}),{home:'/fixture',run:(binary,args,options)=>{called=true;assert.equal(binary,'/fixture/.local/bin/nexus');assert.deepEqual(args,['control','--json']);assert.equal(options.shell,false);const r=JSON.parse(options.input);assert.equal(r.params.args.at(-1),'--json');return{status:0,stdout:JSON.stringify({schemaVersion:1,operation:r.operation,ok:true,result:{exitCode:0,result:{schemaVersion:1,ok:true,action:operation.slice(10),collectionEnabled:false,policyVersion:1,retentionDays:14,storedSpans:0,consents:[]}}})};}});
 assert.equal(called,true);assert.equal(result.ok,true);
 }
});
test('native controls never execute invalid requests or reflect process output',()=>{
 const run=()=>assert.fail('must not execute');
 assert.equal(handleControl({...request('status.get'),command:'sh'}, {run}).error,'request_invalid');
 const r=handleControl(request('status.get'),{run:()=>({status:1,stdout:'secret token'})});assert.equal(JSON.stringify(r).includes('secret'),false);assert.equal(r.error,'control_failed');
});

test('memory translation handles real tab-delimited CLI rows and never exposes arbitrary CLI arguments', () => {
 const cases = [
  ['memory.list',{project:''},['list'],{output:'demo\t2 files\t2026-10-30\n'}],
  ['memory.list',{project:'demo'},['show','demo'],{output:'note.md\tA note\t2026-10-30\tPreview\n'}],
  ['memory.show',{project:'demo',name:'note.md'},['show','demo','note.md'],{output:'A note\nBody'}],
  ['memory.save',{project:'demo',title:'A note',body:'Body'},['save','demo','--title','A note','--body','Body'],{output:'Saved'}],
 ];
 for(const [operation,params,args,result] of cases){
  const reply=handleControl(request(operation,params),{run:(_binary,_args,options)=>{
   const req=JSON.parse(options.input);
   assert.equal(req.operation,'memory.command');
   assert.deepEqual(req.params.args,args);
   return {status:0,stdout:JSON.stringify({schemaVersion:1,operation:'memory.command',ok:true,result})};
  }});
  assert.equal(reply.ok,true,operation);
  if(operation==='memory.list')assert.equal(reply.result.length,1);
 }
});

test('native Chrome registration is explicit and core replies cannot fake successful mutations', () => {
 let registered;
 const result=handleControl(request('native.register',{extensionId:'a'.repeat(32)}),{register:options=>{registered=options;}});
 assert.equal(result.ok,true);
 assert.equal(registered.browser,'chrome');
 assert.deepEqual(registered.extensionIds,['a'.repeat(32)]);
 assert.ok(registered.hostPath.endsWith('/nexus-companion-native-host-chrome.mjs'));
 for(const operation of ['update.apply','uninstall.run','route.run']){
  const params=operation==='update.apply'?{version:'v0.3.0'}:operation==='route.run'?{prompt:'A task',allowCloud:false}:{};
  const reply=handleControl(request(operation,params),{run:(_binary,_args,options)=>{
   const req=JSON.parse(options.input);
   assert.equal(req.confirm,true);
   if(operation==='route.run')assert.equal(req.params.allowCloud,false);
   return{status:0,stdout:JSON.stringify({schemaVersion:1,operation,ok:true,result:{}})};
  }});
  assert.equal(reply.error,'response_invalid');
 }
});
