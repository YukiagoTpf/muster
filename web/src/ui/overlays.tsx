import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import { usePresence } from '../lib/hooks';
import { useLang } from '../lib/i18n';
import { Ripple } from '../motion/Ripple';
import { Button, IconButton } from './controls';
import { Icon, type IconName } from './Icon';

/** Dialog: the backdrop fades and the panel settles in from just above. */
export function Dialog({
  open,
  onClose,
  title,
  children,
  footer,
  width = 520,
  dismissable = true,
}: {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  width?: number;
  dismissable?: boolean;
}) {
  const { mounted, shown } = usePresence(open, 220);
  const panel = useRef<HTMLDivElement>(null);
  const { L } = useLang();
  useEffect(() => {
    if (!open) return;
    const before = document.activeElement as HTMLElement | null;
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && dismissable) onClose();
    };
    window.addEventListener('keydown', key);
    const t = setTimeout(() => {
      panel.current?.querySelector<HTMLElement>('input:not([type=hidden]), textarea, select')?.focus();
    }, 60);
    return () => {
      window.removeEventListener('keydown', key);
      clearTimeout(t);
      before?.focus?.();
    };
  }, [open, onClose, dismissable]);
  if (!mounted) return null;
  return createPortal(
    <div className={'dialog-layer' + (shown ? ' shown' : '')}>
      <div className="dialog-backdrop" onClick={() => dismissable && onClose()} />
      <div ref={panel} className="dialog" role="dialog" aria-modal style={{ width }}>
        <div className="dialog-head">
          <div className="dialog-title">{title}</div>
          <IconButton icon="x" label={L('关闭', 'Close')} onClick={onClose} disabled={!dismissable} />
        </div>
        <div className="dialog-body">{children}</div>
        {footer && <div className="dialog-foot">{footer}</div>}
      </div>
    </div>,
    document.body,
  );
}

export interface MenuItem {
  label: string;
  icon?: IconName;
  danger?: boolean;
  checked?: boolean;
  disabled?: boolean;
  onSelect: () => void;
}

/**
 * Popover opens anchored to a trigger, scaling out of the corner it is
 * attached to; it closes on outside press, Escape, or a choice.
 */
function Popover({
  anchor,
  open,
  onClose,
  align = 'end',
  minWidth,
  children,
}: {
  anchor: HTMLElement | null;
  open: boolean;
  onClose: () => void;
  align?: 'start' | 'end';
  minWidth?: number;
  children: ReactNode;
}) {
  const { mounted, shown } = usePresence(open, 200);
  const box = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ top: number; left?: number; right?: number; up: boolean }>({ top: 0, up: false });
  useLayoutEffect(() => {
    if (!mounted || !anchor) return;
    const r = anchor.getBoundingClientRect();
    const h = box.current?.offsetHeight ?? 0;
    const up = r.bottom + h + 12 > window.innerHeight && r.top > h + 12;
    const top = up ? r.top - h - 6 : r.bottom + 6;
    setPos(align === 'end' ? { top, right: window.innerWidth - r.right, up } : { top, left: r.left, up });
  }, [mounted, anchor, align]);
  useEffect(() => {
    if (!open) return;
    const down = (e: PointerEvent) => {
      if (!box.current?.contains(e.target as Node) && !anchor?.contains(e.target as Node)) onClose();
    };
    const key = (e: KeyboardEvent) => e.key === 'Escape' && onClose();
    const away = () => onClose();
    window.addEventListener('pointerdown', down, true);
    window.addEventListener('keydown', key);
    window.addEventListener('resize', away);
    return () => {
      window.removeEventListener('pointerdown', down, true);
      window.removeEventListener('keydown', key);
      window.removeEventListener('resize', away);
    };
  }, [open, onClose, anchor]);
  if (!mounted) return null;
  const origin = `${pos.up ? 'bottom' : 'top'} ${align === 'end' ? 'right' : 'left'}`;
  return createPortal(
    <div
      ref={box}
      className={'popover' + (shown ? ' shown' : '')}
      style={{ top: pos.top, left: pos.left, right: pos.right, transformOrigin: origin, minWidth: minWidth ?? anchor?.offsetWidth }}
    >
      {children}
    </div>,
    document.body,
  );
}

export function Menu({
  trigger,
  items,
  align = 'end',
}: {
  trigger: (props: { ref: (el: HTMLElement | null) => void; onClick: () => void; 'aria-expanded': boolean }) => ReactNode;
  items: MenuItem[];
  align?: 'start' | 'end';
}) {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const close = useCallback(() => setOpen(false), []);
  return (
    <>
      {trigger({ ref: setAnchor, onClick: () => setOpen((o) => !o), 'aria-expanded': open })}
      <Popover anchor={anchor} open={open} onClose={close} align={align} minWidth={180}>
        <div className="menu" role="menu">
          {items.map((it) => (
            <button
              key={it.label}
              type="button"
              role="menuitem"
              className={'menu-item' + (it.danger ? ' danger' : '')}
              disabled={it.disabled}
              onClick={() => {
                setOpen(false);
                it.onSelect();
              }}
            >
              <Ripple />
              <span className="menu-icon">{it.icon ? <Icon name={it.icon} size={17} /> : null}</span>
              <span className="menu-label">{it.label}</span>
              {it.checked && <Icon name="check" size={16} className="menu-check" />}
            </button>
          ))}
        </div>
      </Popover>
    </>
  );
}

