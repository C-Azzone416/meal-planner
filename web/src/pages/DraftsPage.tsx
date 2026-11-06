import { useState } from 'react'
import { FridgePanel, PaperTitle, PreviewNotice } from '../components/FridgePanel'

const guesses = [
  ['ACTIVE TIME', '10 min'], ['TOTAL TIME', '6 h on low'], ['ATTENTION', 'Low'], ['PREP AHEAD', 'Dice onion + sweet potato the day before'], ['STORAGE', 'Diced sweet potato keeps 3 days, fridge'],
]

export function DraftsPage() {
  const [confirmed, setConfirmed] = useState([false, true, false, false, false])
  const [notice, setNotice] = useState('')
  return <>
    <FridgePanel className="hero-panel"><PaperTitle words={['drafts', 'to', 'review']} /><p className="script-copy">your agent sent these over · nothing is saved until you say so</p></FridgePanel>
    <PreviewNotice />
    <FridgePanel className="content-panel" paw="rust"><div className="draft-layout"><aside className="draft-list"><div className="draft-picker draft-picker--active">Black bean & sweet potato chili<small>rev 3 · reviewing now</small></div><button type="button" className="draft-picker" disabled title="The second draft detail is part of the next functionality phase">Chicken thigh traybake with fennel<small>detail preview comes next</small></button><pre>$ mealplanner draft list{`\n`}2 drafts · 0 skipped</pre></aside><article className="paper-card draft-detail"><header><span className="tag">Draft · rev 3</span><span>{confirmed.filter(Boolean).length} of {confirmed.length} checked</span></header><h2>Slow-cooker black bean & sweet potato chili</h2><p>Serves 4–6 · 11 ingredients · 7 steps</p><p><strong>● Structurally valid</strong> · 2 warnings</p><em>Structural check only. This is not food-safety verification, recipe review, or approval.</em>{guesses.map(([label, value], index) => <section className="guess-row" key={label}><div><small>{label}</small><p>{value}</p></div><div>{confirmed[index] ? <button type="button" className="button button--muted" onClick={() => setConfirmed((items) => items.map((item, i) => i === index ? false : item))}>✓ Confirmed · undo</button> : <><button type="button" className="button button--green" onClick={() => setConfirmed((items) => items.map((item, i) => i === index ? true : item))}>Confirm</button><button type="button" className="button button--paper" onClick={() => setNotice('Editing draft guesses comes in the next functionality phase.')}>Change</button></>}</div></section>)}<button type="button" className="button button--green full-button" disabled={!confirmed.every(Boolean)} onClick={() => setNotice('Review complete in this preview. Saving comes in the next functionality phase.')}>{confirmed.every(Boolean) ? 'Complete preview review' : 'Check every guess to continue'}</button>{notice && <p className="inline-note" role="status">{notice}</p>}</article></div></FridgePanel>
  </>
}
