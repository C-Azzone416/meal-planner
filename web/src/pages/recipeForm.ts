/** Form model, conversion and validation for adding/editing a recipe (docs/ux-decisions.md §1). */
import { newItemId } from '../planner/keys'
import type { Aisle, Attention, NewRecipe, Recipe } from '../planner/types'

export interface IngredientRow {
  id: string
  name: string
  quantity: string
  unit: string
  aisle: Aisle
  isPantry: boolean
  /** The stored quantity, kept so an untouched field round-trips exactly (the input shows 2 decimals). */
  originalQuantity?: number | null
}
export interface PrepAheadRow {
  id: string
  text: string
  label: string
  minutes: string
  keepsDays: string
  storageNote: string
}
export interface PrepTonightRow {
  id: string
  text: string
  minutes: string
}
export interface CookRow {
  id: string
  text: string
  timerMinutes: string
  /** The stored timer, kept so an untouched field round-trips exactly (the input shows minutes to 2 decimals). */
  originalTimerSeconds?: number | null
}

export interface RecipeFormValues {
  title: string
  icon: string
  activeMinutes: string
  totalMinutes: string
  attention: Attention
  tags: string
  servings: string
  ingredients: IngredientRow[]
  prepAhead: PrepAheadRow[]
  prepTonight: PrepTonightRow[]
  cookTonight: CookRow[]
}

export type SectionKey = 'ingredients' | 'prepAhead' | 'prepTonight' | 'cookTonight'

/** DOM id prefix per section; row inputs are `${prefix}-${rowId}-${field}`. */
export const SECTION_PREFIX: Record<SectionKey, string> = {
  ingredients: 'ing',
  prepAhead: 'ahead',
  prepTonight: 'tonight',
  cookTonight: 'cook',
}

export const DEFAULT_ICON = '🍽️'

export function blankRow(section: SectionKey): IngredientRow | PrepAheadRow | PrepTonightRow | CookRow {
  const id = newItemId(SECTION_PREFIX[section])
  switch (section) {
    case 'ingredients':
      return { id, name: '', quantity: '', unit: '', aisle: 'Produce', isPantry: false }
    case 'prepAhead':
      return { id, text: '', label: '', minutes: '', keepsDays: '', storageNote: '' }
    case 'prepTonight':
      return { id, text: '', minutes: '' }
    case 'cookTonight':
      return { id, text: '', timerMinutes: '' }
  }
}

export function emptyForm(): RecipeFormValues {
  return {
    title: '',
    icon: DEFAULT_ICON,
    activeMinutes: '',
    totalMinutes: '',
    attention: 'low',
    tags: '',
    servings: '4',
    ingredients: [blankRow('ingredients') as IngredientRow],
    prepAhead: [],
    prepTonight: [],
    cookTonight: [blankRow('cookTonight') as CookRow],
  }
}

const num = (value: number | null) => (value === null ? '' : String(Math.round(value * 100) / 100))

export function formFromRecipe(recipe: Recipe): RecipeFormValues {
  return {
    title: recipe.title,
    icon: recipe.icon,
    activeMinutes: String(recipe.activeMinutes),
    totalMinutes: String(recipe.totalMinutes),
    attention: recipe.attention,
    tags: recipe.tags.join(', '),
    servings: String(recipe.defaultServings),
    ingredients: recipe.ingredients.map((item) => ({
      id: item.id,
      name: item.name,
      quantity: num(item.quantity),
      unit: item.unit,
      aisle: item.aisle,
      isPantry: item.isPantry,
      originalQuantity: item.quantity,
    })),
    prepAhead: recipe.prepAhead.map((step) => ({
      id: step.id,
      text: step.text,
      label: step.label ?? '',
      minutes: String(step.minutes),
      keepsDays: num(step.keepsDays),
      storageNote: step.storageNote,
    })),
    prepTonight: recipe.prepTonight.map((step) => ({ id: step.id, text: step.text, minutes: String(step.minutes) })),
    cookTonight: recipe.cookTonight.map((step) => ({
      id: step.id,
      text: step.text,
      timerMinutes: step.timerSeconds === null ? '' : num(step.timerSeconds / 60),
      originalTimerSeconds: step.timerSeconds,
    })),
  }
}

