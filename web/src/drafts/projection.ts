import { containsIllFormed } from '../planner/keys'
import { AISLES, type Aisle, type Attention, type NewRecipe } from '../planner/types'
import type { CanonicalDraftContent } from './api'

export type ValueChoice = '' | 'none' | 'value'
export type BooleanChoice = '' | 'yes' | 'no'
export type DraftPhase = '' | 'prep_ahead' | 'prep_tonight' | 'cook_tonight'

export interface ProjectionIngredient {
  id: string
  sourceText: string
  name: string
  quantityChoice: ValueChoice
  quantity: string
  unitChoice: ValueChoice
  unit: string
  aisle: '' | Aisle
  pantry: BooleanChoice
}

export interface ProjectionInstruction {
  id: string
  sourceText: string
  phase: DraftPhase
  text: string
  minutes: string
  label: string
  keepsChoice: ValueChoice
  keepsDays: string
  storageChoice: ValueChoice
  storageNote: string
  timerChoice: ValueChoice
  timerMinutes: string
}

export interface ProjectionValues {
  title: string
  icon: string
  activeMinutes: string
  totalMinutes: string
  attention: '' | Attention
  tagsChoice: ValueChoice
  tags: string
  servings: string
  ingredients: ProjectionIngredient[]
  instructions: ProjectionInstruction[]
}

export interface ProjectionError { field: string; message: string }

export function ingredientRow(sourceText = '', index = 0): ProjectionIngredient {
  return {
    id: `draft-ingredient-${index + 1}`,
    sourceText,
    name: '',
    quantityChoice: '',
    quantity: '',
    unitChoice: '',
    unit: '',
    aisle: '',
    pantry: '',
  }
}

export function instructionRow(sourceText = '', index = 0): ProjectionInstruction {
  return {
    id: `draft-instruction-${index + 1}`,
    sourceText,
    phase: '',
    text: '',
    minutes: '',
    label: '',
    keepsChoice: '',
    keepsDays: '',
    storageChoice: '',
    storageNote: '',
    timerChoice: '',
    timerMinutes: '',
  }
}

/**
 * Start with unresolved fields. Canonical text is retained only as read-only
 * context; it is never copied into the manual recipe without an owner action.
 */
export function projectionFromDraft(content: CanonicalDraftContent): ProjectionValues {
  return {
    title: '',
    icon: '',
    activeMinutes: '',
    totalMinutes: '',
    attention: '',
    tagsChoice: '',
    tags: '',
    servings: '',
    ingredients: content.ingredients.length > 0
      ? content.ingredients.map((item, index) => ingredientRow(item.text, index))
      : [ingredientRow('', 0)],
    instructions: content.instructions.length > 0
      ? content.instructions.map((step, index) => instructionRow(step.text, index))
      : [instructionRow('', 0)],
  }
}

const INT = /^\d+$/
const DECIMAL = /^(\d+\.?\d*|\.\d+)$/
const blank = (value: string) => value.trim() === ''
const isInt = (value: string) => INT.test(value.trim()) && Number.isFinite(Number(value))
const isDecimal = (value: string) => DECIMAL.test(value.trim()) && Number.isFinite(Number(value))
const runes = (value: string) => Array.from(value.trim()).length

function textError(value: string, max: number, required: boolean): string | null {
  if (containsIllFormed(value) || value.includes('\0')) return 'contains text that cannot be saved.'
  if (required && blank(value)) return 'is required.'
  if (runes(value) > max) return `must be ${max} characters or fewer.`
  return null
}

function parseTags(values: ProjectionValues): string[] {
  if (values.tagsChoice !== 'value') return []
  const tags: string[] = []
  for (const raw of values.tags.split(',')) {
    const tag = raw.trim()
    if (tag && !tags.some((existing) => existing.toLowerCase() === tag.toLowerCase())) tags.push(tag)
  }
  return tags
}

