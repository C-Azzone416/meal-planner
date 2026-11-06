import { useMemo, useState } from 'react'
import { FridgePanel, PaperTitle, PreviewNotice } from '../components/FridgePanel'
import { PawMagnet } from '../components/Magnet'
import { prepTasks } from '../data/sampleData'

export function PrepPage() {
  const [done, setDone] = useState(() => prepTasks.map((task) => task.done))
  const complete = done.filter(Boolean).length
  const minutesLeft = useMemo(() => prepTasks.reduce((total, task, index) => total + (done[index] ? 0 : task.minutes), 0), [done])
  return <>
    <FridgePanel className="hero-panel prep-hero"><div><PaperTitle words={['sunday', 'prep']} /><p className="script-copy">one pass · stove and board used once</p></div><div className="progress-block"><p><strong className="big-number">38</strong> min planned · {complete} of {done.length} done · {minutesLeft} min left</p><progress value={complete} max={done.length}>{complete} of {done.length}</progress></div><p className="warning-note">🐈‍⬛ <strong>Keeping an eye on Thursday’s sauce.</strong> It’s at its 4-day limit.</p></FridgePanel>
    <PreviewNotice />
    <FridgePanel className="content-panel" paw="green"><div className="prep-layout"><div><h2>In this order</h2><div className="task-list">{prepTasks.map((task, index) => <label className={done[index] ? 'task-card task-card--done' : 'task-card'} key={task.title}><input type="checkbox" checked={done[index]} onChange={() => setDone((current) => current.map((value, itemIndex) => itemIndex === index ? !value : value))} /><span><strong>{task.title}</strong>{task.note && <small>{task.note}</small>}<em>{task.meal}</em></span><b>{task.minutes}m</b></label>)}</div></div><aside><div className="sticky-note compact"><PawMagnet /><h2>containers to grab</h2><ul><li>zip bag — chicken</li><li>big tub — chili veg + spices</li><li>small jar — herb sauce</li><li>airtight tub — orzo</li></ul></div></aside></div></FridgePanel>
  </>
}
