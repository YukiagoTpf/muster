import { useEffect, useRef, useState } from 'react';
import { PageHeader, Section, useApp } from '../app/App';
import { api } from '../lib/api';
import { fold, sideBySide } from '../lib/diff';
import { itemLabel, platformName } from '../lib/format';
import { useBusy, useResource } from '../lib/hooks';
import { useLang } from '../lib/i18n';
import type { Item, SetupEntry, SetupResult, Sides, StreamEvent, SyncResult } from '../lib/types';
import { Badge, Button, Empty, Field, IconButton, Notice, Segmented, Skeleton, Spinner, TextInput } from '../ui/controls';
import { Icon, type IconName } from '../ui/Icon';
import { Dialog, Menu, useConfirm, useToast } from '../ui/overlays';

const KIND_ICON: Record<Item['kind'], IconName> = { settings: 'sliders', setup: 'box', credential: 'key', file: 'file' };

export function SyncPage() {
  const { L } = useLang();
  const toast = useToast();
  const { refreshState } = useApp();
  const sync = useResource(api.sync);
  const [result, setResult] = useState<SyncResult | null>(null);
  const [syncing, run] = useBusy();

  const doSync = async (prefer?: 'local' | 'remote') => {
    try {
      const r = await run(api.runSync(prefer));
      setResult(r);
      if (r.conflicts.length > 0) toast('error', L('两边都改了同一处，先选一边', 'Both sides changed the same thing — pick a side'));
      else if (r.sent.length + r.received.length === 0) toast('ok', L('已经是最新的', 'Already in sync'));
      else toast('ok', L(`已同步：发出 ${r.sent.length} 项，收到 ${r.received.length} 项`, `Synced: sent ${r.sent.length}, received ${r.received.length}`));
      sync.reload();
      refreshState();
    } catch (e) {
      toast('error', (e as Error).message);
    }
  };

  const data = sync.data;
  return (
    <div className="content">
      <PageHeader
        title={L('同步', 'Sync')}
        sub={
          data?.setUp ? (
            <>
              <Icon name="git" size={14} />
              <span className="mono ellipsis">{data.repo}</span>
              <span className="dot" />
              <span>{platformName(data.platform)}</span>
            </>
          ) : (
            L('通过你自己的私有 git 仓库，让几台机器保持一致', 'Keeps your machines in step through a private git repo of your own')
          )
        }
        actions={
          data?.setUp && (
            <Button variant="primary" icon="sync" busy={syncing} onClick={() => doSync()}>
              {L('立即同步', 'Sync now')}
            </Button>
          )
        }
      />
      {sync.error && (
        <Notice tone="danger" title={L('读不到同步状态', "Couldn't read the sync state")}>
          {sync.error.message}
        </Notice>
      )}
      {!data && !sync.error && <Skeleton width="100%" height={120} />}
      {data && !data.setUp && <NotSetUp />}
      {data?.setUp && (
        <>
          {result && result.conflicts.length > 0 && <Conflicts items={result.conflicts} busy={syncing} onPick={doSync} />}
          {result && result.conflicts.length === 0 && result.sent.length + result.received.length > 0 && <SyncSummary result={result} />}
          <div className="sync-grid">
            <Section title={L('待发送', 'Pending')}>
              <div className="card card-pad">
                {data.pending.length === 0 && data.unpushed ? (
                  <div className="status-ok waiting">
                    <span className="status-ok-icon">
                      <Icon name="upload" size={18} />
                    </span>
                    <div>
                      <div className="status-ok-title">{L('改动已记下，还没推送到仓库', 'Changes recorded, not pushed yet')}</div>
                      <div className="faint">
                        {result?.conflicts.length ? L('先在上面选一边，同步就会推送', 'Pick a side above and the sync pushes them') : L('下次同步时推送', 'The next sync pushes them')}
                      </div>
                    </div>
                  </div>
                ) : data.pending.length === 0 ? (
                  <div className="status-ok">
                    <span className="status-ok-icon">
                      <Icon name="check" size={18} strokeWidth={2.4} />
                    </span>
                    <div>
                      <div className="status-ok-title">{L('没有待发送的改动', 'Nothing to send')}</div>
                      <div className="faint">{L('这台机器的改动都已在仓库里', 'Everything changed here is in the repo')}</div>
                    </div>
                  </div>
                ) : (
                  <div className="change-list stagger">
                    {data.pending.map((c) => (
                      <div key={c.kind + c.name} className="change">
                        <span className="change-icon">
                          <Icon name={KIND_ICON[c.kind]} size={16} />
                        </span>
                        <span className="grow ellipsis">{itemLabel(c, L)}</span>
                        <Badge tone={c.change === 'added' ? 'ok' : c.change === 'removed' ? 'danger' : 'accent'}>
                          {{ added: L('新增', 'added'), modified: L('已修改', 'modified'), removed: L('已删除', 'removed') }[c.change]}
                        </Badge>
                      </div>
                    ))}
                  </div>
                )}
                {data.unpushed && data.pending.length > 0 && (
                  <div className="faint" style={{ marginTop: 10, fontSize: 12.5 }}>
                    {L('另有已记下但还没推送的改动，会一起发出。', 'Changes recorded earlier but not pushed yet go along.')}
                  </div>
                )}
              </div>
            </Section>
            <TrackedFiles tracked={data.tracked} onChange={sync.reload} />
          </div>
          <SetupList />
        </>
      )}
    </div>
  );
}

