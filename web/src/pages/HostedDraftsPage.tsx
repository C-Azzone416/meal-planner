import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import { useAuth } from '../auth/AuthProvider'
import { draftReviewClient, type CanonicalDraftContent, type HostedDraftResponse, type IncomingIntent } from '../drafts/api'
import { ProjectionEditor } from '../drafts/ProjectionEditor'
import { FridgePanel, PaperTitle, PreviewNotice } from '../components/FridgePanel'
import '../styles/hosted-drafts.css'

function show(value: unknown): string {
  return value === undefined || value === null || value === '' ? 'Unknown' : String(value)
}
function CanonicalDraft({ content }: { content: CanonicalDraftContent }) {
  return <div className="paper-card">
    <h3>{content.title}</h3>
    <p>Yield: {show(content.yield)}</p>
    <h4>Ingredients</h4><ol>{content.ingredients.map((item, index) => <li key={index}>{item.text}<small> · quantity {show(item.quantity)} · unit {show(item.unit)} · name {show(item.name)} · note {show(item.note)}</small></li>)}</ol>
    <h4>Instructions</h4><ol>{content.instructions.map((step, index) => <li key={index}>{step.text}<small> · phase {show(step.phase)} · basis {step.phase_basis ? `${step.phase_basis.kind}: ${show(step.phase_basis.source_ref)} ${show(step.phase_basis.note)}` : 'Unknown'}</small></li>)}</ol>
    <h4>Estimates</h4><pre>{JSON.stringify(content.estimates ?? {}, null, 2)}</pre>
    <h4>Phase estimates</h4><pre>{JSON.stringify(content.phase_estimates ?? {}, null, 2)}</pre>
    <h4>Guidance</h4><ul>{content.guidance.map((g, index) => <li key={index}>{g.topic}: {g.text} · {g.basis.kind} · {show(g.basis.source_ref)} · {show(g.basis.note)}</li>)}</ul>
    <h4>Sources</h4><ul>{content.sources.map((source) => <li key={source.id}>{source.id} · {source.kind} · {show(source.label)} · {show(source.url)} · {show(source.locator)}</li>)}</ul>
    {content.notes && <><h4>Notes</h4><p>{content.notes}</p></>}
  </div>
}

export function HostedDraftsPage() {
  const auth = useAuth()
  const { intentId } = useParams<{ intentId?: string }>()
  const accountId = auth.user?.id ?? ''
  const active = useRef(true)
  const [intents, setIntents] = useState<IncomingIntent[]>([])
  const [summaries, setSummaries] = useState<Array<{ id: string; title: string; revision: number; review_state: string; review_revision: number }>>([])
  const [selected, setSelected] = useState<HostedDraftResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [confirmation, setConfirmation] = useState<string | null>(null)
  const orderedIntents = useMemo(() => intentId
    ? [...intents].sort((a, b) => Number(b.id === intentId) - Number(a.id === intentId))
    : intents, [intentId, intents])
  // The client invokes this guard only while handling asynchronous requests.
  // eslint-disable-next-line react-hooks/refs
  const client = useMemo(() => draftReviewClient(() => auth.getAccessToken(accountId), auth.expireSession, fetch, () => active.current), [accountId, auth])
  useEffect(() => { active.current = true; return () => { active.current = false } }, [accountId])
  const reload = useCallback(async () => {
    setError(null)
    try { const [nextIntents, nextDrafts] = await Promise.all([client.intents(), client.drafts()]); if (active.current) { setIntents(nextIntents); setSummaries(nextDrafts) } }
    catch { if (active.current) setError('Incoming drafts could not be loaded. No draft was changed.') }
  }, [client])
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => { void reload() }, [reload])
  const decide = async (intent: IncomingIntent, decision: 'approve' | 'deny') => { try { await client.decide(intent, decision); await reload() } catch (cause) { setError(cause instanceof Error ? cause.message : 'No change was confirmed.') } }
  const review = async () => { if (!selected) return; try { setSelected(await client.review(selected)); await reload() } catch (cause) { setError(cause instanceof Error ? cause.message : 'No review was recorded.') } }
  return <>
    <FridgePanel className="hero-panel"><PaperTitle words={['incoming', 'drafts']} /><p className="script-copy">approve the exact proposal, review every unknown, then save deliberately</p></FridgePanel>
    <PreviewNotice />
    <FridgePanel className="content-panel">
      {error && <p role="alert">{error}</p>}
      {confirmation && <p role="status">{confirmation}</p>}
      <button type="button" className="button button--paper" onClick={() => void reload()}>Reload</button>
      <h2>Waiting for your consent</h2>
      {intentId && intents.length > 0 && !intents.some((intent) => intent.id === intentId) && <p role="status">That proposal is no longer waiting for consent.</p>}
      {intents.length === 0 ? <p>No proposals are waiting.</p> : orderedIntents.map((intent) => <article className="paper-card" key={intent.id}>
        <p>Proposal expires {new Date(intent.expires_at).toLocaleString()} · revision {intent.revision}</p><CanonicalDraft content={intent.submission.content} />
        <button type="button" className="button button--green" onClick={() => void decide(intent, 'approve')}>Accept as unreviewed draft</button>
        <button type="button" className="button button--paper" onClick={() => void decide(intent, 'deny')}>Deny</button>
      </article>)}
      <h2>Unreviewed drafts</h2>
      {summaries.map((item) => <button type="button" className="button button--paper" key={item.id} onClick={async () => { try { setConfirmation(null); setSelected(await client.draft(item.id)) } catch { setError('That draft could not be opened.') } }}>{item.title} · {item.review_state} · rev {item.revision}</button>)}
      {selected && <article><CanonicalDraft content={selected.draft.content} />
        <p>{selected.validation.notice}</p>{selected.validation.warnings.map((warning) => <p key={`${warning.path}:${warning.code}`}>⚠ {warning.path}: {warning.message}</p>)}
        {selected.draft.review.state === 'not_started' && <button type="button" className="button button--green" onClick={() => void review()}>I reviewed this exact revision</button>}
        {selected.draft.review.state === 'ready' && <ProjectionEditor key={`${selected.draft.id}:${selected.draft.revision}:${selected.draft.review.revision}`} item={selected} onSave={(item, projection) => client.save(item, projection)} onSaved={() => {
          if (!active.current) return
          setConfirmation('Recipe saved. The reviewed draft is now approved and cannot be saved a second time.')
          setSelected(null)
          void reload()
        }} />}
      </article>}
    </FridgePanel>
  </>
}
