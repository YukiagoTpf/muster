import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import { useLang } from '../lib/i18n';
import { Badge, Button, IconButton, Segmented, TextArea, TextInput, Toggle } from '../ui/controls';
import { Icon } from '../ui/Icon';
import { Select } from '../ui/overlays';
import { HOOK_EVENTS, MARKET_SOURCES, type ListField, type SetField } from './catalog';
import { del, get, has, isObj, isProviderKey, looksSecret, renameKey, set, type Json, type JsonObject, type Path } from './projection';

interface Editor {
  doc: JsonObject;
  original: JsonObject;
  update: (fn: (d: JsonObject) => JsonObject) => void;
}

export const EditorCtx = createContext<Editor | null>(null);

export function useEditor() {
  const e = useContext(EditorCtx);
  if (!e) throw new Error('useEditor outside EditorCtx');
  const { doc, original, update } = e;
  return {
    doc,
    original,
    value: (p: Path) => get(doc, p),
    write: (p: Path, v: Json) => update((d) => set(d, p, v)),
    remove: (p: Path) => update((d) => del(d, p, original)),
    update,
  };
}

/** fits says whether a field's control can show the value it holds. */
export function fits(f: SetField, v: Json | undefined): boolean {
  if (v === undefined) return true;
  switch (f.k) {
    case 'bool':
      return typeof v === 'boolean';
    case 'enum':
    case 'text':
    case 'text3':
      return typeof v === 'string';
    case 'num':
      return typeof v === 'number';
    case 'numenv':
      return typeof v === 'string' && /^\d*$/.test(v);
    case 'flag':
      return typeof v === 'string' && /^(1|true|yes|on)$/i.test(v);
  }
}

function Locked({ value }: { value: Json | undefined }) {
  const { L } = useLang();
  return (
    <div className="locked">
      <Badge icon="lock">{L('在 JSON 中编辑', 'edit in JSON')}</Badge>
      <code className="locked-value">{JSON.stringify(value)}</code>
    </div>
  );
}

/** Row lays out one setting: what it is on the left, its control on the right. */
export function Row({
  label,
  hint,
  path,
  isSet,
  onReset,
  wide,
  children,
}: {
  label: string;
  hint?: string;
  path: string;
  isSet: boolean;
  onReset?: () => void;
  wide?: boolean;
  children: ReactNode;
}) {
  const { L } = useLang();
  return (
    <div className={'srow' + (wide ? ' wide' : '') + (isSet ? ' is-set' : '')}>
      <div className="srow-text">
        <div className="srow-label">
          <span className="set-dot" aria-hidden />
          {label}
        </div>
        {hint && <div className="srow-hint">{hint}</div>}
        <div className="srow-path mono">{path}</div>
      </div>
      <div className="srow-control">
        {children}
        {onReset ? (
          <IconButton className={'srow-reset' + (isSet ? '' : ' hidden')} icon="undo" size={15} label={L('恢复默认（删除这一项）', 'Back to default (remove the key)')} onClick={onReset} />
        ) : (
          !wide && <span className="srow-reset-space" />
        )}
      </div>
    </div>
  );
}

/** NumberInput keeps what is typed while it isn't a number yet. */
function NumberInput({ value, onValue, placeholder }: { value: string; onValue: (v: string) => void; placeholder?: string }) {
  const [text, setText] = useState(value);
  useEffect(() => setText(value), [value]);
  return (
    <TextInput
      className="num-input"
      inputMode="numeric"
      value={text}
      placeholder={placeholder}
      onChange={(e) => {
        setText(e.target.value);
        if (/^\d*$/.test(e.target.value.trim())) onValue(e.target.value.trim());
      }}
    />
  );
}

