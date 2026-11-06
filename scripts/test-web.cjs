// Exercise the actual browser callbacks without a browser or a live LAN.
const {readFileSync}=require('node:fs');
const vm=require('node:vm');
const assert=require('node:assert/strict');
class Element {
  constructor(){this.children=[];this.hidden=true;this.value='';this.textContent='';}
  append(...items){this.children.push(...items);}
  replaceChildren(...items){this.children=items;}
}
const elements=new Map(),storage=new Map(),calls=[];
let failTest=false,existing=[];
const context=vm.createContext({
  document:{getElementById(id){if(!elements.has(id))elements.set(id,new Element());return elements.get(id)},createElement(){return new Element()}},
  localStorage:{getItem:key=>storage.get(key),setItem:(key,value)=>storage.set(key,value),removeItem:key=>storage.delete(key)},
  setInterval(){},confirm(){return true},
  fetch:async(path,options)=>{
    calls.push({path,options});
    if(path==='/api/session')return {ok:false,status:401,text:async()=>JSON.stringify({error:'login'})};
    if(path==='/api/backend-test')return {ok:!failTest,status:failTest?503:200,text:async()=>JSON.stringify(failTest?{error:'Testfehler'}:{reachable:true,message:'Vom CT erreichbar (TCP).'})};
    if(path==='/api/routes'&&options.method==='GET')return {ok:true,text:async()=>JSON.stringify(existing)};
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
  await actions.children[1].onclick();
  assert.ok(calls.some(call=>call.path==='/api/routes/7'&&call.options.method==='PUT'));
  assert.equal(JSON.parse(storage.get('ft-last-discovery')).services.length,0);
  assert.ok(calls.every(call=>call.path.startsWith('/api/')),'No direct browser request to a LAN backend');
  console.log('Web callbacks: CT test, inline errors and existing-domain publication passed.');
}
run().catch(error=>{console.error(error);process.exitCode=1});
