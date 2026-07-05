// E2E: drive the REAL admin panel in Chromium against a live control plane,
// with a real agent (an OpenWrt rootfs container in CI) enrolling mid-test.
//
// The agent is started through the E2E_ENROLL_HOOK shell command: the script
// mints a claim code in the UI, then invokes the hook with CODE in its
// environment. That keeps docker/local specifics out of the browser logic.
//
// Env: E2E_API (default http://127.0.0.1:18080), E2E_EMAIL, E2E_PASSWORD,
//      E2E_ENROLL_HOOK, E2E_ARTIFACTS (screenshot dir, optional).
import { chromium } from 'playwright';
import { execSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';

const API = process.env.E2E_API || 'http://127.0.0.1:18080';
const EMAIL = process.env.E2E_EMAIL || 'e2e@example.com';
const PASSWORD = process.env.E2E_PASSWORD || 'e2e-password-1';
const HOOK = process.env.E2E_ENROLL_HOOK;
const ARTIFACTS = process.env.E2E_ARTIFACTS || '';
if (!HOOK) { console.error('E2E_ENROLL_HOOK is required'); process.exit(2); }

const say = (m) => console.log(`== ${m}`);
let page;

// The panel talks through native dialogs: confirm() before mutations,
// prompt() for rename, alert() for results. Accept everything; prompts get
// the next queued answer.
const promptAnswers = [];

async function main() {
  // E2E_CHROMIUM points at a system Chromium when the playwright-managed
  // download is unavailable (e.g. local runs); CI installs its own.
  const browser = await chromium.launch(
    process.env.E2E_CHROMIUM ? { executablePath: process.env.E2E_CHROMIUM } : {});
  page = await browser.newPage();
  page.setDefaultTimeout(30_000);
  page.on('dialog', (d) => {
    if (d.type() === 'prompt') return d.accept(promptAnswers.shift() ?? '');
    return d.accept();
  });

  say('login through the panel form');
  await page.goto(API);
  await page.fill('#email', EMAIL);
  await page.fill('#password', PASSWORD);
  await page.click('#login button[type=submit]');
  await page.waitForSelector('#app:not([hidden])');

  say('mint a claim code from the UI');
  await page.click('#new-code');
  const code = (await page.textContent('code.claim')).trim();
  if (!/^LG-/.test(code)) throw new Error(`unexpected claim code: ${code}`);

  say(`enroll the node via hook (code ${code.slice(0, 6)}…)`);
  execSync(HOOK, { env: { ...process.env, CODE: code }, stdio: 'inherit', shell: '/bin/bash' });

  say('node appears online in the nodes table');
  await page.waitForSelector('#nodes .status-online', { timeout: 90_000 });

  say('fleet strip counts one online node');
  await page.waitForFunction(
    () => document.getElementById('stats')?.textContent.includes('1 online'),
    undefined, { timeout: 30_000 });

  say('open the node detail view');
  await page.click('#nodes tr');
  await page.waitForSelector('#detail:not([hidden])');

  say('apply a UCI change through the panel editor');
  await page.click('#detail details > summary'); // expand the collapsed editor
  await page.fill('#uci-editor', 'set system.@system[0].hostname=e2e-panel');
  await page.click('#uci-apply'); // confirm + result alert auto-accepted
  await page.waitForFunction(
    () => document.getElementById('detail-out')?.textContent.includes('applied'),
    undefined, { timeout: 60_000 });

  say('change history reaches "confirmed" (auto-revert window closes)');
  const deadline = Date.now() + 90_000;
  for (;;) {
    await page.click('#load-changes');
    await page.waitForFunction(
      () => !document.getElementById('detail-busy')?.textContent, undefined, { timeout: 15_000 });
    const out = await page.textContent('#detail-out');
    if (out.includes('[confirmed]')) break;
    if (out.includes('[failed]') || out.includes('[reverted]')) {
      throw new Error(`config change did not confirm:\n${out}`);
    }
    if (Date.now() > deadline) throw new Error(`confirm timed out:\n${out}`);
    await page.waitForTimeout(3000);
  }

  say('rename the node from the UI');
  promptAnswers.push('e2e-renamed');
  await page.click('#node-rename');
  await page.waitForFunction(
    () => document.getElementById('nodes')?.textContent.includes('e2e-renamed'),
    undefined, { timeout: 30_000 });

  say('audit section shows the session trail');
  await page.click('#audit-section summary');
  await page.waitForFunction(() => {
    const t = document.getElementById('audit')?.textContent || '';
    return t.includes('auth.login') && t.includes('config.apply') && t.includes('node.rename');
  }, undefined, { timeout: 30_000 });

  say('PANEL E2E OK');
  await browser.close();
}

main().catch(async (err) => {
  console.error(`E2E FAIL: ${err.message}`);
  if (ARTIFACTS && page) {
    try {
      mkdirSync(ARTIFACTS, { recursive: true });
      await page.screenshot({ path: `${ARTIFACTS}/failure.png`, fullPage: true });
      console.error(`screenshot: ${ARTIFACTS}/failure.png`);
    } catch { /* best effort */ }
  }
  process.exit(1);
});
