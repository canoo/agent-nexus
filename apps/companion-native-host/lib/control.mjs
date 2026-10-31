import { spawnSync } from 'node:child_process';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { validRequest, validResponse } from '../../companion-browser-extension/lib/control.js';
import { installNativeHost, uninstallNativeHost } from './registration.mjs';
const failure = (operation,error) => ({schemaVersion:1,operation,ok:false,error});
export function handleControl(request, {run = spawnSync, home = homedir(), register = installNativeHost, unregister = uninstallNativeHost} = {}) {
  const operation = typeof request?.operation === 'string' && request.operation.length < 100 ? request.operation : '';
  if (!validRequest(request)) return failure(operation,'request_invalid');
  try {
    let result;
    const p = request.params;
    if (operation === 'native.register') {
      register({browser:'chrome',extensionIds:[p.extensionId],hostPath:fileURLToPath(new URL('../bin/nexus-companion-native-host-chrome.mjs',import.meta.url))});
      result = {registered:true};
    } else if (operation === 'native.unregister') { unregister({browser:'chrome'}); result = {registered:false}; }
    else {
      let op = operation, params = p;
      if (operation.startsWith('companion.')) {
        const action = operation.slice(10);
        const args = ({status:['status'],setup:['initialize','--confirm','--json'],pause:['pause'],resume:['resume'],disable:['disable','--confirm'],prune:['prune','--json'],clear:['clear','--confirm','--json'],retention:['retention','--days',String(p.days),'--json'],consent:['consent',p.enabled ? 'grant' : 'revoke','--adapter','browser-chrome','--tool',p.tool,...(p.enabled ? ['--acknowledge-policy','1'] : [])]})[action];
        op = 'companion.command'; params = {args: args.at(-1) === '--json' ? args : [...args,'--json']};
      } else if (operation.startsWith('memory.') && operation !== 'memory.delete') {
        op = 'memory.command';
        params = {args: operation === 'memory.list' ? (p.project ? ['show',p.project] : ['list']) : operation === 'memory.show' ? ['show',p.project,p.name] : ['save',p.project,'--title',p.title,'--body',p.body]};
      }
      const input = JSON.stringify({schemaVersion:1,operation:op,params,confirm:request.confirm});
      const child = run(join(home,'.local','bin','nexus'),['control','--json'],{input,encoding:'utf8',shell:false,timeout:11*60*1000,maxBuffer:750000,windowsHide:true});
      if (child.error || child.status !== 0) return failure(operation,'control_failed');
      const reply = JSON.parse(child.stdout);
      if (!reply || reply.schemaVersion !== 1 || reply.ok !== true || reply.operation !== op || Object.keys(reply).sort().join(',') !== 'ok,operation,result,schemaVersion') return failure(operation,'response_invalid');
      result = reply.result;
      if (operation.startsWith('companion.')) {
        if (result?.exitCode !== 0 || result?.result?.ok !== true) return failure(operation,'control_failed');
        result = result.result;
      }
      if (operation === 'memory.list') {
        if (typeof result?.output !== 'string') return failure(operation,'response_invalid');
        result = result.output.trim().split('\n').filter(line => line.includes('\t')).map(line => { const [name,title,date,preview] = line.split('\t'); return {name,title,date:date ?? '',preview:preview ?? ''}; });
      } else if (operation === 'memory.show') result = {body:result.output};
      else if (operation === 'memory.save') result = {saved:true};
    }
    const response = {schemaVersion:1,operation,ok:true,result};
    return validResponse(response,operation) ? response : failure(operation,'response_invalid');
  } catch { return failure(operation,'control_failed'); }
}
