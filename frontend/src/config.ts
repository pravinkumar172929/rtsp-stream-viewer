/**
 * Backend location. When VITE_BACKEND_URL is empty the app talks to its own
 * origin: the Vite dev server proxies /api and /ws to the Go backend, and in
 * the single-container deployment the Go backend serves this app itself.
 */
const configured = (import.meta.env.VITE_BACKEND_URL ?? '').trim().replace(/\/+$/, '');

export const API_BASE = configured || window.location.origin;

export const WS_BASE = API_BASE.replace(/^http/i, 'ws');

export function streamSocketUrl(rtspUrl: string): string {
  return `${WS_BASE}/ws/stream?url=${encodeURIComponent(rtspUrl)}`;
}

/**
 * Each tile uses one WebGL context and browsers cap those at ~16 per page,
 * so the grid is limited to stay safely below that.
 */
export const MAX_STREAMS = 12;
