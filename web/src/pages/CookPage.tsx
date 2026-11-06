import { useState } from 'react'
import { NavLink } from 'react-router-dom'
import { FridgePanel, PreviewNotice } from '../components/FridgePanel'
import { PawMagnet } from '../components/Magnet'
import { cookIngredients, cookSteps } from '../data/sampleData'

export function CookPage() {
  const [done, setDone] = useState(() => cookSteps.map((step) => step.done))
  const active = Math.min(done.findIndex((value) => !value) === -1 ? done.length - 1 : done.findIndex((value) => !value), done.length - 1)
  const advance = () => setDone((items) => items.map((value, index) => index === active ? true : value))
  return <div className="cook-shell"><FridgePanel className="hero-panel cook-hero"><div className="cook-topline"><NavLink to="/week">‹ This week</NavLink><span>thursday · serves 5</span><NavLink to="/week" className="wordmark">dinner, sorted.</NavLink></div><h1 className="single-paper-title">Seared salmon, lemon-herb orzo</h1><p className="script-copy">step {active + 1} of {cookSteps.length}</p><div className="button-row"><span className="button button--paper">② Prep tonight ✓</span><span className="button button--dark">③ Cook tonight</span><button className="button button--green" onClick={advance}>Done — next step</button></div></FridgePanel><PreviewNotice /><FridgePanel className="content-panel"><div className="cook-layout"><div><p className="warning-note">🐈‍⬛ <strong>Stay close.</strong> About 8 minutes at the stove during the sear.</p><div className="task-list">{cookSteps.map((step, index) => <label className={`task-card cook-step${done[index] ? ' task-card--done' : ''}${active === index ? ' cook-step--active' : ''}`} key={step.title}><input type="checkbox" checked={done[index]} onChange={() => setDone((items) => items.map((value, i) => i === index ? !value : value))} /><span><strong>{step.title}</strong>{step.timer && <em className="timer">{step.timer}</em>}</span>{active === index && <PawMagnet color="rust" />}</label>)}</div></div><aside className="paper-card ingredients-card"><h2>Ingredients · for 5</h2>{cookIngredients.map(([name, amount]) => <div key={name}><span>{name}</span><strong>{amount}</strong></div>)}</aside></div></FridgePanel></div>
}
