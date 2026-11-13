import { useEffect, useState, type ReactNode } from 'react';
import Containers from './pages/Containers';
import Images from './pages/Images';
import Settings from './pages/Settings';

type Page = 'containers' | 'images' | 'settings';

const PAGES: Array<{ key: Page; label: string; icon: ReactNode }> = [
  {
    key: 'containers',
    label: '容器',
    icon: (
      <svg viewBox="0 0 24 24">
        <rect x="3.5" y="3.5" width="7" height="7" rx="1" />
        <rect x="13.5" y="3.5" width="7" height="7" rx="1" />
        <rect x="3.5" y="13.5" width="7" height="7" rx="1" />
        <rect x="13.5" y="13.5" width="7" height="7" rx="1" />
      </svg>
    ),
  },
  {
    key: 'images',
    label: '镜像',
    icon: (
      <svg viewBox="0 0 24 24">
        <path d="M12 2.8 21 7.6v8.8L12 21.2 3 16.4V7.6z" />
        <path d="M3 7.6 12 12.4l9-4.8M12 12.4v8.8" />
      </svg>
    ),
  },
  {
    key: 'settings',
    label: '设置',
    icon: (
      <svg viewBox="0 0 24 24">
        <path d="M4 7h9M16.5 7H20M4 17h3.5M11 17h9" />
        <circle cx="14.5" cy="7" r="2" />
        <circle cx="9" cy="17" r="2" />
      </svg>
    ),
  },
];

function readHash(): Page {
  const raw = window.location.hash.replace(/^#\/?/, '');
  return (PAGES.find((p) => p.key === raw)?.key ?? 'containers') as Page;
}

export default function App() {
  const [page, setPage] = useState<Page>(readHash());

  useEffect(() => {
    const onHash = () => setPage(readHash());
    window.addEventListener('hashchange', onHash);
    return () => window.removeEventListener('hashchange', onHash);
  }, []);

  return (
    <div className="app">
      <nav className="rail">
        <div className="logo">
          <svg viewBox="0 0 24 24">
            <path d="M12 2.8 21 7.6v8.8L12 21.2 3 16.4V7.6z" />
            <path d="M3 7.6 12 12.4l9-4.8M12 12.4v8.8" />
          </svg>
        </div>
        {PAGES.map((p) => (
          <a
            key={p.key}
            href={`#/${p.key}`}
            title={p.label}
            aria-current={page === p.key ? 'page' : undefined}
          >
            {p.icon}
          </a>
        ))}
        <div className="sp" />
      </nav>

      {page === 'containers' && <Containers />}
      {page === 'images' && <Images />}
      {page === 'settings' && <Settings />}
    </div>
  );
}
