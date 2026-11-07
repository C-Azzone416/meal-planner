import { useState } from 'react'
import { FridgePanel, PaperTitle, PreviewNotice } from '../components/FridgePanel'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { PawMagnet } from '../components/Magnet'
import { usePlanner } from '../planner/store'

function Stepper({ value, label, onChange }: { value: number; label: string; onChange: (value: number) => void }) {
  return <div className="stepper" role="group" aria-label={label}><button onClick={() => onChange(Math.max(1, value - 1))} aria-label={`Decrease ${label}`}>−</button><strong>{value} {label}</strong><button onClick={() => onChange(value + 1)} aria-label={`Increase ${label}`}>+</button></div>
}

function Choice({ options, value, onChange }: { options: string[]; value: string; onChange: (value: string) => void }) {
  return <div className="chip-row">{options.map((option) => <button type="button" aria-pressed={value === option} className={value === option ? 'chip chip--active' : 'chip'} onClick={() => onChange(option)} key={option}>{option}</button>)}</div>
}

export function HouseRulesPage() {
  const [people, setPeople] = useState(4)
  const [prep, setPrep] = useState('30–45')
  const [minutes, setMinutes] = useState('10–15')
  const [strictness, setStrictness] = useState('Just flag it')
  const [kitchen, setKitchen] = useState('One preps, one cooks')
  const [notice, setNotice] = useState('')
  const [confirmingReset, setConfirmingReset] = useState(false)
  const { resetPreviewData, houseRules, setHouseRules } = usePlanner()
  const resetPreview = () => {
    setConfirmingReset(false)
    resetPreviewData()
    setNotice('Preview data was reset.')
  }
  return <><FridgePanel className="hero-panel"><PaperTitle words={['house', 'rules']} /><p className="script-copy">defaults for every week · change anything on a single night’s card</p></FridgePanel><PreviewNotice /><FridgePanel className="content-panel"><div className="rules-grid"><article className="paper-card rule-card"><PawMagnet color="rust" /><h2>Usually cooking for</h2><p>Every dinner starts here. Change any single night on its card.</p><Stepper value={people} label="people" onChange={setPeople} /></article><article className="paper-card rule-card"><PawMagnet color="green" /><h2>Dinners per week</h2><p>How many nights Plan my week fills.</p><Stepper value={houseRules.targetDinners} label="dinners" onChange={(value) => setHouseRules({ targetDinners: value })} /></article><article className="paper-card rule-card"><PawMagnet color="purple" /><h2>Weekend prep target</h2><p>A target, not a filter. Longer recipes still show up with a note.</p><Choice options={['20–30', '30–45', '45–60']} value={prep} onChange={setPrep} /></article><article className="paper-card rule-card"><PawMagnet color="rust" /><h2>Weeknight hands-on target</h2><p>Active minutes a night, including that night’s prep.</p><Choice options={['under 10', '10–15', '15–25']} value={minutes} onChange={setMinutes} /></article><article className="paper-card rule-card"><PawMagnet color="green" /><h2>How strict are targets?</h2><p>Nothing is hidden unless you decide that later.</p><Choice options={['Just flag it', 'Balance the week', 'Stick to it']} value={strictness} onChange={setStrictness} /></article><article className="paper-card rule-card"><PawMagnet color="purple" /><h2>Who’s in the kitchen</h2><p>Plans never assume two people working at once.</p><Choice options={['One preps, one cooks', 'One person does it all']} value={kitchen} onChange={setKitchen} /></article></div><button className="button button--green save-rules" disabled aria-describedby="save-rules-reason">Save house rules</button><p className="form-hint" id="save-rules-reason">Only “Dinners per week” is saved so far; the other choices are coming soon.</p><button type="button" className="button button--paper save-rules" onClick={() => setConfirmingReset(true)}>Reset preview data</button>{notice && <p role="status" className="inline-note">{notice}</p>}</FridgePanel>{confirmingReset && <ConfirmDialog title="Reset preview data?" message="Recipes, your week and your progress go back to the sample set. This cannot be undone." confirmLabel="Reset data" cancelLabel="Keep my data" onConfirm={resetPreview} onCancel={() => setConfirmingReset(false)} />}</>
}
