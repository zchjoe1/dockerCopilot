import { serve } from '@hono/node-server';
import { createApp } from './app.ts';
import { listContainers, sampleOne, type StatSample } from './docker.ts';
import { checkImages } from './updates.ts';

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

  // 更新检测：启动 20 秒后跑一轮，之后每 10 分钟一次（与 Go 版白名单语义不同，先全量）
  const UPDATE_MS = Number(process.env.UPDATE_MS || 600000);
  const check = async () => {
    try {
      const images = (await listContainers()).map((r) => r.image);
      await checkImages(images);
      console.log('[updates] 已检测', images.length, '个镜像');
    } catch (err) {
      console.error('[updates] 检测失败:', (err as Error).message);
    }
  };
  setTimeout(check, 20000);
  setInterval(check, UPDATE_MS);
}

const app = createApp({ mock: MOCK, stats, sampleMs: SAMPLE_MS });

serve({ fetch: app.fetch, port: PORT }, (info) => {
  console.log(`[dc-ts] ${MOCK ? 'mock' : 'docker'} 模式，http://127.0.0.1:${info.port}`);
});
