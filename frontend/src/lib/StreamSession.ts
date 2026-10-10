import { streamSocketUrl } from '../config';
import type { ServerStatus, StreamSnapshot } from '../types';

/**
 * JSMpeg source fed by us instead of opening its own WebSocket. Owning the
 * socket lets us interleave JSON status messages with binary video and
 * implement our own reconnect policy.
 */
class PushSource implements JSMpeg.Source {
  streaming = true;
  established = false;
  completed = false;
  progress = 0;
  private destination: JSMpeg.Destination | null = null;

  connect(destination: JSMpeg.Destination) {
    this.destination = destination;
  }
  start() {}
  resume() {}
  destroy() {
    this.destination = null;
  }
  write(data: ArrayBuffer) {
    this.established = true;
    this.progress = 1;
    this.destination?.write(data);
  }
}

// Close codes sent by the backend (see backend/internal/server/ws.go).
const CLOSE_SOURCE_FAILED = 4000;
const CLOSE_BAD_REQUEST = 4001;
const CLOSE_SERVER_BUSY = 4002;

const MAX_RECONNECTS = 8;
const STALL_AFTER_MS = 5000;

type Listener = (snapshot: StreamSnapshot) => void;

/**
 * One playing stream: a WebSocket to the backend plus a JSMpeg player that
 * renders into a <canvas> inside `container`. Framework-agnostic; the React
 * tile only calls play/pause/retry/destroy and renders snapshots.
 */
export class StreamSession {
  private socket: WebSocket | null = null;
  private player: JSMpeg.Player | null = null;
  private canvas: HTMLCanvasElement | null = null;
  private poster: HTMLImageElement | null = null;

  private reconnects = 0;
  private reconnectTimer = 0;
  private statsTimer = 0;
  private bytesSinceTick = 0;
  private lastDataAt = 0;
  private lastServerStatus: ServerStatus | null = null;
  private destroyed = false;

  private snapshot: StreamSnapshot = {
    state: 'paused',
    viewers: 0,
    kbps: 0,
    width: 0,
    height: 0,
    stalled: false,
  };

  constructor(
    private readonly container: HTMLElement,
    private readonly rtspUrl: string,
    private readonly onChange: Listener,
  ) {}

  play() {
    if (this.destroyed) return;
    this.reconnects = 0;
    this.connect();
  }

  /**
   * Pausing a live stream means disconnecting: there is nothing to buffer,
   * and it frees bandwidth and server resources. The last frame stays
   * visible as a still image.
   */
  pause() {
    this.teardown();
    this.update({ state: 'paused', message: undefined, kbps: 0, stalled: false });
  }

  retry() {
    this.play();
  }

  destroy() {
    this.destroyed = true;
    this.teardown();
    this.poster?.remove();
    this.poster = null;
  }

  /** PNG of the current frame, or null if nothing has been shown yet. */
  snapshotImage(): string | null {
    if (this.canvas && this.snapshot.width > 0) return this.canvas.toDataURL('image/png');
    return this.poster?.src ?? null;
  }

  private connect() {
    this.teardown();
    this.update(
      this.reconnects > 0
        ? { state: 'reconnecting', stalled: false }
        : { state: 'connecting', message: undefined, stalled: false },
    );
    this.lastServerStatus = null;

    // A fresh canvas per connection; hidden until it shows a real frame so
    // the previous frame (poster) stays on screen meanwhile.
    const canvas = document.createElement('canvas');
    canvas.className = 'stream-canvas';
    canvas.style.visibility = 'hidden';
    this.container.appendChild(canvas);
    this.canvas = canvas;

    const source = new PushSource();
    let firstFrame = true;
    this.player = new JSMpeg.Player('', {
      canvas,
      // JSMpeg calls `new options.source(...)`; returning an object from a
      // constructor makes `new` yield that object, i.e. our instance.
      source: function () {
        return source;
      } as unknown as JSMpeg.PlayerOptions['source'],
      audio: false,
      autoplay: true,
      // JSMpeg's own hidden-tab handling adds a document listener it never
      // removes; requestAnimationFrame already stops in hidden tabs.
      pauseWhenHidden: false,
      preserveDrawingBuffer: true, // needed for the pause still and snapshots
      videoBufferSize: 1024 * 1024,
      onVideoDecode: () => {
        if (!firstFrame) return;
        firstFrame = false;
        canvas.style.visibility = 'visible';
        this.poster?.remove();
        this.poster = null;
        // The renderer sizes the canvas to the decoded picture.
        this.update({ width: canvas.width, height: canvas.height });
      },
    });

    const socket = new WebSocket(streamSocketUrl(this.rtspUrl));
    socket.binaryType = 'arraybuffer';
    this.socket = socket;

    socket.onmessage = (ev: MessageEvent<ArrayBuffer | string>) => {
      if (typeof ev.data === 'string') {
        this.handleStatus(ev.data);
        return;
      }
      this.bytesSinceTick += ev.data.byteLength;
      this.lastDataAt = performance.now();
      source.write(ev.data);
    };
    socket.onclose = (ev) => {
      if (this.socket !== socket) return; // superseded or closed by us
      this.socket = null;
      this.handleClose(ev);
    };

    this.lastDataAt = performance.now();
    this.statsTimer = window.setInterval(() => this.tick(), 1000);
  }

