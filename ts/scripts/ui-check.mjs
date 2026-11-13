#!/usr/bin/env node
/**
 * ui-check —— 多视口验收：在 1440 / 1024 / 390 三档打开五个页面，
 * 断言「无横向溢出 / 关键元素存在 / 关键区域不越界」，并留下截图。
 *
 * 用法：
 *   node scripts/ui-check.mjs                 # 连已跑着的 mock 服务（默认 3093）
 *   node scripts/ui-check.mjs --serve         # 自己起一个 mock 服务再检查
 *   node scripts/ui-check.mjs --base http://192.168.1.250:3093 --cdp http://192.168.1.250:9222
 */
import { spawn } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const WS = require('/root/.dsh/profiles/web/node_modules/ws');

const argv = process.argv.slice(2);
const opt = (name, def) => {
  const i = argv.indexOf(`--${name}`);
  return i >= 0 ? argv[i + 1] : def;
};
const PORT = Number(opt('port', 3093));
const BASE = String(opt('base', `http://192.168.1.250:${PORT}`)).replace(/\/$/, '');
const CDP = String(opt('cdp', process.env.CDP_ENDPOINT || 'http://192.168.1.250:9222')).replace(/\/$/, '');
const OUT = path.resolve('shots');
const SERVE = argv.includes('--serve');

const PAGES = [
  { key: 'containers', hash: '#/containers', must: ['.wall', '.tile', '.insp .kpi'] },
  { key: 'images', hash: '#/images', must: ['.ptable', '.prow', '.phead'] },
  { key: 'backups', hash: '#/backups', must: ['.ptable', '.h h1'] },
  { key: 'compose', hash: '#/compose', must: ['.compose', '.clist', '.ceditor'] },
  { key: 'settings', hash: '#/settings', must: ['.form', '.field textarea'] },
];

const VIEWPORTS = [
  { name: '1440', width: 1440, height: 900 },
  { name: '1024', width: 1024, height: 768 },
  { name: '390', width: 390, height: 844, mobile: true },
];

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function waitHealth(timeoutMs = 20000) {
  const until = Date.now() + timeoutMs;
  while (Date.now() < until) {
    try {
      const r = await fetch(`${BASE}/api/health`);
      if (r.ok) return true;
    } catch {
      /* not up yet */
    }
    await sleep(400);
  }
  return false;
}

