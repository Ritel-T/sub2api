/** Cold-cache Chinese home-page budget. Local asset overlay, no production traffic.
 * Requires WORKSPACE, PLAYWRIGHT_MODULE, OUT. Uses Slow 4G: 1.6 Mbps down,
 * 750 Kbps up, 150 ms RTT; gzip text responses; no CPU throttling.
 */
import {createServer} from 'node:http';
import {readFile,mkdir,writeFile} from 'node:fs/promises';
import {gzipSync} from 'node:zlib';
import {createHash} from 'node:crypto';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
const {chromium}=await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href);
const ROOT=path.resolve(import.meta.dirname,'..'),WS=process.env.WORKSPACE,OUT=process.env.OUT;
await mkdir(OUT,{recursive:true});
const hash=raw=>createHash('sha256').update(raw).digest('hex');
const priority=JSON.parse(await readFile(path.join(ROOT,'priority-codepoints.json'),'utf8'));
const html=await readFile(path.join(WS,'site-navigation','index.html'));
if(hash(html)!==priority.source_sha256)throw new Error('Homepage changed since priority character extraction; regenerate priority-codepoints.json');
const snapshots=new Map();
const mime={'.html':'text/html','.js':'application/javascript','.mjs':'application/javascript','.css':'text/css','.svg':'image/svg+xml','.png':'image/png','.woff2':'font/woff2','.json':'application/json'};
const aliases=new Set(['ritel-r.svg','ritel-r-dark.svg','ritel-si.svg','ritel-wordmark-cinzel.svg','ritel-favicon.svg','apple-touch-icon.png','favicon-16.png','favicon-32.png']);
const server=createServer(async(req,res)=>{
 try{
  if(req.method!=='GET'){res.writeHead(405).end();return;}
  const name=decodeURIComponent(new URL(req.url,'http://localhost').pathname).replace(/^\//,'');
  let file=name.startsWith('brand/')?path.join(ROOT,name.slice(6)):aliases.has(name)?path.join(ROOT,name):path.join(WS,'site-navigation',name||'index.html');
  let entry=snapshots.get(file);
  if(!entry){
   const raw=file===path.join(WS,'site-navigation','index.html')?html:await readFile(file);
   const type=mime[path.extname(file)]||'application/octet-stream';
   const compressed=/text\/|javascript|json|svg/.test(type);
   entry={raw,body:compressed?gzipSync(raw):raw,type,compressed};snapshots.set(file,entry);
  }
  res.writeHead(200,{'Content-Type':entry.type,'Content-Length':entry.body.length,'Cache-Control':'no-store',...(entry.compressed?{'Content-Encoding':'gzip'}:{})}).end(entry.body);
 }catch{res.writeHead(404).end();}
});
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
const origin='http://127.0.0.1:'+server.address().port;
const conditions={offline:false,latency:150,downloadThroughput:1600000/8,uploadThroughput:750000/8,connectionType:'cellular4g'};
const browser=await chromium.launch({channel:'chrome',headless:true});
const trials=[];
try{
 for(let trial=1;trial<=3;trial++){
  const context=await browser.newContext({locale:'zh-CN',colorScheme:'light',viewport:{width:1440,height:1000},deviceScaleFactor:2});
  await context.route('**/*',route=>route.request().url().startsWith(origin)&&route.request().method()==='GET'?route.continue():route.abort());
  await context.addInitScript(()=>{
   localStorage.setItem('ritel-language','zh-CN');localStorage.setItem('ritel-theme','light');
   function check(){
    const heading=document.querySelector('h1');
    const defined=[...document.fonts].some(f=>f.family.replaceAll('"','').replaceAll("'",'')==='I.MingCP');
    if(heading&&defined&&/[\u3400-\u9fff]/u.test(heading.textContent)){
     const style=getComputedStyle(heading);
     if(document.fonts.check(style.fontWeight+' '+style.fontSize+' "I.MingCP"',heading.textContent)){
      window.__titleFontReadyAt=performance.now();
      requestAnimationFrame(()=>requestAnimationFrame(()=>window.__titlePresentedAt=performance.now()));return;
     }
    }
    requestAnimationFrame(check);
   }
   requestAnimationFrame(check);
  });
  const page=await context.newPage(),cdp=await context.newCDPSession(page);
  await cdp.send('Network.enable');await cdp.send('Network.setCacheDisabled',{cacheDisabled:true});
  await cdp.send('Network.emulateNetworkConditions',conditions);
  const responses=new Map(),completed=[],failed=[];
  cdp.on('Network.responseReceived',e=>responses.set(e.requestId,e.response));
  cdp.on('Network.loadingFinished',e=>{const r=responses.get(e.requestId);if(r)completed.push({url:r.url.replace(origin,''),status:r.status,mime:r.mimeType,wireBytes:e.encodedDataLength,bodyBytes:Number(r.headers['Content-Length']??r.headers['content-length']??0)});});
  cdp.on('Network.loadingFailed',e=>failed.push({id:e.requestId,error:e.errorText}));
  await page.goto(origin,{waitUntil:'networkidle',timeout:30000});
  await page.waitForFunction(()=>window.__titlePresentedAt>0,{timeout:15000});
  await page.evaluate(()=>document.fonts.ready);
  // Inspector font inspection can re-fetch CSS with cache disabled; stop page accounting first.
  const pageCompleted=[...completed], pageFailed=[...failed];
  await cdp.send('DOM.enable');await cdp.send('CSS.enable');
  const {root}=await cdp.send('DOM.getDocument');const {nodeId}=await cdp.send('DOM.querySelector',{nodeId:root.nodeId,selector:'h1'});
  const {fonts}=await cdp.send('CSS.getPlatformFontsForNode',{nodeId});
  const heading=await page.locator('h1').evaluate(el=>({text:el.textContent,family:getComputedStyle(el).fontFamily,readyAt:window.__titleFontReadyAt,presentedAt:window.__titlePresentedAt}));
  const fontFiles=pageCompleted.filter(r=>r.mime.startsWith('font/')||/\.woff2(?:\?|$)/.test(r.url));
  const totalBytes=pageCompleted.reduce((n,r)=>n+r.wireBytes,0),fontBytes=fontFiles.reduce((n,r)=>n+r.wireBytes,0);
  const failures=pageCompleted.filter(r=>r.status>=400);
  const pass=totalBytes<=2000000&&fontBytes<=1000000&&heading.presentedAt<=2500&&fonts.some(f=>f.isCustomFont&&f.postScriptName.startsWith('I.MingCP-'))&&!failures.length&&!pageFailed.length;
  trials.push({trial,pass,effectiveLang:await page.locator('html').getAttribute('lang'),totalBytes,fontBytes,heading,platformFonts:fonts,requests:pageCompleted,failed:pageFailed,failures,text:await page.locator('body').innerText()});
  await page.screenshot({path:path.join(OUT,'main-zh-CN-4g-'+trial+'.png'),fullPage:true});
  await context.close();
 }
 const result={ok:trials.every(r=>r.pass),budgets:{totalBytes:2000000,fontBytes:1000000,titlePresentedMs:2500},network:conditions,transport:'Local same-origin overlay with gzip for text/SVG; source root logo URLs resolve to canonical vector aliases. Cold browser cache each trial. No CPU throttle.',homepageSha256:hash(html),brandSha256:hash(await readFile(path.join(ROOT,'brand.css'))),files:[...snapshots].map(([file,r])=>({file,sha256:hash(r.raw),bodyBytes:r.body.length})),trials};
 await writeFile(path.join(OUT,'budget.json'),JSON.stringify(result,null,2)+'\n');
 console.log(JSON.stringify({ok:result.ok,output:OUT,trials:trials.map(({trial,pass,totalBytes,fontBytes,heading,platformFonts,failed,failures})=>({trial,pass,totalBytes,fontBytes,heading,platformFonts,failed,failures}))},null,2));
 if(!result.ok)throw new Error('Phase 0 budget failed; inspect budget.json');
}finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
