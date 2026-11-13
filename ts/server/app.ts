import { Hono } from 'hono';
import { serveStatic } from '@hono/node-server/serve-static';
import fs from 'node:fs';
import path from 'node:path';
import { act, inspectOne, listContainers, renameOne, type ContainerRow, type StatSample } from './docker.ts';
import { mockAct, mockInspect, mockList, mockRename } from './mock.ts';

export type AppOptions = {
  mock: boolean;
  stats?: Map<string, StatSample>;
  sampleMs?: number;
};

/** 建 app（不监听端口），便于单测直接 app.request()。 */
export function createApp(opts: AppOptions) {
  const { mock } = opts;
  const stats = opts.stats ?? new Map<string, StatSample>();
  const app = new Hono();

  const rows = async (): Promise<ContainerRow[]> => {
    if (mock) return mockList();
    const list = await listContainers();
    for (const r of list) {
      const s = stats.get(r.id);
      if (s) {
        r.cpuPct = s.cpuPct;
        r.memBytes = s.memBytes;
      }
    }
    return list;
  };

  app.get('/api/health', (c) => c.json({ ok: true, mode: mock ? 'mock' : 'docker', sampleMs: opts.sampleMs ?? 10000 }));

  app.get('/api/updates', (c) =>
    c.json({
      checkedAt: null,
      pending: [],
      note: '更新检测尚未移植：镜像 digest 比对与重建流程还在计划里（见 ts/README.md）。',
    }),
  );

  app.get('/api/containers', async (c) => {
    try {
      const list = await rows();
      list.sort((a, b) => (b.memBytes ?? 0) - (a.memBytes ?? 0));
      return c.json({ items: list, sampledAt: Date.now() });
    } catch (err) {
      return c.json({ error: (err as Error).message }, 500);
    }
  });

  app.get('/api/containers/:id', async (c) => {
    const id = c.req.param('id');
    try {
      const info = mock ? mockInspect(id) : await inspectOne(id);
      if (!info) return c.json({ error: 'not found' }, 404);
      return c.json(info);
    } catch (err) {
      return c.json({ error: (err as Error).message }, 500);
    }
  });

  app.post('/api/containers/:id/rename', async (c) => {
    const id = c.req.param('id');
    let name = '';
    try {
      name = String(((await c.req.json()) as { name?: string }).name || '').trim();
    } catch {
      return c.json({ error: 'invalid json' }, 400);
    }
    if (!/^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$/.test(name)) return c.json({ error: '容器名不合法' }, 400);
    try {
      if (mock) mockRename(id, name);
      else await renameOne(id, name);
      return c.json({ ok: true, id, name });
    } catch (err) {
      return c.json({ error: (err as Error).message }, 500);
    }
  });

  app.post('/api/containers/:id/update', (c) => {
    const id = c.req.param('id');
    if (mock) return c.json({ ok: true, id, action: 'update' });
    return c.json({ error: '更新检测尚未移植：镜像 digest 比对与重建流程还在计划里（见 ts/README.md）' }, 501);
  });

  app.post('/api/containers/:id/:action', async (c) => {
    const id = c.req.param('id');
    const action = c.req.param('action');
    if (!['start', 'stop', 'restart'].includes(action)) return c.json({ error: 'unsupported action' }, 400);
    try {
      if (mock) mockAct(id, action as 'start' | 'stop' | 'restart');
      else await act(id, action as 'start' | 'stop' | 'restart');
      stats.delete(id);
      return c.json({ ok: true, id, action });
    } catch (err) {
      return c.json({ error: (err as Error).message }, 500);
    }
  });

  // 静态资源（vite 产物）。测试环境不挂，避免依赖 dist 存在。
  if (process.env.NODE_ENV !== 'test') {
    app.use('/assets/*', serveStatic({ root: './dist' }));
    app.get('/logo.png', serveStatic({ root: './dist' }));
    app.get('/', (c) => {
      const file = path.resolve('./dist/index.html');
      if (!fs.existsSync(file)) return c.text('前端还没构建：先跑 npm run build:ui', 500);
      return c.html(fs.readFileSync(file, 'utf8'));
    });
  }

  return app;
}
