import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { UNAUTHORIZED, api } from '../lib/api';
import { useLang } from '../lib/i18n';
import type { StateView } from '../lib/types';
import { PageTransition } from '../motion/PageTransition';
import { Ripple } from '../motion/Ripple';
import { Empty, Segmented } from '../ui/controls';
import { Icon, type IconName } from '../ui/Icon';
import { platformName } from '../lib/format';
import { QuotaPage } from '../pages/QuotaPage';
import { CredentialsPage } from '../pages/CredentialsPage';
import { SyncPage } from '../pages/SyncPage';
import { SettingsPage } from '../pages/SettingsPage';

export type Route = 'quota' | 'credentials' | 'sync' | 'settings';

const ROUTES: { id: Route; icon: IconName; zh: string; en: string }[] = [
  { id: 'quota', icon: 'gauge', zh: '额度', en: 'Quota' },
  { id: 'credentials', icon: 'key', zh: '凭据', en: 'Credentials' },
  { id: 'sync', icon: 'sync', zh: '同步', en: 'Sync' },
  { id: 'settings', icon: 'sliders', zh: '共享设置', en: 'Shared settings' },
];

function routeOf(hash: string): Route {
  const id = hash.replace(/^#\/?/, '').split(/[/?]/)[0];
  return (ROUTES.find((r) => r.id === id)?.id ?? 'quota') as Route;
}

// A page with unsaved edits registers a guard; leaving the page asks it first.
type Guard = () => Promise<boolean>;

interface AppContext {
  state: StateView | undefined;
  refreshState: () => void;
  navigate: (r: Route) => void;
  setGuard: (g: Guard | null) => void;
}

const Ctx = createContext<AppContext | null>(null);

export function useApp(): AppContext {
  const c = useContext(Ctx);
  if (!c) throw new Error('useApp outside App');
  return c;
}

type ThemeChoice = 'system' | 'light' | 'dark';
const THEME_KEY = 'muster.theme';

function useTheme(): [ThemeChoice, (t: ThemeChoice) => void] {
  const [choice, setChoice] = useState<ThemeChoice>(() => (localStorage.getItem(THEME_KEY) as ThemeChoice) || 'system');
  useLayoutEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)');
    const apply = () => {
      const dark = choice === 'dark' || (choice === 'system' && media.matches);
      document.documentElement.dataset.theme = dark ? 'dark' : 'light';
    };
    apply();
    media.addEventListener('change', apply);
    return () => media.removeEventListener('change', apply);
  }, [choice]);
  const set = useCallback((t: ThemeChoice) => {
    const root = document.documentElement;
    root.classList.add('theme-switching');
    setTimeout(() => root.classList.remove('theme-switching'), 400);
    localStorage.setItem(THEME_KEY, t);
    setChoice(t);
  }, []);
  return [choice, set];
}

