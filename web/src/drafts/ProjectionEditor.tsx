import { type FormEvent, useMemo, useRef, useState } from 'react'
import { AISLES, type NewRecipe } from '../planner/types'
import { DraftReviewAPIError, type HostedDraftResponse } from './api'
import {
  ingredientRow,
  instructionRow,
  projectionFromDraft,
  toProjection,
  validateProjection,
  type ProjectionIngredient,
  type ProjectionInstruction,
  type ProjectionValues,
} from './projection'

interface Props {
  item: HostedDraftResponse
  onSave: (item: HostedDraftResponse, projection: NewRecipe) => Promise<unknown>
  onSaved?: () => void
}

const choices = {
  quantity: [{ value: '', label: 'Choose…' }, { value: 'value', label: 'Enter quantity' }, { value: 'none', label: 'No amount (saves null)' }],
  unit: [{ value: '', label: 'Choose…' }, { value: 'value', label: 'Enter unit' }, { value: 'none', label: 'No unit (saves empty)' }],
  timer: [{ value: '', label: 'Choose…' }, { value: 'value', label: 'Set timer' }, { value: 'none', label: 'No timer (saves null)' }],
  keeps: [{ value: '', label: 'Choose…' }, { value: 'value', label: 'Enter days' }, { value: 'none', label: 'No meaningful limit (saves null)' }],
  storage: [{ value: '', label: 'Choose…' }, { value: 'value', label: 'Enter storage note' }, { value: 'none', label: 'No storage note (saves empty)' }],
} as const

function Field({ id, label, value, onChange, error, inputMode = 'text', maxLength, required = false }: {
  id: string; label: string; value: string; onChange: (value: string) => void; error?: string
  inputMode?: 'text' | 'numeric' | 'decimal'; maxLength?: number; required?: boolean
}) {
  return <label className="draft-projection-field" htmlFor={id}>{label}{required && <span aria-hidden="true"> *</span>}
    <input id={id} value={value} inputMode={inputMode} maxLength={maxLength} aria-invalid={error ? true : undefined} aria-describedby={error ? `${id}-error` : undefined} onChange={(event) => onChange(event.target.value)} />
    {error && <span className="field-error" id={`${id}-error`}>Error: {error}</span>}
  </label>
}

