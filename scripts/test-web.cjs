// Exercise the actual browser callbacks without a browser or a live LAN.
const {readFileSync}=require('node:fs');
const vm=require('node:vm');
const assert=require('node:assert/strict');
class Element {
  constructor(){this.children=[];this.hidden=true;this.value='';this.textContent='';}
  set value(value){this._value=String(value)}
  get value(){return this._value}
  append(...items){this.children.push(...items);}
  replaceChildren(...items){this.children=items;}
  scrollIntoView(){this.scrolled=true;}
  focus(){this.focused=true;}
}
const elements=new Map(),storage=new Map(),calls=[];
let failTest=false,failSave=false,failScan=false,failScanGet=false,holdBackend=false,releaseBackend,confirmAllowed=true,existing=[],rawExisting=[],logEntries=[];
const context=vm.createContext({
  URL,
  document:{getElementById(id){if(!elements.has(id))elements.set(id,new Element());return elements.get(id)},createElement(){return new Element()},querySelector(){return null},querySelectorAll(){return []}},
  localStorage:{getItem:key=>storage.get(key),setItem:(key,value)=>storage.set(key,value),removeItem:key=>storage.delete(key)},
  setInterval(){},confirm(){return confirmAllowed},
  fetch:async(path,options)=>{
    calls.push({path,options});
    if(path==='/api/logs'){if(options.method==='DELETE'){logEntries=[];return {ok:true,text:async()=>JSON.stringify({message:'Ereignisse gelöscht'})}}return {ok:true,text:async()=>JSON.stringify(logEntries)}}
    if(path==='/api/scan'){if(failScan||(failScanGet&&options.method==='GET'))return {ok:false,status:503,text:async()=>JSON.stringify({error:'Suche vorübergehend nicht verfügbar'})};return {ok:true,text:async()=>JSON.stringify({running:options.method==='POST',done:0,total:65535,found:0,targets:'192.168.1.29',mode:'full'})}}
    if(path==='/api/ports'&&options.method==='GET')return {ok:true,text:async()=>JSON.stringify(rawExisting)};
    if(path==='/api/setup-status')return {ok:true,text:async()=>JSON.stringify({state:'configured',message:'Tunnel und Oracle-Regeln eingerichtet'})};
    if(path==='/api/port-test'){const input=JSON.parse(options.body),udp=rawExisting.find(p=>p.id===input.id)?.protocol==='udp';return {ok:true,text:async()=>JSON.stringify(udp?{state:'unknown',reachable:null,message:'UDP nicht zuverlässig prüfbar'}:{state:'reachable',reachable:true,message:'TCP-Verbindung möglich'})}}
    if(failSave&&options.method==='PUT')return {ok:false,status:409,text:async()=>JSON.stringify({error:'Port bereits belegt'})};
    if(path==='/api/session')return {ok:false,status:401,text:async()=>JSON.stringify({error:'login'})};
    if(path==='/api/public-test')return {ok:true,text:async()=>JSON.stringify({reachable:true,message:'HTTPS erreichbar'})};
    if(path==='/api/backend-test'){if(holdBackend)return new Promise(resolve=>{releaseBackend=resolve});return {ok:!failTest,status:failTest?503:200,text:async()=>JSON.stringify(failTest?{error:'Testfehler'}:{reachable:true,message:'Vom CT erreichbar (TCP).'})}};
    if(path==='/api/routes'&&options.method==='GET')return {ok:true,text:async()=>JSON.stringify(existing)};
    if(path==='/api/ports/batch'&&options.method==='POST'){
      rawExisting=JSON.parse(options.body).map((route,index)=>({...route,id:index+1}));
      return {ok:true,status:202,text:async()=>JSON.stringify({ports:rawExisting,message:'Portfreigaben gespeichert'})};
    }
    if(path==='/api/ports/publish'&&options.method==='POST')return {ok:true,status:202,text:async()=>JSON.stringify({ports:rawExisting,message:'Einrichtung läuft im Hintergrund'})};
    if(path==='/api/ports/1'&&options.method==='PUT'){
      rawExisting[0]={...JSON.parse(options.body),id:1};
      return {ok:true,status:202,text:async()=>JSON.stringify({ports:rawExisting,message:'Geändert'})};
    }
    if(path.startsWith('/api/routes')&&['POST','PUT'].includes(options.method)) {
      const route=JSON.parse(options.body);
      route.id=options.method==='POST'?Math.max(0,...existing.map(item=>item.id))+1:Number(path.split('/').at(-1));
      existing=existing.filter(item=>item.id!==route.id).concat(route);
      return {ok:true,status:202,text:async()=>JSON.stringify({ok:true,routes:existing,sync_pending:true,message:'Gespeichert. Übertragung läuft.'})};
    }
    return {ok:true,text:async()=>JSON.stringify({ok:true})};
  }
});
vm.runInContext(readFileSync('internal/app/web/app.js','utf8'),context);
async function run(){
  await new Promise(resolve=>setImmediate(resolve));
  const editor=elements.get('routeForm');
  editor.elements=Object.fromEntries(['id','domain','backend','public_port','enabled'].map(key=>[key,new Element()]));
  editor.reset=()=>{for(const field of Object.values(editor.elements)){field.value='';field.checked=false}};
  context.savedRoutes=[{id:25,domain:'edit.example.net',backend:'http://192.168.1.2:8096',public_port:8096,enabled:true}];
  vm.runInContext('renderRoutes(savedRoutes)',context);
  await elements.get('routes').children[0].children[3].children[0].onclick();
  assert.equal(elements.get('routeEditor').open,true,'Editing must open the collapsed editor');
  assert.equal(editor.elements.domain.focused,true);
  assert.equal(editor.elements.id.value,'25');
  editor.elements.domain.value='edited.example.net';
  editor.elements.public_port.value='';
  const saveButton=new Element(),beforeSave=calls.length;
  await editor.onsubmit({preventDefault(){},submitter:saveButton,target:editor});
  const write=calls.slice(beforeSave).find(call=>call.options.method==='PUT');
  assert.equal(write.path,'/api/routes/25');
  assert.equal(JSON.parse(write.options.body).public_port,443,'Empty port defaults to 443');
  assert.equal(calls.slice(beforeSave).length,1,'Saving must render response without waiting for status/logs/stats or another GET');
  assert.ok(elements.get('routes').children.some(row=>row.children[0].children[0].href==='https://edited.example.net/'));
  editor.elements.id.value='';editor.elements.domain.value='manual.example.net';editor.elements.backend.value='http://192.168.1.3:8080';editor.elements.public_port.value='';editor.elements.enabled.checked=true;
  const beforeCreate=calls.length;
  await editor.onsubmit({preventDefault(){},submitter:saveButton,target:editor});
  assert.equal(calls.length-beforeCreate,1);
  assert.equal(calls.at(-1).options.method,'POST');
  assert.equal(JSON.parse(calls.at(-1).options.body).public_port,443);
  assert.equal(saveButton.disabled,false);
  assert.ok(elements.get('routes').children.some(row=>row.children[0].children[0].href==='https://manual.example.net/'));
  vm.runInContext(readFileSync('internal/app/web/checks.js','utf8'),context);
  vm.runInContext(readFileSync('internal/app/web/ports.js','utf8'),context);
  vm.runInContext(readFileSync('internal/app/web/editor.js','utf8'),context);
  const rawForm=elements.get('rawForm');
  rawForm.elements=Object.fromEntries(['id','name','target_ip','target_port','public_port','protocol','enabled'].map(key=>[key,new Element()]));
  rawForm.reset=()=>{for(const field of Object.values(rawForm.elements)){field.value='';field.checked=false}};
  rawForm.elements.name.value='Game';rawForm.elements.target_ip.value='192.168.1.29';rawForm.elements.public_port.value='7777,15777';rawForm.elements.protocol.value='both';
  await rawForm.onsubmit({preventDefault(){},submitter:saveButton,target:rawForm});
  const rawRequest=calls.find(call=>call.path==='/api/ports/batch');
  const rawPayload=JSON.parse(rawRequest.options.body);
  assert.equal(rawPayload.length,4,'Two ports with TCP and UDP must produce four mappings');
  assert.ok(rawPayload.every(route=>route.target_port===route.public_port&&!route.enabled),'Raw mappings default to inactive with matching internal ports');
  assert.equal(elements.get('rawRows').children.length,1,'One device card for four ports on the same IP');
  const device=elements.get('rawRows').children[0];
  assert.equal(device.children[1].children.length,4);
  device.children[0].children[0].checked=true;
  device.children[0].children[0].onchange();
  vm.runInContext('renderDevices()',context);
  assert.equal(elements.get('rawRows').children[0].children[1].children.filter(item=>item.children[0].children[0].checked).length,4,'Device selection survives background renders');
  await elements.get('rawActivate').onclick();
  assert.equal(JSON.parse(calls.find(call=>call.path==='/api/ports/publish').options.body).length,4,'One action publishes every selected device port');
  await elements.get('rawRows').children[0].children[1].children[0].children[1].children[0].onclick();
  assert.equal(elements.get('rawEditor').open,true);
  assert.equal(rawForm.elements.id.value,'1');
  assert.equal(elements.get('rawAdvanced').open,true);
  rawForm.elements.target_port.value='17777';
  await rawForm.onsubmit({preventDefault(){},submitter:saveButton,target:rawForm});
  assert.equal(JSON.parse(calls.filter(call=>call.path==='/api/ports/1').at(-1).options.body).target_port,17777);
  const scanForm=elements.get('portScanForm');
  scanForm.elements={targets:{value:'192.168.1.29'},mode:{value:'full'}};
  await scanForm.onsubmit({preventDefault(){},submitter:saveButton,target:scanForm});
  assert.equal(JSON.parse(calls.filter(call=>call.path==='/api/scan').at(-1).options.body).mode,'full');
  vm.runInContext("renderPortScan({results:[{target_ip:'192.168.1.29',target_port:7777},{target_ip:'192.168.1.40',target_port:8080}]})",context);
  assert.equal(elements.get('rawRows').children.length,2,'New IP gets one separate group; repeated hits share the existing group');
  assert.equal(elements.get('rawRows').children[0].children[1].children.length,5,'Edited port plus old scan endpoint, with all other hits deduplicated');
  vm.runInContext("renderSetup({state:'needs_connection',message:'Oracle einmalig verbinden'})",context);
  assert.match(elements.get('setupStatus').textContent,/Oracle einmalig verbinden/);
  elements.get('serviceDomain').value='public.example.net';
  context.unified=[{id:55,domain:'public.example.net',backend:'http://192.168.1.95:8096',public_port:8096,enabled:true}];
  vm.runInContext("renderRoutes(unified); renderPortScan({domain:'public.example.net',results:[{target_ip:'192.168.1.95',target_port:8096,name:'Jellyfin',backend:'http://192.168.1.95:8096'},{target_ip:'192.168.1.95',target_port:80,name:'Musikboard',backend:'http://192.168.1.95:80'}]})",context);
  const unifiedDevice=elements.get('rawRows').children.find(card=>card.children[0].children[1].textContent.includes('192.168.1.95'));
  assert.match(unifiedDevice.children[0].children[1].textContent,/Jellyfin/,'Recognized name appears above the device ports');
  assert.match(unifiedDevice.children[0].children[1].textContent,/Musikboard/);
  const active=elements.get('activeServices').children.find(card=>card.children[0].textContent==='Jellyfin');
  assert.equal(active.children[2].href,'https://public.example.net:8096/','Active HTTPS service has the right scheme and port');
  await active.children[4].children[2].onclick();
  assert.equal(JSON.parse(calls.filter(call=>call.path==='/api/public-test').at(-1).options.body).public_port,8096);
  const lowWeb=unifiedDevice.children[1].children.find(item=>item.children[0].children[1].textContent.includes('Musikboard'));
  assert.equal(lowWeb.children[0].children[0].disabled,false,'Detected HTTP on port 80 can be selected for HTTPS');
  lowWeb.children[0].children[0].checked=true;lowWeb.children[0].children[0].onchange();
  await elements.get('rawActivate').onclick();
  const unifiedPayload=JSON.parse(calls.filter(call=>call.path==='/api/ports/publish').at(-1).options.body);
  assert.equal(unifiedPayload[0].auto,true);assert.equal(unifiedPayload[0].domain,'public.example.net');assert.equal(unifiedPayload[0].target_port,80);
  vm.runInContext('renderRoutes(unified)',context);
  const editable=elements.get('activeServices').children.find(card=>card.children[0].textContent==='Jellyfin');
  await editable.children[4].children[0].onclick();
  const inline=editable.children.at(-1);
  const inputs=Object.fromEntries(inline.children.filter(label=>label.children[1]?.name).map(label=>[label.children[1].name,label.children[1]]));
  inputs.name.value='Mein Medienserver';inputs.target_ip.value='192.168.1.96';inputs.target_port.value='8097';inputs.public_port.value='8098';inputs.domain.value='tv.example.net';
  vm.runInContext('renderDevices()',context);
  assert.ok(elements.get('activeServices').children.includes(editable),'Polling must retain an open editor');
  assert.equal(inputs.name.value,'Mein Medienserver');
  await inline.onsubmit({preventDefault(){}});
  const edited=elements.get('activeServices').children.find(card=>card.children[0].textContent==='Mein Medienserver');
  assert.ok(edited,'Saved name is immediately visible');assert.equal(edited.children[2].href,'https://tv.example.net:8098/');
  const savedEdit=JSON.parse(calls.filter(call=>call.path==='/api/routes/55'&&call.options.method==='PUT').at(-1).options.body);
  assert.equal(savedEdit.backend,'http://192.168.1.96:8097');
  await edited.children[4].children[0].onclick();
  const failedEditor=edited.children.at(-1),failedName=failedEditor.children.find(label=>label.children[1]?.name==='name').children[1];failedName.value='Noch nicht gespeichert';failSave=true;
  await failedEditor.onsubmit({preventDefault(){}});failSave=false;
  assert.equal(failedName.value,'Noch nicht gespeichert');assert.match(failedEditor.children.at(-2).textContent,/Port bereits belegt/);
  assert.equal(failedEditor.children.at(-1).children[0].disabled,false);
  await failedEditor.children.at(-1).children[1].onclick();
  assert.ok(elements.get('activeServices').children.some(card=>card.children[0].textContent==='Mein Medienserver'),'Cancel keeps the saved name');
  rawExisting[0].enabled=true;context.direct=rawExisting;vm.runInContext('renderPorts(direct)',context);
  const directCard=elements.get('activeServices').children.find(card=>card.children[2]?.textContent.includes('TCP direkt'));
  await directCard.children[4].children[0].onclick();
  const directEditor=directCard.children.at(-1),directFields=Object.fromEntries(directEditor.children.filter(label=>label.children[1]?.name).map(label=>[label.children[1].name,label.children[1]]));
  directFields.name.value='Mein Spiel';directFields.protocol.value='udp';directFields.public_port.value='17778';
  await directEditor.onsubmit({preventDefault(){}});
  const directWrite=JSON.parse(calls.filter(call=>call.path==='/api/ports/1'&&call.options.method==='PUT').at(-1).options.body);
  assert.equal(directWrite.name,'Mein Spiel');assert.equal(directWrite.protocol,'udp');assert.equal(directWrite.public_port,17778);assert.equal(Object.keys(directWrite).length,7,'Only API-supported fields may be sent');
  let media=elements.get('activeServices').children.find(card=>card.children[0].textContent==='Mein Medienserver');
  holdBackend=true;const pending=media.children[4].children[3].onclick();
  vm.runInContext('renderDevices()',context);
  media=elements.get('activeServices').children.find(card=>card.children[0].textContent==='Mein Medienserver');
  assert.match(media.children[5].children[1].textContent,/Prüfung läuft/);assert.equal(media.children[4].children[3].disabled,true,'Polling preserves in-flight checks');
  releaseBackend({ok:true,text:async()=>JSON.stringify({reachable:true,message:'Vom CT erreichbar (TCP).'})});holdBackend=false;await pending;
  assert.match(media.children[5].children[1].textContent,/Vom CT erreichbar/);assert.equal(media.children[4].children[3].disabled,false);
  failTest=true;await media.children[4].children[3].onclick();failTest=false;vm.runInContext('renderDevices()',context);
  media=elements.get('activeServices').children.find(card=>card.children[0].textContent==='Mein Medienserver');assert.match(media.children[5].children[1].textContent,/Testfehler/,'Failed result remains visible after polling');
  assert.ok(storage.get('ft-service-checks').includes('Testfehler'),'Last checks survive reload with timestamps');
  const udpCard=elements.get('activeServices').children.find(card=>card.children[0].textContent==='Mein Spiel');await udpCard.children[4].children[2].onclick();assert.match(udpCard.children[5].children[0].textContent,/UDP nicht zuverlässig/);assert.notEqual(udpCard.children[5].children[0].className,'test-ok');
  vm.runInContext("renderSetup({state:'working',message:'Oracle wird eingerichtet'})",context);assert.match(elements.get('setupStatus').textContent,/Einrichtung läuft/);assert.match(media.children[3].textContent,/Heimnetz-Test fehlgeschlagen/,'A new setup refresh must retain the actual service result');
  failScanGet=true;elements.get('app').hidden=false;await vm.runInContext('refreshPorts()',context);elements.get('app').hidden=true;failScanGet=false;
  assert.match(elements.get('setupStatus').textContent,/Regeln eingerichtet/,'A scan error must not hide setup status');
  vm.runInContext("renderPortScan({running:false,results:[{target_ip:'192.168.1.29',target_port:7777,name:'Mein Spiel'}]})",context);
  const deviceForScan=elements.get('rawRows').children.find(card=>card.children[0].children[1].textContent.includes('192.168.1.29'));
  await deviceForScan.children[3].onclick();
  let scanning=elements.get('rawRows').children.find(card=>card.children[0].children[1].textContent.includes('192.168.1.29'));assert.match(scanning.children[2].textContent,/0 \/ 65535/);assert.equal(scanning.children[3].disabled,true);assert.ok(scanning.children[1].children.length>0,'Starting a full scan must retain existing hits');
  vm.runInContext("renderPortScan({running:true,targets:'192.168.1.29',done:2048,total:65535,found:4})",context);scanning=elements.get('rawRows').children.find(card=>card.children[0].children[1].textContent.includes('192.168.1.29'));assert.match(scanning.children[2].textContent,/2048 \/ 65535/);
  vm.runInContext('renderPortScan({running:false})',context);failScan=true;scanning=elements.get('rawRows').children.find(card=>card.children[0].children[1].textContent.includes('192.168.1.29'));await scanning.children[3].onclick();failScan=false;assert.match(scanning.children[2].textContent,/Suche vorübergehend nicht verfügbar/);
  assert.match(vm.runInContext('formatLocalTime("2026-11-07T20:01:58Z","Europe/Berlin")',context),/21:01:58/,'UTC is displayed in the intended timezone');
  context.logSample=[{at:'2026-11-07T20:01:58Z',kind:'test',message:'Beispiel'}];vm.runInContext('renderLogs(logSample)',context);assert.match(elements.get('logTimezone').textContent,/Ortszeit/);
  const beforeClear=calls.length;confirmAllowed=false;await elements.get('clearLogs').onclick();assert.equal(calls.length,beforeClear,'Cancelling never deletes logs');confirmAllowed=true;await elements.get('clearLogs').onclick();assert.ok(calls.some(call=>call.path==='/api/logs'&&call.options.method==='DELETE'));assert.match(elements.get('logs').textContent,/Keine Ereignisse/);
  context.tcpConfirmed={id:88,name:'TCP-Test',protocol:'tcp',target_ip:'192.168.1.95',target_port:8920,public_port:8920,enabled:true};
  vm.runInContext('renderPorts([tcpConfirmed])',context);
  const tcpCard=elements.get('activeServices').children.find(card=>card.children[0].textContent==='TCP-Test');
  await tcpCard.children[4].children[2].onclick();assert.match(tcpCard.children[3].textContent,/Öffentliche TCP-Verbindung bestätigt/);assert.match(tcpCard.children[5].children[0].textContent,/Öffentliche TCP-Verbindung bestätigt/);assert.equal(tcpCard.children[3].className,'test-ok');
  vm.runInContext('renderDevices()',context);assert.match(elements.get('activeServices').children.find(card=>card.children[0].textContent==='TCP-Test').children[3].textContent,/Öffentliche TCP-Verbindung bestätigt/,'Polling must not replace confirmed reachability with setup text');
    let report='';context.captureReport=async text=>{report=text};vm.runInContext('copyText=captureReport',context);await elements.get('copyReport').onclick();assert.match(report,/DIREKTE PORTFREIGABEN/);assert.match(report,/EINRICHTUNG/);assert.match(report,/UTC:/);
    assert.ok(calls.every(call=>call.path.startsWith('/api/')),'No direct browser request to a LAN backend');
  console.log('Web callbacks: unified names, grouped selection, publication, active list and HTTPS test passed.');
}
run().catch(error=>{console.error(error);process.exitCode=1});
