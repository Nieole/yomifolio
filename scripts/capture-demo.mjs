// Run with Node 24+. Uses real Go server, Chrome CDP and Python's standard-library ZIP writer.
import { spawn, spawnSync } from 'node:child_process';
import fs from 'node:fs/promises';
import net from 'node:net';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { titles, drawArtwork } from './demo-art.mjs';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const demo=path.join(root,'.tmp','yomifolio-demo');
const origin='http://127.0.0.1:18080';
const chrome=process.env.CHROME_PATH || 'C:/Program Files/Google/Chrome/Application/chrome.exe';
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
let server,browser,ws;let seq=0;const pending=new Map();
async function poll(fn,label){for(let i=0;i<180;i++){try{const v=await fn();if(v)return v;}catch{}await sleep(1000);}throw Error('Timed out: '+label);}
async function cdp(method,params={},sessionId){return new Promise((resolve,reject)=>{const id=++seq;pending.set(id,{resolve,reject});ws.send(JSON.stringify({id,method,params,...(sessionId?{sessionId}:{})}));});}
let session;
const call=(m,p)=>cdp(m,p,session);
async function evaluate(expression){const r=await call('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});if(r.exceptionDetails)throw Error(JSON.stringify(r.exceptionDetails));return r.result.value;}
let cookie='',csrf='';
async function api(url,method='GET',body){const res=await fetch(origin+url,{method,headers:{'Content-Type':'application/json',Cookie:cookie,'X-CSRF-Token':csrf},body:body?JSON.stringify(body):undefined});if(!res.ok)throw Error(`${method} ${url}: ${res.status} ${await res.text()}`);if(res.headers.get('set-cookie'))cookie=res.headers.get('set-cookie').split(';')[0];return res.json();}
async function navigate(url){await call('Page.navigate',{url:origin+url});await poll(()=>evaluate('document.readyState === "complete" && !!document.querySelector("#root")?.children.length'),'page');await sleep(2200);}
async function capture(name){await poll(()=>evaluate('Array.from(document.images).filter(i=>i.getBoundingClientRect().width>0).length>0 && Array.from(document.images).filter(i=>i.getBoundingClientRect().width>0).every(i=>i.complete && i.naturalWidth>0)'),'loaded images');await sleep(1200);const shot=await call('Page.captureScreenshot',{format:'png',captureBeyondViewport:false});await fs.writeFile(path.join(root,'images',name),Buffer.from(shot.data,'base64'));console.log('CAPTURE',name,await evaluate('document.body.innerText.slice(0,2200)'));}
try {
  // Refuse occupied ports so an existing service/profile can never be reused accidentally.
  for(const port of [18080,19222])await new Promise((resolve,reject)=>{const probe=net.createServer();probe.once('error',reject);probe.listen(port,'127.0.0.1',()=>probe.close(resolve));});
  await fs.access(chrome);
  await fs.mkdir(path.join(root,'.tmp'),{recursive:true});
  // Refuse to replace an existing fixture: no recursive deletion and no real-user paths.
  await fs.mkdir(demo,{recursive:false});
  for(const dir of ['library','pages','cache','logs','chrome-profile'])await fs.mkdir(path.join(demo,dir));
  const q=p=>JSON.stringify(p.replaceAll('\\','/'));
  await fs.writeFile(path.join(demo,'config.yaml'),`server:\n  host: 127.0.0.1\n  port: 18080\n  cookie_secure: never\n  allowed_origins: ["${origin}"]\ndatabase:\n  path: ${q(path.join(demo,'db.sqlite'))}\ncache:\n  dir: ${q(path.join(demo,'cache'))}\nlibrary:\n  paths: []\nscanner:\n  scan_profile: metadata_scan\n  thumbnail_format: png\n  waifu2x_path: ""\n  realcugan_path: ""\nprotocols:\n  opds:\n    enabled: false\n  mihon:\n    enabled: false\nkoreader:\n  enabled: false\n`);
  const exe=path.join(demo,'yomifolio.exe');
  const build=spawnSync('go',['build','-o',exe,'./cmd/server'],{cwd:root,stdio:'inherit',env:{...process.env,GOTMPDIR:path.join(root,'.tmp')}});if(build.status!==0)throw Error('Go build failed');
  const log=await fs.open(path.join(demo,'process.log'),'w');
  server=spawn(exe,['-config',path.join(demo,'config.yaml'),'-data-dir',path.join(demo,'logs')],{cwd:root,stdio:['ignore',log.fd,log.fd]});
  await poll(async()=>{const r=await fetch(origin+'/api/health');return r.ok;},'server health');
  browser=spawn(chrome,['--headless=new','--remote-debugging-address=127.0.0.1','--remote-debugging-port=19222',`--user-data-dir=${path.join(demo,'chrome-profile')}`,'--no-first-run','--no-default-browser-check','--disable-background-networking','--disable-component-update','--disable-sync','--disable-extensions','--disable-default-apps','--no-proxy-server','--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1','about:blank'],{stdio:'ignore'});
  const version=await poll(async()=>{const r=await fetch('http://127.0.0.1:19222/json/version');return r.json();},'Chrome');
  ws=new WebSocket(version.webSocketDebuggerUrl);await new Promise((resolve,reject)=>{ws.onopen=resolve;ws.onerror=reject;});
  ws.onmessage=e=>{const m=JSON.parse(e.data);if(m.id){const p=pending.get(m.id);pending.delete(m.id);m.error?p.reject(Error(JSON.stringify(m.error))):p.resolve(m.result);}else if(m.method==='Fetch.requestPaused'){const {requestId,request}=m.params;const allowed=request.url.startsWith(origin+'/')||request.url.startsWith('data:')||request.url.startsWith('blob:');cdp(allowed?'Fetch.continueRequest':'Fetch.failRequest',{requestId,...(allowed?{}:{errorReason:'BlockedByClient'})},m.sessionId).catch(console.error);}};
  const target=await cdp('Target.createTarget',{url:'about:blank'});session=(await cdp('Target.attachToTarget',{targetId:target.targetId,flatten:true})).sessionId;
  await call('Page.enable');await call('Network.enable');
  // Browser-level URL allowlist; never replace business responses or inject business DOM.
  await call('Fetch.enable',{patterns:[{urlPattern:'*'}]});
  for(let i=0;i<titles.length;i++){
    const dir=path.join(demo,'pages',String(i));await fs.mkdir(dir);
    for(let p=0;p<7;p++){const b64=await evaluate(`(${drawArtwork.toString()})(${i},${p},1,${JSON.stringify(titles[i])})`);await fs.writeFile(path.join(dir,`${String(p+1).padStart(3,'0')}.png`),Buffer.from(b64,'base64'));}
    for(let v=2;v<=3;v++){const b64=await evaluate(`(${drawArtwork.toString()})(${i},0,${v},${JSON.stringify(titles[i])})`);await fs.writeFile(path.join(dir,`cover-${v}.png`),Buffer.from(b64,'base64'));}
  }
  const manifest=titles.map((title,i)=>({title,index:i,volumes:i===0?3:i%3===0?2:1}));
  await fs.writeFile(path.join(demo,'manifest.json'),JSON.stringify(manifest));
  const py=`import json, pathlib, zipfile, html\nr=pathlib.Path(${JSON.stringify(demo)})\nfor s in json.loads((r/'manifest.json').read_text()):\n d=r/'library'/s['title']; d.mkdir()\n for v in range(1,s['volumes']+1):\n  with zipfile.ZipFile(d/('%02d.cbz'%v),'w',zipfile.ZIP_DEFLATED) as z:\n   z.writestr('ComicInfo.xml','<ComicInfo><Title>'+html.escape(s['title'])+'</Title><Series>'+html.escape(s['title'])+'</Series><Number>'+str(v)+'</Number><Writer>Rowan Vale</Writer><Publisher>Paper Lantern Studio</Publisher><Genre>Adventure, Fantasy</Genre><LanguageISO>en</LanguageISO><Summary>An original illustrated journey through a city of bridges, quiet observatories and wandering lights.</Summary></ComicInfo>')\n   for p in sorted((r/'pages'/str(s['index'])).glob('0*.png')): z.write(p.with_name('cover-%d.png'%v) if p.name=='001.png' and v>1 else p,p.name)\n`;
  const zip=spawnSync('python',['-c',py],{stdio:'inherit'});if(zip.status!==0)throw Error('ZIP creation failed');
  const auth=await api('/api/auth/setup','POST',{username:'demo',password:'LanternAtlas-demo-2026',display_name:'Rowan'});csrf=auth.csrf_token;
  console.log('Demo account created');
  const library=await api('/api/libraries','POST',{name:'The Lantern Library',path:path.join(demo,'library'),scan_mode:'none',scan_interval:60,scan_formats:'cbz',koreader_sync_enabled:false});console.log('LIBRARY',JSON.stringify(library));
  const lid=library.id??library.library?.id;
  await poll(async()=>{const s=await api(`/api/series/${lid}`);return (Array.isArray(s)?s:s.series??s.data??[]).length===12;},'12 scanned series');
  await poll(async()=>{const t=await api(`/api/system/tasks?scope=library&scope_id=${lid}&limit=100`);await fs.writeFile(path.join(demo,'tasks.json'),JSON.stringify(t,null,2));const rows=Array.isArray(t)?t:t.tasks??t.items??[];return rows.some(x=>x.type==='scan_library') && rows.some(x=>x.type==='generate_covers') && rows.every(x=>x.status==='completed');},'completed library tasks');
  await sleep(2000);
  const result=await api(`/api/series/${lid}`);const series=Array.isArray(result)?result:result.series??result.data;
  console.log('Scanned',series.length,'series');
  const focus=series.find(s=>(s.name??s.title??'').includes('Lantern'));
  if(!focus)throw Error('Focus series not found');
  const sid=focus.id;
  await api(`/api/series/info/${sid}`,'PUT',{title:titles[0],summary:'Every evening, the bridges of Bellwether hum with a different tide. Cartographer Mira follows a trail of wandering lanterns to find the sea that vanished from every map. An illustrated journey about small acts of courage, impossible cities, and finding a way home.',publisher:'Paper Lantern Studio',status:'Ongoing',rating:4.5,language:'en',locked_fields:'title,summary',tags:['Adventure','Fantasy','Slice of Life'],authors:[{name:'Rowan Vale',role:'writer'}],links:[]});
  const br=await api(`/api/books/${sid}`);console.log('Focus books loaded');const books=Array.isArray(br)?br:br.books??br.data;const bid=books[0].id;
  await api(`/api/books/${bid}/progress`,'POST',{page:3});await api(`/api/books/${bid}/reading-time`,'POST',{seconds:120});
  await call('Network.setCookie',{name:cookie.split('=')[0],value:cookie.slice(cookie.indexOf('=')+1),url:origin,httpOnly:true,sameSite:'Lax'});
  await call('Page.addScriptToEvaluateOnNewDocument',{source:`localStorage.setItem('yomifolio_locale','en-US');localStorage.setItem('yomifolio_theme','midnight');`});
  await call('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false});
  await navigate(`/library/${lid}`);await capture('yomifolio-library.png');
  await navigate(`/series/${sid}`);await capture('yomifolio-series.png');
  await evaluate(`localStorage.setItem('yomifolio_read_mode',JSON.stringify('paged'));localStorage.setItem('yomifolio_scale_mode',JSON.stringify('fit-screen'));localStorage.setItem('yomifolio_double_page','true');`);
  await navigate(`/reader/${bid}`);await capture('yomifolio-reader-desktop.png');
  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await call('Emulation.setTouchEmulationEnabled',{enabled:true});
  await evaluate(`localStorage.setItem('yomifolio_read_mode',JSON.stringify('webtoon'));localStorage.setItem('yomifolio_double_page','false');`);
  await navigate(`/reader/${bid}`);await capture('yomifolio-reader-mobile.png');
  await fs.writeFile(path.join(demo,'capture.json'),JSON.stringify({date:new Date().toISOString(),libraryId:lid,seriesId:sid,bookId:bid,desktop:[1440,1000],mobile:[390,844],manifest},null,2));
} finally {
  if(ws?.readyState===1){try{await cdp('Browser.close');}catch{}ws.close();}
  for(const child of [browser,server])if(child && child.exitCode===null){child.kill();await Promise.race([new Promise(r=>child.once('exit',r)),sleep(5000)]);}
}
