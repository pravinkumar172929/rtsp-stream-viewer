import { useCallback, useEffect, useState } from 'react';
import type { GridLayout, StreamItem } from '../types';

const STREAMS_KEY = 'rtsp-viewer:streams:v1';
const LAYOUT_KEY = 'rtsp-viewer:layout:v1';

function read<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    return fallback; // private mode, blocked storage, corrupt JSON
  }
}

function write(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* persistence is a convenience only */
  }
}

// crypto.randomUUID only exists in secure contexts (https, localhost).
function newId(): string {
  return typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

function isStreamItem(v: unknown): v is StreamItem {
  const s = v as StreamItem;
  return !!s && typeof s.id === 'string' && typeof s.url === 'string' && typeof s.name === 'string';
}

/** The user's stream list, persisted in localStorage. */
export function useStreams() {
  const [streams, setStreams] = useState<StreamItem[]>(() => {
    const saved = read<unknown>(STREAMS_KEY, []);
    return Array.isArray(saved)
      ? saved.filter(isStreamItem).map((s) => ({ ...s, paused: !!s.paused }))
      : [];
  });

  useEffect(() => write(STREAMS_KEY, streams), [streams]);

  const add = useCallback((url: string, name: string) => {
    setStreams((prev) => [...prev, { id: newId(), url, name, paused: false }]);
  }, []);

  const remove = useCallback((id: string) => {
    setStreams((prev) => prev.filter((s) => s.id !== id));
  }, []);

  const setPaused = useCallback((id: string, paused: boolean) => {
    setStreams((prev) => prev.map((s) => (s.id === id ? { ...s, paused } : s)));
  }, []);

  const setAllPaused = useCallback((paused: boolean) => {
    setStreams((prev) => prev.map((s) => ({ ...s, paused })));
  }, []);

  const rename = useCallback((id: string, name: string) => {
    setStreams((prev) => prev.map((s) => (s.id === id ? { ...s, name } : s)));
  }, []);

  const clear = useCallback(() => setStreams([]), []);

  return { streams, add, remove, setPaused, setAllPaused, rename, clear };
}

export function useLayout() {
  const [layout, setLayout] = useState<GridLayout>(() => read<GridLayout>(LAYOUT_KEY, 'auto'));
  useEffect(() => write(LAYOUT_KEY, layout), [layout]);
  return [layout, setLayout] as const;
}
