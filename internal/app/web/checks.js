'use strict';
const serviceChecks=new Map(),checkNodes=new Map(),serviceStateNodes=new Map();
let setupState={state:'waiting',message:'Einrichtungsstatus wird geladen …'};
try{for(const [key,value] of JSON.parse(localStorage.getItem('ft-service-checks')||'[]'))if(Date.now()-Date.parse(value.checked_at)<86400000)serviceChecks.set(key,value)}catch(_){}
function checkKey(p,location){const r=p.webRoute;return JSON.stringify([r?'web':'port',r?r.id:p.id,p.protocol,p.target_ip,p.target_port,r?r.domain:'',r?.backend_cert_sha256||'',r?.backend_server_name||'',r?portOf(r):p.public_port,location])}
function updateCheck(key){const nodes=checkNodes.get(key)||[];const value=serviceChecks.get(key);for(const node of nodes){const text=value?.state==='reachable'&&node.location==='public'&&!node.p.webRoute&&node.p.protocol==='tcp'?'Öffentliche TCP-Verbindung bestätigt: '+(oracleAddress||'Oracle')+':'+node.p.public_port+'.':value?.message;node.output.textContent=node.title+': '+(value?(text+(value.checked_at?' · '+formatLocalTime(value.checked_at):'')):'Noch nicht geprüft.');node.output.className=value?.state==='reachable'?'test-ok':value?.state==='failed'?'test-error':'';node.button.disabled=value?.state==='running'}for(const stateKey of new Set(nodes.map(node=>checkKey(node.p,'setup')))){const entry=serviceStateNodes.get(stateKey);if(entry)paintServiceStatus(entry.node,entry.p)}}
function saveChecks(){try{localStorage.setItem('ft-service-checks',JSON.stringify([...serviceChecks].filter(([,v])=>v.state!=='running').slice(-400)))}catch(_){}}
async function runServiceCheck(p,location){
 const key=checkKey(p,location);serviceChecks.set(key,{state:'running',message:'Prüfung läuft …'});updateCheck(key);
 try{const r=p.webRoute;const result=r?await api(location==='local'?'backend-test':'public-test','POST',location==='local'?{id:r.id,backend:r.backend}:{domain:r.domain,public_port:portOf(r)}):await api('port-test','POST',{id:p.id,location,domain:location==='public'?$('serviceDomain').value.trim():''});serviceChecks.set(key,{state:result.reachable===true?'reachable':result.reachable===false?'failed':'unknown',message:result.message||'Keine bestätigte Antwort.',checked_at:result.checked_at||new Date().toISOString()})}catch(error){serviceChecks.set(key,{state:'failed',message:error.message,checked_at:new Date().toISOString()})}
 saveChecks();updateCheck(key)
}
function addServiceChecks(actions,p,container){
 container=container||actions;
 for(const [location,title,label] of [['public','Öffentlich','Öffentlich testen'],['local','Heimnetz','Heimnetz testen']]){const test=button(actions,label,async()=>runServiceCheck(p,location));const output=document.createElement('p');output.setAttribute?.('role','status');container.append(output);const key=checkKey(p,location);if(!checkNodes.has(key))checkNodes.set(key,[]);checkNodes.get(key).push({output,button:test,title,p,location});updateCheck(key)}
}
function paintServiceStatus(node,p){
 const outside=serviceChecks.get(checkKey(p,'public')),inside=serviceChecks.get(checkKey(p,'local'));
 let title='',state='',time='';
 if(outside?.state==='running'||inside?.state==='running'){title=outside?.state==='running'?'Öffentlicher Test läuft …':'Heimnetz-Test läuft …'}
 else if(inside?.state==='failed'&&(!outside||Date.parse(inside.checked_at)>Date.parse(outside.checked_at))){title='Heimnetz-Test fehlgeschlagen';state='failed';time=inside.checked_at}
 else if(outside?.state==='reachable'){title=p.webRoute?'Öffentlicher HTTPS-Zugriff bestätigt':'Öffentliche TCP-Verbindung bestätigt';state='reachable';time=outside.checked_at}
 else if(outside?.state==='failed'){title='Öffentlicher Test fehlgeschlagen';state='failed';time=outside.checked_at}
 else if(outside?.state==='unknown'||inside?.state==='unknown'){title='UDP: Bestätigung erfordert eine passende Dienstantwort';time=outside?.checked_at||inside.checked_at}
 else if(inside?.state==='reachable'){title='Heimnetz erreichbar · Öffentlichen Test ausführen';time=inside.checked_at}
 else if(inside?.state==='failed'){title='Heimnetz-Test fehlgeschlagen';state='failed';time=inside.checked_at}
 else{paintSetup(node);if(typeof paintProgress==='function')paintProgress(node,p);return}
 node.textContent=title+(time?' · Geprüft: '+formatLocalTime(time):'');node.className=state==='reachable'?'test-ok':state==='failed'?'test-error':'';if(typeof paintProgress==='function')paintProgress(node,p)
}
function refreshServiceStatuses(){for(const entry of serviceStateNodes.values())paintServiceStatus(entry.node,entry.p)}
function paintSetup(node){const labels={waiting:'Warte auf Bestätigung',working:'Einrichtung läuft',configured:'Regeln eingerichtet',failed:'Einrichtung fehlgeschlagen',needs_connection:'Oracle-Anbindung fehlt'};node.textContent=(labels[setupState.state]||'Status unbekannt')+': '+(setupState.message||'Noch keine Einrichtungsbestätigung.')+(setupState.updated_at?' · '+formatLocalTime(setupState.updated_at):'');node.className=setupState.state==='failed'||setupState.state==='needs_connection'?'test-error':setupState.state==='configured'?'test-ok':''}
