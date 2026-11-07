import { FridgePanel, PaperTitle, PreviewNotice } from '../components/FridgePanel'
import { PawMagnet } from '../components/Magnet'
import { StateCard } from '../components/StateCard'
import { usePlanner } from '../planner/store'
import { DAY_NAMES, DAY_SHORT_NAMES } from '../planner/types'
import '../styles/shopping-prep.css'

export function PrepPage() {
  const { prep, summary, togglePrepDone, confirmPrepTopUp, dismissNoLongerNeeded } = usePlanner()
  const { tasks, totals, doneNoLongerPlanned } = prep
  const warnings = tasks.filter((task) => task.warning)
  const stored = tasks.filter((task) => task.storageNote)
  // "Nothing to prep" means nothing is planned, not merely that no planned recipe has prep-ahead steps.
  const empty = summary.planned === 0 && doneNoLongerPlanned.length === 0
  const prepDayName = DAY_NAMES[prep.prepDay].toLowerCase()

  return <>
    <FridgePanel className="hero-panel prep-hero">
      <div>
        <PaperTitle words={[prepDayName, 'prep']} />
        <p className="script-copy">one pass · stove and board used once</p>
      </div>
      <div className="progress-block">
        <p><strong className="big-number">{totals.minutes}</strong> min planned · {totals.done} of {totals.tasks} done · {totals.minutesLeft} min left</p>
        <progress value={totals.done} max={Math.max(totals.tasks, 1)}>{totals.done} of {totals.tasks}</progress>
      </div>
      {warnings.length > 0 && (
        <div className="warning-note">
          🐈‍⬛ <strong>Heads up on timing.</strong>
          {warnings.map((task) => <span className="sp-warning" key={task.key}>{task.warning?.message}</span>)}
        </div>
      )}
    </FridgePanel>
    <PreviewNotice />
    <FridgePanel className="content-panel" paw="green">
      {empty ? (
        <StateCard icon="🥕" heading="Nothing to prep — plan a dinner first" action={{ label: 'Go to This week', to: '/week' }}>
          Prep tasks come from the dinners on This week.
        </StateCard>
      ) : (
        <div className="prep-layout">
          <div>
            <h2>In this order</h2>
            {tasks.length === 0 && <p className="sp-section-note">None of this week’s dinners have prep-ahead steps.</p>}
            <div className="task-list">
              {tasks.map((task) => (
                <div className="sp-prep-item" key={task.key}>
                  <label className={`task-card sp-prep-task${task.done ? ' task-card--done' : ''}`}>
                    <input type="checkbox" checked={task.done} onChange={() => togglePrepDone(task.key)} />
                    <span>
                      <strong className="sp-name">{task.text}</strong>
                      {task.storageNote && <small>{task.storageNote}</small>}
                      <em>{DAY_SHORT_NAMES[task.day]} · {task.meal.title}</em>
                      {task.status === 'topup' && <span className="sp-badge">Needs a top-up · servings went up since you did this</span>}
                      {task.warning && <span className="sp-warning">{task.warning.message}</span>}
                    </span>
                    <b>{task.minutes}m</b>
                  </label>
                  {task.status === 'topup' && (
                    <button type="button" className="sp-small-button" aria-label={`Top-up done for ${task.text}`} onClick={() => confirmPrepTopUp(task.key)}>
                      Top-up done
                    </button>
                  )}
                </div>
              ))}
            </div>
            {doneNoLongerPlanned.length > 0 && (
              <section className="sp-gone" aria-labelledby="prep-gone-heading">
                <h2 id="prep-gone-heading">Done, no longer planned</h2>
                <p className="sp-section-note">You finished these before the plan changed. They stay here until you dismiss them.</p>
                {doneNoLongerPlanned.map((task) => (
                  <div className="sp-row sp-row--gone" key={task.key}>
                    <div className="sp-text">
                      <span className="sp-name">{task.text}</span>
                      <span className="sp-meals">{task.mealTitle}</span>
                      <span className="sp-badge sp-badge--gone">done · no longer planned</span>
                    </div>
                    <b>{task.minutes}m</b>
                    <button type="button" className="sp-small-button" aria-label={`Dismiss ${task.text}`} onClick={() => dismissNoLongerNeeded('prep', task.key)}>Dismiss</button>
                  </div>
                ))}
              </section>
            )}
          </div>
          {stored.length > 0 && (
            <aside>
              <div className="sticky-note compact">
                <PawMagnet />
                <h2>where it goes</h2>
                <ul className="sp-storage-list">
                  {stored.map((task) => <li key={task.key}>{task.label} — {task.storageNote}</li>)}
                </ul>
              </div>
            </aside>
          )}
        </div>
      )}
    </FridgePanel>
  </>
}
