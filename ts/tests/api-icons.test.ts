import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createApp } from '../server/app.ts';

const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'dc-ts-icons-'));
process.env.DATA_DIR = dir;
const app = createApp({ mock: true });

describe('图标接口', () => {
  test('上传 png 后可列出、可读取、可删除', async () => {
    const png = new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3, 4]);
    const form = new FormData();
    form.append('file', new File([png], 'logo.png', { type: 'image/png' }));
    form.append('image', 'nginx');

    const up = await app.request('/api/icons', { method: 'POST', body: form });
    assert.equal(up.status, 200);
    const { item } = (await up.json()) as { item: { name: string; url: string; size: number } };
    assert.match(item.name, /^nginx-[0-9a-f]{8}\.png$/);
    assert.equal(item.size, png.byteLength);

    const list = (await (await app.request('/api/icons')).json()) as { items: Array<{ name: string }> };
    assert.ok(list.items.some((i) => i.name === item.name));

    const got = await app.request(`/api/icons/${item.name}`);
    assert.equal(got.status, 200);
    assert.equal(got.headers.get('content-type'), 'image/png');

    const del = await app.request(`/api/icons/${item.name}`, { method: 'DELETE' });
    assert.equal(del.status, 200);
    assert.equal((await app.request(`/api/icons/${item.name}`)).status, 404);
  });

  test('拒绝非图片类型', async () => {
    const form = new FormData();
    form.append('file', new File([new Uint8Array([1, 2, 3])], 'x.txt', { type: 'text/plain' }));
    const res = await app.request('/api/icons', { method: 'POST', body: form });
    assert.equal(res.status, 400);
  });

  test('缺少 file 字段返回 400', async () => {
    const form = new FormData();
    form.append('image', 'nginx');
    const res = await app.request('/api/icons', { method: 'POST', body: form });
    assert.equal(res.status, 400);
  });
});
