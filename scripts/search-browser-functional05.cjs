// Real Edge + built UI + authenticated SQLite fixture. No response mocks.
const fs = require('node:fs');
const { chromium } = require(process.env.PLAYWRIGHT_MODULE);
(async () => {
 const fixture = JSON.parse(fs.readFileSync(process.env.SEARCH_BROWSER_READY));
 const out = process.env.SEARCH_BROWSER_OUTPUT;
 const browser = await chromium.launch({ executablePath: process.env.SEARCH_BROWSER_EXE, headless:true });
 const evidence=[]; const errors=[];
 try {
  const context=await browser.newContext({locale:'en-US',viewport:{width:1280,height:900}});
  await context.addInitScript(()=>localStorage.setItem('yomifolio_locale','en-US'));
  const page=await context.newPage(); page.on('pageerror',e=>errors.push(String(e)));
  const login=async username=>{const r=await context.request.post(fixture.url+'/api/auth/login',{data:{username,password:fixture.password}});if(!r.ok())throw Error('login '+r.status());};
  const wait=async predicate=>{const end=Date.now()+30000;while(Date.now()<end){if(await predicate())return;await page.waitForTimeout(100);}throw Error('condition timeout at '+page.url());};
  const search=async query=>{await page.keyboard.press('Control+k'); const d=page.getByRole('dialog'); await d.waitFor(); await d.getByRole('textbox').fill(query);await wait(async()=>await d.getByRole('option').count()>0);return d;};
  await login(fixture.username);await page.goto(fixture.url);await page.waitForTimeout(1500);
  let dialog=await search('Long Needle');
  const text=await dialog.innerText();if(!text.includes('Series'))throw Error('English search locale missing');
  const bounds=await dialog.boundingBox();if(!bounds||bounds.x<0||bounds.x+bounds.width>1281)throw Error('dialog outside viewport');
  const overflow=await dialog.evaluate(el=>el.scrollWidth>el.clientWidth+1);if(overflow)throw Error('long title horizontal overflow');
  await page.screenshot({path:out+'-english-long.png',fullPage:true});evidence.push('English locale and 3000-character metadata title: dialog within 1280x900 viewport, no horizontal overflow');
  await dialog.getByRole('option').first().click();await wait(async()=>new URL(page.url()).pathname==='/series/'+fixture.series);evidence.push('admin series search result navigates to actual series route');
  // Same browser context changes its authenticated session, then reloads the UI
  // so the real connection/auth bootstrap observes the new effective identity.
  await login(fixture.reader);await page.goto(fixture.url);await page.waitForTimeout(1500);
  const me=await context.request.get(fixture.url+'/api/auth/me');if(!me.ok())throw Error('reader bootstrap '+me.status());
  const identity=await me.text();if(!identity.includes(fixture.reader)||identity.includes('"role":"admin"'))throw Error('identity did not switch: '+identity);
  dialog=await search('Long Needle');await dialog.getByRole('option').first().click();await wait(async()=>new URL(page.url()).pathname==='/series/'+fixture.series);evidence.push('same browser switched to ordinary reader and navigated from search to series');
  await page.goto(fixture.url);await page.waitForTimeout(1200);dialog=await search('Readable');await dialog.getByRole('option').first().click();await wait(async()=>new URL(page.url()).pathname==='/reader/'+fixture.book);
  await page.waitForTimeout(2000);await page.screenshot({path:out+'-reader.png',fullPage:true});evidence.push('ordinary reader book search navigates to actual reader with accessible valid CBZ');
  if(errors.length)throw Error('page errors: '+errors.join(';'));
  fs.writeFileSync(out+'.json',JSON.stringify({browser:await browser.version(),locale:'en-US',evidence,errors,notExecuted:['headed human visual review','identity switch while an old search response is in flight']},null,2));
 } finally {await browser.close();fs.writeFileSync(process.env.SEARCH_BROWSER_READY+'.done','done');}
})().catch(e=>{console.error(e);process.exitCode=1;});
