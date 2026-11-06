import { useState } from 'react'
import { Link } from 'react-router-dom'
import { FridgePanel, PaperTitle, PreviewNotice } from '../components/FridgePanel'
import { Magnet, PawMagnet } from '../components/Magnet'
import { recipes } from '../data/sampleData'

const days = [
  { short: 'S', label: 'Sunday', detail: 'prep day', color: 'cream' as const },
  { short: 'M', label: 'Monday', detail: recipes[0].title, color: 'rust' as const },
  { short: 'T', label: 'Tuesday', detail: 'open night', color: 'cream' as const },
  { short: 'W', label: 'Wednesday', detail: recipes[1].title, color: 'green' as const },
  { short: 'T', label: 'Thursday', detail: recipes[2].title, color: 'gold' as const },
  { short: 'F', label: 'Friday', detail: 'open night', color: 'cream' as const },
  { short: 'S', label: 'Saturday', detail: 'open night', color: 'cream' as const },
]

export function WeekPage() {
  const [note, setNote] = useState('')
  const [weekOffset, setWeekOffset] = useState(0)
  const weekLabels = ['nov 1 – 7', 'nov 8 – 14', 'nov 15 – 21']
  return <>
    <FridgePanel className="hero-panel">
      <PaperTitle words={['this', 'week’s', 'dinners']} />
      <div className="week-toolbar">
        <div><div className="date-control"><button type="button" aria-label="Previous week" disabled={weekOffset === -1} onClick={() => setWeekOffset((value) => value - 1)}>←</button><strong>{weekLabels[weekOffset + 1]}</strong><button type="button" aria-label="Next week" disabled={weekOffset === 1} onClick={() => setWeekOffset((value) => value + 1)}>→</button></div><p className="script-copy">3 nights planned · tap a day to preview its details</p></div>
        <div className="button-row"><button className="button button--gold" onClick={() => setNote('Surprise planning is part of the next functionality phase.')}>Surprise me</button><button className="button button--rust" onClick={() => setNote('Automatic planning is part of the next functionality phase.')}>Plan my week</button></div>
      </div>
      <div className="day-row" aria-label="Week at a glance">{days.map((day, index) => <button type="button" key={`${day.label}-${index}`} className="day-button" title={`${day.label}: ${day.detail}`} onClick={() => setNote(`${day.label}: ${day.detail}. Moving meals comes in the next functionality phase.`)}><Magnet color={day.color}>{day.short}</Magnet><span className="sr-only">{day.label}: {day.detail}</span></button>)}</div>
      {note && <p className="inline-note" role="status">{note}</p>}
    </FridgePanel>
    <PreviewNotice />
    <div className="week-grid">
      <div className="meal-card-grid">
        {recipes.slice(0, 3).map((recipe, index) => <article className="paper-card meal-card" key={recipe.title}>
          <span className="card-magnet"><Magnet color={index === 0 ? 'rust' : index === 1 ? 'green' : 'gold'}>{recipe.icon}</Magnet></span>
          <p className="rust-script">{recipe.day}</p><h2>{recipe.title}</h2><p>{recipe.activeMinutes} min hands-on · {recipe.total}</p>
          <div className="phase-chips"><span>① prep Sun</span><span>② 4m</span><span>③ {Math.max(recipe.activeMinutes - 4, 2)}m</span></div>
          {index === 2 && <Link className="button button--dark meal-action" to="/cook">Cook tonight →</Link>}
          <footer><strong>serves {recipe.serves}</strong><span>Preview only</span></footer>
        </article>)}
      </div>
      <aside className="sticky-note"><PawMagnet /><h2>today: sunday prep</h2><p><strong className="big-number">38</strong> of 45 min · 2 of 6 done</p><progress value="38" max="45">38 of 45 minutes</progress><Link className="button button--paper" to="/prep">Keep prepping →</Link></aside>
    </div>
  </>
}
