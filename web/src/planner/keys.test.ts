import { describe, expect, test } from 'vitest'
import { containsIllFormed, isWellFormed, normalizeName, normalizeUnit, pantryLowKey, prepKey, shoppingKey, uniqueId } from './keys'

describe('normalizeName', () => {
  test('keeps letters and numbers of any script instead of collapsing them', () => {
    expect(normalizeName('玉ねぎ')).toBe('玉ねぎ')
    expect(normalizeName('にんじん')).toBe('にんじん')
    expect(normalizeName('玉ねぎ')).not.toBe(normalizeName('にんじん'))
    expect(shoppingKey('玉ねぎ', '')).not.toBe(shoppingKey('にんじん', ''))
    expect(normalizeName('Crème fraîche')).toBe('crème fraîche')
    expect(normalizeName('кефир')).toBe('кефир')
  })

  test('folds compatibility forms (NFKC) and case', () => {
    expect(normalizeName('ＯＮＩＯＮＳ')).toBe('onion')
    expect(normalizeName('ｶﾞｰﾘｯｸ')).toBe(normalizeName('ガーリック'))
    expect(normalizeName('İstanbul')).toBe(normalizeName('i̇stanbul'))
  })

  test('still collapses punctuation, parentheses, spacing and Latin plurals', () => {
    expect(normalizeName('  Yellow   Onions (large) ')).toBe('yellow onion')
    expect(normalizeName('Salt & pepper!')).toBe('salt & pepper')
  })
})

describe('key encoding is unambiguous', () => {
  test('colon in a part cannot shift into the other part (prep keys)', () => {
    expect(prepKey('a:b', 'c')).not.toBe(prepKey('a', 'b:c'))
    expect(prepKey('a', 'b')).toBe('a:b')
  })

  test('pipe in a name cannot shift into the unit (shopping keys)', () => {
    expect(shoppingKey('a|b', 'c')).not.toBe(shoppingKey('a', 'b|c'))
    expect(shoppingKey('Lemons', '')).toBe('lemon|')
  })

  test('the running-low identity never equals a recipe row identity', () => {
    expect(pantryLowKey('lime')).not.toBe(shoppingKey('lime', ''))
    expect(pantryLowKey('limes')).toBe(pantryLowKey('Lime'))
    expect(pantryLowKey('lime')).not.toContain('|')
  })

  test('units fold case and width', () => {
    expect(normalizeUnit('ＬＢ')).toBe('lb')
    expect(normalizeUnit('Tablespoons')).toBe('tbsp')
  })
})

describe('uniqueId reserved words', () => {
  test('never generates ids that collide with /recipes/new or /edit', () => {
    expect(uniqueId('New', [])).toBe('new-2')
    expect(uniqueId('new', [])).toBe('new-2')
    expect(uniqueId('Edit', [])).toBe('edit-2')
    expect(uniqueId('By id', [])).toBe('by-id-2')
    expect(uniqueId('by-id', ['by-id-2'])).toBe('by-id-3')
    expect(uniqueId('New', ['new-2'])).toBe('new-3')
    expect(uniqueId('New recipe', [])).toBe('new-recipe')
  })
})

describe('key encoding is total', () => {
  const lone = 'a\uD800b'
  test('lone surrogates never make key builders throw', () => {
    expect(() => shoppingKey(lone, lone)).not.toThrow()
    expect(() => pantryLowKey(lone)).not.toThrow()
    expect(() => prepKey(lone, '\uDC00')).not.toThrow()
    expect(() => normalizeName(lone)).not.toThrow()
  })
  test('well-formed pairs encode normally and stay distinct', () => {
    expect(shoppingKey('🍕', '')).not.toBe(shoppingKey('🍔', ''))
  })
  test('detects ill-formed text, including nested values and object keys', () => {
    expect(isWellFormed('ok 🍕')).toBe(true)
    expect(isWellFormed(lone)).toBe(false)
    expect(containsIllFormed({ a: [{ b: lone }] })).toBe(true)
    expect(containsIllFormed({ [lone]: 1 })).toBe(true)
    expect(containsIllFormed({ a: ['fine', 3, null] })).toBe(false)
  })
})
