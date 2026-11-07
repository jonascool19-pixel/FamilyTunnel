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
let failTest=false,existing=[],rawExisting=[];
const context=vm.createContext({
  URL,
  document:{getElementById(id){if(!elements.has(id))elements.set(id,new Element());return elements.get(id)},createElement(){return new Element()},querySelector(){return null},querySelectorAll(){return []}},
  localStorage:{getItem:key=>storage.get(key),setItem:(key,value)=>storage.set(key,value),removeItem:key=>storage.delete(key)},
  setInterval(){},confirm(){return true},
  fetch:async(path,options)=>{
    calls.push({path,options});
    if(path==='/api/session')return {ok:false,status:401,text:async()=>JSON.stringify({error:'login'})};
    if(path==='/api/backend-test')return {ok:!failTest,status:failTest?503:200,text:async()=>JSON.stringify(failTest?{error:'Testfehler'}:{reachable:true,message:'Vom CT erreichbar (TCP).'})};
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
  const data={domain:'test.example.net',services:[{name:'Dienst',backend:'http://192.168.1.30:80'}]};
  context.input=data;
  vm.runInContext('renderDiscovery(input)',context);
  const card=elements.get('discovered').children[0],result=card.children[3],actions=card.children[4];
  await actions.children[0].onclick();
  assert.match(result.textContent,/Vom CT erreichbar/);
  assert.equal(calls.filter(call=>call.path==='/api/backend-test').length,1);
  context.savedRoutes=[{id:1,domain:data.domain,backend:data.services[0].backend,enabled:true}];
  vm.runInContext('renderRoutes(savedRoutes)',context);
  const savedRow=elements.get('routes').children[0];
  await savedRow.children[3].children[2].onclick();
  assert.match(savedRow.children[2].textContent,/Erreichbar/);
  const tests=calls.filter(call=>call.path==='/api/backend-test');
  assert.equal(tests.length,2);
  assert.equal(tests[0].options.body,tests[1].options.body,'Both lists must run the same test');
  failTest=true;
  await actions.children[0].onclick();
  assert.match(result.textContent,/Testfehler/);
  assert.equal(actions.children[0].disabled,false);
  assert.equal(actions.children[0].textContent,'Erneut testen');
  existing=[{id:7,domain:'test.example.net',backend:'http://192.168.1.40:80'}];
  await actions.children[2].onclick();
  assert.ok(calls.some(call=>call.path==='/api/routes/7'&&call.options.method==='PUT'));
  assert.equal(JSON.parse(storage.get('ft-last-discovery')).services.length,0);
  const multiple={domain:'test.example.net',services:[{name:'Media',backend:'http://192.168.1.30:8096',port:8096},{name:'Home',backend:'http://192.168.1.40:8123',port:8123}]};
  context.input=multiple;
  vm.runInContext('renderDiscovery(input)',context);
  const multiCards=elements.get('discovered').children;
  assert.equal(Number(multiCards[0].children[2].children[2].value),8096);
  assert.equal(Number(multiCards[1].children[2].children[2].value),8123);
  assert.match(multiCards[0].children[2].children[3].textContent,/:8096\//);
  existing=[{id:7,domain:'test.example.net',backend:'http://192.168.1.40:80',public_port:443}];
  await multiCards[0].children[4].children[2].onclick();
  const publishes=calls.filter(call=>call.path==='/api/routes'&&call.options.method==='POST');
  assert.equal(JSON.parse(publishes.at(-1).options.body).public_port,8096,'New port must create a new route, not replace root');
  vm.runInContext('renderDiscovery(input)',context);
  await elements.get('discovered').children[0].children[4].children[1].onclick();
  assert.equal(JSON.parse(calls.filter(call=>call.path==='/api/public-test').at(-1).options.body).public_port,8123);
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
  vm.runInContext(readFileSync('internal/app/web/ports.js','utf8'),context);
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
  assert.equal(elements.get('setupStatus').textContent,'Oracle einmalig verbinden');
  assert.ok(calls.every(call=>call.path.startsWith('/api/')),'No direct browser request to a LAN backend');
  console.log('Web callbacks: CT test, inline errors and existing-domain publication passed.');
}
run().catch(error=>{console.error(error);process.exitCode=1});