function NotSetUp() {
  const { L } = useLang();
  const toast = useToast();
  const cmd = 'muster sync init git@github.com:<you>/muster-sync.git';
  return (
    <div className="card setup-steps">
      <Empty icon="sync" title={L('还没有设置同步', 'Sync is not set up')}>
        {L(
          '同步的内容：共享设置（每个平台一份）、登记文件、装机清单，以及加密后的凭据。默认凭据只属于每台机器，不会同步。',
          'What syncs: the shared settings (one copy per platform), tracked files, the setup list, and your credentials, encrypted. The default credential stays on each machine.',
        )}
      </Empty>
      <ol className="steps">
        <li>
          <span className="step-n">1</span>
          <div>
            <div className="step-title">{L('在 GitHub 上新建一个空的私有仓库', 'Create an empty private repo on GitHub')}</div>
            <div className="faint">
              {L('比如 ', 'e.g. ')}
              <code>gh repo create muster-sync --private</code>
            </div>
          </div>
        </li>
        <li>
          <span className="step-n">2</span>
          <div className="grow">
            <div className="step-title">{L('在终端里执行', 'Run in a terminal')}</div>
            <div className="cmd">
              <code>{cmd}</code>
              <IconButton
                icon="copy"
                size={16}
                label={L('复制', 'Copy')}
                onClick={() => navigator.clipboard.writeText(cmd).then(() => toast('ok', L('已复制', 'Copied')))}
              />
            </div>
            <div className="faint">
              {L(
                '第一台机器会用自己的内容建好仓库，并让你设一个口令；之后的机器输入同一个口令，就会恢复成仓库里的样子。',
                'The first machine seeds the repo and asks for a new passphrase; every later machine enters the same passphrase and is restored from the repo.',
              )}
            </div>
          </div>
        </li>
        <li>
          <span className="step-n">3</span>
          <div className="step-title">{L('回到这里，刷新页面', 'Come back and reload this page')}</div>
        </li>
      </ol>
    </div>
  );
}

function SyncSummary({ result }: { result: SyncResult }) {
  const { L } = useLang();
  return (
    <div className="stack" style={{ marginBottom: 6 }}>
      <Notice tone="ok" title={L('同步完成', 'Synced')}>
        {result.sent.length > 0 && (
          <div>
            {L('发出：', 'Sent: ')}
            {result.sent.map((it) => itemLabel(it, L)).join('、')}
          </div>
        )}
        {result.received.length > 0 && (
          <div>
            {L('收到：', 'Received: ')}
            {result.received.map((it) => itemLabel(it, L)).join('、')}
          </div>
        )}
      </Notice>
      {result.notInstalled.length > 0 && (
        <Notice tone="info" title={L('装机清单里有这台机器还没装的，见下方', 'The setup list has entries this machine lacks — see below')}>
          {result.notInstalled.join('、')}
        </Notice>
      )}
    </div>
  );
}

