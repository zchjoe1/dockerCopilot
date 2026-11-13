import { docker } from './docker.ts';

export interface ImageRow {
  id: string;
  tags: string[];
  size: number;
  created: number;
  containers: number;
  dangling: boolean;
}

/** 镜像列表；dangling（无 tag）也返回，界面上单独标出。 */
export async function listImages(): Promise<ImageRow[]> {
  const raw = (await docker.listImages({ all: false })) as Array<{
    Id: string;
    RepoTags: string[] | null;
    Size: number;
    Created: number;
    Containers?: number;
  }>;
  return raw
    .map((img) => ({
      id: img.Id,
      tags: (img.RepoTags ?? []).filter((t) => t !== '<none>:<none>'),
      size: img.Size,
      created: img.Created * 1000,
      containers: img.Containers ?? 0,
      dangling: !(img.RepoTags ?? []).some((t) => t !== '<none>:<none>'),
    }))
    .sort((a, b) => b.size - a.size);
}

export async function removeImage(id: string, force = false): Promise<void> {
  await docker.getImage(id).remove({ force });
}

export async function tagImage(source: string, repo: string, tag: string): Promise<void> {
  await docker.getImage(source).tag({ repo, tag });
}
