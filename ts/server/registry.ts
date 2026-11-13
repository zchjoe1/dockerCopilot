/** 镜像引用解析与 registry digest 探测（移植自 Go 版 internal/module/auth.go 的读路径）。 */

export interface ImageRef {
  /** 用于 API 的主机名，例如 registry-1.docker.io / ghcr.io */
  host: string;
  /** 仓库路径，例如 library/nginx、smanx/deepseek-harness */
  repository: string;
  tag: string;
  /** 用户书写的原始引用，用于日志与界面 */
  raw: string;
}

const DOCKER_HUB = 'docker.io';
const DOCKER_HUB_API = 'registry-1.docker.io';

export function parseRef(image: string): ImageRef {
  let raw = image.trim();
  let tag = 'latest';
  const at = raw.indexOf('@');
  if (at > 0) {
    // digest 引用：直接当 tag 用
    const digest = raw.slice(at + 1);
    raw = raw.slice(0, at);
    const slash = raw.lastIndexOf('/');
    const colon = raw.lastIndexOf(':');
    tag = colon > slash ? raw.slice(colon + 1) : digest;
    if (colon > slash) raw = raw.slice(0, colon);
    return finish(raw, digest);
  }
  const slash = raw.lastIndexOf('/');
  const colon = raw.lastIndexOf(':');
  if (colon > slash) {
    tag = raw.slice(colon + 1);
    raw = raw.slice(0, colon);
  }
  return finish(raw, tag);
}

function finish(name: string, tag: string): ImageRef {
  const parts = name.split('/');
  const first = parts[0];
  const hasHost = parts.length > 1 && (first.includes('.') || first.includes(':') || first === 'localhost');
  const host = hasHost ? first : DOCKER_HUB;
  let repository = hasHost ? parts.slice(1).join('/') : name;
  if (host === DOCKER_HUB && !repository.includes('/')) repository = `library/${repository}`;
  return { host, repository, tag, raw: name };
}

export function apiHost(ref: ImageRef): string {
  return ref.host === DOCKER_HUB ? DOCKER_HUB_API : ref.host;
}

const ACCEPT = [
  'application/vnd.oci.image.index.v1+json',
  'application/vnd.docker.distribution.manifest.list.v2+json',
  'application/vnd.oci.image.manifest.v1+json',
  'application/vnd.docker.distribution.manifest.v2+json',
].join(', ');

export interface DigestOptions {
  /** Basic base64(user:pass)，来自 docker authconfig */
  auth?: string;
  fetchImpl?: typeof fetch;
  timeoutMs?: number;
}

function parseChallenge(header: string | null): Record<string, string> | null {
  if (!header || !/^bearer/i.test(header)) return null;
  const out: Record<string, string> = {};
  for (const part of header.slice(6).split(',')) {
    const [k, ...rest] = part.trim().split('=');
    if (!k) continue;
    out[k.toLowerCase()] = rest.join('=').replace(/^"|"$/g, '');
  }
  return out.realm ? out : null;
}

/** 取远端 manifest 的 digest；拿不到返回 null（私有仓库未带凭据、网络失败等）。 */
export async function remoteDigest(ref: ImageRef, opts: DigestOptions = {}): Promise<string | null> {
  const doFetch = opts.fetchImpl ?? fetch;
  const timeout = AbortSignal.timeout(opts.timeoutMs ?? 8000);
  const url = `https://${apiHost(ref)}/v2/${ref.repository}/manifests/${ref.tag}`;
  const headers: Record<string, string> = { accept: ACCEPT };

  const head = async (extra?: Record<string, string>) => {
    const res = await doFetch(url, { method: 'HEAD', headers: { ...headers, ...(extra ?? {}) }, signal: timeout });
    return res;
  };

  let res: Response;
  try {
    res = await head(opts.auth ? { authorization: `Basic ${opts.auth}` } : undefined);
    if (res.status === 401) {
      const challenge = parseChallenge(res.headers.get('www-authenticate'));
      if (!challenge) return null;
      const tokenUrl = new URL(challenge.realm);
      if (challenge.service) tokenUrl.searchParams.set('service', challenge.service);
      tokenUrl.searchParams.set('scope', `repository:${ref.repository}:pull`);
      const treq = await doFetch(tokenUrl, {
        headers: opts.auth ? { authorization: `Basic ${opts.auth}` } : {},
        signal: timeout,
      });
      if (!treq.ok) return null;
      const body = (await treq.json()) as { token?: string; access_token?: string };
      const token = body.token ?? body.access_token;
      if (!token) return null;
      res = await head({ authorization: `Bearer ${token}` });
    }
    if (!res.ok) return null;
    return res.headers.get('docker-content-digest');
  } catch {
    return null;
  }
}

/** 从 docker 的 RepoDigests 里取本地 digest。 */
export function localDigest(repoDigests: string[] | undefined): string | null {
  if (!repoDigests?.length) return null;
  for (const d of repoDigests) {
    const at = d.indexOf('@');
    if (at > 0) return d.slice(at + 1);
  }
  return null;
}
