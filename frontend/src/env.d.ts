// Environment variables injected by Vite at build time.
interface ImportMetaEnv {
  readonly VITE_BACKEND_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

declare module '*.css';

// Minimal typings for the parts of JSMpeg (loaded globally from
// /vendor/jsmpeg.min.js) that this app uses.
declare namespace JSMpeg {
  interface Destination {
    write(data: ArrayBuffer): void;
  }

  /** A data source JSMpeg pulls from; we implement our own (see PushSource). */
  interface Source {
    streaming: boolean;
    established: boolean;
    completed: boolean;
    progress: number;
    connect(destination: Destination): void;
    start(): void;
    resume(secondsHeadroom: number): void;
    destroy(): void;
  }

  interface VideoDecoder {
    width: number;
    height: number;
  }

  interface PlayerOptions {
    canvas?: HTMLCanvasElement;
    source?: new (url: string, options: PlayerOptions) => Source;
    audio?: boolean;
    autoplay?: boolean;
    pauseWhenHidden?: boolean;
    preserveDrawingBuffer?: boolean;
    disableGl?: boolean;
    videoBufferSize?: number;
    onVideoDecode?: (decoder: VideoDecoder, time: number) => void;
  }

  class Player {
    constructor(url: string, options: PlayerOptions);
    video?: VideoDecoder;
    renderer?: { canvas: HTMLCanvasElement; gl?: WebGLRenderingContext };
    play(): void;
    pause(): void;
    destroy(): void;
  }
}
