declare module '@masra91/framebriar/protocol' {
  export interface NormalizedReport {
    v: 1
    id: string
    gameId: string
    buildSha: string | null
    scene: string
    notes: string
    media: { type: string; bytes: number; sha256: string } | null
    [key: string]: unknown
  }
}
declare module '@masra91/framebriar/browser/local-export' {
  import type { NormalizedReport } from '@masra91/framebriar/protocol'
  export interface PreparedFeedback {
    report: NormalizedReport
    media: Blob | null
  }
  export function prepareLocalFeedback(options: {
    blob: Blob | null
    captureContext: Record<string, unknown>
    notes: string
  }): Promise<PreparedFeedback>
}
declare module '@masra91/framebriar/browser/queue' {
  export class OfflineQueue {
    constructor(scope: string)
    all(): Promise<{ blob: Blob | null }[]>
    clear(): Promise<unknown>
  }
}
declare module '@masra91/framebriar/browser/durable-feedback' {
  import type { NormalizedReport } from '@masra91/framebriar/protocol'
  import type { OfflineQueue } from '@masra91/framebriar/browser/queue'
  export interface OutboxStatus {
    state: string
    items: number
    bytes: number
    blocked?: number
  }
  export interface Receipt {
    id: string
    gameId: string
    status: string
  }
  export interface Delivery {
    report: NormalizedReport
    media: Blob | null
    signal?: AbortSignal
  }
  export interface DurableFeedback {
    status(): OutboxStatus
    enqueue(
      options: Delivery & { includeDiagnostics: boolean },
    ): Promise<unknown>
    start(): Promise<void>
    stop(): void
    resume(): Promise<void>
    clear(): Promise<OutboxStatus>
  }
  export function createDurableFeedback(options: {
    gameId: string
    buildSha: string
    endpoint: string
    getToken(): Promise<string | undefined>
    queue: OfflineQueue
    send(options: Delivery): Promise<Receipt>
    onChange(status: OutboxStatus): void
    onReceipt(receipt: Receipt): void
  }): DurableFeedback
}
