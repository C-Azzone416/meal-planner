import { type FormEvent, type ReactNode, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useBlocker, useNavigate, useParams } from 'react-router-dom'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { FridgePanel, PaperTitle, PreviewNotice } from '../components/FridgePanel'
import { joinList } from '../planner/format'
import { usePlanner } from '../planner/store'
import { AISLES, DAY_NAMES, type DeepReadonly, type Recipe } from '../planner/types'
import '../styles/recipes.css'
import { RecipeNotFound } from './RecipeDetailPage'
import { recipePath } from './recipePath'
import {
  blankRow,
  emptyForm,
  formFromRecipe,
  SECTION_PREFIX,
  type CookRow,
  type IngredientRow,
  type PrepAheadRow,
  type PrepTonightRow,
  type RecipeFormValues,
  type SectionKey,
  toRecipeFields,
  validate,
} from './recipeForm'

/** `/recipes/new` and `/recipes/:recipeId/edit`. */
export function RecipeFormPage({ mode }: { mode: 'new' | 'edit' }) {
  const { recipeId } = useParams()
  const planner = usePlanner()
  if (mode === 'new') return <RecipeForm key="new" />
  const recipe = planner.getRecipe(recipeId)
  if (!recipe) return <><RecipeNotFound /><PreviewNotice /></>
  return <RecipeForm key={recipe.id} recipe={recipe} />
}

interface FieldProps {
  id: string
  label: string
  /** Extra text for screen readers so repeated row labels are unique, e.g. "for ingredient 2". */
  context?: string
  value: string
  onChange: (value: string) => void
  error?: string
  hint?: string
  inputMode?: 'numeric' | 'decimal' | 'text'
  maxLength?: number
  className?: string
  required?: boolean
}

function TextField({ id, label, context, value, onChange, error, hint, inputMode, maxLength, className = '', required }: FieldProps) {
  const describedBy = [error ? `${id}-error` : '', hint ? `${id}-hint` : ''].filter(Boolean).join(' ') || undefined
  return <div className={`rx-field ${className}`.trim()}>
    <label htmlFor={id}>{label}{context && <span className="sr-only"> {context}</span>}{required && <span aria-hidden="true"> *</span>}</label>
    <input id={id} type="text" value={value} inputMode={inputMode} maxLength={maxLength} required={required} aria-required={required || undefined} aria-invalid={error ? true : undefined} aria-describedby={describedBy} onChange={(event) => onChange(event.target.value)} />
    {hint && <p id={`${id}-hint`} className="rx-hint">{hint}</p>}
    {error && <p id={`${id}-error`} className="field-error">Error: {error}</p>}
  </div>
}

