import { useId, type ReactNode } from 'react'
import { PawMagnet } from './Magnet'

export function StateCard({ title, children, action }: {
  title: string
  children: ReactNode
  action: ReactNode
}) {
  const heading = useId()

  return (
    <section className="paper-card planner-state-card" aria-labelledby={heading}>
      <span className="card-magnet" aria-hidden="true"><PawMagnet color="gold" /></span>
      <h2 id={heading}>{title}</h2>
      <p>{children}</p>
      {action}
    </section>
  )
}
