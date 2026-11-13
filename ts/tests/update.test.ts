import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { newJob, runUpdate, type UpdateDeps } from '../server/update.ts';

function baseInspect() {
  return {
    Id: 'abc',
    Name: '/dsh-harness',
    Config: { Image: 'ghcr.io/smanx/deepseek-harness:latest', Env: ['A=1'] },
    HostConfig: { RestartPolicy: { Name: 'unless-stopped' } },
    NetworkSettings: { Networks: { bridge: {} } },
    Mounts: [],
  };
}

function deps(overrides: Partial<UpdateDeps> = {}): UpdateDeps {
  const calls: string[] = [];
  const d: UpdateDeps = {
    inspect: async () => baseInspect(),
    pull: async () => {},
    create: async () => ({ id: 'new-id' }),
    start: async () => {},
    stop: async () => {},
    remove: async () => {},
    rename: async () => {},
    ...overrides,
  };
  return d;
}

describe('更新流程', () => {
  test('成功路径：拉取 → 创建 → 启动 → 停旧 → 删旧 → 改名', async () => {
    const order: string[] = [];
    const d = deps({
      pull: async () => { order.push('pull'); },
      create: async () => { order.push('create'); return { id: 'new-id' }; },
      start: async () => { order.push('start'); },
      stop: async () => { order.push('stop-old'); },
      remove: async () => { order.push('remove-old'); },
      rename: async () => { order.push('rename'); },
    });
    const job = newJob('abc', 'dsh-harness', 'image:latest');
    await runUpdate(job, d);
    assert.equal(job.status, 'done');
    assert.deepEqual(order, ['pull', 'create', 'start', 'stop-old', 'remove-old', 'rename']);
    assert.ok(job.events.some((e) => e.step === 'done'));
  });

  test('创建失败时旧容器不受影响', async () => {
    let removedNew = false;
    const d = deps({
      create: async () => { throw new Error('boom'); },
      remove: async (id) => { if (id === 'new-id') removedNew = true; },
    });
    const job = newJob('abc', 'x', 'image:latest');
    await runUpdate(job, d);
    assert.equal(job.status, 'error');
    assert.match(job.error ?? '', /创建新容器失败/);
  });

  test('启动新容器失败会回滚新容器', async () => {
    const removed: string[] = [];
    const d = deps({
      start: async () => { throw new Error('cannot start'); },
      remove: async (id) => { removed.push(id); },
    });
    const job = newJob('abc', 'x', 'image:latest');
    await runUpdate(job, d);
    assert.equal(job.status, 'error');
    assert.deepEqual(removed, ['new-id'], '应删除刚创建的新容器');
    assert.match(job.error ?? '', /旧容器保持不变/);
  });

  test('名字冲突时临时名与原名不同', async () => {
    let createdName = '';
    const d = deps({
      create: async (opts) => { createdName = opts.name; return { id: 'new-id' }; },
    });
    const job = newJob('abc', 'x', 'image:latest');
    await runUpdate(job, d);
    assert.notEqual(createdName, 'dsh-harness');
    assert.match(createdName, /^dsh-harness-dc-new-/);
  });
});
