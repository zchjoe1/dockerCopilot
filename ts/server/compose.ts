/** fnOS 上的 compose 项目：扫描、读取、保存、一键生效。 */
import fs from 'node:fs';
import path from 'node:path';
import { spawn } from 'node:child_process';

const NAMES = ['compose.yaml', 'compose.yml', 'docker-compose.yaml', 'docker-compose.yml'];

export function composeRoot(): string {
  return process.env.COMPOSE_ROOT || '/vol1/1000/docker';
}

export interface ComposeProject {
  name: string;
  dir: string;
  file: string;
  modified: number;
}

/** 扫描深度 2 的项目（root 本身 + 一层子目录）。 */
export function listProjects(): ComposeProject[] {
  const root = composeRoot();
  const out: ComposeProject[] = [];
  const scan = (dir: string, depth: number) => {
    let entries: fs.Dirent[];
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch {
      return;
    }
    const file = NAMES.map((n) => path.join(dir, n)).find((p) => fs.existsSync(p));
    if (file) {
      const stat = fs.statSync(file);
      out.push({ name: path.basename(dir), dir, file, modified: stat.mtimeMs });
      return; // 一个目录只算一个项目
    }
    if (depth <= 0) return;
    for (const e of entries) {
      if (e.isDirectory() && !e.name.startsWith('.')) scan(path.join(dir, e.name), depth - 1);
    }
  };
  scan(root, 1);
  return out.sort((a, b) => b.modified - a.modified);
}

function assertInside(file: string) {
  const root = path.resolve(composeRoot());
  const full = path.resolve(file);
  if (full !== root && !full.startsWith(`${root}${path.sep}`)) throw new Error('路径不在 compose 根目录内');
  return full;
}

export function readProject(file: string): string {
  return fs.readFileSync(assertInside(file), 'utf8');
}

export function saveProject(file: string, content: string): void {
  const full = assertInside(file);
  if (content.length > 512 * 1024) throw new Error('文件过大（>512KB）');
  if (fs.existsSync(full)) fs.writeFileSync(`${full}.bak`, fs.readFileSync(full));
  fs.writeFileSync(full, content);
}

export function applyProject(file: string): Promise<{ code: number; output: string }> {
  const full = assertInside(file);
  return new Promise((resolve) => {
    const child = spawn('docker', ['compose', '-f', full, 'up', '-d', '--remove-orphans'], {
      cwd: path.dirname(full),
      env: process.env,
    });
    let output = '';
    child.stdout.on('data', (d) => (output += String(d)));
    child.stderr.on('data', (d) => (output += String(d)));
    child.on('error', (err) => resolve({ code: -1, output: `无法执行 docker compose：${err.message}` }));
    child.on('close', (code) => resolve({ code: code ?? -1, output: output.slice(-4000) }));
  });
}

/** 把容器 inspect 转成 compose 片段（对应旧版 backup2compose）。 */
export function containerToCompose(info: {
  Name?: string;
  Config?: Record<string, unknown>;
  HostConfig?: Record<string, unknown>;
  Mounts?: Array<{ Type: string; Source?: string; Name?: string; Destination: string; RW: boolean }>;
  NetworkSettings?: { Networks?: Record<string, unknown>; Ports?: Record<string, Array<{ HostIp?: string; HostPort?: string }> | null> };
}): string {
  const name = (info.Name || 'container').replace(/^\//, '');
  const cfg = (info.Config || {}) as { Image?: string; Env?: string[]; Cmd?: unknown; Entrypoint?: unknown; WorkingDir?: string };
  const host = (info.HostConfig || {}) as {
    RestartPolicy?: { Name?: string };
    NetworkMode?: string;
    PortBindings?: Record<string, Array<{ HostIp?: string; HostPort?: string }>>;
    Binds?: string[];
  };
  const lines: string[] = ['services:', `  ${name}:`, `    image: ${quote(cfg.Image ?? 'unknown')}`];
  if (cfg.Env?.length) {
    lines.push('    environment:');
    for (const e of cfg.Env) lines.push(`      - ${quote(e)}`);
  }
  if (Array.isArray(cfg.Cmd) && cfg.Cmd.length) lines.push(`    command: ${quote(cfg.Cmd.join(' '))}`);
  if (cfg.WorkingDir) lines.push(`    working_dir: ${quote(cfg.WorkingDir)}`);
  if (host.RestartPolicy?.Name) lines.push(`    restart: ${host.RestartPolicy.Name}`);
  if (host.NetworkMode && host.NetworkMode !== 'default') lines.push(`    network_mode: ${host.NetworkMode}`);
  const binds = host.Binds?.length ? host.Binds : (info.Mounts || [])
    .filter((m) => m.Type === 'bind' && m.Source && m.Destination)
    .map((m) => `${m.Source}:${m.Destination}${m.RW ? '' : ':ro'}`);
  if (binds.length) {
    lines.push('    volumes:');
    for (const b of binds) lines.push(`      - ${quote(b)}`);
  }
  const ports = host.PortBindings || {};
  const portLines: string[] = [];
  for (const [containerPort, bindsArr] of Object.entries(ports)) {
    for (const b of bindsArr || []) {
      const hostPort = b.HostPort || '';
      portLines.push(`      - ${quote(`${hostPort}:${containerPort}`)}`);
    }
  }
  if (portLines.length) lines.push('    ports:', ...portLines);
  return `${lines.join('\n')}\n`;
}

function quote(v: string): string {
  if (/^[A-Za-z0-9_./:@-]+$/.test(v)) return v;
  return `"${v.replace(/"/g, '\\"')}"`;
}