/** Validation mirrors hosted.ValidateRecipeContent and adds unresolved-choice errors. */
export function validateProjection(values: ProjectionValues): ProjectionError[] {
  const errors: ProjectionError[] = []
  const add = (field: string, message: string) => errors.push({ field, message })
  const checkText = (field: string, label: string, value: string, max: number, required = false) => {
    const detail = textError(value, max, required)
    if (detail) add(field, `${label} ${detail}`)
  }

  checkText('projection-title', 'Title', values.title, 120, true)
  checkText('projection-icon', 'Icon', values.icon, 16, true)
  if (!isInt(values.activeMinutes) || Number(values.activeMinutes) > 600) add('projection-active', 'Active minutes must be an explicit whole number from 0 to 600.')
  if (!isInt(values.totalMinutes) || Number(values.totalMinutes) > 10080) add('projection-total', 'Total minutes must be an explicit whole number from 0 to 10080.')
  else if (isInt(values.activeMinutes) && Number(values.totalMinutes) < Number(values.activeMinutes)) add('projection-total', 'Total time cannot be shorter than active time.')
  if (!values.attention) add('projection-attention', 'Choose an attention level.')
  if (!isInt(values.servings) || Number(values.servings) < 1 || Number(values.servings) > 12) add('projection-servings', 'Servings must be an explicit whole number from 1 to 12.')
  if (!values.tagsChoice) add('projection-tags-choice', 'Choose whether this recipe has tags.')
  if (values.tagsChoice === 'value' && blank(values.tags)) add('projection-tags', 'Enter at least one tag or choose “No tags”.')
  const tags = parseTags(values)
  if (tags.length > 32) add('projection-tags', 'Use at most 32 tags.')
  tags.forEach((tag) => checkText('projection-tags', 'Each tag', tag, 80, true))

  if (values.ingredients.length < 1) add('projection-add-ingredient', 'Add at least one ingredient.')
  if (values.ingredients.length > 200) add('projection-add-ingredient', 'Use at most 200 ingredients.')
  const ingredientIDs = new Set<string>()
  values.ingredients.forEach((row, index) => {
    const prefix = `projection-ingredient-${index}`
    checkText(`${prefix}-name`, 'Ingredient name', row.name, 500, true)
    if (!row.quantityChoice) add(`${prefix}-quantity-choice`, 'Choose a quantity or explicitly choose no amount.')
    if (row.quantityChoice === 'value' && (!isDecimal(row.quantity) || Number(row.quantity) <= 0 || Number(row.quantity) > 1_000_000)) add(`${prefix}-quantity`, 'Quantity must be greater than 0 and at most 1,000,000.')
    if (!row.unitChoice) add(`${prefix}-unit-choice`, 'Choose a unit or explicitly choose no unit.')
    if (row.unitChoice === 'value') checkText(`${prefix}-unit`, 'Unit', row.unit, 80, true)
    if (!row.aisle || !AISLES.includes(row.aisle as Aisle)) add(`${prefix}-aisle`, 'Choose an aisle.')
    if (!row.pantry) add(`${prefix}-pantry`, 'Choose whether this is a pantry staple.')
    checkText(`${prefix}-id`, 'Ingredient ID', row.id, 160, true)
    if (ingredientIDs.has(row.id)) add(`${prefix}-id`, 'Ingredient IDs must be unique.')
    ingredientIDs.add(row.id)
  })

  if (values.instructions.length > 200) add('projection-add-instruction', 'Use at most 200 instructions.')
  let cooks = 0
  const phaseIDs: Record<Exclude<DraftPhase, ''>, Set<string>> = { prep_ahead: new Set(), prep_tonight: new Set(), cook_tonight: new Set() }
  values.instructions.forEach((row, index) => {
    const prefix = `projection-instruction-${index}`
    if (!row.phase) add(`${prefix}-phase`, 'Choose a phase for this instruction.')
    checkText(`${prefix}-text`, 'Instruction', row.text, 4000, true)
    if (!row.phase) return
    checkText(`${prefix}-id`, 'Instruction ID', row.id, 160, true)
    if (phaseIDs[row.phase].has(row.id)) add(`${prefix}-id`, 'Instruction IDs must be unique within a phase.')
    phaseIDs[row.phase].add(row.id)
    if (row.phase === 'cook_tonight') {
      cooks += 1
      if (!row.timerChoice) add(`${prefix}-timer-choice`, 'Choose a timer or explicitly choose no timer.')
      if (row.timerChoice === 'value' && (!isDecimal(row.timerMinutes) || Math.round(Number(row.timerMinutes) * 60) < 1 || Number(row.timerMinutes) > 10080)) add(`${prefix}-timer`, 'Timer must round to at least 1 second and be at most 10080 minutes.')
    } else {
      if (!isInt(row.minutes) || Number(row.minutes) > 10080) add(`${prefix}-minutes`, 'Minutes must be an explicit whole number from 0 to 10080.')
      if (row.phase === 'prep_ahead') {
        checkText(`${prefix}-label`, 'Short name', row.label, 500)
        if (!row.keepsChoice) add(`${prefix}-keeps-choice`, 'Choose a keep time or explicitly choose no meaningful limit.')
        if (row.keepsChoice === 'value' && (!isInt(row.keepsDays) || Number(row.keepsDays) < 1 || Number(row.keepsDays) > 3650)) add(`${prefix}-keeps`, 'Keep time must be a whole number from 1 to 3650 days.')
        if (!row.storageChoice) add(`${prefix}-storage-choice`, 'Choose a storage note or explicitly choose no note.')
        if (row.storageChoice === 'value') checkText(`${prefix}-storage`, 'Storage note', row.storageNote, 2000, true)
      }
    }
  })
  if (cooks < 1) add('projection-add-instruction', 'At least one instruction must be assigned to Cook tonight.')
  if (errors.length === 0 && new TextEncoder().encode(JSON.stringify({ projection: buildProjection(values) })).length > 250_000) {
    add('projection-title', 'This projection is too large. Shorten the text or remove rows.')
  }
  return errors
}

