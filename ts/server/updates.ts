import { docker } from './docker.ts';
import { localDigest, parseRef, remoteDigest } from './registry.ts';
import type { ContainerRow } from './docker.ts';

export interface UpdateVerdict {
  image: string;
  local: string | null;
  remote: string | null;
  hasUpdate: boolean;
  checkedAt: number;
  error?: string;
}

const TTL_MS = 10 * 60 * 1000;
const cache = new Map<string, UpdateVerdict>();
const inFlight = new Map<string, Promise<UpdateVerdict>>();

async function checkOne(image: string): Promise<UpdateVerdict> {
  const now = Date.now();
  const cached = cache.get(image);
  if (cached && now - cached.checkedAt < TTL_MS) return cached;

  const pending = inFlight.get(image);
  if (pending) return pending;

  const task = (async (): Promise<UpdateVerdict> => {
    const ref = parseRef(image);
    let local: string | null = null;
    try {
      const info = await docker.getImage(image).inspect();
      local = localDigest(info.RepoDigests);
    } catch {
      local = null;
    }
    const remote = await remoteDigest(ref);
    const verdict: UpdateVerdict = {
      image,
      local,
      remote,
      hasUpdate: Boolean(local && remote && local !== remote),
      checkedAt: Date.now(),
      ...(local === null || remote === null ? { error: 'digest 不可得（私有仓库或网络）' } : {}),
    };
    cache.set(image, verdict);
    return verdict;
  })();

  inFlight.set(image, task);
  try {
    return await task;
  } finally {
    inFlight.delete(image);
  }
}

export async function checkImages(images: string[]): Promise<UpdateVerdict[]> {
  const unique = [...new Set(images)];
  const queue = [...unique];
  const out: UpdateVerdict[] = [];
  const worker = async () => {
    while (queue.length) {
      const img = queue.shift();
      if (!img) return;
      out.push(await checkOne(img));
    }
  };
  await Promise.all([worker(), worker(), worker()]);
  return out;
}

export function cachedVerdict(image: string): UpdateVerdict | undefined {
  return cache.get(image);
}

export function applyVerdicts(rows: ContainerRow[]): ContainerRow[] {
  for (const row of rows) {
    const v = cache.get(row.image);
    if (v) row.hasUpdate = v.hasUpdate;
  }
  return rows;
}

export function pendingUpdates() {
  return [...cache.values()].filter((v) => v.hasUpdate);
}

/** 测试用：清缓存 */
export function resetUpdateCache() {
  cache.clear();
}