function SelectField({ id, label, context, value, onChange, options }: { id: string; label: string; context?: string; value: string; onChange: (value: string) => void; options: readonly { value: string; label: string }[] }) {
  return <div className="rx-field">
    <label htmlFor={id}>{label}{context && <span className="sr-only"> {context}</span>}</label>
    <select id={id} value={value} onChange={(event) => onChange(event.target.value)}>{options.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select>
  </div>
}

interface RowShellProps {
  section: SectionKey
  noun: string
  rowId: string
  index: number
  count: number
  onMove: (direction: -1 | 1) => void
  onRemove: () => void
  children: ReactNode
}

function RowShell({ section, noun, rowId, index, count, onMove, onRemove, children }: RowShellProps) {
  const prefix = SECTION_PREFIX[section]
  const name = `${noun} ${index + 1}`
  return <li className="rx-row">
    <div className="rx-row-head">
      <strong>{name}</strong>
      <div className="rx-row-tools">
        <button type="button" className="rx-icon-button" id={`${prefix}-${rowId}-up`} disabled={index === 0} aria-label={`Move ${name} up`} onClick={() => onMove(-1)}>↑</button>
        <button type="button" className="rx-icon-button" id={`${prefix}-${rowId}-down`} disabled={index === count - 1} aria-label={`Move ${name} down`} onClick={() => onMove(1)}>↓</button>
        <button type="button" className="rx-icon-button rx-icon-button--danger" aria-label={`Remove ${name}`} onClick={onRemove}>✕</button>
      </div>
    </div>
    <div className="rx-row-fields">{children}</div>
  </li>
}

function RecipeForm({ recipe }: { recipe?: DeepReadonly<Recipe> }) {
  const planner = usePlanner()
  const { clearStatus } = planner
  const navigate = useNavigate()
  const [initial] = useState(() => (recipe ? formFromRecipe(recipe) : emptyForm()))
  const [initialJson] = useState(() => JSON.stringify(initial))
  const [values, setValues] = useState<RecipeFormValues>(initial)
  const [submitted, setSubmitted] = useState(false)
  const bypass = useRef(false)
  const pendingFocus = useRef<string | null>(null)

  const dirty = JSON.stringify(values) !== initialJson
  const errors = useMemo(() => (submitted ? validate(values) : []), [submitted, values])
  const errorFor = useMemo(() => new Map(errors.map((error) => [error.field, error.message])), [errors])

  useEffect(() => clearStatus(), [clearStatus])

  // Move focus after a render that asked for it (first invalid field, new row, after a move/remove).
  useEffect(() => {
    if (!pendingFocus.current) return
    document.getElementById(pendingFocus.current)?.focus()
    pendingFocus.current = null
  })

  // In-app navigation away. Not blocked: Save's own redirect, or the sign-out / expired-session redirect to /sign-in.
  const blocker = useBlocker(({ currentLocation, nextLocation }) =>
    dirty && !bypass.current && nextLocation.pathname !== currentLocation.pathname && nextLocation.pathname !== '/sign-in')

  useEffect(() => {
    if (!dirty) return
    const onBeforeUnload = (event: BeforeUnloadEvent) => event.preventDefault()
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirty])

  const plannedDays = recipe
    ? planner.plan.nights.filter((night) => night.status === 'planned' && night.recipeId === recipe.id).map((night) => night.day)
    : []

  const setField = <K extends keyof RecipeFormValues>(key: K, value: RecipeFormValues[K]) => setValues((current) => ({ ...current, [key]: value }))

  const patchRow = (section: SectionKey, rowId: string, patch: Record<string, unknown>) =>
    setValues((current) => ({
      ...current,
      [section]: (current[section] as { id: string }[]).map((row) => (row.id === rowId ? { ...row, ...patch } : row)),
    }) as RecipeFormValues)

  const addRow = (section: SectionKey) => {
    const row = blankRow(section)
    pendingFocus.current = `${SECTION_PREFIX[section]}-${row.id}-${section === 'ingredients' ? 'name' : 'text'}`
    setValues((current) => ({ ...current, [section]: [...(current[section] as unknown[]), row] }) as RecipeFormValues)
  }

  const removeRow = (section: SectionKey, rowId: string) => {
    pendingFocus.current = `add-${section}`
    setValues((current) => ({ ...current, [section]: (current[section] as { id: string }[]).filter((row) => row.id !== rowId) }) as RecipeFormValues)
  }

  const moveRow = (section: SectionKey, rowId: string, direction: -1 | 1) => {
    const rows = values[section] as { id: string }[]
    const from = rows.findIndex((row) => row.id === rowId)
    const to = from + direction
    if (from < 0 || to < 0 || to >= rows.length) return
    const next = rows.slice()
    ;[next[from], next[to]] = [next[to], next[from]]
    const atEdge = direction === -1 ? to === 0 : to === rows.length - 1
    pendingFocus.current = `${SECTION_PREFIX[section]}-${rowId}-${atEdge ? (direction === -1 ? 'down' : 'up') : direction === -1 ? 'up' : 'down'}`
    setValues((current) => ({ ...current, [section]: next }) as RecipeFormValues)
  }

  const onSubmit = (event: FormEvent) => {
    event.preventDefault()
    setSubmitted(true)
    const found = validate(values)
    if (found.length > 0) {
      // Focus now: on a repeat failed submit nothing re-renders, so a deferred focus request would never run.
      document.getElementById(found[0].field)?.focus()
      return
    }
    const fields = toRecipeFields(values)
    bypass.current = true
    if (recipe) {
      planner.updateRecipe({ ...recipe, ...fields, id: recipe.id })
      navigate(recipePath(recipe.id), { state: { saved: true } })
    } else {
      const id = planner.addRecipe(fields)
      navigate(recipePath(id), { state: { saved: true } })
    }
  }

  const cancelTarget = recipe ? recipePath(recipe.id) : '/recipes'
  const rowProps = (section: SectionKey, noun: string, rowId: string, index: number) => ({
    section,
    noun,
    rowId,
    index,
    count: (values[section] as unknown[]).length,
    onMove: (direction: -1 | 1) => moveRow(section, rowId, direction),
    onRemove: () => removeRow(section, rowId),
  })
  const error = (id: string) => errorFor.get(id)

  return <>
    <FridgePanel className="hero-panel rx-hero">
      <Link className="rx-back" to={cancelTarget}>{recipe ? '← Back to recipe' : '← All recipes'}</Link>
      <PaperTitle words={recipe ? ['edit', 'recipe'] : ['add', 'a', 'recipe']} />
      {plannedDays.length > 0 && <p className="inline-note">This recipe is on this week ({joinList(plannedDays.map((day) => DAY_NAMES[day]))}). Saving updates this week’s shopping and prep.</p>}
    </FridgePanel>
    <PreviewNotice />
    <FridgePanel className="content-panel">
      <form className="rx-form" noValidate onSubmit={onSubmit} aria-label={recipe ? 'Edit recipe' : 'Add recipe'}>
        {errors.length > 0 && <div role="alert" className="rx-summary">
          <h2>{errors.length === 1 ? '1 problem to fix before saving' : `${errors.length} problems to fix before saving`}</h2>
          <ul>{errors.map((item) => <li key={item.field}><button type="button" className="rx-link-button" onClick={() => document.getElementById(item.field)?.focus()}>{item.message}</button></li>)}</ul>
        </div>}

        <fieldset className="rx-section">
          <legend>Basics</legend>
          <div className="rx-grid">
            <TextField id="f-title" label="Title" required className="rx-wide" value={values.title} onChange={(value) => setField('title', value)} error={error('f-title')} maxLength={120} />
            <TextField id="f-icon" label="Icon" hint="One emoji for the recipe card." value={values.icon} onChange={(value) => setField('icon', value)} maxLength={8} />
            <TextField id="f-activeMinutes" label="Active minutes" required inputMode="numeric" hint="Hands-on time, 0–600." value={values.activeMinutes} onChange={(value) => setField('activeMinutes', value)} error={error('f-activeMinutes')} />
            <TextField id="f-totalMinutes" label="Total minutes" required inputMode="numeric" hint="Start to table, including waiting." value={values.totalMinutes} onChange={(value) => setField('totalMinutes', value)} error={error('f-totalMinutes')} />
            <SelectField id="f-attention" label="Attention needed" value={values.attention} onChange={(value) => setField('attention', value as RecipeFormValues['attention'])} options={[{ value: 'low', label: 'Low' }, { value: 'medium', label: 'Medium' }, { value: 'high', label: 'High' }]} />
            <TextField id="f-servings" label="Servings" required inputMode="numeric" hint="1–12." value={values.servings} onChange={(value) => setField('servings', value)} error={error('f-servings')} />
            <TextField id="f-tags" label="Tags" className="rx-wide" hint="Separate with commas, e.g. Low attention, Vegetarian." value={values.tags} onChange={(value) => setField('tags', value)} />
          </div>
        </fieldset>

        <fieldset className="rx-section">
          <legend>Ingredients</legend>
          <ul className="rx-rows">{values.ingredients.map((row: IngredientRow, index) => {
            const key = `ing-${row.id}`
            const context = `for ingredient ${index + 1}`
            return <RowShell key={row.id} {...rowProps('ingredients', 'Ingredient', row.id, index)}>
              <TextField id={`${key}-name`} label="Name" context={context} className="rx-wide" value={row.name} onChange={(value) => patchRow('ingredients', row.id, { name: value })} error={error(`${key}-name`)} />
              <TextField id={`${key}-quantity`} label="Quantity" context={context} inputMode="decimal" hint="e.g. 1.5" value={row.quantity} onChange={(value) => patchRow('ingredients', row.id, { quantity: value })} error={error(`${key}-quantity`)} />
              <TextField id={`${key}-unit`} label="Unit" context={context} hint="lb, can, tbsp… or blank" value={row.unit} onChange={(value) => patchRow('ingredients', row.id, { unit: value })} />
              <SelectField id={`${key}-aisle`} label="Aisle" context={context} value={row.aisle} onChange={(value) => patchRow('ingredients', row.id, { aisle: value })} options={AISLES.map((aisle) => ({ value: aisle, label: aisle }))} />
              <label className="rx-check"><input type="checkbox" checked={row.isPantry} onChange={(event) => patchRow('ingredients', row.id, { isPantry: event.target.checked })} /><span>Pantry staple<span className="sr-only"> {context}</span></span></label>
            </RowShell>
          })}</ul>
          <button type="button" id="add-ingredients" className="button button--paper rx-button" aria-invalid={error('add-ingredients') ? true : undefined} aria-describedby={error('add-ingredients') ? 'add-ingredients-error' : undefined} onClick={() => addRow('ingredients')}>+ Add ingredient</button>
          {error('add-ingredients') && <p id="add-ingredients-error" className="field-error">Error: {error('add-ingredients')}</p>}
        </fieldset>

        <fieldset className="rx-section">
          <legend>Prep ahead</legend>
          <p className="rx-hint">Steps you can do before the meal day. Say how long the result keeps and how to store it.</p>
          <ul className="rx-rows">{values.prepAhead.map((row: PrepAheadRow, index) => {
            const key = `ahead-${row.id}`
            const context = `for prep-ahead step ${index + 1}`
            return <RowShell key={row.id} {...rowProps('prepAhead', 'Prep-ahead step', row.id, index)}>
              <TextField id={`${key}-text`} label="Step" context={context} className="rx-wide" value={row.text} onChange={(value) => patchRow('prepAhead', row.id, { text: value })} error={error(`${key}-text`)} />
              <TextField id={`${key}-label`} label="Short name" context={context} hint="Used in warnings, e.g. Marinade." value={row.label} onChange={(value) => patchRow('prepAhead', row.id, { label: value })} />
              <TextField id={`${key}-minutes`} label="Minutes" context={context} inputMode="numeric" value={row.minutes} onChange={(value) => patchRow('prepAhead', row.id, { minutes: value })} error={error(`${key}-minutes`)} />
              <TextField id={`${key}-keepsDays`} label="Keeps (days)" context={context} inputMode="numeric" hint="Blank = no limit worth warning about." value={row.keepsDays} onChange={(value) => patchRow('prepAhead', row.id, { keepsDays: value })} error={error(`${key}-keepsDays`)} />
              <TextField id={`${key}-storageNote`} label="Storage note" context={context} className="rx-wide" hint="e.g. Fridge · 2 days" value={row.storageNote} onChange={(value) => patchRow('prepAhead', row.id, { storageNote: value })} />
            </RowShell>
          })}</ul>
          {values.prepAhead.length === 0 && <p className="rx-empty">No prep-ahead steps.</p>}
          <button type="button" id="add-prepAhead" className="button button--paper rx-button" onClick={() => addRow('prepAhead')}>+ Add prep-ahead step</button>
        </fieldset>

        <fieldset className="rx-section">
          <legend>Prep tonight</legend>
          <ul className="rx-rows">{values.prepTonight.map((row: PrepTonightRow, index) => {
            const key = `tonight-${row.id}`
            const context = `for prep-tonight step ${index + 1}`
            return <RowShell key={row.id} {...rowProps('prepTonight', 'Prep-tonight step', row.id, index)}>
              <TextField id={`${key}-text`} label="Step" context={context} className="rx-wide" value={row.text} onChange={(value) => patchRow('prepTonight', row.id, { text: value })} error={error(`${key}-text`)} />
              <TextField id={`${key}-minutes`} label="Minutes" context={context} inputMode="numeric" value={row.minutes} onChange={(value) => patchRow('prepTonight', row.id, { minutes: value })} error={error(`${key}-minutes`)} />
            </RowShell>
          })}</ul>
          {values.prepTonight.length === 0 && <p className="rx-empty">No prep-tonight steps.</p>}
          <button type="button" id="add-prepTonight" className="button button--paper rx-button" onClick={() => addRow('prepTonight')}>+ Add prep-tonight step</button>
        </fieldset>

        <fieldset className="rx-section">
          <legend>Cook tonight</legend>
          <ul className="rx-rows">{values.cookTonight.map((row: CookRow, index) => {
            const key = `cook-${row.id}`
            const context = `for cook step ${index + 1}`
            return <RowShell key={row.id} {...rowProps('cookTonight', 'Cook step', row.id, index)}>
              <TextField id={`${key}-text`} label="Step" context={context} className="rx-wide" value={row.text} onChange={(value) => patchRow('cookTonight', row.id, { text: value })} error={error(`${key}-text`)} />
              <TextField id={`${key}-timerMinutes`} label="Timer (minutes)" context={context} inputMode="decimal" hint="Optional, e.g. 1.5" value={row.timerMinutes} onChange={(value) => patchRow('cookTonight', row.id, { timerMinutes: value })} error={error(`${key}-timerMinutes`)} />
            </RowShell>
          })}</ul>
          <button type="button" id="add-cookTonight" className="button button--paper rx-button" aria-invalid={error('add-cookTonight') ? true : undefined} aria-describedby={error('add-cookTonight') ? 'add-cookTonight-error' : undefined} onClick={() => addRow('cookTonight')}>+ Add cook step</button>
          {error('add-cookTonight') && <p id="add-cookTonight-error" className="field-error">Error: {error('add-cookTonight')}</p>}
        </fieldset>

        <div className="button-row rx-form-actions">
          <button type="submit" className="button button--green rx-button">{recipe ? 'Save changes' : 'Save recipe'}</button>
          <button type="button" className="button button--paper rx-button" onClick={() => navigate(cancelTarget)}>Cancel</button>
        </div>
      </form>
    </FridgePanel>
    {blocker.state === 'blocked' && <ConfirmDialog
      title="Discard changes?"
      message="You have unsaved changes to this recipe. If you leave now they will be lost."
      confirmLabel="Discard"
      cancelLabel="Keep editing"
      onConfirm={() => blocker.proceed()}
      onCancel={() => blocker.reset()}
    />}
  </>
}
