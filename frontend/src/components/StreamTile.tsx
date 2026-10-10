import { memo, useEffect, useRef, useState } from 'react';
import { StreamSession } from '../lib/StreamSession';
import { displayUrl } from '../lib/rtspUrl';
import type { StreamItem, StreamSnapshot } from '../types';
import { CameraIcon, CloseIcon, ExpandIcon, PauseIcon, PlayIcon, RetryIcon } from './Icons';

interface Props {
  stream: StreamItem;
  onPausedChange: (id: string, paused: boolean) => void;
  onRemove: (id: string) => void;
}

const INITIAL: StreamSnapshot = {
  state: 'connecting',
  viewers: 0,
  kbps: 0,
  width: 0,
  height: 0,
  stalled: false,
};

const STATE_LABEL: Record<StreamSnapshot['state'], string> = {
  connecting: 'Connecting',
  live: 'Live',
  reconnecting: 'Reconnecting',
  paused: 'Paused',
  error: 'Error',
};

function StreamTileImpl({ stream, onPausedChange, onRemove }: Props) {
  const tileRef = useRef<HTMLElement>(null);
  const screenRef = useRef<HTMLDivElement>(null);
  const sessionRef = useRef<StreamSession | null>(null);
  const [snap, setSnap] = useState<StreamSnapshot>(INITIAL);

  // One session per URL for the lifetime of the tile.
  useEffect(() => {
    const session = new StreamSession(screenRef.current!, stream.url, setSnap);
    sessionRef.current = session;
    return () => {
      session.destroy();
      sessionRef.current = null;
    };
  }, [stream.url]);

  // Play/pause follows the persisted flag (so "pause all" and reloads work).
  useEffect(() => {
    const session = sessionRef.current;
    if (!session) return;
    if (stream.paused) session.pause();
    else session.play();
  }, [stream.paused, stream.url]);

  const toggleFullscreen = () => {
    const el = tileRef.current;
    if (!el) return;
    if (document.fullscreenElement) void document.exitFullscreen();
    else void el.requestFullscreen?.();
  };

  const saveSnapshot = () => {
    const data = sessionRef.current?.snapshotImage();
    if (!data) return;
    const a = document.createElement('a');
    a.href = data;
    a.download = `${stream.name.replace(/[^\w-]+/g, '_')}-${new Date().toISOString().replace(/[:.]/g, '-')}.png`;
    a.click();
  };

  const { state, message, stalled } = snap;
  const busy = state === 'connecting' || state === 'reconnecting';
  const canSnapshot = snap.width > 0;

  return (
    <article
      ref={tileRef}
      className={`tile tile-${state}`}
      aria-label={`${stream.name} — ${STATE_LABEL[state]}`}
      onDoubleClick={toggleFullscreen}
    >
      <div className="screen" ref={screenRef}>
        {/* StreamSession injects the <canvas> and the paused still here. */}
      </div>

      {(busy || state === 'error' || (state === 'live' && stalled)) && (
        <div className={`overlay ${state === 'error' ? 'overlay-error' : ''}`} role="status">
          {busy && <span className="spinner" aria-hidden />}
          <strong>
            {state === 'error'
              ? 'Stream unavailable'
              : state === 'live'
                ? 'Waiting for video…'
                : STATE_LABEL[state] + '…'}
          </strong>
          {message && state !== 'live' && <span className="overlay-msg">{message}</span>}
          {state === 'error' && (
            <button
              type="button"
              className="btn btn-small"
              onClick={() =>
                stream.paused ? onPausedChange(stream.id, false) : sessionRef.current?.retry()
              }
            >
              <RetryIcon /> Retry
            </button>
          )}
        </div>
      )}

      {state === 'paused' && (
        <button
          type="button"
          className="overlay overlay-paused"
          onClick={() => onPausedChange(stream.id, false)}
          aria-label={`Resume ${stream.name}`}
        >
          <span className="big-play">
            <PlayIcon width={28} height={28} />
          </span>
        </button>
      )}

      <header className="tile-top">
        <span className={`badge badge-${state}`}>
          <span className="dot" aria-hidden />
          {STATE_LABEL[state]}
        </span>
        <h2 className="tile-name" title={displayUrl(stream.url)}>
          {stream.name}
        </h2>
      </header>

      <footer className="tile-bar">
        <div className="tile-controls">
          {stream.paused ? (
            <button type="button" className="icon-btn" onClick={() => onPausedChange(stream.id, false)} aria-label="Play" title="Play">
              <PlayIcon />
            </button>
          ) : (
            <button type="button" className="icon-btn" onClick={() => onPausedChange(stream.id, true)} aria-label="Pause" title="Pause">
              <PauseIcon />
            </button>
          )}
          <button type="button" className="icon-btn" onClick={saveSnapshot} disabled={!canSnapshot} aria-label="Save snapshot" title="Save snapshot">
            <CameraIcon />
          </button>
          <button type="button" className="icon-btn" onClick={toggleFullscreen} aria-label="Fullscreen" title="Fullscreen (or double-click)">
            <ExpandIcon />
          </button>
        </div>
        <div className="tile-stats" aria-label="Stream statistics">
          {snap.width > 0 && <span>{snap.width}×{snap.height}</span>}
          {state === 'live' && <span>{snap.kbps} kb/s</span>}
          {snap.viewers > 1 && state !== 'paused' && <span>{snap.viewers} viewers</span>}
        </div>
        <button type="button" className="icon-btn icon-btn-danger" onClick={() => onRemove(stream.id)} aria-label={`Remove ${stream.name}`} title="Remove">
          <CloseIcon />
        </button>
      </footer>
    </article>
  );
}

export const StreamTile = memo(StreamTileImpl);
