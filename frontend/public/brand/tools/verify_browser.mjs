/** Local read-only product overlay. PLAYWRIGHT_MODULE, WORKSPACE, BASE_BRAND, OUT required. */
import {createServer} from 'node:http';
import {readFile, mkdir, writeFile} from 'node:fs/promises';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
const {chromium} = await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href);
const ROOT=path.resolve(import.meta.dirname,'..');
const WS=process.env.WORKSPACE, OUT=process.env.OUT;
await mkdir(OUT,{recursive:true});
const mime={'.html':'text/html','.js':'application/javascript','.mjs':'application/javascript','.css':'text/css','.svg':'image/svg+xml','.png':'image/png','.woff2':'font/woff2','.ttf':'font/ttf','.json':'application/json'};
const pages={main:'site-navigation',ccs:'ccs-site',sub:'subscription-portal/web',account:'ritel-account/runtime/account-ui'};
const snapshot=new Map();
const serverList=[];
async function server(product,baseline=false){
 const server=createServer(async(req,res)=>{
  try{
   if(req.method!=='GET'){res.writeHead(405).end();return;}
   const url=new URL(req.url,'http://localhost');
   let name=decodeURIComponent(url.pathname).replace(/^\/account\/ui\//,'/');
   let file;
   if(name==='/__font-lab'){
    res.setHeader('Content-Type','text/html');
    res.end('<html lang="zh-CN"><meta charset="utf-8"><link rel="stylesheet" href="/brand/brand.css"><body class="ritel-theme"><main style="padding:40px"><h1 class="ritel-type" data-type-role="page">智能，触手可及 智能，觸手可及</h1><p id="dynamic"></p><p style="font-family:var(--ritel-code)">RITEL_API_KEY · abc_012345</p><img width="160" src="/brand/ritel-r.svg"><img width="300" src="/brand/ritel-si.svg"><img width="16" src="/brand/ritel-favicon.svg"><img width="32" src="/brand/ritel-favicon.svg"><img width="90" src="/brand/apple-touch-icon.png"></main><script>setTimeout(()=>document.querySelector("#dynamic").textContent="动态用户名：陳𠮷明 · 龘 · 香港繁體 · 简体用户 · Zoë Ångström Nguyễn Ελληνικά Кириллица",100)</script>');return;
   }
   if(name.includes('/api/')||name.startsWith('/api/')){res.writeHead(503,{'Content-Type':'application/json'}).end(JSON.stringify({ok:false,error:{code:'LOCAL_ASSET_PREVIEW'}}));return;}
   if(name.includes('/brand/')){
    let relative=name.slice(name.indexOf('/brand/')+7);
    file=path.join(baseline?process.env.BASE_BRAND:ROOT,relative);
    // Canonical source historically kept marks under assets/, whereas products used flat paths.
    if(baseline&&!relative.includes('/')&&relative.endsWith('.svg'))file=path.join(process.env.BASE_BRAND,'assets',relative);
   }else{
    if(name==='/'||name==='/index.html')name=product==='ccs'?'/zh-hans.html':'/index.html';
    file=path.join(WS,pages[product],name);
   }
   let raw=snapshot.get(file);
   if(!raw){raw=await readFile(file);snapshot.set(file,raw);}
   res.writeHead(200,{'Content-Type':mime[path.extname(file)]||'application/octet-stream','Cache-Control':'no-store','Content-Length':raw.length}).end(raw);
  }catch{res.writeHead(404).end();}
 });
 await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
 serverList.push(server);
 return 'http://127.0.0.1:'+server.address().port;
}
const browser=await chromium.launch({channel:'chrome',headless:true});
const results=[];
const quick=process.env.QUICK==='1';
const prior=quick?JSON.parse(await readFile(path.join(OUT,'browser.json'),'utf8')):null;
try{
 for(const baseline of (quick?[false]:[true,false])){
  for(const product of baseline||quick?['main']:Object.keys(pages)){
   const origin=await server(product,baseline);
   for(const locale of baseline||quick?['zh-CN']:['zh-CN','zh-HK','en']){
    for(const scheme of baseline||quick?['light']:['light','dark']){
     const context=await browser.newContext({locale,colorScheme:scheme,viewport:{width:1440,height:1000},deviceScaleFactor:2});
     await context.addInitScript(({locale,scheme})=>{localStorage.setItem('ritel-language',locale==='zh-HK'?'zh-Hant-HK':locale);localStorage.setItem('ritel-theme',scheme);},{locale,scheme});
     await context.route('**/*',route=>route.request().url().startsWith(origin)&&route.request().method()==='GET'?route.continue():route.abort());
     const page=await context.newPage();
     const fonts=[],failures=[],pending=[];
     page.on('response',r=>{if(/\.(woff2|ttf)(\?|$)/.test(r.url()))pending.push(r.finished().then(error=>{if(error)throw error;fonts.push({url:r.url().replace(origin,''),bytes:Number(r.headers()['content-length']),status:r.status()});}));if(r.status()>=400)failures.push({url:r.url().replace(origin,''),status:r.status()});});
     await page.goto(origin,{waitUntil:'networkidle'});
     await page.evaluate(()=>document.fonts.ready);
     await Promise.all(pending);
     const text=await page.locator('body').innerText();
     const effectiveLang=await page.locator('html').getAttribute('lang');
     const name=[baseline?'baseline':'candidate',product,locale,scheme].join('-');
     await page.screenshot({path:path.join(OUT,name+'.png'),fullPage:true});
     results.push({name,product,locale,scheme,baseline,effectiveLang,fonts,fontBytes:fonts.reduce((n,f)=>n+f.bytes,0),failures,text});
     await context.close();
    }
   }
  }
 }
 const labOrigin=await server('main');
 for(const scheme of ['light','dark']){
  const context=await browser.newContext({colorScheme:scheme,viewport:{width:1200,height:800},deviceScaleFactor:2});
  const page=await context.newPage();
  await page.goto(labOrigin+'/__font-lab',{waitUntil:'networkidle'});
  await page.evaluate(s=>document.documentElement.classList.toggle('dark',s==='dark'),scheme);
  await page.evaluate(()=>document.fonts.ready);
  await page.screenshot({path:path.join(OUT,'dynamic-icons-'+scheme+'.png'),fullPage:true});
  results.push({name:'dynamic-icons-'+scheme,product:'fixture',text:await page.locator('body').innerText()});
  await context.close();
 }
 await writeFile(path.join(OUT,quick?'browser-final.json':'browser.json'),JSON.stringify({results,scope:'Local asset overlay; API requests return 503; no authenticated business acceptance.'},null,2));
 const candidate=results.find(r=>r.name==='candidate-main-zh-CN-light');
 const baseline=(quick?prior.results:results).find(r=>r.baseline);
 console.log(JSON.stringify({output:OUT,baselineFontBytes:baseline.fontBytes,candidateFontBytes:candidate.fontBytes,ratio:candidate.fontBytes/baseline.fontBytes,pages:results.length,mainFontRequests:candidate.fonts},null,2));
 if(candidate.fontBytes>1000000)throw new Error('Chinese reference page exceeds the phase-0 1 MB font transfer budget');
 if(results.filter(r=>!r.baseline&&r.fonts).some(r=>r.fonts.some(f=>f.url.includes('.ttf'))))throw new Error('Candidate loaded recovery TTF');
}finally{
 await browser.close();
 await Promise.all(serverList.map(s=>new Promise(resolve=>s.close(resolve))));
}
