import { test, describe, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import { createApp } from '../server/app.ts';
import { mockBackups, resetMock } from '../server/mock.ts';

const app = createApp({ mock: true });

beforeEach(() => {
  resetMock();
  mockBackups.length = 0;
});

describe('Compose 接口', () => {
  test('项目列表带文件路径', async () => {
    const body = (await (await app.request('/api/compose')).json()) as {
      items: Array<{ name: string; file: string }>;
    };
    assert.equal(body.items.length, 2);
    assert.ok(body.items.every((p) => p.file.endsWith('.yaml')));
  });

  test('读取文件内容', async () => {
    const file = encodeURIComponent('/vol1/1000/docker/deepseek-harness/compose.yaml');
    const body = (await (await app.request(`/api/compose/file?file=${file}`)).json()) as { content: string };
    assert.match(body.content, /services:/);
    assert.match(body.content, /dsh-harness/);
  });

  test('缺少 file 参数返回 400', async () => {
    assert.equal((await app.request('/api/compose/file')).status, 400);
  });

  test('保存要求 file 与 content', async () => {
    const bad = await app.request('/api/compose/file', {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ file: '/x/compose.yaml' }),
    });
    assert.equal(bad.status, 400);
    const ok = await app.request('/api/compose/file', {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ file: '/x/compose.yaml', content: 'services: {}' }),
    });
    assert.equal(ok.status, 200);
  });

  test('一键生效在 mock 下返回成功', async () => {
    const res = await app.request('/api/compose/apply', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ file: '/vol1/1000/docker/x/compose.yaml' }),
    });
    assert.equal(res.status, 200);
    const body = (await res.json()) as { ok: boolean; code: number };
    assert.equal(body.ok, true);
    assert.equal(body.code, 0);
  });
});

describe('备份接口', () => {
  test('创建 → 列表 → 恢复 → 删除', async () => {
    const created = (await (await app.request('/api/backups/mock0', { method: 'POST' })).json()) as {
      ok: boolean;
      item: { file: string; name: string };
    };
    assert.equal(created.ok, true);
    assert.match(created.item.file, /\.json$/);

    const list = (await (await app.request('/api/backups')).json()) as { items: unknown[] };
    assert.equal(list.items.length, 1);

    const restored = await app.request(`/api/backups/${encodeURIComponent(created.item.file)}/restore`, {
      method: 'POST',
    });
    assert.equal(restored.status, 200);

    const removed = await app.request(`/api/backups/${encodeURIComponent(created.item.file)}`, { method: 'DELETE' });
    assert.equal(removed.status, 200);
    const after = (await (await app.request('/api/backups')).json()) as { items: unknown[] };
    assert.equal(after.items.length, 0);
  });

  test('备份不存在的容器返回 500', async () => {
    const res = await app.request('/api/backups/nope', { method: 'POST' });
    assert.equal(res.status, 500);
  });
});

describe('版本与自更新', () => {
  test('版本接口返回名称/模式/运行时长', async () => {
    const body = (await (await app.request('/api/version')).json()) as {
      name: string; mode: string; node: string; uptimeSec: number;
    };
    assert.equal(body.name, 'dc-ts');
    assert.equal(body.mode, 'mock');
    assert.match(body.node, /^v\d+/);
    assert.ok(body.uptimeSec >= 0);
  });

  test('自更新在容器部署下明确拒绝（501）', async () => {
    const res = await app.request('/api/program', { method: 'PUT' });
    assert.equal(res.status, 501);
    const body = (await res.json()) as { error: string };
    assert.match(body.error, /更新镜像/);
  });

  test('导出 compose 片段', async () => {
    const res = await app.request('/api/compose/export?container=mock0');
    assert.equal(res.status, 200);
    assert.match(res.headers.get('content-type') || '', /yaml/);
    const text = await res.text();
    assert.match(text, /services:/);
    assert.match(text, /image:/);
  });
});
