import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { beforeEach } from 'node:test';
import { createApp } from '../server/app.ts';
import { resetMock } from '../server/mock.ts';

const app = createApp({ mock: true, sampleMs: 10000 });

beforeEach(() => resetMock());

describe('dc-ts API（mock 模式）', () => {
  test('health 返回模式与采样周期', async () => {
    const res = await app.request('/api/health');
    assert.equal(res.status, 200);
    const body = (await res.json()) as { ok: boolean; mode: string; sampleMs: number };
    assert.equal(body.ok, true);
    assert.equal(body.mode, 'mock');
    assert.equal(body.sampleMs, 10000);
  });

  test('容器列表按内存倒序，且字段齐全', async () => {
    const res = await app.request('/api/containers');
    assert.equal(res.status, 200);
    const { items } = (await res.json()) as { items: Array<Record<string, unknown>> };
    assert.ok(items.length >= 10);
    for (const row of items) {
      for (const key of ['id', 'name', 'image', 'state', 'uptimeSec', 'createdAt']) {
        assert.ok(key in row, `缺少字段 ${key}`);
      }
    }
    const mems = items.map((r) => Number(r.memBytes ?? 0));
    const sorted = [...mems].sort((a, b) => b - a);
    assert.deepEqual(mems, sorted, '列表应按内存倒序');
  });

  test('恰好一个容器带可更新标记', async () => {
    const { items } = (await (await app.request('/api/containers')).json()) as {
      items: Array<{ hasUpdate?: boolean; name: string }>;
    };
    const upd = items.filter((r) => r.hasUpdate);
    assert.equal(upd.length, 1);
    assert.equal(upd[0].name, 'dsh-harness');
  });

  test('容器详情返回镜像 ID 与重启策略', async () => {
    const res = await app.request('/api/containers/mock0');
    assert.equal(res.status, 200);
    const body = (await res.json()) as { imageId: string; restartPolicy: string };
    assert.match(body.imageId, /^sha256:/);
    assert.ok(body.restartPolicy.length > 0);
  });

  test('不存在的容器返回 404', async () => {
    const res = await app.request('/api/containers/nope');
    assert.equal(res.status, 404);
  });

  test('停止后列表状态变为 exited', async () => {
    const id = 'mock2';
    const res = await app.request(`/api/containers/${id}/stop`, { method: 'POST' });
    assert.equal(res.status, 200);
    const { items } = (await (await app.request('/api/containers')).json()) as {
      items: Array<{ id: string; state: string }>;
    };
    assert.equal(items.find((r) => r.id === id)?.state, 'exited');
  });

  test('非法动作被拒绝', async () => {
    const res = await app.request('/api/containers/mock0/explode', { method: 'POST' });
    assert.equal(res.status, 400);
  });

  test('重命名校验：非法名字 400，合法名字 200', async () => {
    const bad = await app.request('/api/containers/mock0/rename', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ name: 'bad name!' }),
    });
    assert.equal(bad.status, 400);

    const ok = await app.request('/api/containers/mock0/rename', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ name: 'renamed-by-test' }),
    });
    assert.equal(ok.status, 200);
  });

  test('更新接口在 mock 下返回 jobId，且任务可查询', async () => {
    const res = await app.request('/api/containers/mock0/update', { method: 'POST' });
    assert.equal(res.status, 200);
    const body = (await res.json()) as { ok: boolean; jobId: string };
    assert.equal(body.ok, true);
    assert.ok(body.jobId);
    const job = (await (await app.request(`/api/jobs/${body.jobId}`)).json()) as { status: string; events: unknown[] };
    assert.equal(job.status, 'done');
    assert.ok(job.events.length >= 3);
  });

  test('updates 在 mock 下返回待更新列表', async () => {
    const body = (await (await app.request('/api/updates')).json()) as {
      pending: Array<{ image: string; name: string }>;
      mode: string;
    };
    assert.equal(body.mode, 'mock');
    assert.equal(body.pending.length, 1);
    assert.equal(body.pending[0].name, 'dsh-harness');
  });
});
