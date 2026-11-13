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
