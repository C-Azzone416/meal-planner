import type { DeepReadonly } from './types'

// Freeze shared objects once. Reducers replace changed objects, so memoized slices
// remain stable and old snapshots stay protected even after the next action.
const frozen = new WeakSet<object>()

export function deepFreeze<T>(value: T): DeepReadonly<T> {
  if (value !== null && typeof value === 'object' && !frozen.has(value)) {
    frozen.add(value)
    for (const child of Object.values(value)) deepFreeze(child)
    Object.freeze(value)
  }
  return value as DeepReadonly<T>
}
