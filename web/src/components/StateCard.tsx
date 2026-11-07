import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import '../styles/states.css'
import { Magnet } from './Magnet'

export interface StateAction {
  label: string
  /** Renders a link. */
  to?: string
  /** Renders a button. */
  onClick?: () => void
  /** A disabled action must say why (rendered visibly under the button). */
  disabled?: boolean
  disabledReason?: string
}

export interface StateCardProps {
  /** Emoji or short glyph shown on the magnet. */
  icon?: string
  heading: string
  /** One sentence. */
  children?: ReactNode
  /** At most one primary action. */
  action?: StateAction
  tone?: 'neutral' | 'error'
  /** Live-region semantics: errors use 'alert', everything else 'status' (default: none for static empties). */
  live?: 'status' | 'alert'
}

/**
 * The one component for every non-happy state (empty, no results, error, not available yet).
 * Paper card, icon magnet, heading, one sentence, at most one primary action. See docs/ux-decisions.md §4.
 */
export function StateCard({ icon = '🐾', heading, children, action, tone = 'neutral', live }: StateCardProps) {
  const role = live ?? (tone === 'error' ? 'alert' : undefined)
  return (
    <section className={`state-card${tone === 'error' ? ' state-card--error' : ''}`} role={role}>
      <span className="state-card__magnet"><Magnet color={tone === 'error' ? 'rust' : 'green'}>{icon}</Magnet></span>
      <h2>{heading}</h2>
      {children && <p>{children}</p>}
      {action && <StateActionView action={action} />}
    </section>
  )
}

function StateActionView({ action }: { action: StateAction }) {
  if (action.to && !action.disabled) {
    return <Link className="button button--dark state-card__action" to={action.to}>{action.label}</Link>
  }
  const reasonId = action.disabled && action.disabledReason ? `reason-${action.label.replace(/\W+/g, '-').toLowerCase()}` : undefined
  return (
    <>
      <button
        type="button"
        className="button button--dark state-card__action"
        disabled={action.disabled}
        aria-describedby={reasonId}
        onClick={action.onClick}
      >
        {action.label}
      </button>
      {reasonId && <p className="state-card__reason" id={reasonId}>{action.disabledReason}</p>}
    </>
  )
}

/** Loading placeholder: skeleton cards inside a busy region. No shimmer under prefers-reduced-motion (CSS). */
export function PageSkeleton({ label = 'Loading', cards = 3 }: { label?: string; cards?: number }) {
  return (
    <section className="page-skeleton" role="status" aria-busy="true" aria-label={label}>
      <span className="sr-only">{label}…</span>
      {Array.from({ length: cards }, (_, index) => <div className="page-skeleton__card" aria-hidden="true" key={index} />)}
    </section>
  )
}
