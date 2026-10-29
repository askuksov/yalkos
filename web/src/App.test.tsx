import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'

import App from './App'

it('renders product name and tagline', () => {
  render(<App />)
  expect(screen.getByRole('heading', { name: 'Yalkos' })).toBeInTheDocument()
  expect(screen.getByText('Self-hosted issue tracker')).toBeInTheDocument()
})
