import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { FridgePanel, PaperTitle, PreviewNotice } from '../components/FridgePanel'
import { Magnet, PawMagnet } from '../components/Magnet'
import { StateCard } from '../components/StateCard'
import { formatDuration } from '../planner/format'
import { usePlanner } from '../planner/store'
import { DAY_NAMES, DAY_SHORT_NAMES, type DayIndex, type Night } from '../planner/types'
import '../styles/week.css'

const mealColors = ['rust', 'green', 'gold'] as const

function PinButton({ night, onToggle, compact = false }: {
  night: Night
  onToggle: () => void
  compact?: boolean
}) {
  return (
    <button
      type="button"
      className={compact ? 'week-pin' : 'button button--paper'}
      aria-label={compact ? `Pin ${DAY_NAMES[night.day]} dinner` : undefined}
      aria-pressed={compact ? night.pinned : undefined}
      onClick={onToggle}
    >
      <span aria-hidden="true">📌</span> {compact ? (night.pinned ? 'Pinned' : 'Pin') : (night.pinned ? 'Unpin dinner' : 'Pin dinner')}
    </button>
  )
}

function NightDetail({ night }: { night: Night }) {
  const { getRecipe, plan, moveNight, setOpen, setSkipped, setServings, togglePin } = usePlanner()
  const [destination, setDestination] = useState<DayIndex | null>(null)
  const { day } = night
  const recipe = getRecipe(night.recipeId)
  const servings = night.servings ?? recipe?.defaultServings ?? 1
  const target = destination === null ? null : plan.nights[destination]
  const moveReason = target?.status === 'skipped' ? `${DAY_NAMES[target.day]} is skipped — make it open first.` : null

  return (
    <section className="paper-card week-night-detail" aria-labelledby="night-detail-title">
      <h2 id="night-detail-title">{DAY_NAMES[day]} dinner</h2>
      {recipe && <Link className="week-recipe-link" to={`/recipes/${encodeURIComponent(recipe.id)}`}>{recipe.title} →</Link>}
      {night.status === 'open' && <p>Open night</p>}
      {night.status === 'skipped' && <p>Skipped · no cooking</p>}
      <div className="week-night-controls">
        {night.status === 'planned' && (
          <>
            <div className="stepper week-servings" role="group" aria-label="Servings">
              <button type="button" aria-label="Decrease servings" disabled={servings <= 1} onClick={() => setServings(day, servings - 1)}>−</button>
              <strong>{servings} {servings === 1 ? 'serving' : 'servings'}</strong>
              <button type="button" aria-label="Increase servings" disabled={servings >= 12} onClick={() => setServings(day, servings + 1)}>+</button>
            </div>
            <PinButton night={night} onToggle={() => togglePin(day)} />
          </>
        )}
        {night.status !== 'skipped' && <button type="button" className="button button--paper" onClick={() => setSkipped(day)}>Skip night</button>}
        {night.status !== 'open' && <button type="button" className="button button--paper" onClick={() => setOpen(day)}>Make open</button>}
      </div>
      {night.status === 'planned' && (
        <form className="week-move" onSubmit={(event) => {
          event.preventDefault()
          if (target && target.status !== 'skipped') {
            moveNight(day, target.day)
            setDestination(null)
          }
        }}>
          <label>
            Move to another night
            <select
              value={destination ?? ''}
              aria-describedby={moveReason ? 'week-move-reason' : undefined}
              onChange={(event) => setDestination(
                plan.nights.find(({ day }) => String(day) === event.target.value)?.day ?? null,
              )}
            >
              <option value="">Choose a night</option>
              {plan.nights.filter((other) => other.day !== day).map((other) => (
                <option key={other.day} value={other.day}>
                  {DAY_NAMES[other.day]} · {other.status}
                </option>
              ))}
            </select>
          </label>
          <button type="submit" className="button button--dark" disabled={!target || target.status === 'skipped'}>
            {target?.status === 'planned' ? 'Swap nights' : 'Move dinner'}
          </button>
          {moveReason && <p id="week-move-reason">{moveReason}</p>}
        </form>
      )}
    </section>
  )
}

