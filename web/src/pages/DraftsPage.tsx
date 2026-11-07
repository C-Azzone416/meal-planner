import { useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { StateCard } from '../components/StateCard'
import { FridgePanel, PaperTitle, PreviewNotice } from '../components/FridgePanel'
import { formatDuration } from '../planner/format'
import { usePlanner } from '../planner/store'
import { DRAFT_GUESS_KEYS, type Draft, type DraftGuessKey } from '../planner/types'
import '../styles/recipes.css'
import { recipePath } from './recipePath'

const GUESS_LABELS: Record<DraftGuessKey, string> = {
  activeMinutes: 'ACTIVE TIME',
  totalMinutes: 'TOTAL TIME',
  attention: 'ATTENTION',
  prepAhead: 'PREP AHEAD',
  storage: 'STORAGE',
}

function guessValue(draft: Draft, key: DraftGuessKey): string {
  switch (key) {
    case 'activeMinutes':
      return `${draft.activeMinutes} min`
    case 'totalMinutes':
      return formatDuration(draft.totalMinutes)
    case 'attention':
      return draft.attention[0].toUpperCase() + draft.attention.slice(1)
    case 'prepAhead':
      return draft.prepAhead.length ? draft.prepAhead.map((step) => step.text).join(' · ') : 'Nothing to prep ahead'
    case 'storage': {
      const notes = draft.prepAhead.map((step) => step.storageNote).filter(Boolean)
      return notes.length ? notes.join(' · ') : 'No storage notes'
    }
  }
}

export function DraftsPage() {
  const planner = usePlanner()
  const navigate = useNavigate()
  const [discarding, setDiscarding] = useState<Draft | null>(null)
  const [message, setMessage] = useState('')
  const listRef = useRef<HTMLDivElement>(null)
  const { drafts } = planner

  const approve = (draft: Draft) => {
    const id = planner.approveDraft(draft.id)
    if (id) navigate(recipePath(id), { state: { saved: true } })
  }

  return <>
    <FridgePanel className="hero-panel">
      <PaperTitle words={['drafts', 'to', 'review']} />
      <p className="script-copy">your agent sent these over · nothing is saved until you say so</p>
    </FridgePanel>
    <PreviewNotice />
    <FridgePanel className="content-panel" paw="rust">
      <div ref={listRef} tabIndex={-1} className="rx-drafts" aria-label="Drafts waiting for review">
        {drafts.length === 0 ? (
          <StateCard icon="✎" heading="No drafts waiting">When your agent sends a recipe over, it will show up here for review.</StateCard>
        ) : drafts.map((draft) => {
          const confirmedCount = DRAFT_GUESS_KEYS.filter((key) => draft.confirmed[key]).length
          const allConfirmed = confirmedCount === DRAFT_GUESS_KEYS.length
          const headingId = `draft-${draft.id}-title`
          return <article className="paper-card draft-card" key={draft.id} aria-labelledby={headingId}>
            <header>
              <span className="tag draft-badge">Draft · not saved</span>
              <span>{confirmedCount} of {DRAFT_GUESS_KEYS.length} checked · rev {draft.revision}</span>
            </header>
            <h2 id={headingId}>{draft.icon} {draft.title}</h2>
            <p>Serves {draft.defaultServings} · {draft.ingredients.length} {draft.ingredients.length === 1 ? 'ingredient' : 'ingredients'} · {draft.prepAhead.length + draft.prepTonight.length + draft.cookTonight.length} steps</p>
            {draft.warnings.length > 0 && <ul className="rx-warnings" aria-label="Draft warnings">{draft.warnings.map((warning) => <li key={warning}>⚠ {warning}</li>)}</ul>}
            <em>Structural check only. This is not food-safety verification, recipe review, or approval.</em>
            {DRAFT_GUESS_KEYS.map((key) => <section className="guess-row" key={key}>
              <div><small>{GUESS_LABELS[key]}</small><p>{guessValue(draft, key)}</p></div>
              <div>{draft.confirmed[key]
                ? <button type="button" className="button button--muted rx-button" aria-label={`Undo confirmation of ${GUESS_LABELS[key].toLowerCase()} for ${draft.title}`} onClick={() => planner.setDraftConfirmed(draft.id, key, false)}>✓ Confirmed · undo</button>
                : <button type="button" className="button button--green rx-button" aria-label={`Confirm ${GUESS_LABELS[key].toLowerCase()} for ${draft.title}`} onClick={() => planner.setDraftConfirmed(draft.id, key, true)}>Confirm</button>}</div>
            </section>)}
            <div className="button-row rx-draft-actions">
              <button type="button" className="button button--green rx-button" disabled={!allConfirmed} aria-describedby={allConfirmed ? undefined : `${headingId}-reason`} onClick={() => approve(draft)}>Approve and save recipe</button>
              <button type="button" className="button button--paper rx-button" onClick={() => setDiscarding(draft)}>Discard</button>
            </div>
            {!allConfirmed && <p id={`${headingId}-reason`} className="rx-reason">Check every guess to approve ({DRAFT_GUESS_KEYS.length - confirmedCount} left).</p>}
          </article>
        })}
      </div>
      <div role="status" className="rx-status">{message}</div>
    </FridgePanel>
    {discarding && <ConfirmDialog
      title="Discard this draft?"
      message={`“${discarding.title}” will be removed and can’t be recovered here.`}
      confirmLabel="Discard draft"
      cancelLabel="Keep draft"
      onConfirm={() => {
        planner.discardDraft(discarding.id)
        setDiscarding(null)
        setMessage(`Discarded “${discarding.title}”.`)
        listRef.current?.focus()
      }}
      onCancel={() => setDiscarding(null)}
    />}
  </>
}
