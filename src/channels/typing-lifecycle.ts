type AsyncTick = () => Promise<void> | void;

export type TypingKeepaliveLoop = {
  tick: () => Promise<void>;
  start: () => void;
  stop: () => void;
  isRunning: () => boolean;
};

const TICK_TIMEOUT_MS = 10_000;

export function createTypingKeepaliveLoop(params: {
  intervalMs: number;
  onTick: AsyncTick;
}): TypingKeepaliveLoop {
  let timer: ReturnType<typeof setInterval> | undefined;
  let tickInFlight = false;

  const tick = async () => {
    if (tickInFlight) {
      return;
    }
    tickInFlight = true;
    let tickTimer: ReturnType<typeof setTimeout> | undefined;
    try {
      await Promise.race([
        params.onTick(),
        new Promise<void>((resolve) => {
          tickTimer = setTimeout(resolve, TICK_TIMEOUT_MS);
          tickTimer.unref?.();
        }),
      ]);
    } finally {
      if (tickTimer) {
        clearTimeout(tickTimer);
      }
      tickInFlight = false;
    }
  };

  const start = () => {
    if (params.intervalMs <= 0 || timer) {
      return;
    }
    timer = setInterval(() => {
      void tick();
    }, params.intervalMs);
  };

  const stop = () => {
    if (!timer) {
      return;
    }
    clearInterval(timer);
    timer = undefined;
    tickInFlight = false;
  };

  const isRunning = () => timer !== undefined;

  return {
    tick,
    start,
    stop,
    isRunning,
  };
}
