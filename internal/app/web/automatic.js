'use strict';
let automaticActive=0;const automaticPending=new Set(),automaticLastTry=new Map();
function queueAutomaticCheck(p){
 if($('app').hidden||!p.enabled||!['configured','needs_connection'].includes(setupState.state))return;
 const key=checkKey(p,'automatic'),outside=serviceChecks.get(checkKey(p,'public'));
 if(automaticPending.has(key)||automaticActive>=4||outside?.state==='running')return;
 if(outside&&Date.now()-Date.parse(outside.checked_at)<300000&&(outside.state==='reachable'||outside.state==='unknown'))return;
 if(Date.now()-(automaticLastTry.get(key)||0)<30000)return;
 automaticPending.add(key);automaticActive++;
 Promise.resolve().then(async()=>{if(!['configured','needs_connection'].includes(setupState.state))return;automaticLastTry.set(key,Date.now());await runServiceCheck(p,'local');const inside=serviceChecks.get(checkKey(p,'local'));if(inside?.state!=='failed')await runServiceCheck(p,'public')}).catch(error=>portMessage(error.message)).finally(()=>{automaticPending.delete(key);automaticActive--})
}
function paintProgress(node,p){
 const outside=serviceChecks.get(checkKey(p,'public')),inside=serviceChecks.get(checkKey(p,'local'));
 let progress=setupState.state==='configured'||setupState.state==='needs_connection'?40:20;
 if(inside?.state==='running')progress=50;else if(inside?.state==='reachable')progress=60;
 if(outside?.state==='running'||outside?.state==='failed'||outside?.state==='unknown')progress=80;
 if(outside?.state==='reachable'&&!(inside?.state==='failed'&&Date.parse(inside.checked_at)>Date.parse(outside.checked_at)))progress=100;
 const text=document.createElement('span');text.textContent=' · Einrichtungsschritte: '+progress+' %';const bar=document.createElement('progress');bar.max=100;bar.value=progress;bar.setAttribute?.('aria-label','Einrichtungsschritte für '+endpointName(p));node.append(text,bar)
}
form('manualServiceForm',async f=>{const save=f.querySelector?.('button[type="submit"]');if(save)save.textContent='Erkennung läuft …';try{const result=await api('services/manual','POST',{target_ip:f.elements.target_ip.value.trim(),ports:f.elements.ports.value.trim(),name:f.elements.name.value.trim(),protocol:f.elements.protocol.value,domain:$('serviceDomain').value.trim()});if(result.routes)webRows=result.routes;renderPorts(result.ports);renderSetup({state:'working',message:result.message});portMessage(result.message);f.reset();$('activeServices').scrollIntoView({behavior:'smooth',block:'start'})}finally{if(save)save.textContent='Erkennen und einrichten'}});
async function autoConfigureExisting(p){portMessage('Dienst und Protokoll werden erneut erkannt …');const result=await api('services/manual','POST',{target_ip:p.target_ip,ports:String(p.target_port),name:p.name||'',protocol:'auto',domain:$('serviceDomain').value.trim()});if(result.routes)webRows=result.routes;renderPorts(result.ports);renderSetup({state:'working',message:result.message});portMessage(result.message)}
