import { useMemo, useState } from 'react'
import { FridgePanel, PaperTitle, PreviewNotice } from '../components/FridgePanel'
import { Magnet } from '../components/Magnet'
import { recipes } from '../data/sampleData'

const filters = ['All', 'Low attention', 'Dump-and-go', 'Under 15 min', 'Vegetarian']

export function RecipesPage() {
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState('All')
  const visible = useMemo(() => recipes.filter((recipe) => recipe.title.toLowerCase().includes(query.toLowerCase()) && (filter === 'All' || recipe.tags.includes(filter))), [query, filter])
  return <>
    <FridgePanel className="hero-panel recipe-hero">
      <div><PaperTitle words={['our', 'recipes']} /><p className="script-copy">8 saved · 3 on this week</p></div>
      <label className="search-label">Search recipes<input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="salmon, sheet pan, soup…" /></label>
      <div className="chip-row" aria-label="Recipe filters">{filters.map((item) => <button type="button" aria-pressed={filter === item} className={filter === item ? 'chip chip--active' : 'chip'} onClick={() => setFilter(item)} key={item}>{item}</button>)}</div>
    </FridgePanel>
    <PreviewNotice />
    <FridgePanel className="content-panel"><h2>All recipes · {visible.length}</h2><div className="recipe-grid">{visible.map((recipe, index) => <article className="paper-card recipe-card" key={recipe.title}>
      <span className="card-magnet"><Magnet color={['rust', 'green', 'gold', 'purple'][index % 4] as 'rust' | 'green' | 'gold' | 'purple'}>{recipe.icon}</Magnet></span>
      {recipe.day && <p className="rust-script">on {recipe.day}</p>}<h3>{recipe.title}</h3><p>{recipe.activeMinutes} min hands-on · {recipe.total}</p><div className="tag-list">{recipe.tags.map((tag) => <span key={tag}>{tag}</span>)}</div>
    </article>)}</div>{visible.length === 0 && <p className="empty-state">No sample recipes match those filters.</p>}</FridgePanel>
  </>
}
