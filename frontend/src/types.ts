export interface StreamItem {
  id: string;
  name: string;
  url: string;
  paused: boolean;
}

export interface Preset {
  name: string;
  url: string;
}

/** Lifecycle of a tile, as shown to the user. */
export type StreamState =
  | 'connecting' // waiting for the backend / camera
  | 'live' // frames are arriving
  | 'reconnecting' // temporary failure, retrying automatically
  | 'paused' // stopped by the user
  | 'error'; // gave up; the user can retry

export interface StreamSnapshot {
  state: StreamState;
  message?: string;
  viewers: number;
  kbps: number;
  width: number;
  height: number;
  /** True when live but no data arrived for a few seconds. */
  stalled: boolean;
}

/** JSON text message sent by the backend over the stream WebSocket. */
export interface ServerStatus {
  type: 'status';
  state: 'connecting' | 'live' | 'reconnecting' | 'error';
  message?: string;
  attempt?: number;
  fatal?: boolean;
  viewers: number;
}

export type GridLayout = 'auto' | 1 | 2 | 3 | 4;
