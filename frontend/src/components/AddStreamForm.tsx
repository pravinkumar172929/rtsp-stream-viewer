import { useId, useState, type FormEvent } from 'react';
import { defaultStreamName, validateRtspUrl } from '../lib/rtspUrl';
import type { Preset, StreamItem } from '../types';
import { PlusIcon } from './Icons';

interface Props {
  streams: StreamItem[];
  presets: Preset[];
  limit: number;
  onAdd: (url: string, name: string) => void;
}

export function AddStreamForm({ streams, presets, limit, onAdd }: Props) {
  const [url, setUrl] = useState('');
  const [name, setName] = useState('');
  const [error, setError] = useState<string | null>(null);
  const urlId = useId();
  const nameId = useId();
  const errorId = useId();

  const full = streams.length >= limit;
  const has = (u: string) => streams.some((s) => s.url.toLowerCase() === u.toLowerCase());

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (full) return setError(`You can watch up to ${limit} streams at once. Remove one first.`);
    const result = validateRtspUrl(url);
    if ('error' in result) return setError(result.error);
    if (has(result.url)) return setError('This stream is already in the grid.');
    onAdd(result.url, name.trim() || defaultStreamName(result.url));
    setUrl('');
    setName('');
    setError(null);
  };

  const unusedPresets = presets.filter((p) => !has(p.url));

  return (
    <section className="add-panel" aria-label="Add a stream">
      <form className="add-form" onSubmit={submit} noValidate>
        <div className="field field-url">
          <label htmlFor={urlId}>RTSP URL</label>
          <input
            id={urlId}
            type="text"
            inputMode="url"
            autoComplete="off"
            spellCheck={false}
            placeholder="rtsp://user:pass@192.168.1.20:554/stream1"
            value={url}
            onChange={(e) => {
              setUrl(e.target.value);
              if (error) setError(null);
            }}
            aria-invalid={!!error}
            aria-describedby={error ? errorId : undefined}
          />
        </div>
        <div className="field field-name">
          <label htmlFor={nameId}>Name (optional)</label>
          <input
            id={nameId}
            type="text"
            autoComplete="off"
            placeholder="Front door"
            maxLength={60}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <button type="submit" className="btn btn-primary" disabled={full}>
          <PlusIcon /> Add stream
        </button>
      </form>

      {error && (
        <p id={errorId} className="form-error" role="alert">
          {error}
        </p>
      )}

      {unusedPresets.length > 0 && (
        <div className="presets">
          <span className="presets-label">Demo streams</span>
          {unusedPresets.map((p) => (
            <button
              key={p.url}
              type="button"
              className="chip"
              disabled={full}
              title={p.url}
              onClick={() => onAdd(p.url, p.name)}
            >
              <PlusIcon width={12} height={12} /> {p.name}
            </button>
          ))}
          {unusedPresets.length > 1 && (
            <button
              type="button"
              className="chip chip-ghost"
              disabled={full}
              onClick={() =>
                unusedPresets.slice(0, limit - streams.length).forEach((p) => onAdd(p.url, p.name))
              }
            >
              Add all
            </button>
          )}
        </div>
      )}
    </section>
  );
}
