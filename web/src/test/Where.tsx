import { useLocation } from 'react-router'

/** Shows the current path so tests can assert redirects. */
export function Where() {
  return <div data-testid="where">{useLocation().pathname}</div>
}
