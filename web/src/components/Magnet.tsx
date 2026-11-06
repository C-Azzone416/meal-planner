import type { ReactNode } from 'react'

type MagnetProps = {
  children: ReactNode
  color?: 'green' | 'rust' | 'gold' | 'purple' | 'cream'
  label?: string
  small?: boolean
}

export function Magnet({ children, color = 'green', label, small = false }: MagnetProps) {
  return (
    <span className={`magnet magnet--${color}${small ? ' magnet--small' : ''}`} aria-label={label} role={label ? 'img' : undefined}>
      {children}
    </span>
  )
}

export function PawMagnet({ color = 'green' }: { color?: MagnetProps['color'] }) {
  return <Magnet color={color} label="paw magnet">🐾</Magnet>
}
