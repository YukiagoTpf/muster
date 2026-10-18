import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { PageHeader, useApp } from '../app/App';
import { ApiError, api } from '../lib/api';
import { platformName } from '../lib/format';
import { useBusy, usePresence } from '../lib/hooks';
import { useLang } from '../lib/i18n';
import type { SettingsView } from '../lib/types';
import { Badge, Button, Notice, Segmented, Skeleton, TextInput } from '../ui/controls';
import { Icon, type IconName } from '../ui/Icon';
import { Dialog, useConfirm, useToast } from '../ui/overlays';
import { LIST_FIELDS, SET_FIELDS, type GroupId } from '../settings/catalog';
import { EditorCtx, EnvEditor, HooksEditor, ListFieldRow, MarketplacesEditor, PluginsEditor, SetFieldRow } from '../settings/editors';
import { JsonEditor, parseError } from '../settings/JsonEditor';
import { has, isObj, isProviderKey, stringify, type JsonObject } from '../settings/projection';

interface SectionDef {
  id: string;
  zh: string;
  en: string;
  icon: IconName;
  groups?: GroupId[];
  extra?: 'hooks' | 'plugins' | 'env';
  hintZh?: string;
  hintEn?: string;
}

const SECTIONS: SectionDef[] = [
  { id: 'session', zh: '会话', en: 'Session', icon: 'bolt', groups: ['session'] },
  { id: 'ui', zh: '界面', en: 'Interface', icon: 'monitor', groups: ['ui'] },
  { id: 'cap', zh: '能力', en: 'Capabilities', icon: 'sliders', groups: ['cap'] },
  {
    id: 'perm',
    zh: '权限',
    en: 'Permissions',
    icon: 'shield',
    groups: ['perm', 'permlist'],
    hintZh: '规则写成 Tool 或 Tool(说明)，比如 Read、Bash(git status*)、WebFetch(domain:example.com)。拒绝优先于询问，询问优先于允许。',
    hintEn: 'A rule is Tool or Tool(specifier) — Read, Bash(git status*), WebFetch(domain:example.com). Deny wins over ask, ask over allow.',
  },
  { id: 'hooks', zh: 'Hooks', en: 'Hooks', icon: 'code', groups: ['hookopts'], extra: 'hooks' },
  { id: 'plugins', zh: '插件', en: 'Plugins', icon: 'plug', extra: 'plugins' },
  { id: 'mcp', zh: 'MCP 与技能', en: 'MCP & skills', icon: 'box', groups: ['mcp', 'mcplist'] },
  { id: 'env', zh: '环境变量', en: 'Environment', icon: 'terminal', extra: 'env' },
  { id: 'sandbox', zh: '沙箱', en: 'Sandbox', icon: 'lock', groups: ['sandbox'] },
  { id: 'notify', zh: '通知', en: 'Notifications', icon: 'info', groups: ['notify'] },
  { id: 'priv', zh: '更新与隐私', en: 'Updates & privacy', icon: 'eyeOff', groups: ['priv'] },
  { id: 'git', zh: 'Git 署名', en: 'Git attribution', icon: 'git', groups: ['git'] },
  { id: 'limits', zh: '超时与限额', en: 'Timeouts & limits', icon: 'clock', groups: ['limits', 'modellist'] },
  { id: 'auth', zh: '登录与脚本', en: 'Login & scripts', icon: 'key', groups: ['auth'] },
];

const EXTRA_PATHS: Record<string, string[][]> = {
  hooks: [['hooks']],
  plugins: [['enabledPlugins'], ['extraKnownMarketplaces']],
  env: [['env']],
};

function parse(text: string): JsonObject {
  const v = JSON.parse(text);
  return isObj(v) ? v : {};
}

