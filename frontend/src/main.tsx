import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './App';
import './styles.css';

const root = document.getElementById('root')!;

if (typeof JSMpeg === 'undefined') {
  root.textContent = 'Failed to load the video decoder (vendor/jsmpeg.min.js).';
} else {
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