export interface FormError {
  /** DOM id of the field to focus and describe. */
  field: string
  message: string
}

const INT = /^\d+$/
const DECIMAL = /^(\d+\.?\d*|\.\d+)$/

const blank = (value: string) => value.trim() === ''
// A digit string can still overflow to Infinity (e.g. 400 nines), so every numeric field must also be finite.
const isInt = (value: string) => INT.test(value.trim()) && Number.isFinite(Number(value))
const isDecimal = (value: string) => DECIMAL.test(value.trim()) && Number.isFinite(Number(value))

export const rowIsTouched = {
  ingredients: (row: IngredientRow) => !(blank(row.name) && blank(row.quantity) && blank(row.unit)),
  prepAhead: (row: PrepAheadRow) =>
    !(blank(row.text) && blank(row.label) && blank(row.minutes) && blank(row.keepsDays) && blank(row.storageNote)),
  prepTonight: (row: PrepTonightRow) => !(blank(row.text) && blank(row.minutes)),
  cookTonight: (row: CookRow) => !(blank(row.text) && blank(row.timerMinutes)),
}

/** Errors in DOM order. An empty array means the form is valid. */
export function validate(values: RecipeFormValues): FormError[] {
  const errors: FormError[] = []
  const add = (field: string, message: string) => errors.push({ field, message })

  const title = values.title.trim()
  if (!title) add('f-title', 'Title is required.')
  else if (title.length > 120) add('f-title', 'Title must be 120 characters or fewer.')

  const active = values.activeMinutes.trim()
  if (!isInt(active) || Number(active) > 600) add('f-activeMinutes', 'Active minutes must be a whole number from 0 to 600.')
  const total = values.totalMinutes.trim()
  if (!isInt(total) || Number(total) > 10080) add('f-totalMinutes', 'Total minutes must be a whole number from 0 to 10080.')
  else if (isInt(active) && Number(total) < Number(active)) add('f-totalMinutes', 'Total time can’t be shorter than active time.')

  const servings = values.servings.trim()
  if (!isInt(servings) || Number(servings) < 1 || Number(servings) > 12) add('f-servings', 'Servings must be a whole number from 1 to 12.')

  // Ingredients
  const ingredientFields: FormError[] = []
  for (const row of values.ingredients) {
    if (!rowIsTouched.ingredients(row)) continue
    const key = `ing-${row.id}`
    if (blank(row.name)) ingredientFields.push({ field: `${key}-name`, message: 'Ingredient name is required.' })
    if (!blank(row.quantity) && (!isDecimal(row.quantity) || Number(row.quantity) <= 0 || Number(row.quantity) > 1_000_000)) {
      ingredientFields.push({ field: `${key}-quantity`, message: 'Quantity must be a number greater than 0 and at most 1,000,000, or left empty.' })
    }
  }
  errors.push(...ingredientFields)
  if (!values.ingredients.some((row) => rowIsTouched.ingredients(row))) {
    add(values.ingredients.length ? `ing-${values.ingredients[0].id}-name` : 'add-ingredients', 'Add at least one ingredient.')
  }

  for (const row of values.prepAhead) {
    if (!rowIsTouched.prepAhead(row)) continue
    const key = `ahead-${row.id}`
    if (blank(row.text)) add(`${key}-text`, 'Prep-ahead step text is required.')
    if (!blank(row.minutes) && (!isInt(row.minutes) || Number(row.minutes) > 10080)) add(`${key}-minutes`, 'Minutes must be a whole number up to 10080, or left empty.')
    if (!blank(row.keepsDays) && (!isInt(row.keepsDays) || Number(row.keepsDays) < 1 || Number(row.keepsDays) > 3650)) {
      add(`${key}-keepsDays`, 'Keeps for must be a whole number of days from 1 to 3650, or left empty.')
    }
  }
  for (const row of values.prepTonight) {
    if (!rowIsTouched.prepTonight(row)) continue
    const key = `tonight-${row.id}`
    if (blank(row.text)) add(`${key}-text`, 'Prep-tonight step text is required.')
    if (!blank(row.minutes) && (!isInt(row.minutes) || Number(row.minutes) > 10080)) add(`${key}-minutes`, 'Minutes must be a whole number up to 10080, or left empty.')
  }

  let cookSteps = 0
  for (const row of values.cookTonight) {
    if (!rowIsTouched.cookTonight(row)) continue
    const key = `cook-${row.id}`
    if (blank(row.text)) add(`${key}-text`, 'Cook step text is required.')
    else cookSteps += 1
    if (!blank(row.timerMinutes) && (!isDecimal(row.timerMinutes) || Number(row.timerMinutes) <= 0 || Number(row.timerMinutes) > 10080)) {
      add(`${key}-timerMinutes`, 'Timer must be greater than 0 and at most 10080 minutes, or left empty.')
    }
  }
  if (cookSteps === 0 && !values.cookTonight.some((row) => rowIsTouched.cookTonight(row))) {
    add(values.cookTonight.length ? `cook-${values.cookTonight[0].id}-text` : 'add-cookTonight', 'Add at least one Cook tonight step.')
  }

  return errors
}