  private handleStatus(raw: string) {
    let status: ServerStatus;
    try {
      status = JSON.parse(raw) as ServerStatus;
    } catch {
      return;
    }
    if (status.type !== 'status') return;
    this.lastServerStatus = status;

    if (status.state === 'live') this.reconnects = 0;
    this.update({
      state: status.state,
      message: status.message,
      viewers: status.viewers,
    });
  }

  private handleClose(ev: CloseEvent) {
    const status = this.lastServerStatus;
    const fatal =
      ev.code === CLOSE_SOURCE_FAILED ||
      ev.code === CLOSE_BAD_REQUEST ||
      ev.code === CLOSE_SERVER_BUSY ||
      (status?.state === 'error' && status.fatal);

    if (fatal) {
      this.teardown();
      this.update({
        state: 'error',
        message: status?.message || ev.reason || 'The stream could not be played.',
        kbps: 0,
      });
      return;
    }

    // Network hiccup, backend restart, proxy timeout...: retry with backoff.
    if (this.reconnects >= MAX_RECONNECTS) {
      this.teardown();
      this.update({ state: 'error', message: 'Lost connection to the server.', kbps: 0 });
      return;
    }
    this.reconnects += 1;
    const delay = Math.min(1000 * 2 ** (this.reconnects - 1), 10_000);
    this.teardown();
    this.update({
      state: 'reconnecting',
      message: 'Connection to the server lost, reconnecting…',
      kbps: 0,
    });
    this.reconnectTimer = window.setTimeout(() => this.connect(), delay);
  }

  private tick() {
    const kbps = Math.round((this.bytesSinceTick * 8) / 1000);
    this.bytesSinceTick = 0;
    const stalled =
      this.snapshot.state === 'live' && performance.now() - this.lastDataAt > STALL_AFTER_MS;
    if (kbps !== this.snapshot.kbps || stalled !== this.snapshot.stalled) {
      this.update({ kbps, stalled });
    }
  }

  /** Close the socket and player, keeping the last frame as a still image. */
  private teardown() {
    window.clearTimeout(this.reconnectTimer);
    window.clearInterval(this.statsTimer);

    if (this.socket) {
      const s = this.socket;
      this.socket = null; // mark as intentional before closing
      s.close(1000, 'client closed');
    }

    if (this.canvas && this.canvas.style.visibility !== 'hidden' && !this.destroyed) {
      try {
        const img = this.poster ?? document.createElement('img');
        img.className = 'stream-poster';
        img.alt = '';
        img.src = this.canvas.toDataURL('image/jpeg', 0.85);
        if (!this.poster) this.container.prepend(img);
        this.poster = img;
      } catch {
        /* a still is a nice-to-have */
      }
    }

    if (this.player) {
      const gl = this.player.renderer?.gl;
      this.player.destroy();
      // Browsers limit live WebGL contexts per page; release ours now
      // rather than waiting for garbage collection.
      gl?.getExtension('WEBGL_lose_context')?.loseContext();
      this.player = null;
    }
    this.canvas?.remove();
    this.canvas = null;
  }

  private update(patch: Partial<StreamSnapshot>) {
    if (this.destroyed) return;
    this.snapshot = { ...this.snapshot, ...patch };
    this.onChange(this.snapshot);
  }
}
