import { useEffect, useRef, useState } from 'react';

let seq = 0;

interface Wave {
  id: number;
  x: number;
  y: number;
  size: number;
}

/**
 * Ripple draws the press wave of its parent: put it inside any positioned
 * element and every primary press spreads a circle from the pointer.
 */
export function Ripple({ light = false }: { light?: boolean }) {
  const host = useRef<HTMLSpanElement>(null);
  const [waves, setWaves] = useState<Wave[]>([]);
  useEffect(() => {
    const parent = host.current?.parentElement;
    if (!parent) return;
    const down = (e: PointerEvent) => {
      if (e.button !== 0 || parent.matches(':disabled, [aria-disabled="true"]')) return;
      const r = parent.getBoundingClientRect();
      const size = Math.max(r.width, r.height);
      setWaves((w) => [...w, { id: ++seq, x: e.clientX - r.left - size / 2, y: e.clientY - r.top - size / 2, size }]);
    };
    parent.addEventListener('pointerdown', down);
    return () => parent.removeEventListener('pointerdown', down);
  }, []);
  return (
    <span ref={host} className={'ripple-host' + (light ? ' light' : '')} aria-hidden>
      {waves.map((w) => (
        <span
          key={w.id}
          className="ripple"
          style={{ left: w.x, top: w.y, width: w.size, height: w.size }}
          onAnimationEnd={() => setWaves((ws) => ws.filter((x) => x.id !== w.id))}
        />
      ))}
    </span>
  );
}