export function WeekPage() {
  const {
    plan, prep, summary, nightWarnings, getRecipe, fillWeek, proposeSurprise, keepProposal,
    cancelProposal, togglePin, status, proposal, undoInfo, undo, dismissUndo,
  } = usePlanner()
  const [selectedDay, setSelectedDay] = useState<DayIndex | null>(null)
  const heading = useRef<HTMLHeadingElement>(null)
  const surpriseButton = useRef<HTMLButtonElement>(null)
  const night = selectedDay === null ? null : plan.nights[selectedDay]
  const today = new Date().getDay()
  const planned = plan.nights.filter((night) => night.status === 'planned')
  const suggestion = proposal && proposal.day === selectedDay ? getRecipe(proposal.recipeId) : null
  let surpriseReason = ''
  if (!night) surpriseReason = 'Pick a night first'
  else if (night.pinned) surpriseReason = `${DAY_NAMES[night.day]} is pinned. Unpin it to try a surprise.`
  else if (night.status === 'skipped') surpriseReason = `${DAY_NAMES[night.day]} is skipped. Make it open to try a surprise.`

  useEffect(() => {
    heading.current?.focus()
  }, [])

  const undoSeq = undoInfo?.seq
  useEffect(() => {
    if (undoSeq === undefined) return
    const timer = window.setTimeout(dismissUndo, 10_000)
    return () => window.clearTimeout(timer)
  }, [dismissUndo, undoSeq])

  const fillButton = (
    <button type="button" className="button button--rust" onClick={() => {
      if (planned.length === 0) heading.current?.focus()
      fillWeek()
    }}>Plan my week</button>
  )

  return (
    <div className="week-planner planner-view">
      <FridgePanel className="hero-panel">
        <PaperTitle words={['this', 'week’s', 'dinners']} headingRef={heading} />
        <div className="week-toolbar">
          <p className="script-copy">{summary.label || `${planned.length} nights planned`} · Pick a night to see its details</p>
          <div className="week-toolbar-actions">
            <button
              type="button"
              ref={surpriseButton}
              className="button button--gold"
              aria-describedby={surpriseReason ? 'surprise-reason' : undefined}
              disabled={Boolean(surpriseReason)}
              onClick={() => { if (night) proposeSurprise(night.day) }}
            >Surprise me</button>
            {planned.length > 0 && fillButton}
          </div>
        </div>
        {surpriseReason && <p id="surprise-reason" className="week-surprise-reason">{surpriseReason}</p>}
        <p className={status ? 'inline-note' : 'sr-only'} role="status">
          {status && <span key={status.seq}>{status.text}</span>}
        </p>
        <ul className="week-nights" aria-label="Week at a glance">
          {plan.nights.map((item) => {
            const recipe = getRecipe(item.recipeId)
            const warnings = nightWarnings[item.day]
            const warningId = `night-warnings-${item.day}`
            return (
              <li key={item.day} className="week-night">
                <button
                  type="button"
                  className="week-night-select"
                  aria-label={DAY_NAMES[item.day]}
                  aria-pressed={selectedDay === item.day}
                  aria-describedby={`night-info-${item.day}${warnings.length ? ` ${warningId}` : ''}`}
                  onClick={() => {
                    if (proposal && proposal.day !== item.day) cancelProposal()
                    setSelectedDay(item.day)
                  }}
                >
                  <Magnet color={item.status === 'planned' ? 'green' : 'cream'}>{DAY_SHORT_NAMES[item.day]}</Magnet>
                  <span id={`night-info-${item.day}`} className="week-night-info">
                    <strong>{item.status}{item.day === today ? ' · Today' : ''}</strong>
                    {selectedDay === item.day && <span aria-hidden="true">✓ Selected</span>}
                    {recipe && <span>{recipe.title}</span>}
                    {item.servings !== undefined && <span>Serves {item.servings}</span>}
                  </span>
                </button>
                {item.status === 'planned' && <PinButton night={item} compact onToggle={() => togglePin(item.day)} />}
                {warnings.length > 0 && (
                  <ul id={warningId} className="week-night-warnings">
                    {warnings.map((warning, index) => <li key={index}>{warning}</li>)}
                  </ul>
                )}
              </li>
            )
          })}
        </ul>
        <div className={suggestion ? 'week-proposal inline-note' : 'sr-only'}>
          <p role="status">{suggestion && <>Try: <strong>{suggestion.title}</strong></>}</p>
          {suggestion && proposal && (
            <div className="button-row">
              <button type="button" className="button button--green" onClick={() => {
                keepProposal()
                surpriseButton.current?.focus()
              }}>Keep it</button>
              <button type="button" className="button button--paper" onClick={() => proposeSurprise(proposal.day)}>Try another</button>
              <button type="button" className="button button--paper" onClick={() => {
                cancelProposal()
                surpriseButton.current?.focus()
              }}>Cancel</button>
            </div>
          )}
        </div>
        {night && <NightDetail key={night.day} night={night} />}
      </FridgePanel>
      <PreviewNotice />
      <div className="week-grid">
        {planned.length === 0 ? (
          <StateCard title="No dinners yet" action={fillButton}>Fill open nights with recipes from your fridge.</StateCard>
        ) : (
          <div className="meal-card-grid">
            {planned.map((item, index) => {
              const recipe = getRecipe(item.recipeId)
              if (!recipe) return null
              return (
                <article className="paper-card meal-card" key={item.day}>
                  <span className="card-magnet" aria-hidden="true"><Magnet color={mealColors[index % mealColors.length]}>{recipe.icon}</Magnet></span>
                  <p id={`meal-day-${item.day}`} className="rust-script">{DAY_NAMES[item.day]}</p>
                  <h2>{recipe.title}</h2>
                  <p>{recipe.activeMinutes} min hands-on · {formatDuration(recipe.totalMinutes)} total</p>
                  <div className="phase-chips">
                    {recipe.prepAhead.length > 0 && <span>① Prep ahead</span>}
                    {recipe.prepTonight.length > 0 && <span>② Prep tonight</span>}
                    <span>③ Cook tonight</span>
                  </div>
                  <footer><strong>Serves {item.servings}</strong></footer>
                  {(item.day === today || item.day === selectedDay) && (
                    <Link className="button button--dark meal-action" to={`/cook?day=${item.day}`} aria-describedby={`meal-day-${item.day}`}>Cook tonight →</Link>
                  )}
                </article>
              )
            })}
          </div>
        )}
        <aside className="sticky-note">
          <span aria-hidden="true"><PawMagnet /></span>
          <h2>{DAY_NAMES[prep.prepDay]} prep</h2>
          {prep.totals.tasks > 0 ? (
            <>
              <p>{prep.totals.done} of {prep.totals.tasks} done · {prep.totals.minutesLeft} min left</p>
              <progress aria-label="Prep tasks done" value={prep.totals.done} max={prep.totals.tasks} />
            </>
          ) : <p>No prep-ahead tasks this week.</p>}
          <Link className="button button--paper" to="/prep">See prep plan →</Link>
        </aside>
      </div>
      <div className={undoInfo ? 'week-undo paper-card' : 'sr-only'}>
        <span role="status">{undoInfo?.message}</span>
        {undoInfo && <button type="button" className="button button--dark" onClick={() => {
          undo()
          heading.current?.focus()
        }}>Undo</button>}
      </div>
    </div>
  )
}
