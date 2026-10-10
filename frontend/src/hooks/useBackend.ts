import { useEffect, useState } from 'react';
import { API_BASE } from '../config';
import type { Preset } from '../types';

export type BackendHealth = 'checking' | 'online' | 'offline';

/**
 * Loads demo presets and keeps an eye on backend reachability, so the UI can
 * explain a dead backend (e.g. a sleeping free-tier instance) up front
 * instead of showing every tile as broken.
 */
export function useBackend() {
  const [presets, setPresets] = useState<Preset[]>([]);
  const [health, setHealth] = useState<BackendHealth>('checking');

  useEffect(() => {
    let cancelled = false;
    let timer = 0;

    const check = async () => {
      try {
        const res = await fetch(`${API_BASE}/healthz`, { cache: 'no-store' });
        if (!res.ok) throw new Error(String(res.status));
        if (cancelled) return;
        setHealth('online');
      } catch {
        if (!cancelled) setHealth('offline');
      }
      if (!cancelled) timer = window.setTimeout(check, 15_000);
    };

    const loadPresets = async () => {
      try {
        const res = await fetch(`${API_BASE}/api/presets`);
        if (!res.ok) return;
        const data = (await res.json()) as Preset[];
        if (!cancelled && Array.isArray(data)) setPresets(data);
      } catch {
        /* presets are optional */
      }
    };

    void check();
    void loadPresets();
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, []);

  return { presets, health };
}
