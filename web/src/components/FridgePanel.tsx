import type { ReactNode } from 'react'
import { usePlanner } from '../planner/store'
import { PawMagnet } from './Magnet'

export function FridgePanel({ children, className = '', paw }: { children: ReactNode; className?: string; paw?: 'green' | 'rust' | 'gold' | 'purple' | 'cream' }) {
  return (
    <section className={`fridge-panel ${className}`.trim()}>
      {paw && <span className="panel-paw"><PawMagnet color={paw} /></span>}
      {children}
      <span className="fridge-handle" aria-hidden="true" />
    </section>
  )
}

export function PaperTitle({ words }: { words: string[] }) {
  return <h1 className="paper-title" aria-label={words.join(' ')}>{words.map((word) => <span key={word}>{word}</span>)}</h1>
}

export function PreviewNotice() {
  const { resetNotice, dismissResetNotice } = usePlanner()
  return (
    <>
      <p className="preview-notice">Preview data · saved in this browser only</p>
      {resetNotice && (
        <div className="reset-notice" role="status">
          <span>{resetNotice}</span>
          <button type="button" onClick={dismissResetNotice}>Dismiss</button>
        </div>
      )}
    </>
  )
}
