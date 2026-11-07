import { useId, useState } from 'react'
import { FridgePanel, PaperTitle, PreviewNotice } from '../components/FridgePanel'
import { StateCard } from '../components/StateCard'
import type { ShoppingRow } from '../planner/derive'
import { usePlanner } from '../planner/store'
import { DAY_NAMES } from '../planner/types'
import '../styles/shopping-prep.css'

function Row({ row }: { row: ShoppingRow }) {
  const { toggleShoppingChecked, togglePantryLow } = usePlanner()
  const id = useId()
  const cls = `sp-row${row.checked ? ' sp-row--checked' : ''}${row.status === 'needs-more' ? ' sp-row--needs-more' : ''}`
  return (
    <div className={cls}>
      <label className="sp-check" htmlFor={id}>
        <input id={id} type="checkbox" checked={row.checked} onChange={() => toggleShoppingChecked(row.key)} />
        <span className="sp-text">
          <span className="sp-name">{row.name}</span>
          {row.mealsLabel && <span className="sp-meals">{row.mealsLabel}</span>}
          {row.note && <span className="sp-note">{row.note}</span>}
        </span>
      </label>
      <strong className="sp-amount">{row.amount}</strong>
      {row.source === 'pantry-low' && (
        <button type="button" className="sp-small-button" aria-label={`${row.name} restocked`} onClick={() => togglePantryLow(row.name)}>
          Restocked
        </button>
      )}
    </div>
  )
}

export function ShoppingPage() {
  const { shopping, pantry, dismissNoLongerNeeded, togglePantryLow } = usePlanner()
  const [view, setView] = useState<'list' | 'pantry'>('list')
  const [grouping, setGrouping] = useState<'aisle' | 'meal'>('aisle')
  const lowCount = pantry.filter((item) => item.low).length
  const { counts, noLongerNeeded } = shopping
  const empty = counts.total === 0 && noLongerNeeded.length === 0

  return <>
    <FridgePanel className="hero-panel shopping-hero">
      <div>
        <PaperTitle words={['shopping', 'list']} />
        <p className="script-copy">
          {counts.total === 0 ? 'nothing to buy yet' : `${counts.toBuy} to buy · ${counts.total} items · for this week’s dinners`}
        </p>
      </div>
      <div className="shopping-controls">
        <div className="segmented sp-segmented">
          <button type="button" aria-pressed={view === 'list'} className={view === 'list' ? 'selected' : ''} onClick={() => setView('list')}>This week’s list</button>
          <button type="button" aria-pressed={view === 'pantry'} className={view === 'pantry' ? 'selected' : ''} onClick={() => setView('pantry')}>Pantry shelf · {lowCount} low</button>
        </div>
        {view === 'list' && (
          <div className="segmented segmented--small sp-segmented">
            <button type="button" aria-pressed={grouping === 'aisle'} className={grouping === 'aisle' ? 'selected' : ''} onClick={() => setGrouping('aisle')}>By aisle</button>
            <button type="button" aria-pressed={grouping === 'meal'} className={grouping === 'meal' ? 'selected' : ''} onClick={() => setGrouping('meal')}>By meal</button>
          </div>
        )}
      </div>
    </FridgePanel>
    <PreviewNotice />
    <FridgePanel className="content-panel" paw="green">
      {view === 'pantry' ? (
        <section aria-labelledby="pantry-heading">
          <h2 id="pantry-heading">Pantry shelf</h2>
          <p className="sp-section-note">Staples never go on the list unless you mark them running low. Low items appear under Pantry aisle.</p>
          {pantry.length === 0 && <p className="empty-state">No pantry staples in your saved recipes yet.</p>}
          <div className="sp-grid">
            {pantry.map((item) => (
              <div className="sp-row" key={item.key}>
                <div className="sp-text">
                  <span className="sp-name">{item.name}</span>
                  <span className="sp-meals">
                    {item.usedBy.length > 0
                      ? `used by: ${[...new Set(item.usedBy.map((meal) => meal.label))].join(' + ')}`
                      : item.usedByRecipes.length > 0
                        ? `not needed this week · in ${item.usedByRecipes.length} saved recipe${item.usedByRecipes.length === 1 ? '' : 's'}`
                        : 'not in any saved recipe'}
                  </span>
                  {item.low && <span className="sp-pantry-note">On your shopping list under Pantry aisle</span>}
                </div>
                <span />
                <button type="button" className="sp-small-button" aria-pressed={item.low} aria-label={`${item.name} running low`} onClick={() => togglePantryLow(item.key)}>
                  {item.low ? 'Running low ✓' : 'Running low'}
                </button>
              </div>
            ))}
          </div>
        </section>
      ) : empty ? (
        <StateCard icon="🛒" heading="Nothing to buy — plan a dinner first" action={{ label: 'Go to This week', to: '/week' }}>
          Your list is built from the dinners on This week.
        </StateCard>
      ) : (
        <>
          {grouping === 'aisle' ? (
            <div className="sp-grid">
              {shopping.groups.map((group) => (
                <section className="sp-group sp-section" key={group.aisle} aria-label={group.aisle}>
                  <h2>{group.aisle}</h2>
                  {group.rows.map((row) => <Row key={row.key} row={row} />)}
                </section>
              ))}
            </div>
          ) : (
            <div className="sp-grid">
              {shopping.byMeal.map(({ meal, rows }) => (
                <section className="sp-group sp-section" key={meal.day} aria-label={`${DAY_NAMES[meal.day]} · ${meal.title}`}>
                  <h2>{DAY_NAMES[meal.day]} · {meal.label}</h2>
                  {rows.length === 0 && <p className="sp-section-note">Nothing to buy for this dinner.</p>}
                  {rows.map((row) => <Row key={row.key} row={row} />)}
                </section>
              ))}
            </div>
          )}
          {noLongerNeeded.length > 0 && (
            <section className="sp-gone" aria-labelledby="gone-heading">
              <h2 id="gone-heading">No longer needed</h2>
              <p className="sp-section-note">You already checked these, but your plan changed. Nothing is lost; dismiss them when you are ready.</p>
              {noLongerNeeded.map((row) => (
                <div className="sp-row sp-row--gone" key={row.key}>
                  <div className="sp-text">
                    <span className="sp-name">{row.name}</span>
                    <span className="sp-badge sp-badge--gone">checked · no longer needed</span>
                  </div>
                  <strong className="sp-amount">{row.amount}</strong>
                  <button type="button" className="sp-small-button" aria-label={`Dismiss ${row.name}`} onClick={() => dismissNoLongerNeeded('shopping', row.key)}>Dismiss</button>
                </div>
              ))}
            </section>
          )}
        </>
      )}
    </FridgePanel>
  </>
}