function Conflicts({ items, busy, onPick }: { items: Item[]; busy: boolean; onPick: (p: 'local' | 'remote') => void }) {
  const { L } = useLang();
  const [active, setActive] = useState(0);
  const [sides, setSides] = useState<Sides | null>(null);
  const [error, setError] = useState('');
  const item = items[Math.min(active, items.length - 1)];
  useEffect(() => {
    let live = true;
    setSides(null);
    setError('');
    api.sides(item).then(
      (s) => live && setSides(s),
      (e) => live && setError((e as Error).message),
    );
    return () => {
      live = false;
    };
  }, [item]);
  return (
    <div className="card conflicts">
      <div className="conflicts-head">
        <span className="conflicts-icon">
          <Icon name="alert" size={20} />
        </span>
        <div className="grow">
          <div className="conflicts-title">{L('这几项在这台机器和其他机器上都被改了', 'These changed here and on another machine')}</div>
          <div className="faint">
            {L('这台机器上的文件还没被改动。选一边后，冲突的地方按那一边算，其余改动照常合并。', 'No file here was changed yet. Pick a side: it wins where they clash, and every other change merges as usual.')}
          </div>
        </div>
      </div>
      {items.length > 1 && (
        <div style={{ margin: '4px 0 12px' }}>
          <Segmented
            size="sm"
            value={String(active)}
            onChange={(v) => setActive(Number(v))}
            options={items.map((it, i) => ({ value: String(i), label: itemLabel(it, L) }))}
          />
        </div>
      )}
      {error ? <Notice tone="danger">{error}</Notice> : sides ? <DiffView sides={sides} /> : <Skeleton width="100%" height={160} />}
      <div className="conflicts-actions">
        <Button icon="pc" busy={busy} onClick={() => onPick('local')}>
          {L('保留这台机器的', 'Keep this machine’s')}
        </Button>
        <Button variant="primary" icon="download" busy={busy} onClick={() => onPick('remote')}>
          {L('采用其他机器的', 'Take the other machines’')}
        </Button>
      </div>
    </div>
  );
}

function DiffView({ sides }: { sides: Sides }) {
  const { L } = useLang();
  const rows = fold(sideBySide(sides.here, sides.there));
  return (
    <div className="diff">
      <div className="diff-head">
        <div>
          {L('这台机器', 'This machine')}
          {!sides.hereExists && <Badge tone="danger">{L('已删除', 'deleted')}</Badge>}
        </div>
        <div>
          {L('其他机器', 'Other machines')}
          {!sides.thereExists && <Badge tone="danger">{L('已删除', 'deleted')}</Badge>}
        </div>
      </div>
      <div className="diff-body mono">
        {rows.map((r, i) =>
          r === null ? (
            <div key={i} className="diff-gap">
              ⋯
            </div>
          ) : (
            <div key={i} className={'diff-row ' + r.kind}>
              <div className={'diff-cell' + (r.kind === 'del' || r.kind === 'change' ? ' del' : '') + (r.left ? '' : ' empty')}>
                <span className="diff-n">{r.left?.n}</span>
                <span className="diff-text">{r.left?.text}</span>
              </div>
              <div className={'diff-cell' + (r.kind === 'add' || r.kind === 'change' ? ' add' : '') + (r.right ? '' : ' empty')}>
                <span className="diff-n">{r.right?.n}</span>
                <span className="diff-text">{r.right?.text}</span>
              </div>
            </div>
          ),
        )}
      </div>
    </div>
  );
}

