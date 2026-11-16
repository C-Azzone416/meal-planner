import { describe, expect, it } from 'vitest'
import type { CanonicalDraftContent } from './api'
import { projectionFromDraft, toProjection, validateProjection, type ProjectionValues } from './projection'

const canonical: CanonicalDraftContent = {
  title: 'Canonical soup',
  yield: 'unknown',
  ingredients: [{ text: '2 onions', quantity: '2', unit: 'whole', name: 'onions' }],
  instructions: [{ text: 'Simmer until ready.', phase: 'cook_tonight', phase_basis: { kind: 'agent_estimate' } }],
  estimates: { active_minutes: { value: 0, basis: { kind: 'agent_estimate' } } },
  sources: [{ id: 'source-1', kind: 'user_supplied', label: '<script>not markup</script>' }],
  guidance: [],
}

function complete(): ProjectionValues {
  const values = projectionFromDraft(canonical)
  return {
    ...values,
    title: 'Owner soup',
    icon: '🍲',
    activeMinutes: '0',
    totalMinutes: '45',
    attention: 'low',
    tagsChoice: 'none',
    servings: '4',
    ingredients: [{
      ...values.ingredients[0], name: 'onions', quantityChoice: 'none', unitChoice: 'none', aisle: 'Produce', pantry: 'no',
    }],
    instructions: [{
      ...values.instructions[0], phase: 'cook_tonight', text: 'Simmer until ready.', timerChoice: 'none',
    }],
  }
}

describe('explicit hosted-draft projection', () => {
  it('retains canonical content only as context and leaves every lossy field unresolved', () => {
    const values = projectionFromDraft(canonical)
    expect(values.title).toBe('')
    expect(values.activeMinutes).toBe('')
    expect(values.ingredients[0]).toMatchObject({ sourceText: '2 onions', name: '', quantityChoice: '', unitChoice: '', aisle: '', pantry: '' })
    expect(values.instructions[0]).toMatchObject({ sourceText: 'Simmer until ready.', phase: '', text: '', timerChoice: '' })
    expect(validateProjection(values).map((error) => error.field)).toEqual(expect.arrayContaining([
      'projection-title', 'projection-active', 'projection-ingredient-0-quantity-choice', 'projection-ingredient-0-aisle', 'projection-instruction-0-phase',
    ]))
    expect(() => toProjection(values)).toThrow('unresolved')
  })

  it('preserves explicit zero and explicit null choices in the exact manual projection', () => {
    const projection = toProjection(complete())
    expect(projection).toEqual({
      title: 'Owner soup', icon: '🍲', activeMinutes: 0, totalMinutes: 45, attention: 'low', tags: [], defaultServings: 4,
      ingredients: [{ id: 'draft-ingredient-1', name: 'onions', quantity: null, unit: '', aisle: 'Produce', isPantry: false }],
      prepAhead: [], prepTonight: [],
      cookTonight: [{ id: 'draft-instruction-1', text: 'Simmer until ready.', timerSeconds: null }],
    })
  })

  it('requires explicit phases and time decisions instead of converting blanks to zero', () => {
    const values = complete()
    values.instructions[0].phase = 'prep_ahead'
    values.instructions[0].minutes = ''
    values.instructions[0].keepsChoice = ''
    values.instructions[0].storageChoice = ''
    const errors = validateProjection(values)
    expect(errors).toContainEqual(expect.objectContaining({ field: 'projection-instruction-0-minutes' }))
    expect(errors).toContainEqual(expect.objectContaining({ field: 'projection-instruction-0-keeps-choice' }))
    expect(errors).toContainEqual(expect.objectContaining({ field: 'projection-instruction-0-storage-choice' }))
    expect(errors).toContainEqual(expect.objectContaining({ field: 'projection-add-instruction' }))
  })

  it('rejects hostile or over-limit owner input while canonical hostile-looking text stays inert context', () => {
    const values = complete()
    values.title = `bad\0title`
    values.ingredients[0].name = 'x'.repeat(501)
    values.instructions[0].text = '\ud800'
    const fields = validateProjection(values).map((error) => error.field)
    expect(fields).toContain('projection-title')
    expect(fields).toContain('projection-ingredient-0-name')
    expect(fields).toContain('projection-instruction-0-text')
    expect(canonical.sources[0].label).toBe('<script>not markup</script>')
  })
})