export function App() {
  const { L, lang, setLang } = useLang();
  const [route, setRoute] = useState<Route>(() => routeOf(location.hash));
  const [forward, setForward] = useState(true);
  const [state, setState] = useState<StateView>();
  const [ended, setEnded] = useState(false);
  const [theme, setTheme] = useTheme();
  const guard = useRef<Guard | null>(null);
  const routeRef = useRef(route);
  routeRef.current = route;

  const refreshState = useCallback(() => {
    api.state().then(setState, () => {});
  }, []);
  useEffect(refreshState, [refreshState]);

  useEffect(() => {
    const end = () => setEnded(true);
    window.addEventListener(UNAUTHORIZED, end);
    return () => window.removeEventListener(UNAUTHORIZED, end);
  }, []);

  const go = useCallback((to: Route) => {
    const from = routeRef.current;
    if (to === from) return;
    setForward(ROUTES.findIndex((r) => r.id === to) > ROUTES.findIndex((r) => r.id === from));
    setRoute(to);
    if (routeOf(location.hash) !== to) history.pushState(null, '', '#/' + to);
  }, []);

  const navigate = useCallback(
    async (to: Route) => {
      if (to === routeRef.current) return;
      if (guard.current && !(await guard.current())) return;
      guard.current = null;
      go(to);
    },
    [go],
  );

  useEffect(() => {
    const pop = async () => {
      const to = routeOf(location.hash);
      if (to === routeRef.current) return;
      if (guard.current) {
        history.pushState(null, '', '#/' + routeRef.current);
        if (!(await guard.current())) return;
        guard.current = null;
      }
      go(to);
    };
    window.addEventListener('popstate', pop);
    return () => window.removeEventListener('popstate', pop);
  }, [go]);

  const setGuard = useCallback((g: Guard | null) => {
    guard.current = g;
  }, []);

  if (ended) {
    return (
      <div className="ended">
        <Empty icon="lock" title={L('这个页面的会话已结束', "This page's session has ended")}>
          {L('在终端里重新运行 ', 'Run ')}
          <code>muster web</code>
          {L('，用它打开的新地址继续。', ' again in a terminal and continue at the address it opens.')}
        </Empty>
      </div>
    );
  }

  const pages: Record<Route, ReactNode> = {
    quota: <QuotaPage />,
    credentials: <CredentialsPage />,
    sync: <SyncPage />,
    settings: <SettingsPage />,
  };

  return (
    <Ctx.Provider value={{ state, refreshState, navigate, setGuard }}>
      <div className="shell">
        <aside className="sidebar">
          <div className="brand">
            <span className="brand-mark" aria-hidden>
              <svg viewBox="0 0 32 32" width="30" height="30">
                <rect width="32" height="32" rx="10" fill="var(--accent)" />
                <path d="M9 22V10l7 7 7-7v12" fill="none" stroke="#fff" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            </span>
            <span className="brand-name">Muster</span>
          </div>
          <Nav route={route} onNavigate={navigate} L={L} />
          <div className="sidebar-foot">
            <Segmented
              size="sm"
              value={theme}
              onChange={setTheme}
              options={[
                { value: 'system', label: <Icon name="monitor" size={15} aria-label={L('跟随系统', 'System')} /> },
                { value: 'light', label: <Icon name="sun" size={15} aria-label={L('浅色', 'Light')} /> },
                { value: 'dark', label: <Icon name="moon" size={15} aria-label={L('深色', 'Dark')} /> },
              ]}
            />
            <Segmented
              size="sm"
              value={lang}
              onChange={setLang}
              options={[
                { value: 'zh', label: '中' },
                { value: 'en', label: 'EN' },
              ]}
            />
            {state && (
              <div className="machine">
                <Icon name="pc" size={14} />
                <span>
                  {platformName(state.platform)} · {state.muster}
                </span>
              </div>
            )}
          </div>
        </aside>
        <main className="main">
          <PageTransition pageKey={route} forward={forward}>
            {pages[route]}
          </PageTransition>
        </main>
      </div>
    </Ctx.Provider>
  );
}

/** Nav: the active pill slides to the chosen page. */
function Nav({ route, onNavigate, L }: { route: Route; onNavigate: (r: Route) => void; L: (zh: string, en: string) => string }) {
  const refs = useRef<Record<string, HTMLButtonElement | null>>({});
  const [pill, setPill] = useState<{ top: number; height: number } | null>(null);
  useLayoutEffect(() => {
    const el = refs.current[route];
    if (el) setPill({ top: el.offsetTop, height: el.offsetHeight });
  }, [route]);
  return (
    <nav className="nav">
      {pill && <span className="nav-pill" style={{ transform: `translateY(${pill.top}px)`, height: pill.height }} />}
      {ROUTES.map((r) => (
        <button
          key={r.id}
          ref={(el) => {
            refs.current[r.id] = el;
          }}
          type="button"
          className={'nav-item' + (r.id === route ? ' active' : '')}
          aria-current={r.id === route ? 'page' : undefined}
          onClick={() => onNavigate(r.id)}
        >
          <Ripple />
          <Icon name={r.icon} size={19} />
          <span>{L(r.zh, r.en)}</span>
        </button>
      ))}
    </nav>
  );
}

/** PageHeader is the top of every page: title, a line of context, actions. */
export function PageHeader({ title, sub, actions }: { title: string; sub?: ReactNode; actions?: ReactNode }) {
  return (
    <header className="page-head">
      <div className="page-head-text">
        <h1>{title}</h1>
        {sub && <div className="page-sub">{sub}</div>}
      </div>
      {actions && <div className="page-actions">{actions}</div>}
    </header>
  );
}

/** Section groups a page's content under a small title. */
export function Section({ title, aside, children }: { title?: ReactNode; aside?: ReactNode; children: ReactNode }) {
  return (
    <section className="section">
      {(title || aside) && (
        <div className="section-head">
          {title && <h2>{title}</h2>}
          {aside && <div className="section-aside">{aside}</div>}
        </div>
      )}
      {children}
    </section>
  );
}
