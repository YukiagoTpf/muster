import {
  forwardRef,
  useLayoutEffect,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type TextareaHTMLAttributes,
} from 'react';
import { Ripple } from '../motion/Ripple';
import { Icon, type IconName } from './Icon';

type Variant = 'primary' | 'secondary' | 'ghost' | 'danger';

export const Button = forwardRef<
  HTMLButtonElement,
  ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; icon?: IconName; busy?: boolean; size?: 'sm' | 'md' }
>(function Button({ variant = 'secondary', icon, busy, size = 'md', children, className, disabled, ...rest }, ref) {
  return (
    <button
      ref={ref}
      type="button"
      className={`btn btn-${variant} btn-${size} ${busy ? 'is-busy' : ''} ${className ?? ''}`}
      disabled={disabled || busy}
      {...rest}
    >
      <Ripple light={variant === 'primary' || variant === 'danger'} />
      <span className="btn-icon-slot">
        {busy ? <Spinner size={size === 'sm' ? 14 : 16} /> : icon ? <Icon name={icon} size={size === 'sm' ? 15 : 17} /> : null}
      </span>
      {children && <span className="btn-label">{children}</span>}
    </button>
  );
});

export const IconButton = forwardRef<
  HTMLButtonElement,
  ButtonHTMLAttributes<HTMLButtonElement> & { icon: IconName; label: string; busy?: boolean; size?: number }
>(function IconButton({ icon, label, busy, size = 18, className, disabled, ...rest }, ref) {
  return (
    <button
      ref={ref}
      type="button"
      className={`icon-btn ${className ?? ''}`}
      aria-label={label}
      title={label}
      disabled={disabled || busy}
      {...rest}
    >
      <Ripple />
      {busy ? <Spinner size={size - 2} /> : <Icon name={icon} size={size} />}
    </button>
  );
});

/** Spinner is a rotating arc that breathes, as a material spinner does. */
export function Spinner({ size = 18 }: { size?: number }) {
  return (
    <svg className="spinner" width={size} height={size} viewBox="0 0 24 24" aria-hidden>
      <circle cx="12" cy="12" r="9" />
    </svg>
  );
}

export function Badge({
  tone = 'neutral',
  children,
  icon,
}: {
  tone?: 'neutral' | 'accent' | 'ok' | 'warn' | 'danger';
  children: ReactNode;
  icon?: IconName;
}) {
  return (
    <span className={`badge badge-${tone}`}>
      {icon && <Icon name={icon} size={12} strokeWidth={2.2} />}
      {children}
    </span>
  );
}

/** Toggle is an on/off switch whose knob slides. */
export function Toggle({
  checked,
  onChange,
  disabled,
  label,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
  label?: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      className={'toggle' + (checked ? ' on' : '')}
      disabled={disabled}
      onClick={() => onChange(!checked)}
    >
      <span className="toggle-knob" />
    </button>
  );
}

export interface SegmentOption<T extends string> {
  value: T;
  label: ReactNode;
}

/** Segmented is a row of options with an indicator that slides to the chosen one. */
export function Segmented<T extends string>({
  options,
  value,
  onChange,
  disabled,
  size = 'md',
}: {
  options: SegmentOption<T>[];
  value: T;
  onChange: (v: T) => void;
  disabled?: boolean;
  size?: 'sm' | 'md';
}) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const [box, setBox] = useState<{ left: number; width: number } | null>(null);
  const index = options.findIndex((o) => o.value === value);
  useLayoutEffect(() => {
    const el = refs.current[index];
    setBox(el ? { left: el.offsetLeft, width: el.offsetWidth } : null);
  }, [index, options.length]);
  return (
    <div className={`segmented segmented-${size}`} role="radiogroup" aria-disabled={disabled}>
      {box && <span className="segmented-thumb" style={{ transform: `translateX(${box.left}px)`, width: box.width }} />}
      {options.map((o, i) => (
        <button
          key={o.value}
          ref={(el) => {
            refs.current[i] = el;
          }}
          type="button"
          role="radio"
          aria-checked={o.value === value}
          className={'segmented-option' + (o.value === value ? ' active' : '')}
          disabled={disabled}
          onClick={() => onChange(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** Bar is a quota bar: it grows to its value and takes the tone of how full it is. */
export function Bar({ value, tone }: { value: number; tone?: 'auto' | 'accent' }) {
  const [shown, setShown] = useState(0);
  useLayoutEffect(() => {
    const id = requestAnimationFrame(() => setShown(Math.min(1, Math.max(0, value))));
    return () => cancelAnimationFrame(id);
  }, [value]);
  const level = tone === 'accent' ? 'accent' : value >= 0.85 ? 'danger' : value >= 0.6 ? 'warn' : 'accent';
  return (
    <div className="bar" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(value * 100)}>
      <div className={'bar-fill tone-' + level} style={{ transform: `scaleX(${shown})` }} />
    </div>
  );
}

export const TextInput = forwardRef<
  HTMLInputElement,
  InputHTMLAttributes<HTMLInputElement> & { mono?: boolean; invalid?: boolean }
>(function TextInput({ mono, invalid, className, ...rest }, ref) {
  return (
    <input
      ref={ref}
      className={`input ${mono ? 'mono' : ''} ${invalid ? 'invalid' : ''} ${className ?? ''}`}
      spellCheck={false}
      autoComplete="off"
      {...rest}
    />
  );
});

export const TextArea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement> & { mono?: boolean }>(
  function TextArea({ mono, className, ...rest }, ref) {
    return <textarea ref={ref} className={`input textarea ${mono ? 'mono' : ''} ${className ?? ''}`} spellCheck={false} {...rest} />;
  },
);

export function Field({
  label,
  hint,
  error,
  children,
  aside,
}: {
  label: ReactNode;
  hint?: ReactNode;
  error?: string;
  children: ReactNode;
  aside?: ReactNode;
}) {
  return (
    <label className="field">
      <span className="field-label">
        {label}
        {aside && <span className="field-aside">{aside}</span>}
      </span>
      {children}
      {error ? <span className="field-error">{error}</span> : hint ? <span className="field-hint">{hint}</span> : null}
    </label>
  );
}

export function Skeleton({ width, height = 14, round }: { width?: number | string; height?: number; round?: boolean }) {
  return <span className={'skeleton' + (round ? ' round' : '')} style={{ width, height }} />;
}

export function Empty({ icon, title, children }: { icon: IconName; title: string; children?: ReactNode }) {
  return (
    <div className="empty">
      <div className="empty-icon">
        <Icon name={icon} size={28} />
      </div>
      <div className="empty-title">{title}</div>
      {children && <div className="empty-body">{children}</div>}
    </div>
  );
}

export function Notice({
  tone = 'info',
  icon,
  title,
  children,
  action,
}: {
  tone?: 'info' | 'warn' | 'danger' | 'ok';
  icon?: IconName;
  title?: ReactNode;
  children?: ReactNode;
  action?: ReactNode;
}) {
  const fallback: Record<string, IconName> = { info: 'info', warn: 'alert', danger: 'alert', ok: 'check' };
  return (
    <div className={`notice notice-${tone}`}>
      <Icon name={icon ?? fallback[tone]} size={18} className="notice-icon" />
      <div className="notice-body">
        {title && <div className="notice-title">{title}</div>}
        {children && <div className="notice-text">{children}</div>}
      </div>
      {action && <div className="notice-action">{action}</div>}
    </div>
  );
}
