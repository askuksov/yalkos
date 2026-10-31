// Session generations: every sign-in and every handled session loss starts a
// new generation. A request that started under an earlier generation cannot
// end the current session (see SessionWatcher).

let generation = 0

export function sessionGeneration(): number {
  return generation
}

/** Starts a new generation; call after sign-in, sign-out and session loss. */
export function startSession(): void {
  generation++
}