export function SettingsPage() {
  const { L } = useLang();
  const toast = useToast();
  const { setGuard, state } = useApp();
  const [platform, setPlatform] = useState<string>('');
  const [view, setView] = useState<SettingsView | null>(null);
  const [loadError, setLoadError] = useState('');
  const [original, setOriginal] = useState<JsonObject>({});
  const [doc, setDoc] = useState<JsonObject>({});
  const [mode, setMode] = useState<'form' | 'json'>('form');
  const [text, setText] = useState('');
  const [query, setQuery] = useState('');
  const [saving, runSave] = useBusy();
  const [conflict, setConflict] = useState(false);
  const [confirmNode, confirm] = useConfirm();

  const load = useCallback(
    async (p?: string) => {
      setLoadError('');
      try {
        const v = await api.settings(p);
        const d = parse(v.doc.content);
        setView(v);
        setPlatform(v.doc.platform);
        setOriginal(d);
        setDoc(d);
        setText(stringify(d));
      } catch (e) {
        setLoadError((e as Error).message);
      }
    },
    [],
  );
  useEffect(() => {
    load();
  }, [load]);

  const baseline = useMemo(() => stringify(original), [original]);
  const jsonError = mode === 'json' ? parseError(text, L) : '';
  const current = mode === 'form' ? stringify(doc) : jsonError ? null : stringify(parse(text));
  const dirty = current !== baseline;

  // Leaving with unsaved edits asks first.
  const askDiscard = useCallback(
    () =>
      confirm({
        title: L('放弃未保存的修改？', 'Discard unsaved changes?'),
        body: L('这一页的修改还没有保存。', 'The changes on this page are not saved.'),
        confirm: L('放弃修改', 'Discard'),
        danger: true,
      }),
    [confirm, L],
  );
  useEffect(() => {
    setGuard(dirty ? askDiscard : null);
    const unload = (e: BeforeUnloadEvent) => {
      if (dirty) e.preventDefault();
    };
    window.addEventListener('beforeunload', unload);
    return () => {
      window.removeEventListener('beforeunload', unload);
      setGuard(null);
    };
  }, [dirty, askDiscard, setGuard]);

  const switchPlatform = async (p: string) => {
    if (p === platform) return;
    if (dirty && !(await askDiscard())) return;
    setView(null);
    await load(p);
  };

  const switchMode = (m: 'form' | 'json') => {
    if (m === mode) return;
    if (m === 'json') setText(stringify(doc));
    else {
      const err = parseError(text, L);
      if (err) {
        toast('error', L('先把 JSON 改成合法的，再切回表单', 'Fix the JSON before going back to the form') + ' — ' + err);
        return;
      }
      setDoc(parse(text));
    }
    setMode(m);
  };

  const content = () => (mode === 'form' ? stringify(doc) : text);

  const save = async (overwrite = false) => {
    if (!view) return;
    const c = content();
    const env = parse(c).env;
    const provider = isObj(env) ? Object.keys(env).filter(isProviderKey) : [];
    if (provider.length) {
      toast('error', L(`${provider.join(', ')} 属于供应商，请在凭据里设置`, `${provider.join(', ')} belongs to the provider: set it on a credential`));
      return;
    }
    let hash = view.doc.hash;
    try {
      if (overwrite) hash = (await api.settings(platform)).doc.hash;
      const res = await runSave(api.saveSettings(platform, c, hash));
      const d = parse(res.doc.content);
      setView({ ...view, doc: res.doc });
      setOriginal(d);
      setDoc(d);
      setText(stringify(d));
      setConflict(false);
      toast('ok', view.doc.local ? L('已保存到 settings.json', 'Saved to settings.json') : L('已保存，下次同步时发出', 'Saved — the next sync sends it'));
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) setConflict(true);
      else toast('error', (e as Error).message);
    }
  };

  // Ctrl/Cmd+S saves.
  const saveRef = useRef(save);
  saveRef.current = save;
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
        e.preventDefault();
        if (dirty && !saving && !jsonError) saveRef.current();
      }
    };
    window.addEventListener('keydown', key);
    return () => window.removeEventListener('keydown', key);
  }, [dirty, saving, jsonError]);

  const discard = () => {
    setDoc(original);
    setText(stringify(original));
  };

  const editor = useMemo(() => ({ doc, original, update: (fn: (d: JsonObject) => JsonObject) => setDoc(fn) }), [doc, original]);
  const doc_ = view?.doc;
  return (
    <div className="content settings-content">
      <PageHeader
        title={L('共享设置', 'Shared settings')}
        sub={
          doc_ ? (
            <>
              <Icon name="file" size={14} />
              <span className="mono ellipsis">{doc_.path}</span>
            </>
          ) : (
            L('所有实例共用、在同平台机器之间同步的 Claude Code 设置', 'The Claude Code settings every instance shares, synced across machines of a platform')
          )
        }
        actions={
          <Segmented
            value={mode}
            onChange={switchMode}
            options={[
              { value: 'form', label: <><Icon name="sliders" size={15} />{L('表单', 'Form')}</> },
              { value: 'json', label: <><Icon name="code" size={15} />JSON</> },
            ]}
          />
        }
      />
      {view && view.platforms.length > 1 && (
        <div className="platform-bar">
          <Segmented
            value={platform}
            onChange={switchPlatform}
            options={view.platforms.map((p) => ({
              value: p.platform,
              label: (
                <>
                  {platformName(p.platform)}
                  {p.local && <span className="here-tag">{L('本机', 'this machine')}</span>}
                </>
              ),
            }))}
          />
        </div>
      )}
      {loadError && <Notice tone="danger" title={L('读不到设置', "Couldn't read the settings")}>{loadError}</Notice>}
      {!view && !loadError && <Skeleton width="100%" height={300} />}
      {view && doc_ && (
        <>
          <div className="stack settings-notices">
            {!doc_.local && (
              <Notice tone="info" title={L(`这是同步仓库里 ${platformName(doc_.platform)} 的那一份`, `This is the sync repo’s copy for ${platformName(doc_.platform)}`)}>
                {L('保存后写进本机的同步仓库，下次同步时发给那个平台的机器。', 'Saving writes it into the sync clone here; the next sync sends it to that platform’s machines.')}
                {!doc_.exists && L(' 这个平台还没有共享设置，保存就会创建。', ' This platform has no shared settings yet; saving creates them.')}
              </Notice>
            )}
            {doc_.local && !state?.synced && (
              <Notice tone="info">{L('这台机器还没设置同步，所以这里只能编辑本机的设置。', 'This machine does not sync yet, so only its own settings are here.')}</Notice>
            )}
            {doc_.secretEnv.length > 0 && (
              <Notice tone="warn" title={L('env 里有像秘密的变量', 'The env has secret-looking keys')}>
                <span className="mono">{doc_.secretEnv.join(', ')}</span>
                {L(' —— 共享设置会明文同步，同步会拒绝它们。', ' — shared settings sync in plain text, so a sync refuses them.')}
              </Notice>
            )}
          </div>
          {mode === 'json' ? (
            <JsonEditor value={text} onChange={setText} />
          ) : (
            <EditorCtx.Provider value={editor}>
              <FormView doc={doc} query={query} setQuery={setQuery} />
            </EditorCtx.Provider>
          )}
        </>
      )}
      <SaveBar dirty={dirty} saving={saving} invalid={!!jsonError} onSave={() => save()} onDiscard={discard} />
      <Dialog
        open={conflict}
        onClose={() => setConflict(false)}
        title={L('文件在别处被改过了', 'The file changed elsewhere')}
        width={460}
        footer={
          <>
            <Button
              variant="ghost"
              onClick={async () => {
                setConflict(false);
                await load(platform);
              }}
            >
              {L('重新载入（丢掉我的修改）', 'Reload (drop my changes)')}
            </Button>
            <Button variant="danger" busy={saving} onClick={() => save(true)}>
              {L('仍然保存（覆盖）', 'Save anyway (overwrite)')}
            </Button>
          </>
        }
      >
        <div className="confirm-body">
          {L(
            '打开这一页之后，这个文件被别的程序（比如 Claude Code 自己，或另一个 muster）改过了。保存会覆盖那些改动；覆盖前的文件会先备份。',
            'Since this page loaded it, the file was changed by something else (Claude Code itself, or another muster). Saving overwrites that change; the file is backed up first.',
          )}
        </div>
      </Dialog>
      {confirmNode}
    </div>
  );
}