export function SetFieldRow({ f }: { f: SetField }) {
  const { L } = useLang();
  const ed = useEditor();
  const v = ed.value(f.p);
  const ok = fits(f, v);
  const isSet = v !== undefined;
  const reset = () => ed.remove(f.p);
  let control: ReactNode;
  if (!ok) control = <Locked value={v} />;
  else
    switch (f.k) {
      case 'bool':
        control = (
          <Segmented
            size="sm"
            value={v === undefined ? 'unset' : v ? 'on' : 'off'}
            onChange={(c) => (c === 'unset' ? reset() : ed.write(f.p, c === 'on'))}
            options={[
              { value: 'unset', label: L('默认', 'Default') },
              { value: 'on', label: L('开', 'On') },
              { value: 'off', label: L('关', 'Off') },
            ]}
          />
        );
        break;
      case 'enum': {
        const opts = [{ value: '\u0000', label: L('默认（不设置）', 'Default (not set)') }, ...(f.v ?? []).map((x) => ({ value: x, label: x }))];
        if (typeof v === 'string' && !(f.v ?? []).includes(v)) opts.push({ value: v, label: v + L('（自定义）', ' (custom)') });
        control = (
          <div className="enum-select">
            <Select value={typeof v === 'string' ? v : '\u0000'} options={opts} onChange={(x) => (x === '\u0000' ? reset() : ed.write(f.p, x))} />
          </div>
        );
        break;
      }
      case 'text':
        control = (
          <TextInput
            className={f.wide ? 'text-wide' : 'text-input'}
            mono={f.wide}
            value={typeof v === 'string' ? v : ''}
            placeholder={f.ph ?? L('不设置', 'not set')}
            onChange={(e) => (e.target.value === '' ? reset() : ed.write(f.p, e.target.value))}
          />
        );
        break;
      case 'num':
        control = <NumberInput value={typeof v === 'number' ? String(v) : ''} placeholder={f.ph ?? L('不设置', 'not set')} onValue={(t) => (t === '' ? reset() : ed.write(f.p, Number(t)))} />;
        break;
      case 'numenv':
        control = <NumberInput value={typeof v === 'string' ? v : ''} placeholder={f.ph ?? L('不设置', 'not set')} onValue={(t) => (t === '' ? reset() : ed.write(f.p, t))} />;
        break;
      case 'flag':
        control = <Toggle checked={v !== undefined} label={L(f.zh, f.en)} onChange={(on) => (on ? ed.write(f.p, '1') : reset())} />;
        break;
      case 'text3':
        control = <Text3 f={f} v={v as string | undefined} />;
        break;
    }
  return (
    <Row label={L(f.zh, f.en)} hint={f.hintZh ? L(f.hintZh, f.hintEn ?? '') : undefined} path={f.p.join('.')} isSet={isSet} onReset={ok && f.k !== 'bool' && f.k !== 'flag' ? reset : undefined} wide={f.wide}>
      {control}
    </Row>
  );
}

/** Text3 is a string where "" and absence differ: default, custom or blank. */
function Text3({ f, v }: { f: SetField; v: string | undefined }) {
  const { L } = useLang();
  const ed = useEditor();
  const [mode, setMode] = useState<'default' | 'custom' | 'blank'>(v === undefined ? 'default' : v === '' ? 'blank' : 'custom');
  const [text, setText] = useState(v ?? '');
  useEffect(() => {
    setMode(v === undefined ? 'default' : v === '' ? 'blank' : 'custom');
    if (v) setText(v);
  }, [v]);
  return (
    <div className="text3">
      <Segmented
        size="sm"
        value={mode}
        onChange={(m) => {
          setMode(m);
          if (m === 'default') ed.remove(f.p);
          else if (m === 'blank') ed.write(f.p, '');
          else if (text) ed.write(f.p, text);
        }}
        options={[
          { value: 'default', label: L('默认', 'Default') },
          { value: 'custom', label: L('自定义', 'Custom') },
          { value: 'blank', label: L('留空', 'Blank') },
        ]}
      />
      {mode === 'custom' && (
        <TextInput
          className="text-wide"
          value={text}
          autoFocus={!v}
          onChange={(e) => {
            setText(e.target.value);
            if (e.target.value) ed.write(f.p, e.target.value);
          }}
        />
      )}
    </div>
  );
}

/** ListFieldRow edits an array of strings, one chip per entry. */
export function ListFieldRow({ f }: { f: ListField }) {
  const { L } = useLang();
  const ed = useEditor();
  const v = ed.value(f.p);
  const ok = v === undefined || (Array.isArray(v) && v.every((x) => typeof x === 'string'));
  const items = ok && Array.isArray(v) ? (v as string[]) : [];
  const [draft, setDraft] = useState('');
  const writeList = (next: string[]) => {
    if (next.length === 0 && !has(ed.original, f.p)) ed.remove(f.p);
    else ed.write(f.p, next);
  };
  const add = () => {
    const t = draft.trim();
    if (!t || items.includes(t)) return;
    writeList([...items, t]);
    setDraft('');
  };
  return (
    <Row label={L(f.zh, f.en)} hint={f.hintZh ? L(f.hintZh, f.hintEn ?? '') : undefined} path={f.p.join('.')} isSet={v !== undefined} wide>
      {!ok ? (
        <Locked value={v} />
      ) : (
        <div className="chips-editor">
          {items.map((it, i) => (
            <span key={it + i} className="chip">
              <span className="mono">{it}</span>
              <button type="button" aria-label={L('删除', 'Remove')} onClick={() => writeList(items.filter((_, j) => j !== i))}>
                <Icon name="x" size={12} strokeWidth={2.4} />
              </button>
            </span>
          ))}
          <input
            className="chip-input mono"
            value={draft}
            placeholder={items.length ? L('添加…', 'add…') : (f.ph ?? '')}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                add();
              } else if (e.key === 'Backspace' && !draft && items.length) writeList(items.slice(0, -1));
            }}
            onBlur={add}
          />
        </div>
      )}
    </Row>
  );
}

