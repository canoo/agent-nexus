// Closed protocol shared by the Chrome UI and native adapter. No executable,
// path, environment, shell command or arbitrary CLI argument crosses this API.
export const READS = ['status.get','config.get','health.check','tasks.list','usage.get','activity.list','update.check'];
export const OPERATIONS = [...READS,'install.run','config.set','update.apply','uninstall.run','route.preview','route.run','memory.list','memory.show','memory.save','memory.delete','companion.status','companion.setup','companion.pause','companion.resume','companion.retention','companion.prune','companion.clear','companion.disable','companion.consent','native.register','native.unregister'];
export const MUTATIONS = OPERATIONS.filter(x => !READS.includes(x) && !['route.preview','memory.list','memory.show','companion.status'].includes(x));
export const CONFIG_KEYS = ['NEXUS_LOCAL_AI','OLLAMA_HOST_URL','NEXUS_SUPERVISOR_MODEL','NEXUS_LOGIC_MODEL','NEXUS_CLAUDE_SESSION_RETENTION_DAYS'];
const object = x => x !== null && typeof x === 'object' && !Array.isArray(x);
const exact = (x, keys) => object(x) && Object.keys(x).length === keys.length && keys.every(k => Object.hasOwn(x,k));
const text = (x, max = 2048) => typeof x === 'string' && x.length <= max && !/[\u0000\r]/.test(x);
const component = x => text(x,100) && /^[A-Za-z0-9][A-Za-z0-9_.-]*$/.test(x) && !x.includes('..');
export function validRequest(r) {
  if (!exact(r,['kind','schemaVersion','operation','params','confirm']) || r.kind !== 'nexus-control' || r.schemaVersion !== 1 || !OPERATIONS.includes(r.operation) || typeof r.confirm !== 'boolean' || !object(r.params)) return false;
  if (MUTATIONS.includes(r.operation) && !r.confirm) return false;
  const p = r.params;
  switch (r.operation) {
    case 'config.set': return exact(p,['values']) && object(p.values) && Object.keys(p.values).length > 0 && Object.entries(p.values).every(([k,v]) => CONFIG_KEYS.includes(k) && text(v) && !v.includes('\n') && (k !== 'NEXUS_LOCAL_AI' || ['true','false'].includes(v)));
    case 'update.apply': return exact(p,['version']) && text(p.version,100) && /^v?\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?$/.test(p.version);
    case 'route.preview': case 'route.run': return exact(p,['prompt','allowCloud']) && text(p.prompt,16000) && p.prompt.trim().length > 0 && typeof p.allowCloud === 'boolean';
    case 'memory.list': return exact(p,['project']) && (p.project === '' || component(p.project));
    case 'memory.show': case 'memory.delete': return exact(p,['project','name']) && component(p.project) && component(p.name);
    case 'memory.save': return exact(p,['project','title','body']) && component(p.project) && text(p.title,200) && p.title.trim().length > 0 && text(p.body,16000);
    case 'companion.retention': return exact(p,['days']) && Number.isInteger(p.days) && p.days >= 0 && p.days <= 365;
    case 'companion.consent': return exact(p,['tool','enabled']) && ['chatgpt','claude','gemini','copilot','perplexity'].includes(p.tool) && typeof p.enabled === 'boolean';
    case 'native.register': return exact(p,['extensionId']) && typeof p.extensionId === 'string' && /^[a-p]{32}$/.test(p.extensionId);
    default: return exact(p,[]);
  }
}
// Bounded JSON only. Rendering uses textContent, never HTML, terminal escapes,
// object prototypes or host-provided asset URLs. Replies are operation-correlated.
function safeData(v, depth = 0) {
  if (depth > 12) return false;
  if (v === null || typeof v === 'boolean') return true;
  if (typeof v === 'number') return Number.isFinite(v);
  if (typeof v === 'string') return v.length <= 65536 && !/[\u0000-\u0008\u000b\u000c\u000e-\u001f]/.test(v);
  if (Array.isArray(v)) return v.length <= 5000 && v.every(x => safeData(x,depth+1));
  return object(v) && Object.keys(v).length <= 100 && Object.entries(v).every(([k,x]) => !['__proto__','prototype','constructor'].includes(k) && k.length <= 200 && safeData(x,depth+1));
}
export function validResponse(r, operation) {
  if (!object(r) || r.schemaVersion !== 1 || r.operation !== operation || typeof r.ok !== 'boolean') return false;
  if (!r.ok) return exact(r,['schemaVersion','operation','ok','error']) && ['host_unavailable','request_invalid','control_failed','response_invalid'].includes(r.error);
  if (!exact(r,['schemaVersion','operation','ok','result']) || !safeData(r.result) || JSON.stringify(r).length > 750000) return false;
  const v = r.result;
  const count = x => Number.isSafeInteger(x) && x >= 0;
  const strings = (x, keys) => object(x) && keys.every(k => typeof x[k] === 'string');
  const snapshot = x => object(x) && x.schemaVersion === 1 && x.ok === true
    && typeof x.collectionEnabled === 'boolean' && count(x.retentionDays) && x.retentionDays <= 365
    && count(x.storedSpans) && x.policyVersion === 1 && Array.isArray(x.consents)
    && x.consents.every(c => strings(c,['adapterId','toolId']) && typeof c.enabled === 'boolean');
  switch (operation) {
    case 'tasks.list': return v === null || (Array.isArray(v) && v.every(x => strings(x,['tool','model']) && typeof x.ok === 'boolean' && count(x.ms)));
    case 'memory.list': return Array.isArray(v) && v.every(x => strings(x,['name','title','date','preview']));
    case 'memory.show': return exact(v,['body']) && typeof v.body === 'string';
    case 'config.get': case 'config.set': return exact(v,CONFIG_KEYS) && Object.values(v).every(x => typeof x === 'string');
    case 'status.get': return object(v) && typeof v.version === 'string' && object(v.personas) && object(v.executables);
    case 'health.check': return object(v) && typeof v.ollamaUp === 'boolean' && object(v.links);
    case 'route.preview': return object(v) && ['local','agy'].includes(v.destination) && typeof v.complexity === 'string' && typeof v.reason === 'string';
    case 'route.run': return exact(v,['output']) && typeof v.output === 'string';
    case 'install.run': return exact(v,['ok','steps']) && typeof v.ok === 'boolean' && Array.isArray(v.steps)
      && v.steps.every(x => strings(x,['label']) && count(x.index) && typeof x.ok === 'boolean');
    case 'update.check': return strings(v,['current','latest','url']);
    case 'update.apply': return exact(v,['updated']) && v.updated === true;
    case 'uninstall.run': return exact(v,['uninstalled']) && v.uninstalled === true;
    case 'memory.save': return exact(v,['saved']) && v.saved === true;
    case 'memory.delete': return exact(v,['deleted']) && v.deleted === true;
    case 'native.register': case 'native.unregister': return exact(v,['registered']) && v.registered === (operation === 'native.register');
    case 'activity.list': return strings(v,['state']) && Array.isArray(v.entries)
      && v.entries.every(x => strings(x,['toolId','surface','startedAt','endedAt']));
    case 'usage.get': return object(v) && object(v.native) && object(v.native.models) && object(v.tokscale)
      && count(v.native.tasks) && typeof v.tokscale.state === 'string' && Array.isArray(v.tokscale.entries)
      && v.tokscale.entries.every(x => strings(x,['provider','model']) && Number.isFinite(x.inputTokens) && Number.isFinite(x.outputTokens) && Number.isFinite(x.costUSD));
    case 'companion.status': case 'companion.pause': case 'companion.resume': case 'companion.disable': case 'companion.consent':
      return snapshot(v) && v.action === operation.slice(10);
    case 'companion.setup': return object(v) && v.schemaVersion === 1 && v.ok === true && v.action === 'initialize' && count(v.retentionDays) && count(v.storedSpans);
    case 'companion.prune': case 'companion.clear': case 'companion.retention': return object(v) && v.schemaVersion === 1 && v.ok === true
      && v.action === operation.slice(10) && count(v.deleted) && count(v.retentionDays) && v.retentionDays <= 365;
    default: return false;
  }
}
export async function requestControl(operation, params = {}, confirm = false, send = payload => chrome.runtime.sendNativeMessage('com.codelogiic.nexus.companion',payload)) {
  const request = {kind:'nexus-control',schemaVersion:1,operation,params,confirm};
  if (!validRequest(request)) throw new Error('Check the highlighted settings and confirm this action.');
  let reply;
  try { reply = await send(request); } catch { throw new Error('Native host unavailable. Install the Companion package and register this Chrome extension first.'); }
  if (!validResponse(reply,operation)) throw new Error('The native host returned an invalid response. Update the Companion package.');
  if (!reply.ok) throw new Error('This action could not complete. Check installation and local permissions, then retry.');
  return reply.result;
}
