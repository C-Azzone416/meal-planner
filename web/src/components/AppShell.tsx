import type { ReactNode } from 'react'
import { NavLink } from 'react-router-dom'

const links = [
  { to: '/week', label: 'This week', short: 'Week' },
  { to: '/recipes', label: 'Recipes', short: 'Recipes' },
  { to: '/prep', label: 'Prep', short: 'Prep' },
  { to: '/shopping', label: 'Shopping', short: 'Shop' },
  { to: '/drafts', label: 'Drafts', short: 'Drafts', count: 2 },
]

function MainLinks({ mobile = false }: { mobile?: boolean }) {
  return <>{links.map((link) => (
    <NavLink key={link.to} to={link.to} className={({ isActive }) => isActive ? 'nav-link nav-link--active' : 'nav-link'}>
      <span aria-hidden="true" className="nav-symbol">{mobile ? ({ Week: '◷', Recipes: '▤', Prep: '✓', Shop: '☷', Drafts: '✎' }[link.short] ?? '•') : ''}</span>
      <span>{mobile ? link.short : link.label}</span>
      {link.count && <span className="nav-count" aria-label={`${link.count} drafts`}>{link.count}</span>}
    </NavLink>
  ))}</>
}

export function AppShell({ children, onSignOut }: { children: ReactNode; onSignOut?: () => void }) {
  return (
    <div className="app-shell">
      <header className="brand-row">
        <NavLink to="/week" className="wordmark">dinner, sorted.</NavLink>
        <nav className="desktop-nav" aria-label="Main navigation"><MainLinks /></nav>
        <NavLink className={({ isActive }) => isActive ? 'settings-link settings-link--active' : 'settings-link'} to="/house-rules" aria-label="House rules">⚙</NavLink>
        {onSignOut && <button className="sign-out-link" type="button" onClick={onSignOut}>Sign out</button>}
      </header>
      <main className="page-stack">{children}</main>
      <nav className="mobile-nav" aria-label="Main navigation"><MainLinks mobile /></nav>
    </div>
  )
}