async function shoot(page, vp) {
  const url = `${BASE}/${page.hash}`;
  const res = await fetch(`${CDP}/json/new?${encodeURIComponent(url)}`, { method: 'PUT' });
  if (!res.ok) throw new Error(`创建标签页失败：${res.status}`);
  const target = await res.json();
  const ws = new WS(target.webSocketDebuggerUrl, { perMessageDeflate: false });
  let id = 0;
  const pending = new Map();
  ws.on('message', (raw) => {
    const msg = JSON.parse(raw.toString());
    if (msg.id && pending.has(msg.id)) {
      const { resolve, reject } = pending.get(msg.id);
      pending.delete(msg.id);
      msg.error ? reject(new Error(JSON.stringify(msg.error))) : resolve(msg.result);
    }
  });
  const send = (method, params = {}) =>
    new Promise((resolve, reject) => {
      const mid = ++id;
      pending.set(mid, { resolve, reject });
      ws.send(JSON.stringify({ id: mid, method, params }));
      setTimeout(() => {
        if (pending.has(mid)) {
          pending.delete(mid);
          reject(new Error(`timeout ${method}`));
        }
      }, 20000);
    });

  try {
    await new Promise((r, j) => {
      ws.on('open', r);
      ws.on('error', j);
    });
    await send('Page.enable');
    await send('Runtime.enable');
    await send('Emulation.setDeviceMetricsOverride', {
      width: vp.width,
      height: vp.height,
      deviceScaleFactor: 1,
      mobile: Boolean(vp.mobile),
    });
    await send('Page.navigate', { url });
    await sleep(1800);

    const probe = async (expression) => {
      const { result } = await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
      return result.value;
    };

    const must = await probe(`JSON.stringify(${JSON.stringify(page.must)}.map((s) => [s, !!document.querySelector(s)]))`);
    const missing = JSON.parse(must).filter(([, ok]) => !ok).map(([s]) => s);

    const overflow = await probe(
      'JSON.stringify({scroll: document.documentElement.scrollWidth, inner: window.innerWidth, body: document.body.scrollWidth})',
    );
    const o = JSON.parse(overflow);
    const horizontalOverflow = o.scroll > o.inner + 1 || o.body > o.inner + 1;

    // 关键块不越界（右侧不超出视口）
    const rects = await probe(`JSON.stringify(${JSON.stringify(page.must)}.map((s) => {
      const el = document.querySelector(s);
      if (!el) return [s, null];
      const r = el.getBoundingClientRect();
      return [s, Math.round(r.right)];
    }))`);
    const outOfBounds = JSON.parse(rects).filter(([, right]) => right !== null && right > vp.width + 1).map(([s]) => s);

    const shot = await send('Page.captureScreenshot', { format: 'png' });
    fs.mkdirSync(OUT, { recursive: true });
    const file = path.join(OUT, `${page.key}-${vp.name}.png`);
    fs.writeFileSync(file, Buffer.from(shot.data, 'base64'));

    // 手机端交互：点格子应弹出详情抽屉；桌面端 inspector 应常驻
    let mobile = null;
    if (page.key === 'containers') {
      if (vp.mobile) {
        await probe("(()=>{const t=document.querySelector('.tile');if(t)t.click();return 1})()");
        await sleep(700);
        const state = JSON.parse(
          await probe(`JSON.stringify({
            sheet: getComputedStyle(document.querySelector('.sheet') || document.body).display,
            insp: getComputedStyle(document.querySelector('.insp')).display,
            actions: !!document.querySelector('.sheet .actions .b')
          })`),
        );
        const bad = [];
        if (state.sheet === 'none') bad.push('详情抽屉未打开');
        if (state.insp !== 'none') bad.push('手机端 inspector 未隐藏');
        if (!state.actions) bad.push('抽屉里没有操作按钮');
        mobile = bad;
        const sheetShot = await send('Page.captureScreenshot', { format: 'png' });
        fs.writeFileSync(path.join(OUT, `${page.key}-${vp.name}-sheet.png`), Buffer.from(sheetShot.data, 'base64'));
      } else {
        const inspDisplay = await probe("getComputedStyle(document.querySelector('.insp')).display");
        mobile = inspDisplay === 'none' ? ['桌面端 inspector 被隐藏'] : [];
      }
    }

    return { missing, horizontalOverflow, outOfBounds, file, mobile };
  } finally {
    ws.close();
    await fetch(`${CDP}/json/close/${target.id}`).catch(() => {});
  }
}

async function main() {
  let child = null;
  if (SERVE) {
    child = spawn('node', ['dist-server/index.mjs'], {
      env: { ...process.env, MOCK: '1', PORT: String(PORT) },
      stdio: 'ignore',
    });
  }
  const ok = await waitHealth();
  if (!ok) {
    console.error(`[ui-check] 服务没起来：${BASE}/api/health`);
    child?.kill();
    process.exit(1);
  }

  const failures = [];
  for (const page of PAGES) {
    for (const vp of VIEWPORTS) {
      let r;
      try {
        r = await shoot(page, vp);
      } catch (err) {
        failures.push(`${page.key}@${vp.name}: ${(err && err.message) || String(err)}`);
        continue;
      }
      const bad = [];
      if (r.missing.length) bad.push(`缺元素 ${r.missing.join(',')}`);
      if (r.horizontalOverflow) bad.push('横向溢出');
      if (r.outOfBounds.length) bad.push(`越界 ${r.outOfBounds.join(',')}`);
      if (r.mobile && r.mobile.length) bad.push(...r.mobile);
      const status = bad.length ? '✗' : '✓';
      console.log(`${status} ${page.key.padEnd(10)} ${vp.name.padEnd(5)} ${bad.join(' · ')}`);
      if (bad.length) failures.push(`${page.key}@${vp.name}: ${bad.join(' · ')}`);
    }
  }

  child?.kill();
  if (failures.length) {
    console.error(`\n[ui-check] 失败 ${failures.length} 项：`);
    for (const f of failures) console.error('  - ' + f);
    process.exit(1);
  }
  console.log(`\n[ui-check] 全部通过，截图在 ${OUT}`);
}

main();
