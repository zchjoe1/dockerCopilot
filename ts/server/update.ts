/** 容器更新：拉取新镜像 → 用原配置重建 → 清旧镜像。
 *  与 Go 版行为对齐：保留挂载、网络、端口、环境变量与启动参数。
 */
import { docker } from './docker.ts';

export type UpdateStep = 'preparing' | 'pulling' | 'creating' | 'swapping' | 'cleaning' | 'done' | 'error';

export interface UpdateEvent {
  step: UpdateStep;
  message: string;
  at: number;
}

export interface UpdateJob {
  id: string;
  containerId: string;
  containerName: string;
  image: string;
  status: 'running' | 'done' | 'error';
  events: UpdateEvent[];
  error?: string;
  startedAt: number;
  endedAt?: number;
}

/** 可注入的 docker 能力，便于单测。 */
export interface UpdateDeps {
  inspect(id: string): Promise<DockerInspect>;
  pull(image: string, onLine: (line: string) => void): Promise<void>;
  create(opts: CreateOpts): Promise<{ id: string }>;
  start(id: string): Promise<void>;
  stop(id: string): Promise<void>;
  remove(id: string): Promise<void>;
  rename(id: string, name: string): Promise<void>;
}

type DockerInspect = {
  Id: string;
  Name: string;
  Config: Record<string, unknown> & { Image: string };
  HostConfig: Record<string, unknown>;
  NetworkSettings?: { Networks?: Record<string, unknown> };
  Mounts?: Array<{ Type: string; Name?: string; Source?: string; Destination: string; RW: boolean }>;
};

export type CreateOpts = {
  name: string;
  Config: Record<string, unknown>;
  HostConfig: Record<string, unknown>;
  NetworkingConfig?: { EndpointsConfig: Record<string, unknown> };
};

const jobs = new Map<string, UpdateJob>();

export function getJob(id: string): UpdateJob | undefined {
  return jobs.get(id);
}

export function listJobs(): UpdateJob[] {
  return [...jobs.values()].sort((a, b) => b.startedAt - a.startedAt);
}

export function newJob(containerId: string, containerName: string, image: string): UpdateJob {
  const job: UpdateJob = {
    id: `u${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`,
    containerId,
    containerName,
    image,
    status: 'running',
    events: [],
    startedAt: Date.now(),
  };
  jobs.set(job.id, job);
  // 只留最近 30 个
  if (jobs.size > 30) {
    const oldest = listJobs().slice(30);
    for (const j of oldest) jobs.delete(j.id);
  }
  return job;
}

export async function runUpdate(job: UpdateJob, deps: UpdateDeps = realDeps): Promise<void> {
  const emit = (step: UpdateStep, message: string) => {
    job.events.push({ step, message, at: Date.now() });
  };
  try {
    emit('preparing', '读取容器配置');
    const old = await deps.inspect(job.containerId);
    const name = (old.Name || '').replace(/^\//, '') || job.containerName;
    const image = old.Config?.Image || job.image;

    emit('pulling', `拉取 ${image}`);
    await deps.pull(image, (line) => emit('pulling', line));

    emit('creating', '按原配置创建新容器');
    const tempName = `${name}-dc-new-${Date.now().toString(36).slice(-4)}`;
    const endpoints = old.NetworkSettings?.Networks && Object.keys(old.NetworkSettings.Networks).length
      ? { EndpointsConfig: old.NetworkSettings.Networks }
      : undefined;

    const created = await deps.create({
      name: tempName,
      Config: { ...old.Config, Hostname: undefined },
      HostConfig: old.HostConfig,
      ...(endpoints ? { NetworkingConfig: endpoints } : {}),
    }).catch(async (err) => {
      throw new Error(`创建新容器失败：${(err as Error).message}`);
    });

    try {
      emit('swapping', '启动新容器');
      await deps.start(created.id);
      emit('swapping', '停止并移除旧容器');
      await deps.stop(job.containerId).catch(() => {});
      await deps.remove(job.containerId);
      await deps.rename(created.id, name);
    } catch (err) {
      await deps.remove(created.id).catch(() => {});
      throw new Error(`切换容器失败，旧容器保持不变：${(err as Error).message}`);
    }

    emit('cleaning', '旧镜像由下轮检测清理');
    emit('done', `更新完成：${name}`);
    job.status = 'done';
  } catch (err) {
    job.status = 'error';
    job.error = (err as Error).message;
    job.events.push({ step: 'error', message: job.error, at: Date.now() });
  } finally {
    job.endedAt = Date.now();
  }
}

/** 真实 docker 实现 */
export const realDeps: UpdateDeps = {
  inspect: (id) => docker.getContainer(id).inspect() as unknown as Promise<DockerInspect>,
  pull: async (image, onLine) => {
    await new Promise<void>((resolve, reject) => {
      docker.pull(image, (err: Error | null, stream: NodeJS.ReadableStream) => {
        if (err) return reject(err);
        docker.modem.followProgress(
          stream,
          (doneErr: Error | null) => (doneErr ? reject(doneErr) : resolve()),
          (evt: { status?: string; id?: string }) => {
            if (evt.status) onLine(`${evt.status}${evt.id ? ` ${evt.id}` : ''}`);
          },
        );
      });
    });
  },
  create: async (opts) => {
    const c = await docker.createContainer({
      name: opts.name,
      ...(opts.Config as object),
      HostConfig: opts.HostConfig,
      ...(opts.NetworkingConfig ? { NetworkingConfig: opts.NetworkingConfig } : {}),
    } as never);
    return { id: c.id };
  },
  start: (id) => docker.getContainer(id).start(),
  stop: (id) => docker.getContainer(id).stop({ t: 10 }),
  remove: (id) => docker.getContainer(id).remove({ force: true }),
  rename: (id, name) => docker.getContainer(id).rename({ name }),
};