function FormView({ doc, query, setQuery }: { doc: JsonObject; query: string; setQuery: (q: string) => void }) {
  const { L } = useLang();
  const [active, setActive] = useState(SECTIONS[0].id);
  const refs = useRef<Record<string, HTMLElement | null>>({});
  const q = query.trim().toLowerCase();
  const match = (zh: string, en: string, p: string[]) => !q || zh.toLowerCase().includes(q) || en.toLowerCase().includes(q) || p.join('.').toLowerCase().includes(q);

  // Highlight the section in view.
  useEffect(() => {
    const scroller = refs.current[SECTIONS[0].id]?.closest('.page');
    if (!scroller) return;
    const io = new IntersectionObserver(
      (entries) => {
        const top = entries.filter((e) => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0];
        if (top) setActive((top.target as HTMLElement).dataset.section!);
      },
      { root: scroller, rootMargin: '-80px 0px -60% 0px' },
    );
    Object.values(refs.current).forEach((el) => el && io.observe(el));
    return () => io.disconnect();
  }, [q]);

  const countSet = (s: SectionDef) => {
    let n = 0;
    for (const f of SET_FIELDS) if (s.groups?.includes(f.g) && has(doc, f.p)) n++;
    for (const f of LIST_FIELDS) if (s.groups?.includes(f.g) && has(doc, f.p)) n++;
    for (const p of EXTRA_PATHS[s.extra ?? ''] ?? []) {
      const v = doc[p[0]];
      if (isObj(v)) n += Object.keys(v).length;
    }
    return n;
  };

  const sections = SECTIONS.map((s) => {
    const fields = SET_FIELDS.filter((f) => s.groups?.includes(f.g) && match(f.zh, f.en, f.p));
    const lists = LIST_FIELDS.filter((f) => s.groups?.includes(f.g) && match(f.zh, f.en, f.p));
    const extraShown = !!s.extra && match(s.zh, s.en, EXTRA_PATHS[s.extra].flat());
    return { s, fields, lists, extraShown, empty: fields.length + lists.length === 0 && !extraShown };
  });

  const jump = (id: string) => {
    refs.current[id]?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  };

  return (
    <div className="settings-layout">
      <nav className="settings-nav">
        <div className="settings-search">
          <Icon name="search" size={15} />
          <TextInput value={query} onChange={(e) => setQuery(e.target.value)} placeholder={L('搜索设置', 'Search settings')} />
        </div>
        {sections.map(({ s, empty }) => {
          const n = countSet(s);
          return (
            <button key={s.id} type="button" className={'settings-nav-item' + (active === s.id ? ' active' : '') + (empty ? ' dim' : '')} onClick={() => jump(s.id)} disabled={empty}>
              <Icon name={s.icon} size={16} />
              <span className="grow">{L(s.zh, s.en)}</span>
              {n > 0 && <span className="count">{n}</span>}
            </button>
          );
        })}
      </nav>
      <div className="settings-sections">
        {sections.every((x) => x.empty) && (
          <div className="faint" style={{ padding: 40, textAlign: 'center' }}>
            {L('没有匹配的设置。不在表单里的项可以在 JSON 里改。', 'No setting matches. Anything the form lacks can be edited as JSON.')}
          </div>
        )}
        {sections.map(({ s, fields, lists, extraShown, empty }) =>
          empty ? null : (
            <section
              key={s.id}
              ref={(el) => {
                refs.current[s.id] = el;
              }}
              data-section={s.id}
              className="settings-section"
            >
              <h3>
                <Icon name={s.icon} size={17} />
                {L(s.zh, s.en)}
              </h3>
              {s.hintZh && <p className="settings-hint">{L(s.hintZh, s.hintEn ?? '')}</p>}
              <div className="card settings-card">
                {fields.map((f) => (
                  <SetFieldRow key={f.p.join('.')} f={f} />
                ))}
                {lists.map((f) => (
                  <ListFieldRow key={f.p.join('.')} f={f} />
                ))}
                {extraShown && s.extra === 'hooks' && <ExtraBlock>{<HooksEditor />}</ExtraBlock>}
                {extraShown && s.extra === 'plugins' && (
                  <>
                    <ExtraBlock title={L('已启用的插件', 'Enabled plugins')}>
                      <PluginsEditor />
                    </ExtraBlock>
                    <ExtraBlock title={L('插件市场', 'Marketplaces')}>
                      <MarketplacesEditor />
                    </ExtraBlock>
                  </>
                )}
                {extraShown && s.extra === 'env' && (
                  <ExtraBlock>
                    <EnvEditor />
                  </ExtraBlock>
                )}
              </div>
            </section>
          ),
        )}
      </div>
    </div>
  );
}

function ExtraBlock({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <div className="extra-block">
      {title && <div className="extra-title">{title}</div>}
      {children}
    </div>
  );
}

/** SaveBar slides up while there are unsaved edits. */
function SaveBar({ dirty, saving, invalid, onSave, onDiscard }: { dirty: boolean; saving: boolean; invalid: boolean; onSave: () => void; onDiscard: () => void }) {
  const { L } = useLang();
  const { mounted, shown } = usePresence(dirty, 320);
  if (!mounted) return null;
  return (
    <div className={'savebar' + (shown ? ' shown' : '')}>
      <div className="savebar-inner card">
        <span className="savebar-dot" />
        <span className="grow">{invalid ? L('JSON 还不合法，改好才能保存', 'The JSON is not valid yet') : L('有未保存的修改', 'Unsaved changes')}</span>
        <Badge>Ctrl+S</Badge>
        <Button variant="ghost" onClick={onDiscard} disabled={saving}>
          {L('放弃', 'Discard')}
        </Button>
        <Button variant="primary" icon="check" busy={saving} disabled={invalid} onClick={onSave}>
          {L('保存', 'Save')}
        </Button>
      </div>
    </div>
  );
}
