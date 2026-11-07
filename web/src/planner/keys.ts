/** Stable keys and ids. Progress records are keyed by these so recalculation never loses them. */

/** Lowercase, trim, drop parentheticals, collapse spaces and singularise so "Onions" and "onion" match. */
export function normalizeName(name: string): string {
  const base = name
    .toLowerCase()
    .replace(/\([^)]*\)/g, ' ')
    .replace(/[^a-z0-9\s&-]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
  return base
    .split(' ')
    .map(singularize)
    .join(' ')
}

function singularize(word: string): string {
  if (word.length <= 3) return word
  if (word.endsWith('ies')) return `${word.slice(0, -3)}y`
  if (word.endsWith('oes')) return word.slice(0, -2)
  if (word.endsWith('ss') || word.endsWith('us') || word.endsWith('is')) return word
  if (word.endsWith('s')) return word.slice(0, -1)
  return word
}

const UNIT_ALIASES: Record<string, string> = {
  lb: 'lb', lbs: 'lb', pound: 'lb', pounds: 'lb',
  oz: 'oz', ounce: 'oz', ounces: 'oz',
  tbsp: 'tbsp', tbs: 'tbsp', tablespoon: 'tbsp', tablespoons: 'tbsp',
  tsp: 'tsp', teaspoon: 'tsp', teaspoons: 'tsp',
  cup: 'cup', cups: 'cup',
  clove: 'clove', cloves: 'clove',
  can: 'can', cans: 'can',
  g: 'g', gram: 'g', grams: 'g',
  kg: 'kg', kilogram: 'kg', kilograms: 'kg',
  ml: 'ml', milliliter: 'ml', milliliters: 'ml',
  l: 'l', liter: 'l', liters: 'l',
  qt: 'quart', quart: 'quart', quarts: 'quart',
  gal: 'gallon', gallon: 'gallon', gallons: 'gallon',
}

/** Canonical unit: case-insensitive, alias-merged ("Cups", "lbs", "tablespoons" → cup, lb, tbsp), singularised otherwise. */
export function normalizeUnit(unit: string): string {
  const base = unit.toLowerCase().trim().replace(/\.$/, '').replace(/\s+/g, ' ')
  return UNIT_ALIASES[base] ?? singularize(base)
}

/** Shopping item key = normalised ingredient name + unit. */
export function shoppingKey(name: string, unit: string): string {
  return `${normalizeName(name)}|${normalizeUnit(unit)}`
}

/** Prep task key = recipe id + prep-ahead step id. */
export function prepKey(recipeId: string, stepId: string): string {
  return `${recipeId}:${stepId}`
}

export function slugify(text: string): string {
  const slug = text
    .toLowerCase()
    .normalize('NFKD')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 60)
    .replace(/-+$/g, '')
  return slug || 'recipe'
}

/** A slug for `title` that does not collide with `existing`. */
export function uniqueId(title: string, existing: Iterable<string>): string {
  const taken = new Set(existing)
  const base = slugify(title)
  if (!taken.has(base)) return base
  for (let n = 2; ; n += 1) {
    const candidate = `${base}-${n}`
    if (!taken.has(candidate)) return candidate
  }
}

let counter = 0
/** A short id for new ingredients/steps created in forms. Unique within a session. */
export function newItemId(prefix = 'item'): string {
  counter += 1
  return `${prefix}-${Date.now().toString(36)}${counter.toString(36)}`
}
