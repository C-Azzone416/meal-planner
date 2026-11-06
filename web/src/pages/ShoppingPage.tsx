import { useMemo, useState } from 'react'
import { FridgePanel, PaperTitle, PreviewNotice } from '../components/FridgePanel'
import { shoppingGroups } from '../data/sampleData'

export function ShoppingPage() {
  const allItems = useMemo(() => shoppingGroups.flatMap((group) => group.items.map((item) => ({ ...item, group: group.name }))), [])
  const [checked, setChecked] = useState<string[]>([])
  const [view, setView] = useState<'list' | 'pantry'>('list')
  const [grouping, setGrouping] = useState<'aisle' | 'meal'>('aisle')
  const groups = grouping === 'aisle' ? shoppingGroups : [{ name: 'Monday · Wednesday · Thursday', items: allItems }]
  return <>
    <FridgePanel className="hero-panel shopping-hero"><div><PaperTitle words={['shopping', 'list']} /><p className="script-copy">{allItems.length - checked.length} to buy · {allItems.length} items · for this week’s dinners</p></div><div className="shopping-controls"><div className="segmented"><button type="button" aria-pressed={view === 'list'} className={view === 'list' ? 'selected' : ''} onClick={() => setView('list')}>This week’s list</button><button type="button" aria-pressed={view === 'pantry'} className={view === 'pantry' ? 'selected' : ''} onClick={() => setView('pantry')}>Pantry shelf · 1 low</button></div>{view === 'list' && <div className="segmented segmented--small"><button type="button" aria-pressed={grouping === 'aisle'} className={grouping === 'aisle' ? 'selected' : ''} onClick={() => setGrouping('aisle')}>By aisle</button><button type="button" aria-pressed={grouping === 'meal'} className={grouping === 'meal' ? 'selected' : ''} onClick={() => setGrouping('meal')}>By meal</button></div>}</div></FridgePanel>
    <PreviewNotice />
    <FridgePanel className="content-panel" paw="green">{view === 'pantry' ? <div className="empty-state"><h2>Pantry shelf</h2><p>One sample staple is running low: olive oil.</p><p>Pantry editing comes with the next functionality phase.</p></div> : <div className="shopping-grid">{groups.map((group) => <section key={group.name}><h2>{group.name}</h2>{group.items.map((item) => <div className="shopping-row" key={`${group.name}-${item.name}`}><label className="shopping-check"><input type="checkbox" checked={checked.includes(item.name)} onChange={() => setChecked((current) => current.includes(item.name) ? current.filter((name) => name !== item.name) : [...current, item.name])} /><span><span className={checked.includes(item.name) ? 'struck' : ''}>{item.name}</span>{item.meals && <small>{item.meals}</small>}</span></label><strong>{item.amount}</strong><span className="shelf-future" title="Pantry editing comes in a later phase">+ shelf later</span></div>)}</section>)}</div>}</FridgePanel>
  </>
}
