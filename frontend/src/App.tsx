import { useCallback } from 'react';
import { AddStreamForm } from './components/AddStreamForm';
import { PauseIcon, PlayIcon, VideoIcon } from './components/Icons';
import { StreamTile } from './components/StreamTile';
import { API_BASE, MAX_STREAMS } from './config';
import { useBackend } from './hooks/useBackend';
import { useLayout, useStreams } from './hooks/useStreams';
import type { GridLayout } from './types';

const LAYOUTS: { value: GridLayout; label: string }[] = [
  { value: 'auto', label: 'Auto' },
  { value: 1, label: '1' },
  { value: 2, label: '2' },
  { value: 3, label: '3' },
  { value: 4, label: '4' },
];

export default function App() {
  const { streams, add, remove, setPaused, setAllPaused } = useStreams();
  const [layout, setLayout] = useLayout();
  const { presets, health } = useBackend();

  const addLimited = useCallback(
    (url: string, name: string) => {
      if (streams.length < MAX_STREAMS) add(url, name);
    },
    [add, streams.length],
  );

  const anyPlaying = streams.some((s) => !s.paused);

  return (
    <div className="app">
      <header className="app-header">
        <div className="brand">
          <span className="brand-mark" aria-hidden>
            <VideoIcon width={18} height={18} />
          </span>
          <div>
            <h1>RTSP Stream Viewer</h1>
            <p className="brand-sub">Live camera feeds in your browser</p>
          </div>
        </div>
        <div className={`health health-${health}`} title={API_BASE}>
          <span className="dot" aria-hidden />
          {health === 'online' ? 'Server online' : health === 'offline' ? 'Server unreachable' : 'Checking server…'}
        </div>
      </header>

      {health === 'offline' && (
        <div className="banner" role="alert">
          Can't reach the streaming server at <code>{API_BASE}</code>. If it is hosted on a free tier it
          may be waking up; this can take up to a minute. Streams will reconnect automatically.
        </div>
      )}

      <AddStreamForm streams={streams} presets={presets} limit={MAX_STREAMS} onAdd={addLimited} />

      {streams.length > 0 && (
        <div className="toolbar">
          <span className="count">
            {streams.length} / {MAX_STREAMS} streams
          </span>
          <div className="toolbar-right">
            <div className="segmented" role="radiogroup" aria-label="Grid columns">
              {LAYOUTS.map((l) => (
                <button
                  key={l.value}
                  type="button"
                  role="radio"
                  aria-checked={layout === l.value}
                  className={layout === l.value ? 'active' : ''}
                  onClick={() => setLayout(l.value)}
                  title={l.value === 'auto' ? 'Fit to screen' : `${l.value} column${l.value > 1 ? 's' : ''}`}
                >
                  {l.label}
                </button>
              ))}
            </div>
            <button type="button" className="btn" onClick={() => setAllPaused(anyPlaying)}>
              {anyPlaying ? <PauseIcon /> : <PlayIcon />}
              {anyPlaying ? 'Pause all' : 'Play all'}
            </button>
          </div>
        </div>
      )}

      {streams.length === 0 ? (
        <section className="empty">
          <div className="empty-art" aria-hidden>
            <VideoIcon width={40} height={40} />
          </div>
          <h2>No streams yet</h2>
          <p>
            Paste an RTSP URL above{presets.length > 0 ? ' or pick one of the demo streams' : ''} to start
            watching. Add several to see them side by side.
          </p>
        </section>
      ) : (
        <main className={`grid grid-${layout}`}>
          {streams.map((s) => (
            <StreamTile key={s.id} stream={s} onPausedChange={setPaused} onRemove={remove} />
          ))}
        </main>
      )}

      <footer className="app-footer">
        Video is transcoded server-side by FFmpeg and delivered over WebSockets. Double-click a stream for
        fullscreen.
      </footer>
    </div>
  );
}