function TrackedFiles({ tracked, onChange }: { tracked: string[]; onChange: () => void }) {
  const { L } = useLang();
  const toast = useToast();
  const [path, setPath] = useState('');
  const [busy, run] = useBusy();
  const add = async () => {
    try {
      await run(api.track(path.trim()));
      setPath('');
      toast('ok', L('已登记，下次同步会发出', 'Tracked — the next sync sends it'));
      onChange();
    } catch (e) {
      toast('error', (e as Error).message);
    }
  };
  const remove = async (p: string) => {
    try {
      await api.untrack(p);
      toast('ok', L('不再同步这个文件（文件本身还在）', 'No longer synced (the file stays)'));
      onChange();
    } catch (e) {
      toast('error', (e as Error).message);
    }
  };
  return (
    <Section title={L('登记文件', 'Tracked files')}>
      <div className="card card-pad">
        <div className="faint" style={{ marginBottom: 12, fontSize: 12.5 }}>
          {L('你自己写的脚本，比如 statusline、hook。只能是家目录下的文件。', 'Files you wrote — a statusline script, a hook. Only files under your home directory.')}
        </div>
        {tracked.length > 0 && (
          <div className="change-list" style={{ marginBottom: 12 }}>
            {tracked.map((p) => (
              <div key={p} className="change">
                <span className="change-icon">
                  <Icon name="file" size={16} />
                </span>
                <span className="grow ellipsis mono">{p}</span>
                <IconButton icon="x" size={16} label={L('不再同步', 'Stop syncing')} onClick={() => remove(p)} />
              </div>
            ))}
          </div>
        )}
        <div className="input-with-action">
          <TextInput mono value={path} placeholder="~/.claude/statusline.sh" onChange={(e) => setPath(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && path.trim() && add()} />
          <Button icon="plus" busy={busy} disabled={!path.trim()} onClick={add}>
            {L('登记', 'Track')}
          </Button>
        </div>
      </div>
    </Section>
  );
}

function SetupList() {
  const { L } = useLang();
  const toast = useToast();
  const setup = useResource(api.setup);
  const [adding, setAdding] = useState(false);
  const [log, setLog] = useState<{ title: string; text: string; results: SetupResult[]; done: boolean; error?: string } | null>(null);
  const [confirmNode, confirm] = useConfirm();

  const runSetup = async (kind: 'install' | 'update', names: string[], force = false) => {
    const title =
      kind === 'install' ? L(names.length ? `安装 ${names.join('、')}` : '安装缺少的条目', names.length ? `Install ${names.join(', ')}` : 'Install what is missing') : L('更新', 'Update');
    setLog({ title, text: '', results: [], done: false });
    const onEvent = (e: StreamEvent) =>
      setLog((l) => {
        if (!l) return l;
        if (e.type === 'output') return { ...l, text: l.text + e.text };
        if (e.type === 'result' && e.result) return { ...l, results: [...l.results, e.result] };
        return { ...l, done: true, error: e.error || undefined };
      });
    try {
      if (kind === 'install') await api.installSetup(names, force, onEvent);
      else await api.updateSetup(names, onEvent);
    } catch (e) {
      setLog((l) => l && { ...l, done: true, error: (e as Error).message });
    }
    setup.reload();
  };

  const remove = async (e: SetupEntry) => {
    const ok = await confirm({
      title: L(`从装机清单删除 ${e.name}？`, `Remove ${e.name} from the setup list?`),
      body: L('已经装好的东西不会被卸载。下次同步时，其他同平台的机器也会看到它被删除。', 'Nothing gets uninstalled. The next sync removes it from the list on your other machines of this platform.'),
      confirm: L('删除', 'Remove'),
      danger: true,
    });
    if (!ok) return;
    try {
      await api.removeSetup(e.name);
      setup.reload();
    } catch (err) {
      toast('error', (err as Error).message);
    }
  };

  const entries = setup.data?.entries ?? [];
  const missing = entries.filter((e) => !e.installed);
  return (
    <Section
      title={L('装机清单', 'Setup list')}
      aside={
        entries.length > 0 && (
          <>
            <Button size="sm" icon="download" disabled={missing.length === 0} onClick={() => runSetup('install', [])}>
              {missing.length > 0 ? L(`安装缺少的 ${missing.length} 项`, `Install ${missing.length} missing`) : L('都已安装', 'All installed')}
            </Button>
            <Button size="sm" icon="refresh" disabled={!entries.some((e) => e.update)} onClick={() => runSetup('update', [])}>
              {L('全部更新', 'Update all')}
            </Button>
            <Button size="sm" variant="primary" icon="plus" onClick={() => setAdding(true)}>
              {L('添加', 'Add')}
            </Button>
          </>
        )
      }
    >
      <div className="card">
        {setup.error ? (
          <div className="card-pad">
            <Notice tone="danger">{setup.error.message}</Notice>
          </div>
        ) : !setup.data ? (
          <div className="card-pad">
            <Skeleton width="100%" height={60} />
          </div>
        ) : entries.length === 0 ? (
          <Empty icon="box" title={L('装机清单是空的', 'The setup list is empty')}>
            {L(
              '每一项是一条安装命令（可以再加更新命令和检查命令），比如一组 skills。新机器按清单装一遍，平时一键更新。Muster 只执行命令，不管它装了什么。',
              'Each entry is an install command (plus optional update and check commands), say for a set of skills. A new machine installs the list; day to day, update it in one go. Muster only runs the commands.',
            )}
            <div style={{ marginTop: 16 }}>
              <Button variant="primary" icon="plus" onClick={() => setAdding(true)}>
                {L('添加第一项', 'Add the first entry')}
              </Button>
            </div>
          </Empty>
        ) : (
          <div className="setup-rows stagger">
            {entries.map((e) => (
              <div key={e.name} className="setup-row">
                <span className={'setup-state' + (e.installed ? ' on' : '')}>
                  <Icon name={e.installed ? 'check' : 'download'} size={15} strokeWidth={2.2} />
                </span>
                <div className="grow">
                  <div className="setup-name">
                    {e.name}
                    <Badge tone={e.installed ? 'ok' : 'neutral'}>{e.installed ? L('已安装', 'installed') : L('未安装', 'not installed')}</Badge>
                  </div>
                  <div className="setup-cmds mono">
                    <div>
                      <span className="setup-k">install</span> {e.install}
                    </div>
                    {e.update && (
                      <div>
                        <span className="setup-k">update</span> {e.update}
                      </div>
                    )}
                    {e.check && (
                      <div>
                        <span className="setup-k">check</span> {e.check}
                      </div>
                    )}
                  </div>
                </div>
                <Menu
                  trigger={(p) => <IconButton {...p} icon="more" label={L('更多', 'More')} />}
                  items={[
                    { label: e.installed ? L('重新安装', 'Reinstall') : L('安装', 'Install'), icon: 'download', onSelect: () => runSetup('install', [e.name], e.installed) },
                    { label: L('更新', 'Update'), icon: 'refresh', disabled: !e.update, onSelect: () => runSetup('update', [e.name]) },
                    { label: L('删除', 'Remove'), icon: 'trash', danger: true, onSelect: () => remove(e) },
                  ]}
                />
              </div>
            ))}
          </div>
        )}
      </div>
      <AddSetupDialog
        open={adding}
        onClose={() => setAdding(false)}
        onAdded={() => {
          setAdding(false);
          setup.reload();
        }}
      />
      <RunLog log={log} onClose={() => setLog(null)} />
      {confirmNode}
    </Section>
  );
}

function RunLog({
  log,
  onClose,
}: {
  log: { title: string; text: string; results: SetupResult[]; done: boolean; error?: string } | null;
  onClose: () => void;
}) {
  const { L } = useLang();
  const body = useRef<HTMLPreElement>(null);
  const [last, setLast] = useState(log);
  useEffect(() => {
    if (log) setLast(log);
  }, [log]);
  useEffect(() => {
    body.current?.scrollTo({ top: body.current.scrollHeight });
  }, [log?.text]);
  const shown = log ?? last;
  const actionText: Record<SetupResult['action'], string> = {
    installed: L('已安装', 'installed'),
    present: L('已经装好', 'installed already'),
    updated: L('已更新', 'updated'),
    failed: L('失败', 'failed'),
    skipped: L('跳过', 'skipped'),
  };
  return (
    <Dialog
      open={!!log}
      onClose={onClose}
      dismissable={!!log?.done}
      width={680}
      title={
        <span className="row">
          {shown && !shown.done && <Spinner size={16} />}
          {shown?.title}
        </span>
      }
      footer={
        <Button variant="primary" disabled={!log?.done} onClick={onClose}>
          {log?.done ? L('完成', 'Done') : L('正在运行…', 'Running…')}
        </Button>
      }
    >
      <pre ref={body} className="runlog mono">
        {shown?.text || (shown?.done ? '' : L('启动中…', 'Starting…'))}
      </pre>
      {shown && shown.results.length > 0 && (
        <div className="run-results">
          {shown.results.map((r) => (
            <div key={r.name} className="run-result">
              <Icon name={r.action === 'failed' ? 'alert' : r.action === 'skipped' ? 'info' : 'check'} size={15} className={'rr-' + r.action} />
              <strong>{r.name}</strong>
              <span className={'rr-' + r.action}>{actionText[r.action]}</span>
              {r.reason && r.action !== 'failed' && <span className="faint">{r.reason}</span>}
              {r.action === 'failed' && <span className="faint">exit {r.exitCode}</span>}
            </div>
          ))}
        </div>
      )}
      {shown?.error && (
        <div style={{ marginTop: 12 }}>
          <Notice tone="danger">{shown.error}</Notice>
        </div>
      )}
    </Dialog>
  );
}

function AddSetupDialog({ open, onClose, onAdded }: { open: boolean; onClose: () => void; onAdded: () => void }) {
  const { L } = useLang();
  const [name, setName] = useState('');
  const [install, setInstall] = useState('');
  const [update, setUpdate] = useState('');
  const [check, setCheck] = useState('');
  const [error, setError] = useState('');
  const [busy, run] = useBusy();
  useEffect(() => {
    if (open) {
      setName('');
      setInstall('');
      setUpdate('');
      setCheck('');
      setError('');
    }
  }, [open]);
  const save = async () => {
    try {
      await run(api.addSetup({ name: name.trim(), install: install.trim(), update: update.trim() || undefined, check: check.trim() || undefined }));
      onAdded();
    } catch (e) {
      setError((e as Error).message);
    }
  };
  return (
    <Dialog
      open={open}
      onClose={onClose}
      width={560}
      title={L('添加到装机清单', 'Add to the setup list')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {L('取消', 'Cancel')}
          </Button>
          <Button variant="primary" busy={busy} disabled={!name.trim() || !install.trim()} onClick={save}>
            {L('添加', 'Add')}
          </Button>
        </>
      }
    >
      <div className="form">
        <Field label={L('名称', 'Name')}>
          <TextInput value={name} onChange={(e) => setName(e.target.value)} placeholder="skills" />
        </Field>
        <Field label={L('安装命令', 'Install command')} hint={L('Windows 上用 cmd 执行，其他平台用 sh', 'Runs in cmd on Windows, sh elsewhere')}>
          <TextInput mono value={install} onChange={(e) => setInstall(e.target.value)} placeholder="npx skills add <source> -g -a claude-code -y" />
        </Field>
        <Field label={L('更新命令', 'Update command')} aside={L('可选', 'optional')}>
          <TextInput mono value={update} onChange={(e) => setUpdate(e.target.value)} placeholder="npx skills update -g -y" />
        </Field>
        <Field label={L('检查命令', 'Check command')} aside={L('可选', 'optional')} hint={L('退出码为 0 就当作已经装好', 'Exit code 0 means it is installed already')}>
          <TextInput mono value={check} onChange={(e) => setCheck(e.target.value)} />
        </Field>
        {error && (
          <div className="form-error shake">
            <Icon name="alert" size={15} />
            {error}
          </div>
        )}
      </div>
    </Dialog>
  );
}
