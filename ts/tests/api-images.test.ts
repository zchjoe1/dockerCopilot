import { test, describe, beforeEach } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createApp } from '../server/app.ts';
import { resetMock } from '../server/mock.ts';

const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'dc-ts-'));
process.env.DATA_DIR = dir;

const app = createApp({ mock: true });

beforeEach(() => {
  resetMock();
  fs.rmSync(path.join(dir, 'settings.json'), { force: true });
});

describe('镜像接口', () => {
  test('列表返回按大小倒序', async () => {
    const res = await app.request('/api/images');
    assert.equal(res.status, 200);
    const body = (await res.json()) as { items: Array<{ size: number; tags: string[] }>; totalSize: number };
    assert.ok(body.items.length >= 10);
    const sizes = body.items.map((i) => i.size);
    assert.deepEqual(sizes, [...sizes].sort((a, b) => b - a));
    assert.ok(body.totalSize > 0);
    assert.ok(body.items.every((i) => i.tags.length > 0));
  });

  test('删除镜像返回 ok', async () => {
    const res = await app.request('/api/images/sha256:abc', { method: 'DELETE' });
    assert.equal(res.status, 200);
    assert.equal(((await res.json()) as { ok: boolean }).ok, true);
  });

  test('打标签校验参数', async () => {
    const missing = await app.request('/api/images/tag', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ source: 'a' }),
    });
    assert.equal(missing.status, 400);

    const badTag = await app.request('/api/images/tag', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ source: 'a', repo: 'x/y', tag: 'bad tag!' }),
    });
    assert.equal(badTag.status, 400);

    const ok = await app.request('/api/images/tag', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ source: 'a', repo: 'x/y', tag: 'v2' }),
    });
    assert.equal(ok.status, 200);
  });
});

describe('设置接口', () => {
  test('默认值可读', async () => {
    const res = await app.request('/api/settings');
    const body = (await res.json()) as { checkCron: string; cleanOldImage: boolean };
    assert.equal(body.checkCron, '30 * * * *');
    assert.equal(body.cleanOldImage, true);
  });

  test('保存后能读回，且写入磁盘', async () => {
    const put = await app.request('/api/settings', {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ checkOnly: 'a/b, c/d', checkCron: '*/15 * * * *', cleanOldImage: false }),
    });
    assert.equal(put.status, 200);
    const back = (await (await app.request('/api/settings')).json()) as Record<string, unknown>;
    assert.equal(back.checkOnly, 'a/b, c/d');
    assert.equal(back.checkCron, '*/15 * * * *');
    assert.equal(back.cleanOldImage, false);
    assert.ok(fs.existsSync(path.join(dir, 'settings.json')));
  });

  test('非法 cron 被拒绝且不落盘', async () => {
    const put = await app.request('/api/settings', {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ checkCron: 'every hour' }),
    });
    assert.equal(put.status, 400);
    assert.equal(fs.existsSync(path.join(dir, 'settings.json')), false);
  });

  test('慢档填 off 视为关闭，可保存', async () => {
    const put = await app.request('/api/settings', {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ checkCronOthers: 'off' }),
    });
    assert.equal(put.status, 200);
  });
});
