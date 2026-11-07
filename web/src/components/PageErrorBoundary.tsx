import { Component, type ErrorInfo, type ReactNode } from 'react'
import { StateCard } from './StateCard'

interface Props {
  children: ReactNode
  /** When this value changes (e.g. the route path) a shown error is cleared. */
  resetKey?: unknown
}

interface BoundaryState {
  failed: boolean
  attempt: number
}

/**
 * Catches render errors in one page so the shell and navigation survive (§4).
 * "Try again" remounts the page subtree from scratch.
 */
export class PageErrorBoundary extends Component<Props, BoundaryState> {
  state: BoundaryState = { failed: false, attempt: 0 }

  static getDerivedStateFromError(): Partial<BoundaryState> {
    return { failed: true }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Local only: no telemetry or network calls.
    console.error('Page failed to render', error, info.componentStack)
  }

  componentDidUpdate(previous: Props) {
    if (this.state.failed && previous.resetKey !== this.props.resetKey) {
      this.setState((current) => ({ failed: false, attempt: current.attempt + 1 }))
    }
  }

  private tryAgain = () => {
    this.setState((current) => ({ failed: false, attempt: current.attempt + 1 }))
  }

  render() {
    if (this.state.failed) {
      return (
        <StateCard icon="🐈‍⬛" tone="error" heading="Something went wrong on this page" action={{ label: 'Try again', onClick: this.tryAgain }}>
          The rest of the app is fine. Your preview data is still saved in this browser.
        </StateCard>
      )
    }
    return <div key={this.state.attempt} className="page-boundary">{this.props.children}</div>
  }
}
