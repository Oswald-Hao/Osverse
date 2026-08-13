import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import App from './App'

test('renders the Osverse heading', () => {
  render(<App />)

  expect(screen.getByRole('heading', { name: 'Osverse' })).toBeVisible()
})
