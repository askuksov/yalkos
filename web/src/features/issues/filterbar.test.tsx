import { act, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { states } from '../../test/fixtures'
import type { PickerState } from '../pickers/StatePicker'
import { FilterBar, FilterChips } from './FilterBar'
import { parseParams } from './filters'

const users = [
  { id: 'u1', name: 'Olga', disabled: false },
  { id: 'u2', name: 'Andrii', disabled: false },
]
const scope = { states: states as PickerState[], users, viewerId: 'u1' }

afterEach(() => vi.useRealTimers())

describe('FilterBar', () => {
  it('shows chips and removes one', async () => {
    const onChange = vi.fn()
    const params = parseParams(new URLSearchParams('state=s-todo,s-done&assignee=me,u2&children=1'))
    render(<FilterChips params={params} onChange={onChange} {...scope} />)
    expect(screen.getByText('State: Todo or Done')).toBeInTheDocument()
    expect(screen.getByText('Assignee: Me or Andrii')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Remove filter: State' }))
    expect(onChange).toHaveBeenCalledWith({ ...params, state: [] })
  })

  it('focuses the text filter with F and sets q after a pause', () => {
    vi.useFakeTimers()
    const onChange = vi.fn()
    const params = parseParams(new URLSearchParams(''))
    render(<FilterBar params={params} onChange={onChange} {...scope} />)
    fireEvent.keyDown(document.body, { key: 'f', code: 'KeyF' })
    const input = screen.getByRole('searchbox', { name: 'Filter by text…' })
    expect(input).toHaveFocus()
    fireEvent.change(input, { target: { value: ' login ' } })
    expect(onChange).not.toHaveBeenCalled()
    act(() => vi.advanceTimersByTime(260))
    expect(onChange).toHaveBeenCalledWith({ ...params, q: 'login' })
  })

  it('adds a priority filter from the menu', async () => {
    const onChange = vi.fn()
    render(<FilterBar params={parseParams(new URLSearchParams(''))} onChange={onChange} {...scope} />)
    await userEvent.click(screen.getByRole('button', { name: 'Filter' }))
    await userEvent.click(screen.getByRole('option', { name: 'Priority' }))
    await userEvent.click(screen.getByRole('option', { name: 'Urgent' }))
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ priority: [1] }))
  })
})
