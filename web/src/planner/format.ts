/** Display helpers shared by derivations and pages. */

const FRACTIONS: Record<number, string> = { 0.25: '¼', 0.5: '½', 0.75: '¾' }

/** 1.25 → "1¼", 0.5 → "½", 3 → "3". Only quarter steps are expressed as fractions. */
export function formatQuantity(quantity: number): string {
  const whole = Math.floor(quantity + 1e-9)
  const fraction = Math.round((quantity - whole) * 4) / 4
  if (Math.abs(quantity - (whole + fraction)) < 1e-6 && fraction in FRACTIONS) {
    return whole === 0 ? FRACTIONS[fraction] : `${whole}${FRACTIONS[fraction]}`
  }
  if (Math.abs(quantity - Math.round(quantity)) < 1e-6) return String(Math.round(quantity))
  return String(Math.round(quantity * 100) / 100)
}

const PLURAL_UNITS = new Set(['cup', 'quart', 'can', 'head', 'bunch', 'pint', 'bag', 'jar', 'tub', 'small tub', 'clove', 'pkg', 'package', 'bulb'])

function pluralizeUnit(unit: string, quantity: number): string {
  if (quantity <= 1 || !PLURAL_UNITS.has(unit.toLowerCase())) return unit
  if (unit.endsWith('ch') || unit.endsWith('s')) return `${unit}es`
  return `${unit}s`
}

/** (2,'lb') → "2 lb"; (0.5,'bunch') → "½ bunch"; (3,'') → "3"; (null,'tsp') → "". */
export function formatAmount(quantity: number | null, unit: string): string {
  if (quantity === null) return ''
  const q = formatQuantity(quantity)
  return unit ? `${q} ${pluralizeUnit(unit, quantity)}` : q
}

/** 40 → "40 min", 360 → "6 hrs", 90 → "1 hr 30 min". */
export function formatDuration(minutes: number): string {
  if (minutes < 60) return `${minutes} min`
  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60
  const h = `${hours} ${hours === 1 ? 'hr' : 'hrs'}`
  return rest === 0 ? h : `${h} ${rest} min`
}

/** 540 → "9:00". */
export function formatTimer(seconds: number): string {
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

/** ['a','b','c'] → "a, b and c". */
export function joinList(items: string[]): string {
  if (items.length <= 1) return items.join('')
  return `${items.slice(0, -1).join(', ')} and ${items[items.length - 1]}`
}
