import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './app/App';
import { takeToken } from './lib/api';
import { LangProvider } from './lib/i18n';
import { ToastProvider } from './ui/overlays';
import './styles/tokens.css';
import './styles/base.css';
import './styles/ui.css';
import './styles/app.css';
import './styles/pages.css';
import './styles/settings.css';

takeToken();

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <LangProvider>
      <ToastProvider>
        <App />
      </ToastProvider>
    </LangProvider>
  </StrictMode>,
);
