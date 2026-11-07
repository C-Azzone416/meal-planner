import { useMemo, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { FridgePanel, PaperTitle, PreviewNotice } from '../components/FridgePanel'
import { Magnet } from '../components/Magnet'
import { formatDuration } from '../planner/format'
import { usePlanner } from '../planner/store'
import { DAY_SHORT_NAMES, type DayIndex } from '../planner/types'
import '../styles/recipes.css'
import { recipePath } from './recipePath'

const filters = ['All', 'Low attention', 'Dump-and-go', 'Under 15 min', 'Vegetarian']
const magnetColors = ['rust', 'green', 'gold', 'purple'] as const

export function RecipesPage() {
  const { recipes, plan } = usePlanner()
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState('All')
  const searchRef = useRef<HTMLInputElement>(null)

  const plannedDays = useMemo(() => {
    const byRecipe = new Map<string, DayIndex[]>()
    for (const night of plan.nights) {
      if (night.status === 'planned' && night.recipeId) {
        byRecipe.set(night.recipeId, [...(byRecipe.get(night.recipeId) ?? []), night.day])
      }
    }
    return byRecipe
  }, [plan])

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase()
    return recipes.filter((recipe) => recipe.title.toLowerCase().includes(needle) && (filter === 'All' || recipe.tags.includes(filter)))
  }, [recipes, query, filter])

  const onWeek = plannedDays.size
  const clearFilters = () => {
    setQuery('')
    setFilter('All')
    searchRef.current?.focus()
  }

  return <>
    <FridgePanel className="hero-panel recipe-hero">
      <div>
        <PaperTitle words={['our', 'recipes']} />
        <p className="script-copy">{recipes.length} saved · {onWeek} on this week</p>
        <Link className="button button--rust rx-button" to="/recipes/new">Add recipe</Link>
      </div>
      <label className="search-label">Search recipes<input ref={searchRef} type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="salmon, sheet pan, soup…" /></label>
      <div className="chip-row" role="group" aria-label="Recipe filters">{filters.map((item) => <button type="button" aria-pressed={filter === item} className={filter === item ? 'chip chip--active rx-chip' : 'chip rx-chip'} onClick={() => setFilter(item)} key={item}>{item}</button>)}</div>
    </FridgePanel>
    <PreviewNotice />
    <FridgePanel className="content-panel">
      <h2>All recipes · {visible.length}</h2>
      {recipes.length === 0 ? (
        <div className="empty-state">
          <h3>No recipes yet</h3>
          <p>Add your first recipe, or approve a draft your agent sent over.</p>
          <Link className="button button--rust rx-button" to="/recipes/new">Add recipe</Link>
        </div>
      ) : visible.length === 0 ? (
        <div className="empty-state" role="status">
          <h3>No recipes match{query.trim() ? ` “${query.trim()}”` : ''}{filter !== 'All' ? ` with ${filter}` : ''}</h3>
          <p>Try a different word or filter.</p>
          <button type="button" className="button button--paper rx-button" onClick={clearFilters}>Clear filters</button>
        </div>
      ) : (
        <ul className="recipe-grid rx-list">{visible.map((recipe, index) => {
          const days = plannedDays.get(recipe.id)
          return <li className="paper-card recipe-card rx-card" key={recipe.id}>
            <span className="card-magnet"><Magnet color={magnetColors[index % magnetColors.length]}>{recipe.icon}</Magnet></span>
            {days && <p className="rust-script">on {days.map((day) => DAY_SHORT_NAMES[day]).join(', ')}</p>}
            <h3><Link className="rx-card-link" to={recipePath(recipe.id)}>{recipe.title}</Link></h3>
            <p>{recipe.activeMinutes} min hands-on · {formatDuration(recipe.totalMinutes)} total</p>
            <div className="tag-list">{recipe.tags.map((tag) => <span key={tag}>{tag}</span>)}</div>
          </li>
        })}</ul>
      )}
    </FridgePanel>
  </>
}