function Select({ id, label, value, onChange, options, error }: {
  id: string; label: string; value: string; onChange: (value: string) => void
  options: readonly { value: string; label: string }[]; error?: string
}) {
  return <label className="draft-projection-field" htmlFor={id}>{label} <span aria-hidden="true">*</span>
    <select id={id} value={value} aria-invalid={error ? true : undefined} aria-describedby={error ? `${id}-error` : undefined} onChange={(event) => onChange(event.target.value)}>
      {options.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
    </select>
    {error && <span className="field-error" id={`${id}-error`}>Error: {error}</span>}
  </label>
}

export function ProjectionEditor({ item, onSave, onSaved }: Props) {
  const [values, setValues] = useState<ProjectionValues>(() => projectionFromDraft(item.draft.content))
  const [submitted, setSubmitted] = useState(false)
  const [saving, setSaving] = useState(false)
  const [attempt, setAttempt] = useState<NewRecipe | null>(null)
  const [conflict, setConflict] = useState(false)
  const [saved, setSaved] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const savingRef = useRef(false)
  const nextIngredient = useRef(values.ingredients.length)
  const nextInstruction = useRef(values.instructions.length)
  const errors = useMemo(() => submitted ? validateProjection(values) : [], [submitted, values])
  const errorsByField = useMemo(() => new Map(errors.map((error) => [error.field, error.message])), [errors])
  const error = (id: string) => errorsByField.get(id)
  const locked = saving || attempt !== null || conflict || saved

  const setField = <K extends keyof ProjectionValues>(key: K, value: ProjectionValues[K]) => setValues((current) => ({ ...current, [key]: value }))
  const patchIngredient = (index: number, patch: Partial<ProjectionIngredient>) => setValues((current) => ({ ...current, ingredients: current.ingredients.map((row, at) => at === index ? { ...row, ...patch } : row) }))
  const patchInstruction = (index: number, patch: Partial<ProjectionInstruction>) => setValues((current) => ({ ...current, instructions: current.instructions.map((row, at) => at === index ? { ...row, ...patch } : row) }))

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (savingRef.current || conflict || saved) return
    setSubmitted(true)
    const found = validateProjection(values)
    if (!attempt && found.length > 0) {
      document.getElementById(found[0].field)?.focus()
      return
    }
    const projection = attempt ?? toProjection(values)
    savingRef.current = true
    setAttempt(projection)
    setSaving(true)
    setSaveError(null)
    try {
      await onSave(item, projection)
      setSaved(true)
      setAttempt(null)
      onSaved?.()
    } catch (cause) {
      const stale = cause instanceof DraftReviewAPIError && cause.status === 409
      setConflict(stale)
      // A 400 is a definitive pre-write rejection. Every other failure keeps the
      // exact attempted payload locked so a retry cannot fork an uncertain save.
      if (cause instanceof DraftReviewAPIError && cause.status === 400) setAttempt(null)
      setSaveError(stale
        ? 'This exact draft or review revision is stale. Reload the draft; this projection was not applied here.'
        : cause instanceof Error ? cause.message : 'Save could not be confirmed. Retry the identical projection.')
    } finally {
      savingRef.current = false
      setSaving(false)
    }
  }

  return <section className="draft-projection" aria-labelledby="draft-projection-title">
    <header>
      <h3 id="draft-projection-title">Create the manual recipe projection</h3>
      <p>Bound to draft revision <strong>{item.draft.revision}</strong> and review revision <strong>{item.draft.review.revision}</strong>. Canonical unknowns stay unresolved until you choose a value or an explicit “none”.</p>
    </header>
    {saveError && <div className="draft-projection-alert" role="alert"><p>{saveError}</p>{attempt && !conflict && <p>Fields are locked. Retry sends the identical projection and revision pair.</p>}{conflict && <p>Use Reload above before making another decision.</p>}</div>}
    {saved && <p className="draft-projection-success" role="status">Recipe saved to your account from this exact reviewed revision.</p>}
    {errors.length > 0 && <div className="draft-projection-alert" role="alert"><p><strong>{errors.length} unresolved or invalid fields</strong></p><ul>{errors.map((item) => <li key={item.field}><button type="button" onClick={() => document.getElementById(item.field)?.focus()}>{item.message}</button></li>)}</ul></div>}
    <form onSubmit={submit} noValidate>
      <fieldset disabled={locked}>
        <legend>Recipe basics</legend>
        <p className="draft-projection-source">Canonical title: <q>{item.draft.content.title}</q> <button type="button" onClick={() => setField('title', item.draft.content.title)}>Use canonical title</button></p>
        <div className="draft-projection-grid">
          <Field id="projection-title" label="Recipe title" required maxLength={120} value={values.title} onChange={(value) => setField('title', value)} error={error('projection-title')} />
          <Field id="projection-icon" label="Icon" required maxLength={16} value={values.icon} onChange={(value) => setField('icon', value)} error={error('projection-icon')} />
          <Field id="projection-active" label="Active minutes (0–600)" required inputMode="numeric" value={values.activeMinutes} onChange={(value) => setField('activeMinutes', value)} error={error('projection-active')} />
          <Field id="projection-total" label="Total minutes (0–10080)" required inputMode="numeric" value={values.totalMinutes} onChange={(value) => setField('totalMinutes', value)} error={error('projection-total')} />
          <Select id="projection-attention" label="Attention" value={values.attention} onChange={(value) => setField('attention', value as ProjectionValues['attention'])} error={error('projection-attention')} options={[{ value: '', label: 'Choose…' }, { value: 'low', label: 'Low' }, { value: 'medium', label: 'Medium' }, { value: 'high', label: 'High' }]} />
          <Field id="projection-servings" label="Default servings (1–12)" required inputMode="numeric" value={values.servings} onChange={(value) => setField('servings', value)} error={error('projection-servings')} />
          <Select id="projection-tags-choice" label="Tags" value={values.tagsChoice} onChange={(value) => setField('tagsChoice', value as ProjectionValues['tagsChoice'])} error={error('projection-tags-choice')} options={[{ value: '', label: 'Choose…' }, { value: 'value', label: 'Enter tags' }, { value: 'none', label: 'No tags (saves empty list)' }]} />
          {values.tagsChoice === 'value' && <Field id="projection-tags" label="Comma-separated tags" value={values.tags} onChange={(value) => setField('tags', value)} error={error('projection-tags')} />}
        </div>
      </fieldset>

      <fieldset disabled={locked}>
        <legend>Ingredients</legend>
        <p>Each ingredient needs an explicit amount choice, unit choice, aisle, and pantry decision.</p>
        <ol className="draft-projection-rows">{values.ingredients.map((row, index) => {
          const prefix = `projection-ingredient-${index}`
          return <li key={row.id}>
            <p className="draft-projection-source">Canonical ingredient {index + 1}: <q>{row.sourceText || 'No source text'}</q> {row.sourceText && <button type="button" onClick={() => patchIngredient(index, { name: row.sourceText })}>Use source text as name</button>}</p>
            <div className="draft-projection-grid">
              <Field id={`${prefix}-name`} label={`Name for ingredient ${index + 1}`} required value={row.name} onChange={(value) => patchIngredient(index, { name: value })} error={error(`${prefix}-name`)} />
              <Select id={`${prefix}-quantity-choice`} label={`Amount choice for ingredient ${index + 1}`} value={row.quantityChoice} onChange={(value) => patchIngredient(index, { quantityChoice: value as ProjectionIngredient['quantityChoice'] })} error={error(`${prefix}-quantity-choice`)} options={choices.quantity} />
              {row.quantityChoice === 'value' && <Field id={`${prefix}-quantity`} label={`Quantity for ingredient ${index + 1}`} required inputMode="decimal" value={row.quantity} onChange={(value) => patchIngredient(index, { quantity: value })} error={error(`${prefix}-quantity`)} />}
              <Select id={`${prefix}-unit-choice`} label={`Unit choice for ingredient ${index + 1}`} value={row.unitChoice} onChange={(value) => patchIngredient(index, { unitChoice: value as ProjectionIngredient['unitChoice'] })} error={error(`${prefix}-unit-choice`)} options={choices.unit} />
              {row.unitChoice === 'value' && <Field id={`${prefix}-unit`} label={`Unit for ingredient ${index + 1}`} required value={row.unit} onChange={(value) => patchIngredient(index, { unit: value })} error={error(`${prefix}-unit`)} />}
              <Select id={`${prefix}-aisle`} label={`Aisle for ingredient ${index + 1}`} value={row.aisle} onChange={(value) => patchIngredient(index, { aisle: value as ProjectionIngredient['aisle'] })} error={error(`${prefix}-aisle`)} options={[{ value: '', label: 'Choose…' }, ...AISLES.map((aisle) => ({ value: aisle, label: aisle }))]} />
              <Select id={`${prefix}-pantry`} label={`Pantry status for ingredient ${index + 1}`} value={row.pantry} onChange={(value) => patchIngredient(index, { pantry: value as ProjectionIngredient['pantry'] })} error={error(`${prefix}-pantry`)} options={[{ value: '', label: 'Choose…' }, { value: 'yes', label: 'Pantry staple' }, { value: 'no', label: 'Shopping item' }]} />
            </div>
            <button type="button" className="draft-projection-remove" onClick={() => setField('ingredients', values.ingredients.filter((_, at) => at !== index))}>Remove ingredient {index + 1}</button>
          </li>
        })}</ol>
        <button id="projection-add-ingredient" type="button" className="button button--paper" onClick={() => { const next = nextIngredient.current++; setField('ingredients', [...values.ingredients, ingredientRow('', next)]) }}>+ Add ingredient</button>
        {error('projection-add-ingredient') && <p className="field-error">Error: {error('projection-add-ingredient')}</p>}
      </fieldset>

      <fieldset disabled={locked}>
        <legend>Instructions and phases</legend>
        <p>Assign every instruction deliberately. Prep times require an explicit number; cook steps require an explicit timer or “no timer”.</p>
        <ol className="draft-projection-rows">{values.instructions.map((row, index) => {
          const prefix = `projection-instruction-${index}`
          return <li key={row.id}>
            <p className="draft-projection-source">Canonical instruction {index + 1}: <q>{row.sourceText || 'No source text'}</q> {row.sourceText && <button type="button" onClick={() => patchInstruction(index, { text: row.sourceText })}>Use canonical text</button>}</p>
            <div className="draft-projection-grid">
              <Select id={`${prefix}-phase`} label={`Phase for instruction ${index + 1}`} value={row.phase} onChange={(value) => patchInstruction(index, { phase: value as ProjectionInstruction['phase'] })} error={error(`${prefix}-phase`)} options={[{ value: '', label: 'Choose…' }, { value: 'prep_ahead', label: 'Prep ahead' }, { value: 'prep_tonight', label: 'Prep tonight' }, { value: 'cook_tonight', label: 'Cook tonight' }]} />
              <Field id={`${prefix}-text`} label={`Text for instruction ${index + 1}`} required value={row.text} onChange={(value) => patchInstruction(index, { text: value })} error={error(`${prefix}-text`)} />
              {row.phase && row.phase !== 'cook_tonight' && <Field id={`${prefix}-minutes`} label={`Minutes for instruction ${index + 1}`} required inputMode="numeric" value={row.minutes} onChange={(value) => patchInstruction(index, { minutes: value })} error={error(`${prefix}-minutes`)} />}
              {row.phase === 'prep_ahead' && <>
                <Field id={`${prefix}-label`} label={`Optional short name for instruction ${index + 1}`} value={row.label} onChange={(value) => patchInstruction(index, { label: value })} error={error(`${prefix}-label`)} />
                <Select id={`${prefix}-keeps-choice`} label={`Keep-time choice for instruction ${index + 1}`} value={row.keepsChoice} onChange={(value) => patchInstruction(index, { keepsChoice: value as ProjectionInstruction['keepsChoice'] })} error={error(`${prefix}-keeps-choice`)} options={choices.keeps} />
                {row.keepsChoice === 'value' && <Field id={`${prefix}-keeps`} label={`Keeps days for instruction ${index + 1}`} required inputMode="numeric" value={row.keepsDays} onChange={(value) => patchInstruction(index, { keepsDays: value })} error={error(`${prefix}-keeps`)} />}
                <Select id={`${prefix}-storage-choice`} label={`Storage-note choice for instruction ${index + 1}`} value={row.storageChoice} onChange={(value) => patchInstruction(index, { storageChoice: value as ProjectionInstruction['storageChoice'] })} error={error(`${prefix}-storage-choice`)} options={choices.storage} />
                {row.storageChoice === 'value' && <Field id={`${prefix}-storage`} label={`Storage note for instruction ${index + 1}`} required value={row.storageNote} onChange={(value) => patchInstruction(index, { storageNote: value })} error={error(`${prefix}-storage`)} />}
              </>}
              {row.phase === 'cook_tonight' && <>
                <Select id={`${prefix}-timer-choice`} label={`Timer choice for instruction ${index + 1}`} value={row.timerChoice} onChange={(value) => patchInstruction(index, { timerChoice: value as ProjectionInstruction['timerChoice'] })} error={error(`${prefix}-timer-choice`)} options={choices.timer} />
                {row.timerChoice === 'value' && <Field id={`${prefix}-timer`} label={`Timer minutes for instruction ${index + 1}`} required inputMode="decimal" value={row.timerMinutes} onChange={(value) => patchInstruction(index, { timerMinutes: value })} error={error(`${prefix}-timer`)} />}
              </>}
            </div>
            <button type="button" className="draft-projection-remove" onClick={() => setField('instructions', values.instructions.filter((_, at) => at !== index))}>Remove instruction {index + 1}</button>
          </li>
        })}</ol>
        <button id="projection-add-instruction" type="button" className="button button--paper" onClick={() => { const next = nextInstruction.current++; setField('instructions', [...values.instructions, instructionRow('', next)]) }}>+ Add instruction</button>
        {error('projection-add-instruction') && <p className="field-error">Error: {error('projection-add-instruction')}</p>}
      </fieldset>
      <button type="submit" className="button button--green" disabled={saving || conflict || saved}>{conflict ? 'Reload required' : saving ? 'Saving exact projection…' : attempt ? 'Retry exact projection' : saved ? 'Saved' : 'Save reviewed recipe'}</button>
    </form>
  </section>
}