const toInt = (value: string) => (blank(value) ? 0 : Number(value.trim()))

/** Convert valid form values to recipe fields. Call only when `validate` returned no errors. */
export function toRecipeFields(values: RecipeFormValues): NewRecipe {
  const tags: string[] = []
  for (const raw of values.tags.split(',')) {
    const tag = raw.trim()
    if (tag && !tags.some((existing) => existing.toLowerCase() === tag.toLowerCase())) tags.push(tag)
  }
  return {
    title: values.title.trim(),
    icon: values.icon.trim() || DEFAULT_ICON,
    activeMinutes: toInt(values.activeMinutes),
    totalMinutes: toInt(values.totalMinutes),
    attention: values.attention,
    tags,
    defaultServings: toInt(values.servings),
    ingredients: values.ingredients.filter(rowIsTouched.ingredients).map((row) => ({
      id: row.id,
      name: row.name.trim(),
      quantity: blank(row.quantity) ? null : row.originalQuantity != null && row.quantity.trim() === num(row.originalQuantity) ? row.originalQuantity : Number(row.quantity.trim()),
      unit: row.unit.trim(),
      aisle: row.aisle,
      isPantry: row.isPantry,
    })),
    prepAhead: values.prepAhead.filter(rowIsTouched.prepAhead).map((row) => ({
      id: row.id,
      text: row.text.trim(),
      ...(blank(row.label) ? {} : { label: row.label.trim() }),
      minutes: toInt(row.minutes),
      keepsDays: blank(row.keepsDays) ? null : Number(row.keepsDays.trim()),
      storageNote: row.storageNote.trim(),
    })),
    prepTonight: values.prepTonight.filter(rowIsTouched.prepTonight).map((row) => ({
      id: row.id,
      text: row.text.trim(),
      minutes: toInt(row.minutes),
    })),
    cookTonight: values.cookTonight.filter(rowIsTouched.cookTonight).map((row) => ({
      id: row.id,
      text: row.text.trim(),
      timerSeconds: blank(row.timerMinutes)
        ? null
        : row.originalTimerSeconds != null && row.timerMinutes.trim() === num(row.originalTimerSeconds / 60)
          ? row.originalTimerSeconds
          : Math.round(Number(row.timerMinutes.trim()) * 60),
    })),
  }
}
