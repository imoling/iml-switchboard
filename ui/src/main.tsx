import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import './styles.css';
import { ThemeProvider } from './theme/ThemeContext';
import { windowLabel } from './tauri';
import { About } from './windows/About';
import { Console } from './windows/Console';
import { Notch } from './windows/Notch';
import { Popover } from './windows/Popover';
import { Quick } from './windows/Quick';

const label = windowLabel();
if (label === 'popover' || label === 'quick' || label === 'notch') document.body.classList.add('transparent');

const Root = { main: Console, popover: Popover, quick: Quick, about: About, notch: Notch }[label];

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider>
      <Root />
    </ThemeProvider>
  </StrictMode>,
);
