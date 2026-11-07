import { useEffect, useRef } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { FridgePanel, PreviewNotice } from '../components/FridgePanel'
import { StateCard } from '../components/StateCard'
import { formatAmount, formatDuration, formatTimer } from '../planner/format'
import { usePlanner } from '../planner/store'
import { DAY_NAMES } from '../planner/types'
import '../styles/week.css'

export function CookPage() {
  const [searchParams] = useSearchParams()
  const { plan, getRecipe } = usePlanner()
  const heading = useRef<HTMLHeadingElement>(null)
  const requestedDay = searchParams.get('day')
  const planned = plan.nights.filter((night) => night.status === 'planned')
  const today = new Date().getDay()
  const night = requestedDay === null
    ? planned.find(({ day }) => day === today) ?? planned[0]
    : planned.find(({ day }) => String(day) === requestedDay)
  const recipe = getRecipe(night?.recipeId)

  useEffect(() => {
    heading.current?.focus()
  }, [requestedDay])

  if (!night || !recipe) {
    return (
      <div className="cook-shell planner-view">
        <FridgePanel className="hero-panel cook-hero">
          <h1 ref={heading} tabIndex={-1} className="single-paper-title">Cook tonight</h1>
          <StateCard icon="🍳" heading="Nothing to cook tonight" action={{ label: 'This week →', to: '/week' }}>
            Pick a dinner from your week to start cooking.
          </StateCard>
        </FridgePanel>
        <PreviewNotice />
      </div>
    )
  }

  const servings = night.servings ?? recipe.defaultServings
  const factor = servings / recipe.defaultServings

  return (
    <div className="cook-shell planner-view">
      <FridgePanel className="hero-panel cook-hero">
        <div className="cook-topline">
          <Link className="button button--paper" to="/week">‹ This week</Link>
          <span>{DAY_NAMES[night.day]} · Serves {servings}</span>
        </div>
        <h1 ref={heading} tabIndex={-1} className="single-paper-title">{recipe.title}</h1>
        <p className="script-copy">{recipe.activeMinutes} min hands-on · {formatDuration(recipe.totalMinutes)} total</p>
      </FridgePanel>
      <PreviewNotice />
      <FridgePanel className="content-panel cook-content">
        <div className="cook-layout">
          <div className="cook-phases">
            <section aria-labelledby="prep-tonight-title">
              <h2 id="prep-tonight-title">Prep tonight</h2>
              {recipe.prepTonight.length > 0 ? (
                <ol className="task-list cook-steps">
                  {recipe.prepTonight.map((step) => (
                    <li className="task-card" key={step.id}>
                      <strong>{step.text}</strong>
                      <span>{formatDuration(step.minutes)}</span>
                    </li>
                  ))}
                </ol>
              ) : <p>No prep steps for tonight.</p>}
            </section>
            <section aria-labelledby="cook-tonight-title">
              <h2 id="cook-tonight-title">Cook tonight</h2>
              <ol className="task-list cook-steps">
                {recipe.cookTonight.map((step) => (
                  <li className="task-card" key={step.id}>
                    <strong>{step.text}</strong>
                    {step.timerSeconds !== null && <em className="timer">Timer {formatTimer(step.timerSeconds)}</em>}
                  </li>
                ))}
              </ol>
            </section>
            <section aria-labelledby="cook-feedback-title">
              <h2 id="cook-feedback-title">After dinner</h2>
              <button type="button" className="button button--paper" disabled aria-describedby="cook-feedback-reason">How did it go?</button>
              <p className="form-hint" id="cook-feedback-reason">Feedback after cooking — coming soon</p>
            </section>
          </div>
          <aside className="paper-card ingredients-card" aria-labelledby="cook-ingredients-title">
            <h2 id="cook-ingredients-title">Ingredients · for {servings}</h2>
            <dl className="cook-ingredients">
              {recipe.ingredients.map((ingredient) => (
                <div key={ingredient.id}>
                  <dt>{ingredient.name}</dt>
                  <dd>{ingredient.quantity === null ? 'To taste' : formatAmount(ingredient.quantity * factor, ingredient.unit)}</dd>
                </div>
              ))}
            </dl>
          </aside>
        </div>
      </FridgePanel>
    </div>
  )
}
