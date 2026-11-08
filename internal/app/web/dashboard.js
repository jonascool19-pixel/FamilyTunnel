'use strict';
const panelNames=['overview','services','statistics','maintenance','settings','diagnostics'];
let maintenanceDirty=false,maintenanceLoaded=false,maintenanceBusy=false;
function selectPanel(name){
 if(!panelNames.includes(name))name='overview';
 for(const key of panelNames){const panel=$('panel-'+key),tab=$('tab-'+key);if(panel)panel.hidden=key!==name;if(tab){tab.setAttribute('aria-selected',String(key===name));tab.tabIndex=key===name?0:-1;tab.className=key===name?'nav-active':''}}
}
for(const name of panelNames){const tab=$('tab-'+name);if(tab){tab.onclick=()=>selectPanel(name);tab.onkeydown=event=>{if(!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;event.preventDefault();const index=event.key==='Home'?0:event.key==='End'?panelNames.length-1:(panelNames.indexOf(name)+(event.key==='ArrowRight'?1:panelNames.length-1))%panelNames.length;selectPanel(panelNames[index]);$('tab-'+panelNames[index]).focus()}}}
selectPanel('overview');
function renderOverview(status){
 const root=$('connectionSummary');if(!root)return;root.replaceChildren();
 const health=status.health||{},connected=health.wireguard==='connected';
 const values=[['System',status.role==='server'?'Oracle-Server':'Heim-CT'],['WireGuard',health.wireguard?(connected?'Verbunden':'Noch keine frische Verbindung'):'Wird geprüft'],['Betriebszeit',Math.floor((status.uptime_seconds||0)/3600)+' Std. '+Math.floor(((status.uptime_seconds||0)%3600)/60)+' Min.'],['Einrichtung',status.role==='server'?(health.caddy===true?'Caddy bereit':health.caddy===false?'Caddy prüfen':'Wird geprüft'):(health.server_sync==='ok'?'Mit Oracle abgeglichen':health.server_sync||'Wird geprüft')]];
 for(const [title,value] of values){const card=document.createElement('article'),label=document.createElement('span'),text=document.createElement('strong');card.className='summary-card';label.textContent=title;text.textContent=value;card.append(label,text);root.append(card)}
}
function renderMaintenance(data,fill=false){
 const v=data.state||{},plan=v.plan||{},f=$('maintenanceForm');
 if(!maintenanceDirty||!maintenanceLoaded||fill){f.elements.enabled.checked=Boolean(plan.enabled);f.elements.time.value=plan.time||'07:00';f.elements.zone.value=plan.zone||'Europe/Berlin';f.elements.mode.value=plan.mode||'daily';maintenanceLoaded=true;maintenanceDirty=false}
 $('maintenancePlanState').textContent=(plan.enabled?'Zeitplan aktiv':'Zeitplan ausgeschaltet')+' · '+(plan.time||'07:00')+' · '+(plan.zone||'Europe/Berlin')+(plan.mode==='interval24'?' · alle 24 Stunden':' · täglich zur festen Ortszeit');
 $('maintenanceJobState').textContent=v.job?v.job.message+' · '+formatLocalTime(v.job.created):'Noch kein Neustart angefordert.';
 $('maintenancePeer').textContent=v.client_seen&&!String(v.client_seen).startsWith('0001')?'Letzte CT-Verbindung: '+formatLocalTime(v.client_seen):'Warte auf die erste Verbindung des Wartungsdienstes.';
 const active=v.job&&!['completed','failed'].includes(v.job.state);
 const unavailable=data.role!=='client'||!data.worker_installed;
 $('maintenanceSave').disabled=unavailable||Boolean(active);$('maintenanceTest').disabled=unavailable||Boolean(active);
 $('maintenanceError').textContent=data.local_error||v.server_reason||v.client_reason||(data.role==='server'?'Zeitplan und Test bitte in der Verwaltung deines Heim-CT einstellen.':!data.worker_installed?'Bitte beide Systeme mit dem vollständigen Installer aktualisieren.':'');
}
async function refreshMaintenance(){
 if(maintenanceBusy||$('app').hidden)return;maintenanceBusy=true;
 try{renderMaintenance(await api('maintenance'))}catch(error){$('maintenanceError').textContent=error.message}finally{maintenanceBusy=false}
}
$('maintenanceForm').oninput=()=>{maintenanceDirty=true};
form('maintenanceForm',async f=>{
 const enabled=f.elements.enabled.checked;
 if(enabled&&!confirm('Diesen wiederkehrenden Neustart aktivieren? Zuerst startet Oracle neu, danach nur der FamilyTunnel-CT. Alle darüber laufenden Verbindungen werden dabei unterbrochen.'))return;
 const saved=await api('maintenance','POST',{action:'settings',plan:{enabled,time:f.elements.time.value,zone:f.elements.zone.value.trim(),mode:f.elements.mode.value}});
 renderMaintenance({state:saved.state,role:'client',worker_installed:true},true);
});
$('maintenanceTest').onclick=async()=>{
 if(!confirm('ECHTER NEUSTART-TEST: Oracle-VM und danach der FamilyTunnel-CT werden tatsächlich neu gestartet. Die Verbindung fällt mehrere Minuten aus. Jetzt beide neu starten?'))return;
 const button=$('maintenanceTest');button.disabled=true;
 try{const result=await api('maintenance','POST',{action:'test',confirmation:'RESTART_BOTH'});renderMaintenance({state:result.state,role:'client',worker_installed:true})}catch(error){$('maintenanceError').textContent=error.message;button.disabled=false}
};
$('resetStats').onclick=async()=>{
 if(!confirm('Alle gespeicherten Statistikwerte dieses Controllers dauerhaft auf null setzen? Neue und noch laufende Zugriffe werden danach wieder gezählt. Dienste und Suchtreffer bleiben erhalten.'))return;
 const button=$('resetStats');button.disabled=true;try{const result=await api('stats','DELETE');await stats();message(result.message)}catch(error){message(error.message)}finally{button.disabled=false}
};
$('resetScan').onclick=async()=>{
 if(!confirm('Laufende Suche stoppen und alle gespeicherten Suchtreffer sowie inaktive Suchvorschläge löschen? Aktivierte Dienste bleiben erhalten.'))return;
 const button=$('resetScan');button.disabled=true;
 try{scanResetEpoch++;const result=await api('scan/reset','POST');scanRows=[];selectedPorts.clear();openDevices.clear();deviceScanMessages.clear();scanState={};const [ports,scan]=await Promise.all([api('ports'),api('scan')]);portRows=ports;renderPortScan(scan);portMessage(result.message)}catch(error){portMessage(error.message)}finally{button.disabled=false}
};
const dashboardShow=show;show=function(on){dashboardShow(on);if(on)refreshMaintenance()};
setInterval(()=>{if(!$('app').hidden)refreshMaintenance()},15000);
if(!$('app').hidden)refreshMaintenance();
