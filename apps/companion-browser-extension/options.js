import { renderToolToggles } from './ui.js';
import { requestControl, MUTATIONS, CONFIG_KEYS } from './lib/control.js';
import { identity, identityKey } from './lib/identity.js';
const view = document.querySelector('#view'), notice = document.querySelector('#notice');
const areas = ['Overview','Install','Configure','Health','Task Log','Usage','Update','Uninstall','Memory','Routing','Companion'];
let epoch = 0;
function el(tag,text,className) {
  const n=document.createElement(tag);
  if(text !== undefined) n.textContent=String(text);
  if(className)n.className=className;
  return n;
}
function input(label,value='',type='text') {
  const box=el('label',undefined,'field');
  box.append(el('span',label));
  const n=el(type==='textarea'?'textarea':'input');
  if(type!=='textarea')n.type=type;
  n.value=value;
  box.append(n);
  view.append(box);
  return n;
}
function cards(values) {
  const grid=el('div',undefined,'cards');
  for(const [key,value] of Object.entries(values ?? {}
  )){
    const card=el('article',undefined,'card');
    card.append(identityKey(key) !== 'model' ? identity(key) : el('p',key),el('strong',typeof value==='boolean' ? (value?'● Ready':'○ Unavailable'):value ?? '—'));
    grid.append(card);
  }
  view.append(grid);
}
function rows(items,columns) {
  if(!items?.length){
    view.append(el('p','Nothing here yet.'));
    return;
  }
  const wrap=el('div',undefined,'table-wrap'),table=el('table'),head=el('tr');
  for(const [label]of columns)head.append(el('th',label));
  table.append(head);
  for(const row of items){
    const tr=el('tr');
    for(const [,key,isIdentity]of columns){
      const td=el('td');
      td.append(isIdentity?identity(row[key] || 'Unknown model'):el('span',row[key]??'—'));
      tr.append(td);
    }
    table.append(tr);
  }
  wrap.append(table);
  view.append(wrap);
}
function button(label,operation,params=()=>({}
),after=renderResult) {
  const b=el('button',label);
  b.type='button';
  view.append(b);
  b.addEventListener('click',async()=>{
    if(MUTATIONS.includes(operation)&&!window.confirm(`${label}? This changes your local NEXUS installation or data.`))return;
    b.disabled=true;
    notice.textContent='Working… Keep this page open.';
    const current=epoch;
    try{
      const result=await requestControl(operation,params(),MUTATIONS.includes(operation));
      if(current===epoch){
        after(result);
        notice.textContent=result.ok === false ? 'Some steps need attention. Review the results.' : 'Completed.';
      }
    }
    catch(error){
      notice.textContent=error.message;
    }
    finally{
      b.disabled=false;
    }
  }
  );
  return b;
}
function renderResult(result) {
  const panel=el('section',undefined,'result');
  panel.append(el('h2',result.ok===false?'○ Needs attention':'● Action complete'));
  for (const step of result.steps ?? []) {
    const card = el('article',undefined,'card');
    card.append(el('strong',step.ok ? '✓ Complete' : '○ Needs attention'),el('p',step.label));
    panel.append(card);
  }
  if (typeof result.deleted === 'number') panel.append(el('p',`${result.deleted} activity records removed.`));
  if (typeof result.retentionDays === 'number') panel.append(el('p',`Activity retention: ${result.retentionDays} days.`));
  view.append(panel);
}
async function load(operation,after){
  const current=epoch;
  notice.textContent='Loading…';
  try{
    const data=await requestControl(operation);
    if(current===epoch){
      after(data);
      notice.textContent='';
    }
  }
  catch(error){
    if(current===epoch)notice.textContent=error.message;
  }
}
function route() {
  const prompt=input('Describe your task','','textarea'),cloud=input('Allow cloud routing and fallback','','checkbox');
  view.append(el('p','Cloud acknowledgement allows task content to leave this device. Preview makes no provider request.'));
  const params=()=>({
    prompt:prompt.value,allowCloud:cloud.checked
  }
  );
  button('Preview route','route.preview',params,r=>{
    const p=el('article',undefined,'card');
    p.append(identity(r.destination==='local'?'Ollama local':'Cloud · provider selected by core'),el('h2',r.complexity),el('p',r.reason));
    view.append(p);
  }
  );
  button('Run task','route.run',params,r=>{
    const output=el('article',undefined,'result');
    output.append(el('h2','Task response'),el('p',r.output));
    view.append(output);
  }
  );
}
function memory(){
  const project=input('Project'),name=input('Memory name');
  const browser=el('div',undefined,'cards');
  view.append(browser);
  async function browse(projectName='') {
    notice.textContent='Loading memories…';
    try{
      const items=await requestControl('memory.list',{
        project:projectName
      }
      );
      browser.replaceChildren();
      if(!items?.length)browser.append(el('p','No memories yet. Save your first memory below.'));
      for(const item of items??[]){
        const card=el('button',undefined,'card');
        card.type='button';
        card.append(el('strong',item.name),el('p',item.title),el('p',item.preview));
        card.addEventListener('click',async()=>{
          if(!projectName){
            project.value=item.name;
            await browse(item.name);
          }
          else{
            name.value=item.name;
            try{
              const r=await requestControl('memory.show',{
                project:projectName,name:item.name
              }
              );
              body.value=r.body;
              notice.textContent='Memory opened.';
            }
            catch(e){
              notice.textContent=e.message;
            }
          }
        }
        );
        browser.append(card);
      }
      notice.textContent='';
    }
    catch(e){
      notice.textContent=e.message;
    }
  }
  const browseButton=el('button','Browse projects');
  browseButton.addEventListener('click',()=>browse());
  view.append(browseButton);
  const refresh=el('button','Open project');
  refresh.addEventListener('click',()=>browse(project.value));
  view.append(refresh);
  const title=input('Title'),body=input('Memory content','','textarea');
  button('Save memory','memory.save',()=>({
    project:project.value,title:title.value,body:body.value
  }
  ),()=>{
    void browse(project.value);
  }
  );
  button('Delete memory','memory.delete',()=>({
    project:project.value,name:name.value
  }
  ),()=>{
    body.value='';
    void browse(project.value);
  }
  );
  void browse();
}
function companion(){
  load('companion.status',r=>{
    cards({
      Collection:r.collectionEnabled?'● Collecting':'○ Paused',Retention:`${r.retentionDays??'—'} days`,'Stored activity':r.storedSpans??0
    }
    );
    rows((r.consents??[]).filter(c=>c.adapterId==='browser-chrome'),[['Native tool consent','toolId',true],['Enabled','enabled']]);
  }
  );
  view.append(el('p','Setup creates the local activity store. Pause stops collection; resume does not grant tool consent. Clear removes stored activity. Disable also revokes collection.'));
  for(const [label,op]of [['Set up activity','setup'],['Pause','pause'],['Resume','resume'],['Prune expired activity','prune'],['Clear activity','clear'],['Disable collection','disable']])button(label,`companion.${op}`,()=>({}),()=>show('Companion'));
  const days=input('Activity retention in days (0 disables storage)','14','number');
  days.min=0;
  days.max=365;
  button('Save retention','companion.retention',()=>({
    days:Number(days.value)
  }
  ),()=>show('Companion'));
  let selectedTool='chatgpt';
  const choices=el('div',undefined,'cards');
  const toolButtons=[];
  for(const id of ['chatgpt','claude','gemini','copilot','perplexity']){
    const choice=el('button',undefined,'card');
    choice.type='button';
    choice.append(identity(id));
    choice.setAttribute('aria-pressed',String(id===selectedTool));
    choice.addEventListener('click',()=>{
      selectedTool=id;
      for(const [key,node] of toolButtons)node.setAttribute('aria-pressed',String(key===id));
    });
    toolButtons.push([id,choice]);
    choices.append(choice);
  }
  choices.setAttribute('aria-label','Choose a tool for native consent');
  view.append(choices);
  view.append(el('p','Grant acknowledges activity consent policy version 1 for Chrome. Browser permission must also be enabled below.'));
  button('Grant native consent','companion.consent',()=>({
    tool:selectedTool,enabled:true
  }
  ),()=>show('Companion'));
  button('Revoke native consent','companion.consent',()=>({
    tool:selectedTool,enabled:false
  }
  ),()=>show('Companion'));
  button('Register Chrome native host','native.register',()=>({
    extensionId:chrome.runtime.id
  }
  ));
  button('Unregister Chrome native host','native.unregister');
  view.append(el('p','First connection requires the Companion package and bootstrap registration to register this extension ID. Registration controls work after a native host connection exists.'));
  button('Refresh activity','activity.list',()=>({}
  ),r=>rows(r.entries,[['Tool','toolId',true],['Surface','surface'],['Started','startedAt'],['Ended','endedAt']]));
}
function show(area){
  epoch++;
  view.replaceChildren();
  notice.textContent='';
  for(const b of document.querySelectorAll('nav button'))b.setAttribute('aria-current',String(b.textContent===area));
  view.append(el('h2',area));
  switch(area){
    case 'Overview':load('status.get',r=>{
      cards({
        Version:r.version,...r.executables
      }
      );
      cards(r.personas);
    }
    );
    break;
    case 'Install':view.append(el('p','Install NEXUS using the same ordered installer as the TUI. The Companion package and trusted NEXUS checkout must already be available.'));
    button('Install NEXUS','install.run');
    break;
    case 'Configure':load('config.get',r=>{
      const fields={}
      ;
      for(const key of CONFIG_KEYS){
        if(key==='NEXUS_LOCAL_AI'){
          const n=input('Enable local AI','','checkbox');
          n.checked=r[key]==='true';
          fields[key]=()=>String(n.checked);
        }
        else{
          const n=input(({
            OLLAMA_HOST_URL:'Ollama address',NEXUS_SUPERVISOR_MODEL:'Supervisor model',NEXUS_LOGIC_MODEL:'Logic model',NEXUS_CLAUDE_SESSION_RETENTION_DAYS:'Claude session retention (days)'
          }
          )[key],r[key]);
          fields[key]=()=>n.value;
          if(key.includes('MODEL')){
            let badge=identity(n.value);
            view.append(badge);
            const updateBadge=()=>{
              const next=identity(n.value);
              badge.replaceWith(next);
              badge=next;
            };
            n.addEventListener('input',updateBadge);
            const presets=el('div',undefined,'cards');
            presets.setAttribute('aria-label',`${key==='NEXUS_SUPERVISOR_MODEL'?'Supervisor':'Logic'} model presets`);
            for(const model of new Set([r[key],'qwen2.5-coder:1.5b','llama3.2:3b'].filter(Boolean))){
              const choice=el('button',undefined,'card');
              choice.type='button';
              choice.append(identity(model));
              choice.addEventListener('click',()=>{n.value=model;updateBadge();});
              presets.append(choice);
            }
            view.append(presets);
            view.append(el('p','Choose a model preset or enter an exact model name above. The model must already be available in Ollama. Save configuration to apply.'));
          }
        }
      }
      button('Save configuration','config.set',()=>({
        values:Object.fromEntries(Object.entries(fields).map(([k,get])=>[k,get()]))
      }
      ));
    }
    );
    break;
    case 'Health':load('health.check',r=>{
      cards({
        Ollama:r.ollamaUp,GPU:r.gpu,'Usage integration':r.tokscale?.state
      }
      );
      cards(r.links);
    }
    );
    break;
    case 'Task Log':load('tasks.list',r=>rows(r,[['Model','model',true],['Tool','tool',true],['Route','routing'],['Type','task_type'],['Duration ms','ms'],['Success','ok']]));
    break;
    case 'Usage':load('usage.get',r=>{
      cards(Object.fromEntries(Object.entries(r.native??{}
      ).filter(([,v])=>typeof v!=='object')));
      rows(Object.entries(r.native?.models??{}
      ).map(([model,count])=>({
        model,count
      }
      )),[['Native model','model',true],['Tasks','count']]);
      rows(r.tokscale?.entries,[['Provider','provider',true],['Model','model',true],['Input tokens','inputTokens'],['Output tokens','outputTokens'],['Cost USD','costUSD']]);
    }
    );
    break;
    case 'Update':button('Check for updates','update.check',()=>({}
    ),r=>{
      cards({
        Installed:r.current,Available:r.latest
      }
      );
      button('Install available update','update.apply',()=>({
        version:r.latest
      }
      ));
    }
    );
    break;
    case 'Uninstall':view.append(el('p','Remove NEXUS through its existing teardown workflow. Review any local work before continuing.'));
    button('Uninstall NEXUS','uninstall.run');
    break;
    case 'Memory':memory();
    break;
    case 'Routing':route();
    break;
    case 'Companion':companion();
    break;
  }
}
for(const area of areas){
  const b=el('button',area);
  b.type='button';
  b.addEventListener('click',()=>show(area));
  document.querySelector('#navigation').append(b);
}
void renderToolToggles(document.querySelector('#tool-list'));
show('Overview');
