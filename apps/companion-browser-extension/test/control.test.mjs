import test from 'node:test';
import assert from 'node:assert/strict';
import { validRequest, validResponse, requestControl, OPERATIONS, MUTATIONS } from '../lib/control.js';
import { identityKey } from '../lib/identity.js';
const req=(operation,params={},confirm=false)=>({kind:'nexus-control',schemaVersion:1,operation,params,confirm});
test('closed control protocol rejects injection, unknown fields and missing confirmations',()=>{
 for(const operation of OPERATIONS) if(MUTATIONS.includes(operation)) assert.equal(validRequest(req(operation)),false,operation);
 for(const r of [req('shell.run'),{...req('status.get'),path:'/tmp/run'},req('memory.show',{project:'../x',name:'x'}),req('native.register',{extensionId:'x'},true),req('companion.retention',{days:366},true),req('config.set',{values:{PATH:'unsafe'}},true),req('route.run',{prompt:'task',allowCloud:'yes'},true)]) assert.equal(validRequest(r),false);
 assert.equal(validRequest(req('route.run',{prompt:'task',allowCloud:false},true)),true);
});
test('replies are operation correlated, bounded and reject prototype or remote payloads',()=>{
 const r={schemaVersion:1,operation:'health.check',ok:true,result:{ollamaUp:true,links:{}}};
 assert.equal(validResponse(r,'health.check'),true);assert.equal(validResponse(r,'status.get'),false);
 for(const result of [{ollamaUp:1,links:{}},JSON.parse('{"ollamaUp":true,"links":{"__proto__":1}}'),{ollamaUp:true,links:{},data:'x'.repeat(65537)}]) assert.equal(validResponse({...r,result},'health.check'),false);
});
test('transport fails closed for unavailable and malformed hosts',async()=>{
 await assert.rejects(requestControl('status.get',{},false,async()=>{throw Error('secret');}),/Native host unavailable/);
 await assert.rejects(requestControl('status.get',{},false,async()=>({ok:true})),/invalid response/);
});
test('model family identification uses fixed bundled assets',()=>{
 for(const [model,key]of [['claude-sonnet','claude'],['gpt-5','chatgpt'],['gemini-pro','gemini'],['qwen2.5','ollama'],['llama3.2','ollama'],['<script>','model']])assert.equal(identityKey(model),key);
});

test('ordinary config strings and semantic versions pass while control characters fail',()=>{
 assert.equal(validRequest(req('config.set',{values:{NEXUS_SUPERVISOR_MODEL:'qwen2.5-coder:1.5b',OLLAMA_HOST_URL:'http://localhost:11434'}},true)),true);
 assert.equal(validRequest(req('update.apply',{version:'v0.3.0-dev.4'},true)),true);
 assert.equal(validRequest(req('config.set',{values:{OLLAMA_HOST_URL:'line\nline'}},true)),false);
 assert.equal(validResponse({schemaVersion:1,operation:'route.run',ok:true,result:{output:'ordinary output 0 running'}},'route.run'),true);
});

test('every operation rejects an empty success body instead of reporting a completed action', () => {
 for (const operation of OPERATIONS) {
  assert.equal(validResponse({schemaVersion:1,operation,ok:true,result:{}},operation),false,operation);
 }
 const reply = (operation,result) => validResponse({schemaVersion:1,operation,ok:true,result},operation);
 assert.equal(reply('update.apply',{updated:true}),true);
 assert.equal(reply('uninstall.run',{uninstalled:true}),true);
 assert.equal(reply('native.unregister',{registered:true}),false);
 assert.equal(reply('tasks.list',[null]),false);
 assert.equal(reply('activity.list',{state:'ready',entries:[{toolId:'claude'}]}),false);
 assert.equal(reply('companion.status',{schemaVersion:1,ok:true,action:'status',collectionEnabled:false,policyVersion:1,retentionDays:14,storedSpans:0,consents:[]}),true);
});
