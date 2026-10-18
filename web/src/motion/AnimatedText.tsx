import { useEffect, useRef, useState } from 'react';

interface Roll {
  from: string;
  to: string;
  up: boolean;
  key: number;
}

/**
 * AnimatedText rolls the characters that change: the old one slides out and
 * the new one slides in, upwards when the number grows. Characters line up
 * from the right, as digits do.
 */
export function AnimatedText({ value, className }: { value: string; className?: string }) {
  const last = useRef(value);
  const [roll, setRoll] = useState<Roll>({ from: value, to: value, up: true, key: 0 });
  useEffect(() => {
    if (value === last.current) return;
    const up = numeric(value) >= numeric(last.current);
    setRoll((r) => ({ from: last.current, to: value, up, key: r.key + 1 }));
    last.current = value;
  }, [value]);

  const { from, to, up, key } = roll;
  const width = Math.max(from.length, to.length);
  const a = from.padStart(width, '\u0000');
  const b = to.padStart(width, '\u0000');
  const cells = [];
  for (let i = 0; i < width; i++) {
    const was = a[i];
    const now = b[i];
    if (now === '\u0000' && was === '\u0000') continue;
    if (was === now || key === 0) {
      cells.push(
        <span key={i} className="roll-cell">
          {now === '\u0000' ? '' : now}
        </span>,
      );
      continue;
    }
    cells.push(
      <span key={`${i}-${key}`} className={'roll-cell ' + (up ? 'up' : 'down')}>
        {was !== '\u0000' && <span className="roll-old">{was}</span>}
        {now !== '\u0000' && <span className="roll-new">{now}</span>}
      </span>,
    );
  }
  return (
    <span className={'roll ' + (className ?? '')} aria-label={to}>
      {cells}
    </span>
  );
}

function numeric(s: string): number {
  const n = parseFloat(s.replace(/[^0-9.\-]/g, ''));
  return Number.isNaN(n) ? 0 : n;
}