export interface SelectOption {
  value: string;
  label: string;
  hint?: string;
}

/** Select is a dropdown in the menu's style. */
export function Select({
  value,
  options,
  onChange,
  placeholder,
  disabled,
  mono,
}: {
  value: string;
  options: SelectOption[];
  onChange: (v: string) => void;
  placeholder?: string;
  disabled?: boolean;
  mono?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null);
  const close = useCallback(() => setOpen(false), []);
  const current = options.find((o) => o.value === value);
  return (
    <>
      <button
        ref={setAnchor}
        type="button"
        className={'select' + (open ? ' open' : '') + (mono ? ' mono' : '')}
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
      >
        <span className={current ? '' : 'select-placeholder'}>{current?.label ?? value ?? placeholder}</span>
        <Icon name="chevronDown" size={16} className="select-chevron" />
      </button>
      <Popover anchor={anchor} open={open} onClose={close} align="start">
        <div className="menu menu-scroll" role="listbox">
          {options.map((o) => (
            <button
              key={o.value}
              type="button"
              role="option"
              aria-selected={o.value === value}
              className={'menu-item' + (o.value === value ? ' selected' : '')}
              onClick={() => {
                setOpen(false);
                onChange(o.value);
              }}
            >
              <Ripple />
              <span className={'menu-label' + (mono ? ' mono' : '')}>
                {o.label}
                {o.hint && <span className="menu-hint">{o.hint}</span>}
              </span>
              {o.value === value && <Icon name="check" size={16} className="menu-check" />}
            </button>
          ))}
        </div>
      </Popover>
    </>
  );
}

// Toasts drop in from the top and leave on their own.

interface ToastItem {
  id: number;
  tone: 'ok' | 'error' | 'info';
  text: string;
  leaving?: boolean;
}

const ToastCtx = createContext<(tone: ToastItem['tone'], text: string) => void>(() => {});

let toastSeq = 0;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const push = useCallback((tone: ToastItem['tone'], text: string) => {
    const id = ++toastSeq;
    setItems((xs) => [...xs.slice(-3), { id, tone, text }]);
    const ttl = tone === 'error' ? 6000 : 3200;
    setTimeout(() => setItems((xs) => xs.map((x) => (x.id === id ? { ...x, leaving: true } : x))), ttl);
    setTimeout(() => setItems((xs) => xs.filter((x) => x.id !== id)), ttl + 260);
  }, []);
  const icons: Record<ToastItem['tone'], IconName> = { ok: 'check', error: 'alert', info: 'info' };
  return (
    <ToastCtx.Provider value={push}>
      {children}
      {createPortal(
        <div className="toasts" aria-live="polite">
          {items.map((t) => (
            <div key={t.id} className={`toast toast-${t.tone}` + (t.leaving ? ' leaving' : '')}>
              <span className="toast-icon">
                <Icon name={icons[t.tone]} size={16} strokeWidth={2.2} />
              </span>
              <span>{t.text}</span>
            </div>
          ))}
        </div>,
        document.body,
      )}
    </ToastCtx.Provider>
  );
}

export function useToast() {
  return useContext(ToastCtx);
}

/** useConfirm shows a yes/no dialog and resolves with the answer. */
export function useConfirm(): [ReactNode, (o: { title: string; body?: ReactNode; confirm: string; danger?: boolean }) => Promise<boolean>] {
  const { L } = useLang();
  const [state, setState] = useState<{
    title: string;
    body?: ReactNode;
    confirm: string;
    danger?: boolean;
    resolve: (v: boolean) => void;
  } | null>(null);
  const [open, setOpen] = useState(false);
  const ask = useCallback(
    (o: { title: string; body?: ReactNode; confirm: string; danger?: boolean }) =>
      new Promise<boolean>((resolve) => {
        setState({ ...o, resolve });
        setOpen(true);
      }),
    [],
  );
  const answer = (v: boolean) => {
    state?.resolve(v);
    setOpen(false);
  };
  const node = (
    <Dialog
      open={open}
      onClose={() => answer(false)}
      title={state?.title}
      width={420}
      footer={
        <>
          <Button variant="ghost" onClick={() => answer(false)}>
            {L('取消', 'Cancel')}
          </Button>
          <Button variant={state?.danger ? 'danger' : 'primary'} onClick={() => answer(true)}>
            {state?.confirm}
          </Button>
        </>
      }
    >
      {state?.body && <div className="confirm-body">{state.body}</div>}
    </Dialog>
  );
  return [node, ask];
}
