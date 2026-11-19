// Real built UI + SQLite HTTP fixture. Faults are injected at the network seam,
// never by replacing the UI/session. Install Playwright outside the repository.
const fs = require('node:fs');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE);
let runningBrowser;
(async () => {
 const ready = process.env.SEARCH_BROWSER_READY;
 const fixture = JSON.parse(fs.readFileSync(ready, 'utf8'));
 const out = process.env.SEARCH_BROWSER_OUTPUT;
 const evidence = [];
 const browser = await chromium.launch({executablePath: process.env.SEARCH_BROWSER_EXE, headless: true});
 runningBrowser=browser;
 const context = await browser.newContext({ locale:'zh-CN', viewport: { width: 1280, height: 900 }});
 const login = await context.request.post(fixture.url + '/api/auth/login', {data:{username:fixture.username,password:fixture.password}});
 if (!login.ok()) throw Error('real login failed: '+login.status());
 const page=await context.newPage();
 page.on('pageerror', e => evidence.push({pageerror:String(e)}));
 let mode='normal', calls=0;
 await page.route('**/api/search?*', async route => {
  calls++;
  const url=new URL(route.request().url());
  if (mode==='initial-fail') {mode='normal';return route.fulfill({status:503,json:{error:'acceptance injected outage'}});}
  if (mode==='append-fail' && url.searchParams.get('cursor')) {mode='normal';return route.fulfill({status:503,json:{error:'acceptance injected append outage'}});}
  if (mode==='invalid-cursor' && url.searchParams.get('cursor')) {mode='normal';return route.fulfill({status:400,json:{code:'invalid_search_cursor',error:'acceptance injected cursor expiry'}});}
  const response=await route.fetch();
  if(mode==='slow') await new Promise(r=>setTimeout(r,1200));
  await route.fulfill({response});
 });
 const assert = async (name, action) => {await action();evidence.push({passed:name});};
 const wait = async predicate => { const end=Date.now()+15000; while(Date.now()<end){if(await predicate())return;await page.waitForTimeout(50);}throw Error('condition timeout'); };
 await page.goto(fixture.url);
 await page.waitForTimeout(1500);
 await page.screenshot({path:out+'-initial.png',fullPage:true});
 console.log('initial',await page.locator('body').innerText());
 await page.keyboard.press('Control+k');
 const dialog=page.getByRole('dialog');
 const input=dialog.getByPlaceholder('输入关键字搜索...');
 await input.waitFor();
 await assert('real HTTP search and first mixed batch',async()=>{await input.fill('Browser Needle');await wait(async()=>await dialog.getByRole('option').count()===20);});
 await assert('append failure preserves results and retry restores next batch',async()=>{mode='append-fail';await dialog.getByRole('button',{name:'继续加载',exact:true}).click();await dialog.getByRole('button',{name:'重试加载',exact:true}).waitFor();if(await dialog.getByRole('option').count()!==20)throw Error('lost results');await dialog.getByRole('button',{name:'重试加载',exact:true}).click();await wait(async()=>await dialog.getByRole('option').count()===40);});
 await assert('invalid cursor offers explicit restart',async()=>{mode='invalid-cursor';await dialog.getByRole('button',{name:'继续加载',exact:true}).click();await dialog.getByRole('button',{name:'重新搜索',exact:true}).click();await wait(async()=>await dialog.getByRole('option').count()===20);});
 await assert('slow query invalidates old options immediately; first failure retry',async()=>{mode='slow';await input.fill('unmatched');if(await dialog.getByRole('option').count())throw Error('stale options');await input.press('Enter');await wait(async()=> (await dialog.getByRole('status').textContent()).includes('未找到'));mode='initial-fail';await input.fill('Browser');await dialog.getByRole('alert').waitFor();await dialog.getByRole('button',{name:'重试',exact:true}).click();await wait(async()=>await dialog.getByRole('option').count()===20);});
 await assert('scope change and complete series pagination',async()=>{await dialog.getByRole('button',{name:'仅系列',exact:true}).click();await wait(async()=>await dialog.getByRole('option').count()===20);await dialog.getByRole('button',{name:'继续加载',exact:true}).dblclick();await wait(async()=>await dialog.getByRole('option').count()===40);await dialog.getByRole('button',{name:'继续加载',exact:true}).click();await wait(async()=>await dialog.getByRole('option').count()===45);if(await dialog.getByRole('button',{name:'继续加载',exact:true}).count())throw Error('append past end');});
 await assert('keyboard selection and close/reopen fresh generation',async()=>{await input.press('ArrowDown');if(await dialog.getByRole('option',{selected:true}).count()!==1)throw Error('selection');await input.press('Escape');await wait(async()=>await page.getByRole('dialog').count()===0);await page.keyboard.press('Control+k');await wait(async()=>await dialog.getByRole('option').count()===20);});
 await page.screenshot({path:out+'.png',fullPage:true});
 fs.writeFileSync(out+'.json',JSON.stringify({browser:await browser.version(),fixture:'real SQLite API routes + rebuilt web/dist',faults:'network route interception only',calls,evidence,notExecuted:['headed visual review','English locale','long-title visual boundary','reader-account browser navigation']},null,2));
 await browser.close();fs.writeFileSync(ready+'.done','done');
})().catch(async e=>{console.error(e);if(runningBrowser)await runningBrowser.close();process.exitCode=1;});
