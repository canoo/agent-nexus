import test from 'node:test';
import assert from 'node:assert/strict';
import { CONFIG_KEYS } from '../lib/control.js';
class Node {
 constructor(tag){this.tag=tag;this.children=[];this.listeners={};this.attributes={};this.value='';this.checked=false;this.textContent='';}
 append(...nodes){this.children.push(...nodes);for(const n of nodes)n.parent=this;}
 replaceChildren(...nodes){this.children=[];this.append(...nodes);}
 replaceWith(node){const i=this.parent.children.indexOf(this);this.parent.children[i]=node;node.parent=this.parent;}
 setAttribute(k,v){this.attributes[k]=v;}
 addEventListener(event,fn){this.listeners[event]=fn;}
 async click(){await this.listeners.click?.();await new Promise(r=>setTimeout(r,0));}
 all(){return [this,...this.children.flatMap(n=>n.all())];}
}
test('dashboard navigation renders all surfaces and explicit confirmed actions use the native protocol',async()=>{
 const selectors=Object.fromEntries(['#view','#notice','#navigation','#tool-list'].map(k=>[k,new Node('div')]));
 const prior={document:globalThis.document,chrome:globalThis.chrome,window:globalThis.window};
 const calls=[];let confirm=false;
 const replies={
 'status.get':{version:'0.3.0',personas:{count:5},executables:{nexus:true}},
 'config.get':Object.fromEntries(CONFIG_KEYS.map(k=>[k,k==='NEXUS_LOCAL_AI'?'true':'qwen2.5'])),
 'health.check':{ollamaUp:true,links:{core:true},gpu:'GPU ready'},
 'tasks.list':[{model:'qwen2.5',tool:'ollama',ok:true,ms:20}],
 'usage.get':{native:{tasks:1,models:{'qwen2.5':1}},tokscale:{state:'unavailable',entries:[]}},
 'memory.list':[{name:'demo',title:'A memory',date:'2026-10-30',preview:'Preview'}],
 'memory.show':{body:'Memory body'},
 'companion.status':{schemaVersion:1,ok:true,action:'status',policyVersion:1,collectionEnabled:false,retentionDays:14,storedSpans:0,consents:[{adapterId:'browser-chrome',toolId:'claude',enabled:false}]},
 'route.preview':{destination:'local',complexity:'simple',reason:'Local eligible'},
 'install.run':{ok:true,steps:[{index:0,label:'Install core',ok:true}]},
 'route.run':{output:'Task response'},
 'update.check':{current:'0.3.0-dev.1',latest:'v0.3.0',url:'https://example.invalid/release'},
 'update.apply':{updated:true},
 'uninstall.run':{uninstalled:true},
 'memory.save':{saved:true},
 'memory.delete':{deleted:true},
 'native.register':{registered:true},
 'native.unregister':{registered:false},
 'activity.list':{state:'available',entries:[]},
 };
 replies['config.set']=replies['config.get'];
 for(const action of ['pause','resume','disable','consent'])replies[`companion.${action}`]={...replies['companion.status'],action};
 replies['companion.setup']={schemaVersion:1,ok:true,action:'initialize',retentionDays:14,storedSpans:0};
 for(const action of ['prune','clear','retention'])replies[`companion.${action}`]={schemaVersion:1,ok:true,action,retentionDays:14,deleted:0};
 globalThis.document={createElement:tag=>new Node(tag),querySelector:s=>selectors[s],querySelectorAll:s=>s==='nav button'?selectors['#navigation'].children:[]};
 globalThis.window={confirm:()=>confirm};
 globalThis.chrome={storage:{local:{get:async()=>({})}},runtime:{id:'a'.repeat(32),sendNativeMessage:async(host,r)=>{assert.equal(host,'com.codelogiic.nexus.companion');calls.push(r);return {schemaVersion:1,operation:r.operation,ok:true,result:replies[r.operation]??{done:true}};}}};
 try{
 await import(`../options.js?dashboard-test=${Date.now()}`);await new Promise(r=>setTimeout(r,0));
 const nav=selectors['#navigation'].children;
 assert.equal(nav.length,11);
 for(const b of nav){await b.click();assert.equal(selectors['#view'].children[0].textContent,b.textContent);}
 const go=async area=>nav.find(b=>b.textContent===area).click();
 await go('Install');let install=selectors['#view'].children.find(n=>n.textContent==='Install NEXUS');await install.click();assert.equal(calls.some(r=>r.operation==='install.run'),false);
 confirm=true;await install.click();assert.equal(calls.find(r=>r.operation==='install.run').confirm,true);
 await go('Routing');const run=selectors['#view'].children.find(n=>n.textContent==='Run task');const fields=selectors['#view'].all();fields.find(n=>n.tag==='textarea').value='Write a test';await run.click();assert.equal(calls.find(r=>r.operation==='route.run').params.allowCloud,false);
 await go('Memory');const projectCard=selectors['#view'].all().find(n=>n.tag==='button'&&n.children.some(c=>c.textContent==='demo'));await projectCard.click();assert.equal(calls.at(-1).params.project,'demo');
 await go('Companion');assert.ok(selectors['#view'].all().some(n=>n.tag==='img'&&n.alt==='claude'));
 const click=async label=>{const b=selectors['#view'].all().find(n=>n.tag==='button'&&n.textContent===label);assert.ok(b,label);await b.click();assert.equal(selectors['#notice'].textContent.includes('invalid'),false,label);};
 for(const label of ['Set up activity','Pause','Resume','Prune expired activity','Clear activity','Disable collection','Save retention','Grant native consent','Revoke native consent','Register Chrome native host','Refresh activity','Unregister Chrome native host'])await click(label);
 await go('Configure');await click('Save configuration');
 assert.equal(calls.find(r=>r.operation==='config.set').params.values.NEXUS_LOCAL_AI,'true');
 await go('Update');await click('Check for updates');await click('Install available update');
 assert.equal(calls.find(r=>r.operation==='update.apply').params.version,'v0.3.0');
 await go('Uninstall');await click('Uninstall NEXUS');
 assert.ok(calls.every(r=>r.kind==='nexus-control'&&r.schemaVersion===1));
 }finally{for(const [k,v]of Object.entries(prior)){if(v===undefined)delete globalThis[k];else globalThis[k]=v;}}
});
