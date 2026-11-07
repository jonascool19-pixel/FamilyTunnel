'use strict';
function directPayload(p){return {id:p.id,name:p.name||'',protocol:p.protocol,target_ip:p.target_ip,target_port:p.target_port,public_port:p.public_port,enabled:Boolean(p.enabled)}}
function openActiveEditor(p,card){
 if(activeEditor){portMessage('Speichere oder schließe zuerst die geöffnete Bearbeitung.');return}
 const route=p.webRoute,fields={},editor=document.createElement('form');editor.className='active-editor';
 const heading=document.createElement('strong');heading.textContent='Dienst bearbeiten';editor.append(heading);
 function field(name,title,value,type='text',options){const label=document.createElement('label'),text=document.createElement('span'),input=document.createElement(options?'select':'input');text.textContent=title;input.name=name;if(options){for(const item of options){const option=document.createElement('option');option.value=item;option.textContent=item.toUpperCase();input.append(option)}}else input.type=type;input.value=value;input.required=true;if(type==='number'){input.min='1';input.max='65535'}if(name==='name'){input.maxLength=80;input.required=false}fields[name]=input;label.append(text,input);editor.append(label);return input}
 field('name','Angezeigter Name',endpointName(p));
 field('target_ip','Lokale IP-Adresse',p.target_ip);
 field('target_port','Interner Port',p.target_port,'number');
 field('public_port','Öffentlicher Port',route?portOf(route):p.public_port,'number');
 if(route){field('domain','Öffentliche Domain',route.domain);field('scheme','Verbindung zum lokalen Dienst',new URL(route.backend).protocol.slice(0,-1),'text',['http','https'])}else field('protocol','Protokoll',p.protocol,'text',['tcp','udp']);
 const note=document.createElement('p');note.textContent='Änderungen werden gespeichert und im Hintergrund an Oracle übertragen.';const error=document.createElement('p');error.className='test-error';error.setAttribute?.('role','alert');
 const actions=document.createElement('div');actions.className='port-actions';const save=document.createElement('button');save.type='submit';save.textContent='Änderungen speichern';actions.append(save);button(actions,'Abbrechen',async()=>{activeEditor=null;renderDevices()}).type='button';editor.append(note,error,actions);
 editor.onsubmit=async event=>{
  event.preventDefault();save.disabled=true;error.textContent='';
  try{
   const common={name:fields.name.value.trim(),enabled:Boolean(p.enabled)};
   const payload=route?{...route,...common,domain:fields.domain.value.trim().toLowerCase(),backend:fields.scheme.value+'://'+fields.target_ip.value.trim()+':'+rawPort(fields.target_port.value),public_port:readPort(fields.public_port.value)}:{...directPayload(p),...common,protocol:fields.protocol.value,target_ip:fields.target_ip.value.trim(),target_port:rawPort(fields.target_port.value),public_port:rawPublic(fields.public_port.value)};
   const result=await api((route?'routes/':'ports/')+(route?route.id:p.id),'PUT',payload);
   activeEditor=null;if(route)renderRoutes(result.routes);else renderPorts(result.ports);
   renderSetup({state:'working',message:result.message});portMessage(result.message);
  }catch(problem){error.textContent=problem.message;portMessage(problem.message)}finally{save.disabled=false}
 };
 activeEditor={editor};card.append(editor);fields.name.focus();editor.scrollIntoView({behavior:'smooth',block:'nearest'})
}