function buildProjection(values: ProjectionValues): NewRecipe {
  const prepAhead: NewRecipe['prepAhead'] = []
  const prepTonight: NewRecipe['prepTonight'] = []
  const cookTonight: NewRecipe['cookTonight'] = []
  for (const row of values.instructions) {
    if (row.phase === 'prep_ahead') prepAhead.push({
      id: row.id,
      text: row.text.trim(),
      ...(blank(row.label) ? {} : { label: row.label.trim() }),
      minutes: Number(row.minutes),
      keepsDays: row.keepsChoice === 'none' ? null : Number(row.keepsDays),
      storageNote: row.storageChoice === 'none' ? '' : row.storageNote.trim(),
    })
    if (row.phase === 'prep_tonight') prepTonight.push({ id: row.id, text: row.text.trim(), minutes: Number(row.minutes) })
    if (row.phase === 'cook_tonight') cookTonight.push({
      id: row.id,
      text: row.text.trim(),
      timerSeconds: row.timerChoice === 'none' ? null : Math.round(Number(row.timerMinutes) * 60),
    })
  }
  return {
    title: values.title.trim(),
    icon: values.icon.trim(),
    activeMinutes: Number(values.activeMinutes),
    totalMinutes: Number(values.totalMinutes),
    attention: values.attention as Attention,
    tags: parseTags(values),
    defaultServings: Number(values.servings),
    ingredients: values.ingredients.map((row) => ({
      id: row.id,
      name: row.name.trim(),
      quantity: row.quantityChoice === 'none' ? null : Number(row.quantity),
      unit: row.unitChoice === 'none' ? '' : row.unit.trim(),
      aisle: row.aisle as Aisle,
      isPantry: row.pantry === 'yes',
    })),
    prepAhead,
    prepTonight,
    cookTonight,
  }
}

/** Convert only a validated projection. No blank value is converted to zero or false. */
export function toProjection(values: ProjectionValues): NewRecipe {
  if (validateProjection(values).length > 0) throw new Error('Cannot project unresolved recipe fields.')
  return buildProjection(values)
}
