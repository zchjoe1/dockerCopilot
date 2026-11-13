import { serve } from '@hono/node-server';
import { createApp } from './app.ts';
import { listContainers, sampleOne, type StatSample } from './docker.ts';

const MOCK = process.env.MOCK === '1' || process.env.MOCK === 'true';
const PORT = Number(process.env.PORT || 3080);
const SAMPLE_MS = Number(process.env.SAMPLE_MS || 10000);

const stats = new Map<string, StatSample>();
const disabled = new Set<string>();

async function refreshStats() {
  const rows = (await listContainers()).filter((r) => r.state === 'running' && !disabled.has(r.id));
  const queue = [...rows];
  const worker = async () => {
    while (queue.length) {
      const row = queue.shift();
      if (!row) return;
      const s = await sampleOne(row.id);
      if (s) stats.set(row.id, s);
      else disabled.add(row.id);
    }
  };
  await Promise.all([worker(), worker(), worker(), worker()]);
}

if (!MOCK) {
  const tick = async () => {
    try {
      await refreshStats();
    } catch (err) {
      console.error('[stats] 采样失败:', (err as Error).message);
    }
  };
  tick();
  setInterval(tick, SAMPLE_MS);
}

const app = createApp({ mock: MOCK, stats, sampleMs: SAMPLE_MS });

serve({ fetch: app.fetch, port: PORT }, (info) => {
  console.log(`[dc-ts] ${MOCK ? 'mock' : 'docker'} 模式，http://127.0.0.1:${info.port}`);
});
