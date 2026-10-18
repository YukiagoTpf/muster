import { useLayoutEffect, useRef, useState, type ReactNode } from 'react';

interface Layer {
  key: string;
  node: ReactNode;
  phase: 'idle' | 'enter' | 'leave';
}

/**
 * PageTransition slides between pages: the leaving page drifts away and
 * fades while the entering one slides in from the side it lies on (`forward`
 * means further down the navigation).
 */
export function PageTransition({
  pageKey,
  forward,
  children,
  duration = 300,
}: {
  pageKey: string;
  forward: boolean;
  children: ReactNode;
  duration?: number;
}) {
  const [layers, setLayers] = useState<Layer[]>([{ key: pageKey, node: children, phase: 'idle' }]);
  const current = useRef(pageKey);
  const timer = useRef<number>(0);

  useLayoutEffect(() => {
    if (pageKey === current.current) {
      // Same page: keep its content current.
      setLayers((ls) => ls.map((l) => (l.key === pageKey ? { ...l, node: children } : l)));
      return;
    }
    current.current = pageKey;
    setLayers((ls) => {
      const leaving = ls.filter((l) => l.phase !== 'leave' && l.key !== pageKey).map((l) => ({ ...l, phase: 'leave' as const }));
      return [...leaving, { key: pageKey, node: children, phase: 'enter' }];
    });
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      setLayers((ls) => ls.filter((l) => l.phase !== 'leave').map((l) => ({ ...l, phase: 'idle' })));
    }, duration);
  }, [pageKey, children, duration]);

  return (
    <div className="pages">
      {layers.map((l) => (
        <div key={l.key} className="page" data-phase={l.phase} data-dir={forward ? 'forward' : 'back'} inert={l.phase === 'leave' || undefined}>
          {l.node}
        </div>
      ))}
    </div>
  );
}
