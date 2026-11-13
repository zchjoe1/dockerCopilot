import type { ContainerRow } from './docker.ts';

interface Seed {
  name: string;
  image: string;
  days: number;
  cpu: number;
  memMB: number;
  update?: boolean;
}

const seeds: Seed[] = [
  { name: 'dsh-harness', image: 'ghcr.io/smanx/deepseek-harness:latest', days: 3.16, cpu: 25, memMB: 2150, update: true },
  { name: 'Hermes', image: 'nousresearch/hermes-agent:latest', days: 3.08, cpu: 3.5, memMB: 1740 },
  { name: 'embyplus', image: 'zeyu8023/embypulse-plus:latest', days: 3.04, cpu: 0.9, memMB: 627 },
  { name: 'homeassistant', image: 'homeassistant/home-assistant:stable', days: 3.0, cpu: 0.1, memMB: 625 },
  { name: 'symedia', image: 'shenxianmq/symedia:latest', days: 1.54, cpu: 0.4, memMB: 562 },
  { name: 'obsidian', image: 'linuxserver/obsidian:latest', days: 3.0, cpu: 1.9, memMB: 423 },
  { name: 'nc-bridge', image: 'zchjoe/nc-bridge:latest', days: 0.22, cpu: 0.3, memMB: 352 },
  { name: 'lucky', image: 'gdy666/lucky:v3', days: 0.56, cpu: 0.5, memMB: 331 },
  { name: 'danmu', image: 'logvar/danmu-api:latest', days: 3.0, cpu: 0.0, memMB: 55.6 },
  { name: 'new-api', image: 'calciumion/new-api:latest', days: 0.54, cpu: 0.0, memMB: 54.5 },
  { name: 'dockercopilot', image: 'zchjoe/dockercopilot:latest', days: 3.0, cpu: 0.9, memMB: 49.8 },
  { name: 'procurement-center', image: 'zchjoe/procurement-center:latest', days: 0.03, cpu: 0.0, memMB: 60.3 },
  { name: 'redia', image: 'shenxianmq/redia:latest', days: 3.0, cpu: 0.2, memMB: 281 },
  { name: 'tailscale', image: 'tailscale/tailscale:stable', days: 3.0, cpu: 0.0, memMB: 38.2 },
  { name: 'memos', image: 'neosmemo/memos:stable', days: 3.0, cpu: 0.1, memMB: 41.7 },
  { name: 'codebuddy2api-new', image: 'ghcr.io/maiphucg/langchain-api:latest', days: 3.0, cpu: 0.0, memMB: 33.4 },
];

const pristine = structuredClone(seeds);

const state = new Map<string, ContainerRow['state']>();

export function resetMock() {
  seeds.length = 0;
  seeds.push(...structuredClone(pristine));
  state.clear();
}

export function mockList(): ContainerRow[] {
  return seeds.map((s, i) => {
    const id = `mock${i}`;
    const st = state.get(id) ?? 'running';
    return {
      id,
      name: s.name,
      image: s.image,
      state: st,
      status: st === 'running' ? 'Up' : 'Exited (0)',
      uptimeSec: Math.round(s.days * 86400),
      cpuPct: st === 'running' ? s.cpu : 0,
      memBytes: st === 'running' ? Math.round(s.memMB * 1024 * 1024) : 0,
      createdAt: Date.now() - Math.round(s.days * 86400000),
      project: s.name === 'dsh-harness' ? 'deepseek-harness' : undefined,
      hasUpdate: s.update === true,
    };
  });
}

export function mockAct(id: string, action: 'start' | 'stop' | 'restart') {
  const cur = state.get(id) ?? 'running';
  if (action === 'stop') state.set(id, 'exited');
  else state.set(id, 'running');
  return cur;
}

export function mockRename(id: string, name: string) {
  const row = mockList().find((r) => r.id === id);
  if (!row) return false;
  seeds[Number(id.replace('mock', ''))].name = name;
  return true;
}

export function mockInspect(id: string) {
  const row = mockList().find((r) => r.id === id);
  if (!row) return null;
  return {
    id: row.id,
    name: row.name,
    image: row.image,
    imageId: 'sha256:32a9c8060060aaa810335b2f866aed57670a655b118cded04f5c20032e227460',
    state: row.state,
    startedAt: new Date(Date.now() - row.uptimeSec * 1000).toISOString(),
    restartPolicy: 'unless-stopped',
    createdAt: row.createdAt,
    mounts: [],
  };
}

export function mockImages() {
  const seen = new Map<string, { id: string; tags: string[]; size: number; created: number; containers: number; dangling: boolean }>();
  for (const row of mockList()) {
    const name = row.image;
    const tag = name;
    const key = name;
    if (!seen.has(key)) {
      seen.set(key, {
        id: 'sha256:' + Math.abs(hash(name)).toString(16).padStart(12, '0').slice(0, 12),
        tags: [tag],
        size: 40_000_000 + (Math.abs(hash(name)) % 900_000_000),
        created: Date.now() - 86_400_000 * (3 + (Math.abs(hash(name)) % 30)),
        containers: 1,
        dangling: false,
      });
    }
  }
  return [...seen.values()].sort((a, b) => b.size - a.size);
}

function hash(s: string) {
  let h = 0;
  for (let i = 0; i < s.length; i += 1) h = (h * 31 + s.charCodeAt(i)) | 0;
  return h;
}

// ── 备份与 compose 的 mock ─────────────────────
export const mockBackups: Array<{ file: string; name: string; image: string; createdAt: number; size: number }> = [];

export function mockCreateBackup(containerId: string) {
  const row = mockList().find((r) => r.id === containerId);
  if (!row) throw new Error('容器不存在');
  const now = Date.now();
  const item = {
    file: `${row?.name ?? containerId}-${new Date(now).toISOString().replace(/[:.]/g, '-')}.json`,
    name: row?.name ?? containerId,
    image: row?.image ?? '',
    createdAt: now,
    size: 2048 + Math.floor(Math.random() * 4096),
  };
  mockBackups.unshift(item);
  return item;
}

export function mockComposeProjects() {
  const d = '/vol1/1000/docker/deepseek-harness';
  return [
    { name: 'deepseek-harness', dir: d, file: d + '/compose.yaml', modified: Date.now() - 3600_000 },
    { name: 'dockercopilot', dir: '/vol1/1000/docker/dockercopilot', file: '/vol1/1000/docker/dockercopilot/compose.yaml', modified: Date.now() - 86400_000 },
  ];
}

export const MOCK_COMPOSE = `services:
  dsh-harness:
    image: ghcr.io/smanx/deepseek-harness:latest
    container_name: dsh-harness
    restart: unless-stopped
    network_mode: host
    volumes:
      - ./data:/root/.dsh
    environment:
      TZ: Asia/Shanghai
`;
