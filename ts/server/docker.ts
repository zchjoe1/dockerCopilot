import Docker from 'dockerode';

export const docker = new Docker({ socketPath: process.env.DOCKER_SOCKET || '/var/run/docker.sock' });

export interface ContainerRow {
  id: string;
  name: string;
  image: string;
  state: 'running' | 'exited' | 'created' | 'paused' | 'restarting' | 'dead';
  status: string;
  uptimeSec: number;
  cpuPct: number | null;
  memBytes: number | null;
  createdAt: number;
  project?: string;
  hasUpdate?: boolean;
}

/** docker 列表 → 界面要的行。CPU/内存由采样器补。 */
export async function listContainers(): Promise<ContainerRow[]> {
  const raw = await docker.listContainers({ all: true });
  const now = Date.now();
  return raw.map((c) => {
    const started = Date.parse(c.State === 'running' ? c.Status.replace(/^Up\s+/, '') : '');
    const startedAt = (c as unknown as { StartedAt?: number }).StartedAt;
    const labels = c.Labels || {};
    return {
      id: c.Id,
      name: (c.Names?.[0] || '').replace(/^\//, ''),
      image: c.Image,
      state: c.State as ContainerRow['state'],
      status: c.Status,
      // StartedAt 不总在列表里，退化为按 Status 文本估算；采样器会用 inspect 校正
      uptimeSec: Number.isFinite(started) ? Math.max(0, Math.round((now - started) / 1000)) : 0,
      cpuPct: null,
      memBytes: null,
      createdAt: (c.Created || 0) * 1000,
      project: labels['com.docker.compose.project'],
    };
  });
}

export interface StatSample {
  cpuPct: number;
  memBytes: number;
  memLimit: number;
}

/** 一次性 stats（非流式），并发受控。失败返回 null，不阻断整轮。 */
export async function sampleOne(id: string): Promise<StatSample | null> {
  try {
    const c = docker.getContainer(id);
    const s = (await c.stats({ stream: false })) as Docker.ContainerStats;
    const cpu = s.cpu_stats;
    const pre = s.precpu_stats;
    const cpuDelta = cpu.cpu_usage.total_usage - (pre?.cpu_usage?.total_usage ?? 0);
    const sysDelta = (cpu.system_cpu_usage ?? 0) - (pre?.system_cpu_usage ?? 0);
    const cores = cpu.online_cpus || cpu.cpu_usage.percpu_usage?.length || 1;
    const cpuPct = sysDelta > 0 ? (cpuDelta / sysDelta) * cores * 100 : 0;
    const mem = s.memory_stats;
    const cache = mem.stats?.cache ?? mem.stats?.inactive_file ?? 0;
    return {
      cpuPct: Math.max(0, Math.round(cpuPct * 10) / 10),
      memBytes: Math.max(0, (mem.usage ?? 0) - cache),
      memLimit: mem.limit ?? 0,
    };
  } catch {
    return null;
  }
}

export async function inspectOne(id: string) {
  const info = await docker.getContainer(id).inspect();
  return {
    id: info.Id,
    name: (info.Name || '').replace(/^\//, ''),
    image: info.Config?.Image ?? '',
    imageId: info.Image ?? '',
    state: info.State?.Status ?? 'unknown',
    startedAt: info.State?.StartedAt ?? null,
    restartPolicy: info.HostConfig?.RestartPolicy?.Name || 'no',
    createdAt: info.Created ?? null,
    mounts: (info.Mounts || []).map((m) => ({ type: m.Type, source: m.Source, dest: m.Destination })),
  };
}

export async function renameOne(id: string, name: string) {
  await docker.getContainer(id).rename({ name });
  return true;
}

export async function act(id: string, action: 'start' | 'stop' | 'restart') {
  const c = docker.getContainer(id);
  if (action === 'start') await c.start();
  else if (action === 'stop') await c.stop({ t: 10 });
  else await c.restart({ t: 10 });
  return true;
}
