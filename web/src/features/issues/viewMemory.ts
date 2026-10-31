/** Scroll offset and highlighted row per history entry, so Back returns to the same spot. */
export const viewMemory = new Map<string, { scroll: number; selected: string | null }>()
