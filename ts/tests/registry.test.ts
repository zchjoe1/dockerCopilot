import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { apiHost, localDigest, parseRef, remoteDigest } from '../server/registry.ts';

describe('镜像引用解析', () => {
  test('docker hub 短名补 library/ 与默认 tag', () => {
    const r = parseRef('nginx');
    assert.equal(r.host, 'docker.io');
    assert.equal(r.repository, 'library/nginx');
    assert.equal(r.tag, 'latest');
    assert.equal(apiHost(r), 'registry-1.docker.io');
  });

  test('带命名空间的 docker hub 引用', () => {
    const r = parseRef('calciumion/new-api:latest');
    assert.equal(r.host, 'docker.io');
    assert.equal(r.repository, 'calciumion/new-api');
    assert.equal(r.tag, 'latest');
  });

  test('自定义 registry 与端口', () => {
    const r = parseRef('ghcr.io/smanx/deepseek-harness:latest');
    assert.equal(r.host, 'ghcr.io');
    assert.equal(r.repository, 'smanx/deepseek-harness');
    assert.equal(apiHost(r), 'ghcr.io');

    const local = parseRef('192.168.1.250:5000/team/app:1.2.3');
    assert.equal(local.host, '192.168.1.250:5000');
    assert.equal(local.repository, 'team/app');
    assert.equal(local.tag, '1.2.3');
  });
});

describe('本地 digest 提取', () => {
  test('从 RepoDigests 取 digest', () => {
    assert.equal(
      localDigest(['ghcr.io/smanx/deepseek-harness@sha256:abc123']),
      'sha256:abc123',
    );
  });
  test('空值返回 null', () => {
    assert.equal(localDigest(undefined), null);
    assert.equal(localDigest([]), null);
    assert.equal(localDigest(['weird']), null);
  });
});

describe('远端 digest 探测', () => {
  test('401 挑战后带 token 重试成功', async () => {
    const calls: string[] = [];
    const fakeFetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      calls.push(`${init?.method ?? 'GET'} ${url}`);
      if (url.startsWith('https://auth.example/token')) {
        return new Response(JSON.stringify({ token: 'tok' }), { status: 200 });
      }
      const auth = new Headers(init?.headers).get('authorization');
      if (!auth) {
        return new Response('', {
          status: 401,
          headers: { 'www-authenticate': 'Bearer realm="https://auth.example/token",service="registry.example"' },
        });
      }
      return new Response('', { status: 200, headers: { 'docker-content-digest': 'sha256:remote' } });
    }) as typeof fetch;

    const digest = await remoteDigest(parseRef('ghcr.io/x/y:1'), { fetchImpl: fakeFetch });
    assert.equal(digest, 'sha256:remote');
    assert.equal(calls.filter((c) => c.startsWith('HEAD')).length, 2, '应重试一次');
    assert.ok(calls.some((c) => c.includes('scope=repository%3Ax%2Fy%3Apull')));
  });

  test('网络异常返回 null 而不是抛出', async () => {
    const fakeFetch = (async () => {
      throw new Error('boom');
    }) as unknown as typeof fetch;
    assert.equal(await remoteDigest(parseRef('nginx'), { fetchImpl: fakeFetch }), null);
  });

  test('非 2xx 返回 null', async () => {
    const fakeFetch = (async () => new Response('', { status: 404 })) as unknown as typeof fetch;
    assert.equal(await remoteDigest(parseRef('nginx'), { fetchImpl: fakeFetch }), null);
  });
});
