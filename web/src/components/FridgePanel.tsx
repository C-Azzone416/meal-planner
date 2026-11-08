import type { ReactNode, Ref } from 'react'
import { useRemoteRecipes } from '../recipes/RemoteRecipes'
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

export function PaperTitle({ words, headingRef }: { words: string[]; headingRef?: Ref<HTMLHeadingElement> }) {
  return <h1 className="paper-title" aria-label={words.join(' ')} ref={headingRef} tabIndex={headingRef ? -1 : undefined}>{words.map((word) => <span key={word}>{word}</span>)}</h1>
}

export function PreviewNotice() {
  const remote = useRemoteRecipes()
  const { resetNotice, dismissResetNotice, persistenceStatus } = usePlanner()
  return (
    <>
      <p className="preview-notice">{remote ? `Manual recipes save to your account database. Sample recipes, drafts and planning stay in this browser${persistenceStatus === 'saved' ? ' only.' : '; browser changes are not saved.'} Existing browser recipes are not automatically uploaded.` : persistenceStatus === 'saved' ? 'Preview data · saved in this browser only' : 'Preview data · changes not saved'}</p>
      {resetNotice && (
        <div className="reset-notice" role="status">
          <span>{resetNotice}</span>
          <button type="button" onClick={dismissResetNotice}>Dismiss</button>
        </div>
      )}
    </>
  )
}
