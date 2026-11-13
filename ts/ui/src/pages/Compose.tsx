import { useCallback, useEffect, useState } from 'react';

type Project = { name: string; dir: string; file: string; modified: number };

export default function Compose() {
  const [projects, setProjects] = useState<Project[]>([]);
  const [active, setActive] = useState<Project | null>(null);
  const [content, setContent] = useState('');
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [output, setOutput] = useState<string>('');
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/compose');
      const data = (await res.json()) as { items?: Project[]; error?: string };
      if (data.error) throw new Error(data.error);
      setProjects(data.items ?? []);
      setError(null);
      setActive((prev) => prev ?? data.items?.[0] ?? null);
    } catch (err) {
      setError((err as Error).message);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    if (!active) return;
    setBusy('load');
    fetch(`/api/compose/file?file=${encodeURIComponent(active.file)}`)
      .then((r) => r.json())
      .then((d: { content?: string; error?: string }) => {
        if (d.error) throw new Error(d.error);
        setContent(d.content ?? '');
        setDirty(false);
      })
      .catch((err) => setOutput(`读取失败：${(err as Error).message}`))
      .finally(() => setBusy(null));
  }, [active]);

  const save = async () => {
    if (!active) return;
    setBusy('save');
    try {
      const res = await fetch('/api/compose/file', {
        method: 'PUT',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ file: active.file, content }),
      });
      const d = (await res.json()) as { ok?: boolean; error?: string };
      if (!res.ok || d.error) throw new Error(d.error || `HTTP ${res.status}`);
      setDirty(false);
      setOutput('已保存（原文件已留 .bak）');
    } catch (err) {
      setOutput(`保存失败：${(err as Error).message}`);
    } finally {
      setBusy(null);
    }
  };

  const apply = async () => {
    if (!active) return;
    if (!window.confirm(`对 ${active.name} 执行 docker compose up -d？`)) return;
    setBusy('apply');
    try {
      const res = await fetch('/api/compose/apply', {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ file: active.file }),
      });
      const d = (await res.json()) as { ok?: boolean; output?: string; error?: string; code?: number };
      setOutput(d.output || d.error || (d.ok ? '已生效' : `失败（code ${d.code}）`));
    } catch (err) {
      setOutput(`执行失败：${(err as Error).message}`);
    } finally {
      setBusy(null);
    }
  };

  return (
    <main className="main">
      <div className="top">
        <div className="crumb">docker <span>/</span> fnos <span>/</span> <b>Compose</b></div>
      </div>

      <div className="h">
        <h1>Compose</h1>
        <p>{projects.length} 个项目 · 改完先保存，再一键生效</p>
      </div>

      <div className="compose">
        <aside className="clist">
          {error && <div className="err" style={{ margin: 10 }}>{error}</div>}
          {projects.map((p) => (
            <button
              key={p.file}
              className={`citem${active?.file === p.file ? ' on' : ''}`}
              onClick={() => setActive(p)}
            >
              <b>{p.name}</b>
              <span>{p.file.replace(/^.*\/docker\//, '')}</span>
            </button>
          ))}
          {projects.length === 0 && <div className="empty">没有找到 compose 项目</div>}
        </aside>

        <section className="ceditor">
          {active ? (
            <>
              <div className="cbar">
                <span className="cfile">{active.file}</span>
                {dirty && <span className="dirty">未保存</span>}
                <span className="sp" />
                <button className="mini" onClick={save} disabled={busy !== null || !dirty}>保存</button>
                <button className="mini go" onClick={apply} disabled={busy !== null}>一键生效</button>
              </div>
              <textarea
                className="ctext"
                value={content}
                spellCheck={false}
                onChange={(e) => { setContent(e.target.value); setDirty(true); }}
                disabled={busy === 'load'}
              />
              {output && <pre className="cout">{output}</pre>}
            </>
          ) : (
            <div className="empty">选一个项目</div>
          )}
        </section>
      </div>
    </main>
  );
}
