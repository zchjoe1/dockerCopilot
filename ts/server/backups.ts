/** 容器备份：把 inspect 的关键部分存成 JSON，可原样重建。
 *  与 Go 版差异：Go 版导出 compose。（JSON 更简单且能完整还原，见 README。）
 */
import fs from 'node:fs';
import path from 'node:path';
import { docker } from './docker.ts';

export interface BackupMeta {
  file: string;
  name: string;
  image: string;
  createdAt: number;
  size: number;
}

export interface BackupFile {
  version: 1;
  createdAt: number;
  name: string;
  config: Record<string, unknown>;
  hostConfig: Record<string, unknown>;
  networkingConfig?: { EndpointsConfig: Record<string, unknown> };
}

function dir(): string {
  const d = path.join(process.env.DATA_DIR || path.resolve('./data'), 'backups');
  fs.mkdirSync(d, { recursive: true });
  return d;
}

export function listBackups(): BackupMeta[] {
  const d = dir();
  return fs
    .readdirSync(d)
    .filter((f) => f.endsWith('.json'))
    .map((file) => {
      const full = path.join(d, file);
      const stat = fs.statSync(full);
      let image = '';
      let name = file.replace(/\.json$/, '');
      try {
        const parsed = JSON.parse(fs.readFileSync(full, 'utf8')) as BackupFile;
        image = String((parsed.config as { Image?: string }).Image ?? '');
        name = parsed.name || name;
      } catch {
        /* 坏文件也列出来，交给界面删 */
      }
      return { file, name, image, createdAt: stat.mtimeMs, size: stat.size };
    })
    .sort((a, b) => b.createdAt - a.createdAt);
}

export async function createBackup(containerId: string): Promise<BackupMeta> {
  const info = await docker.getContainer(containerId).inspect();
  const name = (info.Name || '').replace(/^\//, '') || containerId.slice(0, 12);
  const payload: BackupFile = {
    version: 1,
    createdAt: Date.now(),
    name,
    config: info.Config as unknown as Record<string, unknown>,
    hostConfig: info.HostConfig as unknown as Record<string, unknown>,
    ...(info.NetworkSettings?.Networks && Object.keys(info.NetworkSettings.Networks).length
      ? { networkingConfig: { EndpointsConfig: info.NetworkSettings.Networks } }
      : {}),
  };
  const file = `${name}-${new Date(payload.createdAt).toISOString().replace(/[:.]/g, '-')}.json`;
  const full = path.join(dir(), file);
  fs.writeFileSync(full, `${JSON.stringify(payload, null, 2)}\n`);
  const size = fs.statSync(full).size;
  return { file, name, image: String(info.Config?.Image ?? ''), createdAt: payload.createdAt, size };
}

export async function restoreBackup(file: string): Promise<{ id: string; name: string }> {
  const full = path.join(dir(), path.basename(file));
  const payload = JSON.parse(fs.readFileSync(full, 'utf8')) as BackupFile;
  if (payload.version !== 1) throw new Error('备份文件版本不支持');
  const name = `${payload.name || 'restored'}-restored-${Date.now().toString(36).slice(-4)}`;
  const container = await docker.createContainer({
    name,
    ...(payload.config as object),
    HostConfig: payload.hostConfig,
    ...(payload.networkingConfig ? { NetworkingConfig: payload.networkingConfig } : {}),
  } as never);
  await container.start();
  return { id: container.id, name };
}

export function removeBackup(file: string): void {
  fs.rmSync(path.join(dir(), path.basename(file)), { force: true });
}
