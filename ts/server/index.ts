import { Hono } from 'hono';
import { serve } from '@hono/node-server';
import { serveStatic } from '@hono/node-server/serve-static';
import fs from 'node:fs';
import path from 'node:path';
import { act, inspectOne, listContainers, sampleOne, type ContainerRow, type StatSample } from './docker.ts';
import { mockAct, mockInspect, mockList } from './mock.ts';

const MOCK = process.env.MOCK === '1' || process.env.MOCK === 'true';
const PORT = Number(process.env.PORT || 3080);
const SAMPLE_MS = Number(process.env.SAMPLE_MS || 10000);

const stats = new Map<string, StatSample>();
const DISABLED = new Set<string>();

async function refreshStats(rows: ContainerRow[]) {
  const running = rows.filter((r) => r.state === 'running' && !DISABLED.has(r.id));
  const queue = [...running];
  const worker = async () => {
    while (queue.length) {
      const row = queue.shift();
      if (!row) return;
      const s = await sampleOne(row.id);
      if (s) stats.set(row.id, s);
      else DISABLED.add(row.id);
    }
  };
  await Promise.all([worker(), worker(), worker(), worker()]);
}

async function loadRows(): Promise<ContainerRow[]> {
  if (MOCK) return mockList();
  const rows = await listContainers();
  for (const r of rows) {
    const s = stats.get(r.id);
    if (s) {
      r.cpuPct = s.cpuPct;
      r.memBytes = s.memBytes;
    }
  }
  return rows;
}

if (!MOCK) {
  const tick = async () => {
    try {
      await refreshStats(await listContainers());
    } catch (err) {
      console.error('[stats] 采样失败:', (err as Error).message);
    }
  };
  tick();
  setInterval(tick, SAMPLE_MS);
}

const app = new Hono();

app.get('/api/health', (c) => c.json({ ok: true, mode: MOCK ? 'mock' : 'docker', sampleMs: SAMPLE_MS }));

app.get('/api/updates', (c) =>
  c.json({
    checkedAt: null,
    pending: [],
    note: '更新检测尚未移植：Go 版的 registry digest 比对（含私有仓库认证与加速站回退）还没搬过来。',
  }),
);

app.get('/api/containers', async (c) => {
  try {
    const rows = await loadRows();
    rows.sort((a, b) => (b.memBytes ?? 0) - (a.memBytes ?? 0));
    return c.json({ items: rows, sampledAt: Date.now() });
  } catch (err) {
    return c.json({ error: (err as Error).message }, 500);
  }
});

app.get('/api/containers/:id', async (c) => {
  const id = c.req.param('id');
  try {
    const info = MOCK ? mockInspect(id) : await inspectOne(id);
    if (!info) return c.json({ error: 'not found' }, 404);
    return c.json(info);
  } catch (err) {
    return c.json({ error: (err as Error).message }, 500);
  }
});

app.post('/api/containers/:id/update', (c) => {
  const id = c.req.param('id');
  if (MOCK) return c.json({ ok: true, id, action: 'update' });
  return c.json({ error: '更新检测尚未移植：镜像 digest 比对与重建流程还在计划里（见 README）' }, 501);
});

app.post('/api/containers/:id/:action', async (c) => {
  const id = c.req.param('id');
  const action = c.req.param('action');
  if (!['start', 'stop', 'restart'].includes(action)) return c.json({ error: 'unsupported action' }, 400);
  try {
    if (MOCK) mockAct(id, action as 'start' | 'stop' | 'restart');
    else await act(id, action as 'start' | 'stop' | 'restart');
    stats.delete(id);
    return c.json({ ok: true, id, action });
  } catch (err) {
    return c.json({ error: (err as Error).message }, 500);
  }
});

// 静态资源（vite 产物）
app.use('/assets/*', serveStatic({ root: './dist' }));
app.get('/logo.png', serveStatic({ root: './dist' }));
app.get('/', (c) => {
  const file = path.resolve('./dist/index.html');
  if (!fs.existsSync(file)) return c.text('前端还没构建：先跑 npm run build:ui', 500);
  return c.html(fs.readFileSync(file, 'utf8'));
});

serve({ fetch: app.fetch, port: PORT }, (info) => {
  console.log(`[dc-ts] ${MOCK ? 'mock' : 'docker'} 模式，http://127.0.0.1:${info.port}`);
});
