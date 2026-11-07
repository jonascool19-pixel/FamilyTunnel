// Local UI fixture only: no scanner, firewall or external network requests.
const http=require('node:http'),fs=require('node:fs'),path=require('node:path');
const assets=path.join(__dirname,'../internal/app/web');
const hits=[{target_ip:'192.168.1.40',target_port:80,protocol:'tcp',name:'Musikboard',backend:'http://192.168.1.40:80'},{target_ip:'192.168.1.95',target_port:8096,protocol:'tcp',name:'Jellyfin',backend:'http://192.168.1.95:8096'},{target_ip:'192.168.1.29',target_port:7777,protocol:'tcp',name:'TCP-Dienst 7777',backend:''}];
let web=[],ports=[],configured=false,scanStarted=0,scanTarget='',logs=[{at:new Date().toISOString(),kind:'preview',message:'Beispielereignis zum Prüfen der Ortszeit'}];
const server=http.createServer(async(req,res)=>{
 if(!req.url.startsWith('/api/')){const file={'/':'index.html','/style.css':'style.css','/app.js':'app.js','/ports.js':'ports.js','/editor.js':'editor.js','/checks.js':'checks.js'}[req.url.split('?')[0]];if(!file){res.writeHead(404);res.end();return}res.setHeader('Content-Type',file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':'text/html; charset=utf-8');res.end(fs.readFileSync(path.join(assets,file)));return}
 res.setHeader('Content-Type','application/json');let body='';for await(const chunk of req)body+=chunk;let result={};
 switch(req.url){
 case '/api/session':result={CSRF:'preview'};break;
 case '/api/status':result={wireguard:'Vorschau: verbunden',server_sync:'Vorschau'};break;
 case '/api/routes':result=web;break;
 case '/api/ports':result=ports;break;
 case '/api/logs':if(req.method==='DELETE'){const deleted=logs.length;logs=[];result={deleted,message:'Vorschau: Ereignisse gelöscht'}}else result=logs;break;
 case '/api/setup-status':result={state:configured?'configured':'',message:configured?'Vorschau: Regeln eingerichtet.':'Vorschau mit Beispieldaten. Es werden keine echten Dienste veröffentlicht.'};break;
 case '/api/scan':if(req.method==='POST'){scanStarted=Date.now();scanTarget=JSON.parse(body).targets||hits.map(x=>x.target_ip).join(',')}if(req.method==='DELETE')scanStarted=0;const progress=scanStarted?Math.min(65535,Math.floor((Date.now()-scanStarted)*2)):100;result={done:progress,total:scanStarted?65535:100,found:3,running:Boolean(scanStarted&&progress<65535),targets:scanTarget,mode:scanStarted?'full':'quick',domain:'zugang.example.net',results:hits,message:'Vorschau: simulierte Suche'};break;
 case '/api/backend-test':result={reachable:true,message:'Vorschau: Heimnetz erreichbar (TCP)'};break;
 case '/api/public-test':result={reachable:true,message:'Vorschau: HTTPS über Oracle und Tunnel bestätigt'};break;
 case '/api/port-test':const input=JSON.parse(body),udp=ports.find(x=>x.id===input.id)?.protocol==='udp';result={reachable:udp?null:true,message:udp?'UDP ohne dienstspezifische Antwort nicht prüfbar':'Vorschau: TCP-Verbindung möglich'};break;
 case '/api/ports/publish':{
  for(const choice of JSON.parse(body)){const hit=hits.find(x=>x.target_ip===choice.target_ip&&x.target_port===choice.target_port);if(hit.backend){if(!web.some(x=>x.backend===hit.backend))web.push({id:web.length+1,domain:choice.domain,backend:hit.backend,public_port:hit.target_port===80?443:hit.target_port,enabled:true})}else if(!ports.some(x=>x.target_ip===hit.target_ip&&x.target_port===hit.target_port))ports.push({...hit,id:ports.length+1,public_port:hit.target_port,enabled:true})}configured=true;result={ports,routes:web,message:'Vorschau: Auswahl gespeichert.'};break;
 }
 default:if(req.url.startsWith('/api/stats'))result=[];else if(req.url.startsWith('/api/routes/')&&req.method==='PUT'){const changed=JSON.parse(body);web=web.map(item=>item.id===changed.id?changed:item);result={routes:web,message:'Vorschau: Änderungen gespeichert.'}}else if(req.url.startsWith('/api/ports/')&&req.method==='PUT'){const changed=JSON.parse(body);ports=ports.map(item=>item.id===changed.id?changed:item);result={ports,message:'Vorschau: Änderungen gespeichert.'}}
 }
 res.end(JSON.stringify(result));
});
server.listen(18767,'127.0.0.1',()=>console.log('Local UI fixture: http://127.0.0.1:18767'));
