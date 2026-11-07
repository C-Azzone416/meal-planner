import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { FridgePanel, PreviewNotice } from '../components/FridgePanel'
import { StateCard } from '../components/StateCard'
import { formatAmount, formatDuration, formatTimer } from '../planner/format'
import { normalizeUnit } from '../planner/keys'
import { usePlanner } from '../planner/store'
import { DAY_NAMES } from '../planner/types'
import '../styles/week.css'

const completedCookSteps = new Map<string, ReadonlySet<string>>()
const measuredUnits = new Set(['cup', 'lb', 'oz', 'tbsp', 'tsp', 'quart', 'gallon', 'g', 'kg', 'ml', 'l'])

function roundCookQuantity(quantity: number, unit: string): number {
  if (quantity <= 0) return quantity
  const step = measuredUnits.has(normalizeUnit(unit)) ? 0.25 : 1
  return Math.max(1, Math.round(quantity / step)) * step
}

function formatCookTimer(seconds: number): string {
  const hours = Math.floor(seconds / 3600)
  if (hours === 0) return formatTimer(seconds)
  return `${hours}:${formatTimer(seconds % 3600).padStart(5, '0')}`
}

export function CookPage() {
  const [searchParams] = useSearchParams()
  const { plan, getRecipe } = usePlanner()
  const heading = useRef<HTMLHeadingElement>(null)
  const [completedSteps, setCompletedSteps] = useState(() => new Map(completedCookSteps))
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
  const isFallback = requestedDay === null && night.day !== today
  const dinnerKey = `${night.day}:${recipe.id}`
  const completed = completedSteps.get(dinnerKey)
  const phases = [
    {
      id: 'prep-tonight',
      title: 'Prep tonight',
      empty: 'No prep steps for tonight.',
      steps: recipe.prepTonight.map(({ id, text, minutes }) => ({
        id: `prep:${id}`,
        text,
        detail: <span>{formatDuration(minutes)}</span>,
      })),
    },
    {
      id: 'cook-tonight',
      title: 'Cook tonight',
      empty: 'No cooking steps for tonight.',
      steps: recipe.cookTonight.map(({ id, text, timerSeconds }) => ({
        id: `cook:${id}`,
        text,
        detail: timerSeconds === null ? null : <em className="timer">Timer {formatCookTimer(timerSeconds)}</em>,
      })),
    },
  ]
  const nextStep = phases.flatMap(({ steps }) => steps).find(({ id }) => !completed?.has(id))

  function setStepDone(stepId: string, done: boolean) {
    const next = new Set(completedCookSteps.get(dinnerKey))
    if (done) next.add(stepId)
    else next.delete(stepId)

    if (next.size > 0) completedCookSteps.set(dinnerKey, next)
    else completedCookSteps.delete(dinnerKey)
    setCompletedSteps(new Map(completedCookSteps))
  }

  return (
    <div className="cook-shell planner-view">
      <FridgePanel className="hero-panel cook-hero">
        <div className="cook-topline">
          <Link className="button button--paper" to="/week">‹ This week</Link>
          <span>{DAY_NAMES[night.day]} · Serves {servings}</span>
        </div>
        <h1 ref={heading} tabIndex={-1} className="single-paper-title">
          {isFallback ? `Cook ${DAY_NAMES[night.day]}’s dinner` : recipe.title}
        </h1>
        {isFallback && <h2 className="cook-recipe-title">{recipe.title}</h2>}
        <p className="script-copy">{recipe.activeMinutes} min hands-on · {formatDuration(recipe.totalMinutes)} total</p>
        <p role="status">{nextStep ? `Next: ${nextStep.text}` : 'All steps done'}</p>
        <button
          type="button"
          className="button button--green"
          disabled={!nextStep}
          onClick={() => { if (nextStep) setStepDone(nextStep.id, true) }}
        >Done — next step</button>
      </FridgePanel>
      <PreviewNotice />
      <FridgePanel className="content-panel cook-content">
        <div className="cook-layout">
          <div className="cook-phases">
            {phases.map(({ id, title, empty, steps }) => (
              <section key={id} aria-labelledby={`${id}-title`}>
                <h2 id={`${id}-title`}>{title}</h2>
                {steps.length > 0 ? (
                  <ol className="task-list cook-steps">
                    {steps.map((step) => (
                      <li className="task-card" key={step.id} aria-current={nextStep?.id === step.id ? 'step' : undefined}>
                        <label className="cook-step-check">
                          <input
                            type="checkbox"
                            checked={completed?.has(step.id) ?? false}
                            onChange={(event) => setStepDone(step.id, event.target.checked)}
                          />
                          <span>
                            <strong>{step.text}</strong>
                            {step.detail}
                          </span>
                        </label>
                      </li>
                    ))}
                  </ol>
                ) : <p>{empty}</p>}
              </section>
            ))}
            <section aria-labelledby="cook-feedback-title">
              <h2 id="cook-feedback-title">After dinner</h2>
              <button type="button" className="button button--paper" disabled aria-describedby="cook-feedback-reason">How did it go?</button>
              <p className="form-hint" id="cook-feedback-reason">Feedback after cooking — coming soon</p>
            </section>
          </div>
          <aside className="paper-card ingredients-card" aria-labelledby="cook-ingredients-title">
            <h2 id="cook-ingredients-title">Ingredients · for {servings}</h2>
            <dl className="cook-ingredients">
              {recipe.ingredients.map(({ id, name, quantity, unit }) => {
                const amount = quantity === null ? null : roundCookQuantity(quantity * factor, unit)

                return (
                  <div key={id}>
                    <dt>{name}</dt>
                    <dd>{amount === null ? 'To taste' : formatAmount(amount, unit)}</dd>
                  </div>
                )
              })}
            </dl>
          </aside>
        </div>
      </FridgePanel>
    </div>
  )
}
