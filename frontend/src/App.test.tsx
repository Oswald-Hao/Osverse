/// <reference types="vite/client" />

import { render, screen } from '@testing-library/react'
import { expect, test } from 'vitest'

import indexDocument from '../index.html?raw'
import App from './App'

test('renders the real App in a Simplified Chinese document', () => {
  const appDocument = new DOMParser().parseFromString(indexDocument, 'text/html')
  document.documentElement.lang = appDocument.documentElement.lang

  render(<App />)

  expect(document.documentElement.lang).toBe('zh-CN')
  expect(screen.getByRole('heading', { name: 'Osverse' })).toBeVisible()
})
