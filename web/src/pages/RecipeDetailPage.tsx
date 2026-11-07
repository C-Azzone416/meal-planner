import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { FridgePanel, PreviewNotice } from '../components/FridgePanel'
import { Magnet } from '../components/Magnet'
import { focusPageHeading } from '../components/RouteFocus'
import { StateCard } from '../components/StateCard'
import { formatAmount, formatDuration, formatTimer, joinList } from '../planner/format'
import { usePlanner } from '../planner/store'
import { DAY_NAMES } from '../planner/types'
import '../styles/recipes.css'
import { addToWeekNight } from './addToWeek'

const ATTENTION_LABEL = { low: 'Low attention', medium: 'Medium attention', high: 'High attention' } as const

export function RecipeNotFound() {
  useEffect(() => {
    focusPageHeading()
  }, [])
  return <FridgePanel className="content-panel">
    <StateCard level={1} icon="🔍" heading="Recipe not found" action={{ label: 'Back to Recipes', to: '/recipes' }}>
      We couldn’t find that recipe. It may have been renamed or never saved in this browser.
    </StateCard>
  </FridgePanel>
}

export function RecipeDetailPage() {
  const { recipeId } = useParams()
  const planner = usePlanner()
  const recipe = planner.getRecipe(recipeId)
  if (!recipe) return <><RecipeNotFound /><PreviewNotice /></>
  return <RecipeDetail key={recipe.id} />
}

function RecipeDetail() {
  const { recipeId } = useParams()
  const planner = usePlanner()
  const navigate = useNavigate()
  const location = useLocation()
  const recipe = planner.getRecipe(recipeId)
  const headingRef = useRef<HTMLHeadingElement>(null)

  // "Saved" comes from the save redirect. Remember it, then drop it from history so a reload doesn't repeat it.
  const [saved] = useState(() => Boolean((location.state as { saved?: boolean } | null)?.saved))
  const [statusSeqAtMount] = useState(() => planner.status?.seq ?? 0)
  useEffect(() => {
    headingRef.current?.focus()
    if (saved) navigate(location.pathname, { replace: true, state: null })
    // eslint-disable-next-line react-hooks/exhaustive-deps -- run once on arrival
  }, [])

  if (!recipe) return null

  const plannedDays = planner.plan.nights.filter((night) => night.status === 'planned' && night.recipeId === recipe.id).map((night) => night.day)
  const openDay = addToWeekNight(planner.plan, planner.houseRules.prepDay)
  const status = planner.status && (saved || planner.status.seq > statusSeqAtMount) ? planner.status : null
  const reasonId = 'add-to-week-reason'

  return <>
    <FridgePanel className="hero-panel rx-hero">
      <Link className="rx-back" to="/recipes">← All recipes</Link>
      <div className="rx-title-row">
        <Magnet color="rust">{recipe.icon}</Magnet>
        <h1 className="single-paper-title rx-title" tabIndex={-1} ref={headingRef}>{recipe.title}</h1>
      </div>
      <dl className="rx-facts">
        <div><dt>Hands-on</dt><dd>{recipe.activeMinutes} min</dd></div>
        <div><dt>Total time</dt><dd>{formatDuration(recipe.totalMinutes)}</dd></div>
        <div><dt>Attention</dt><dd>{ATTENTION_LABEL[recipe.attention]}</dd></div>
        <div><dt>Serves</dt><dd>{recipe.defaultServings}</dd></div>
      </dl>
      {recipe.tags.length > 0 && <div className="tag-list" aria-label="Tags">{recipe.tags.map((tag) => <span key={tag}>{tag}</span>)}</div>}
      {plannedDays.length > 0 && <p className="rust-script">On this week: <Link to="/week">{joinList(plannedDays.map((day) => DAY_NAMES[day]))}</Link></p>}
      <div className="button-row rx-actions">
        <Link className="button button--paper rx-button" to={`/recipes/${recipe.id}/edit`}>Edit</Link>
        <button
          type="button"
          className="button button--green rx-button"
          disabled={openDay === undefined}
          aria-describedby={openDay === undefined ? reasonId : undefined}
          onClick={() => openDay !== undefined && planner.assignRecipe(openDay, recipe.id)}
        >{openDay !== undefined ? `Add to week · ${DAY_NAMES[openDay]}` : 'Add to week'}</button>
      </div>
      {openDay === undefined && <p id={reasonId} className="rx-reason">Every night this week is already planned, skipped or pinned. Open a night on This week first.</p>}
      <div role="status" className="rx-status">
        {saved && <span className="rx-saved">Saved.</span>}
        {status && <span>{status.text}</span>}
      </div>
    </FridgePanel>
    <PreviewNotice />
    <FridgePanel className="content-panel rx-body">
      <section aria-labelledby="rx-ingredients">
        <h2 id="rx-ingredients">Ingredients · for {recipe.defaultServings}</h2>
        {recipe.ingredients.length === 0 ? <p className="rx-empty">No ingredients listed.</p> : (
          <ul className="rx-ingredients">{recipe.ingredients.map((item) => <li key={item.id}>
            <span className="rx-amount">{formatAmount(item.quantity, item.unit) || '—'}</span>
            <span>{item.name}{item.isPantry && <small> · pantry</small>}</span>
            <span className="rx-aisle">{item.aisle}</span>
          </li>)}</ul>
        )}
      </section>

      <section aria-labelledby="rx-prep-ahead">
        <h2 id="rx-prep-ahead">Prep ahead</h2>
        {recipe.prepAhead.length === 0 ? <p className="rx-empty">Nothing to prep ahead.</p> : (
          <ol className="rx-steps">{recipe.prepAhead.map((step) => <li key={step.id}>
            <span>{step.text}</span>
            <small>{step.minutes} min{step.keepsDays !== null ? ` · keeps ${step.keepsDays} ${step.keepsDays === 1 ? 'day' : 'days'}` : ''}</small>
            {step.storageNote && <small className="rx-storage">Storage: {step.storageNote}</small>}
          </li>)}</ol>
        )}
      </section>

      <section aria-labelledby="rx-prep-tonight">
        <h2 id="rx-prep-tonight">Prep tonight</h2>
        {recipe.prepTonight.length === 0 ? <p className="rx-empty">Nothing to prep tonight.</p> : (
          <ol className="rx-steps">{recipe.prepTonight.map((step) => <li key={step.id}>
            <span>{step.text}</span>
            <small>{step.minutes} min</small>
          </li>)}</ol>
        )}
      </section>

      <section aria-labelledby="rx-cook-tonight">
        <h2 id="rx-cook-tonight">Cook tonight</h2>
        {recipe.cookTonight.length === 0 ? <p className="rx-empty">No cooking steps yet.</p> : (
          <ol className="rx-steps">{recipe.cookTonight.map((step) => <li key={step.id}>
            <span>{step.text}</span>
            {step.timerSeconds !== null && <small>Timer {step.timerSeconds >= 3600 ? formatDuration(Math.round(step.timerSeconds / 60)) : formatTimer(step.timerSeconds)}</small>}
          </li>)}</ol>
        )}
      </section>
    </FridgePanel>
  </>
}
