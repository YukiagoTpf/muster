import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react';

// Strings are written in both languages where they are used: L('中文', 'English').
// Keeping the pair together is what keeps them in step.

export type Lang = 'zh' | 'en';

const LANG_KEY = 'muster.lang';

function initialLang(): Lang {
  const saved = localStorage.getItem(LANG_KEY);
  if (saved === 'zh' || saved === 'en') return saved;
  return navigator.language.toLowerCase().startsWith('zh') ? 'zh' : 'en';
}

interface LangContext {
  lang: Lang;
  setLang: (l: Lang) => void;
  L: (zh: string, en: string) => string;
}

const Ctx = createContext<LangContext | null>(null);

export function LangProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(initialLang);
  useEffect(() => {
    document.documentElement.lang = lang === 'zh' ? 'zh-CN' : 'en';
  }, [lang]);
  const setLang = useCallback((l: Lang) => {
    localStorage.setItem(LANG_KEY, l);
    setLangState(l);
  }, []);
  const L = useCallback((zh: string, en: string) => (lang === 'zh' ? zh : en), [lang]);
  return <Ctx.Provider value={{ lang, setLang, L }}>{children}</Ctx.Provider>;
}

export function useLang(): LangContext {
  const c = useContext(Ctx);
  if (!c) throw new Error('useLang outside LangProvider');
  return c;
}