/** PluginsEditor: enabledPlugins is plugin@marketplace → on/off. */
export function PluginsEditor() {
  const { L } = useLang();
  const ed = useEditor();
  const path = ['enabledPlugins'];
  const v = ed.value(path);
  const ok = v === undefined || (isObj(v) && Object.values(v).every((x) => typeof x === 'boolean'));
  const entries = ok && isObj(v) ? Object.entries(v as Record<string, boolean>) : [];
  const [draft, setDraft] = useState('');
  return (
    <div className="map-editor">
      <div className="map-hint">{L('每行一个 plugin@marketplace。没有列出的插件按它自己的默认值。', 'One row per plugin@marketplace. A plugin with no row keeps its own default.')}</div>
      {!ok ? (
        <Locked value={v} />
      ) : (
        <>
          {entries.map(([name, on]) => (
            <div key={name} className="map-row">
              <Icon name="plug" size={16} className="faint" />
              <span className="mono grow ellipsis">{name}</span>
              <Toggle checked={on} label={name} onChange={(x) => ed.write([...path, name], x)} />
              <IconButton icon="x" size={15} label={L('删除', 'Remove')} onClick={() => ed.remove([...path, name])} />
            </div>
          ))}
          <div className="map-add">
            <TextInput mono value={draft} placeholder="plugin@marketplace" onChange={(e) => setDraft(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && addPlugin()} />
            <Button icon="plus" disabled={!draft.trim()} onClick={addPlugin}>
              {L('添加', 'Add')}
            </Button>
          </div>
        </>
      )}
    </div>
  );
  function addPlugin() {
    const n = draft.trim();
    if (!n) return;
    ed.write([...path, n], true);
    setDraft('');
  }
}

/** MarketplacesEditor: extraKnownMarketplaces is name → { source, autoUpdate }. */
export function MarketplacesEditor() {
  const { L } = useLang();
  const ed = useEditor();
  const path = ['extraKnownMarketplaces'];
  const v = ed.value(path);
  if (v !== undefined && !isObj(v)) return <Locked value={v} />;
  const entries = isObj(v) ? Object.entries(v) : [];
  const addRow = () => {
    let n = 'marketplace';
    for (let i = 2; isObj(v) && n in v; i++) n = 'marketplace-' + i;
    ed.write([...path, n], { source: { source: 'github', repo: '' } });
  };
  return (
    <div className="map-editor">
      <div className="map-hint">{L('额外的插件市场。github 填 owner/repo；git 和 url 填地址；file 和 directory 填路径。', 'Extra plugin marketplaces. github takes owner/repo; git and url take a URL; file and directory take a path.')}</div>
      {entries.map(([name, m]) => (
        <MarketRow key={name} name={name} m={m} names={entries.map(([n]) => n)} />
      ))}
      <div>
        <Button icon="plus" onClick={addRow}>
          {L('添加市场', 'Add marketplace')}
        </Button>
      </div>
    </div>
  );
}

function MarketRow({ name, m, names }: { name: string; m: Json; names: string[] }) {
  const { L } = useLang();
  const ed = useEditor();
  const base = ['extraKnownMarketplaces', name];
  const src = isObj(m) && isObj(m.source) ? m.source : null;
  const def = src ? MARKET_SOURCES.find((x) => x.s === src.source) : undefined;
  const fitsRow =
    isObj(m) &&
    !!src &&
    !!def &&
    Object.keys(src).every((k) => k === 'source' || k === def.field) &&
    Object.keys(m).every((k) => k === 'source' || k === 'autoUpdate') &&
    (m.autoUpdate === undefined || typeof m.autoUpdate === 'boolean') &&
    (src[def.field] === undefined || typeof src[def.field] === 'string');
  const [draftName, setDraftName] = useState(name);
  useEffect(() => setDraftName(name), [name]);
  if (!fitsRow) {
    return (
      <div className="map-row">
        <span className="mono">{name}</span>
        <Locked value={m} />
      </div>
    );
  }
  const commitName = () => {
    const n = draftName.trim();
    if (n && n !== name && !names.includes(n)) ed.update((d) => renameKey(d, ['extraKnownMarketplaces'], name, n));
    else setDraftName(name);
  };
  return (
    <div className="market-row">
      <TextInput className="market-name" mono value={draftName} onChange={(e) => setDraftName(e.target.value)} onBlur={commitName} onKeyDown={(e) => e.key === 'Enter' && commitName()} />
      <div className="market-type">
        <Select
          value={def!.s}
          options={MARKET_SOURCES.map((x) => ({ value: x.s, label: x.s }))}
          onChange={(t) => {
            const nd = MARKET_SOURCES.find((x) => x.s === t)!;
            const value = (src![def!.field] as string) ?? '';
            ed.write([...base, 'source'], { source: t, [nd.field]: value });
          }}
        />
      </div>
      <TextInput mono className="grow" value={(src![def!.field] as string) ?? ''} placeholder={def!.ph} onChange={(e) => ed.write([...base, 'source', def!.field], e.target.value)} />
      <label className="market-auto" title={L('自动更新', 'Auto-update')}>
        <Toggle
          checked={(m as JsonObject).autoUpdate === true}
          label={L('自动更新', 'Auto-update')}
          onChange={(on) => (on ? ed.write([...base, 'autoUpdate'], true) : has(ed.original, [...base, 'autoUpdate']) ? ed.write([...base, 'autoUpdate'], false) : ed.remove([...base, 'autoUpdate']))}
        />
        <span>{L('自动更新', 'Auto-update')}</span>
      </label>
      <IconButton icon="x" size={15} label={L('删除', 'Remove')} onClick={() => ed.remove(base)} />
    </div>
  );
}

/** EnvEditor: the shared env — every key but the provider's, which belongs to credentials. */
export function EnvEditor() {
  const { L } = useLang();
  const ed = useEditor();
  const v = ed.value(['env']);
  const [key, setKey] = useState('');
  const [val, setVal] = useState('');
  const [error, setError] = useState('');
  if (v !== undefined && !isObj(v)) return <Locked value={v} />;
  const entries = isObj(v) ? Object.entries(v) : [];
  const add = () => {
    const k = key.trim();
    if (!k) return;
    if (isProviderKey(k)) {
      setError(L(`${k} 属于供应商，请在凭据里设置`, `${k} belongs to the provider: set it on a credential`));
      return;
    }
    if (isObj(v) && k in v) {
      setError(L(`${k} 已经有了`, `${k} is there already`));
      return;
    }
    ed.write(['env', k], val);
    setKey('');
    setVal('');
    setError('');
  };
  return (
    <div className="map-editor">
      <div className="map-hint">
        {L('供应商相关的变量（ANTHROPIC_* 等）属于凭据，不在这里。', 'Provider keys (ANTHROPIC_* and the like) belong to credentials, not here.')}
      </div>
      {entries.map(([k, x]) => (
        <div key={k} className="map-row">
          <span className="mono env-key ellipsis" title={k}>
            {k}
          </span>
          {typeof x === 'string' ? (
            <TextInput mono className="grow" value={x} onChange={(e) => ed.write(['env', k], e.target.value)} />
          ) : (
            <Locked value={x} />
          )}
          {looksSecret(k) && (
            <Badge tone="warn" icon="alert">
              {L('像秘密，同步会拒绝', 'looks secret: sync refuses it')}
            </Badge>
          )}
          <IconButton icon="x" size={15} label={L('删除', 'Remove')} onClick={() => ed.remove(['env', k])} />
        </div>
      ))}
      <div className="map-add">
        <TextInput mono className="env-key-input" value={key} placeholder="NAME" onChange={(e) => setKey(e.target.value.toUpperCase())} onKeyDown={(e) => e.key === 'Enter' && add()} />
        <TextInput mono className="grow" value={val} placeholder={L('值', 'value')} onChange={(e) => setVal(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && add()} />
        <Button icon="plus" disabled={!key.trim()} onClick={add}>
          {L('添加', 'Add')}
        </Button>
      </div>
      {error && <div className="field-error">{error}</div>}
    </div>
  );
}

/**
 * HooksEditor follows the file's own shape: event → matcher groups →
 * handlers. A tree it cannot read whole locks the editor; a handler that is
 * not a command shows as JSON.
 */
export function HooksEditor() {
  const { L } = useLang();
  const ed = useEditor();
  const hooks = ed.value(['hooks']);
  const [event, setEvent] = useState('PreToolUse');
  const readable =
    hooks === undefined ||
    (isObj(hooks) &&
      Object.values(hooks).every(
        (groups) =>
          Array.isArray(groups) &&
          groups.every((g) => isObj(g) && Array.isArray(g.hooks) && g.hooks.every(isObj) && (g.matcher === undefined || typeof g.matcher === 'string')),
      ));
  if (!readable) return <Locked value={hooks} />;
  const events = isObj(hooks) ? Object.entries(hooks as Record<string, JsonObject[]>) : [];
  const addHook = () => {
    const groups = (get(ed.doc, ['hooks', event]) as Json[] | undefined) ?? [];
    ed.write(['hooks', event, groups.length], { hooks: [{ type: 'command', command: '' }] });
  };
  return (
    <div className="hooks">
      <div className="map-hint">
        {L('Hook 是 Claude Code 在生命周期事件上执行的 shell 命令。只有退出码 2 会拦截。', 'A hook is a shell command Claude Code runs at a lifecycle event. Only exit code 2 blocks.')}
      </div>
      {events.length === 0 && <div className="faint hooks-none">{L('还没有 hook', 'No hooks yet')}</div>}
      {events.map(([ev, groups]) => (
        <div key={ev} className="hook-event">
          <div className="hook-event-name">
            <Icon name="bolt" size={14} />
            {ev}
          </div>
          {groups.map((g, gi) => (
            <HookGroup key={gi} ev={ev} gi={gi} g={g} groupCount={groups.length} />
          ))}
        </div>
      ))}
      <div className="map-add">
        <div className="hook-event-select">
          <Select value={event} options={HOOK_EVENTS.map((e) => ({ value: e.n, label: e.n }))} onChange={setEvent} mono />
        </div>
        <Button icon="plus" onClick={addHook}>
          {L('添加 hook', 'Add hook')}
        </Button>
      </div>
    </div>
  );
}

function HookGroup({ ev, gi, g, groupCount }: { ev: string; gi: number; g: JsonObject; groupCount: number }) {
  const { L } = useLang();
  const ed = useEditor();
  const base: Path = ['hooks', ev, gi];
  const sug = HOOK_EVENTS.find((e) => e.n === ev)?.sug;
  const handlers = g.hooks as JsonObject[];
  return (
    <div className="hook-group card">
      {(sug || g.matcher !== undefined) && (
        <div className="hook-matcher">
          <span className="faint">matcher</span>
          <TextInput
            mono
            value={typeof g.matcher === 'string' ? g.matcher : ''}
            placeholder={L('留空或 * 匹配全部', 'blank or * matches all')}
            onChange={(e) => (e.target.value === '' && !has(ed.original, [...base, 'matcher']) ? ed.remove([...base, 'matcher']) : ed.write([...base, 'matcher'], e.target.value))}
            list={sug ? `sug-${ev}` : undefined}
          />
          {sug && (
            <datalist id={`sug-${ev}`}>
              {sug.map((s) => (
                <option key={s} value={s} />
              ))}
            </datalist>
          )}
        </div>
      )}
      {handlers.map((h, hi) => {
        const commandLike = h.type === 'command' || (h.type === undefined && typeof h.command === 'string');
        const p: Path = [...base, 'hooks', hi];
        // The last handler takes its group along, and the last group its event.
        const removeHandler = () => ed.remove(handlers.length > 1 ? p : groupCount > 1 ? base : ['hooks', ev]);
        if (!commandLike || (h.command !== undefined && typeof h.command !== 'string')) {
          return (
            <div key={hi} className="hook-handler">
              <Locked value={h} />
              <IconButton icon="x" size={15} label={L('删除', 'Remove')} onClick={removeHandler} />
            </div>
          );
        }
        return (
          <div key={hi} className="hook-handler">
            <Icon name="terminal" size={15} className="faint" />
            <TextArea
              mono
              className="grow hook-command"
              rows={Math.min(8, Math.max(1, Math.ceil((((h.command as string) ?? '').length + 1) / 90)))}
              value={(h.command as string) ?? ''}
              placeholder={L('shell 命令', 'shell command')}
              onChange={(e) => ed.write([...p, 'command'], e.target.value)}
            />
            <TextInput
              className="hook-timeout"
              inputMode="numeric"
              value={typeof h.timeout === 'number' ? String(h.timeout) : ''}
              placeholder={L('超时 s', 'timeout s')}
              onChange={(e) => {
                const t = e.target.value.trim();
                if (t === '') ed.remove([...p, 'timeout']);
                else if (/^\d+$/.test(t)) ed.write([...p, 'timeout'], Number(t));
              }}
            />
            <IconButton icon="x" size={15} label={L('删除', 'Remove')} onClick={removeHandler} />
          </div>
        );
      })}
    </div>
  );
}
