// sync.js — pure drift math, mirrors internal/live/watch.go PositionAt
export function expectedPosition(state, serverNowMs) {
  if (state.paused || serverNowMs < state.updatedAt) return state.position; // ponytail: clamp mirrors Go's backward-clock guard
  return state.position + ((serverNowMs - state.updatedAt) / 1000) * state.rate;
}

export function correction(currentSec, expectedSec, currentRate = 1) {
  const drift = currentSec - expectedSec;
  if (Math.abs(drift) > 1) return { seek: expectedSec };
  if (Math.abs(drift) > 0.15) return { rate: drift > 0 ? 0.95 : 1.05 };
  if (currentRate !== 1) return { rate: 1 };
  return null;
}
