import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { AssigneePicker } from '../features/pickers/AssigneePicker'
import { PriorityPicker } from '../features/pickers/PriorityPicker'
import { Picker } from './Picker'

type Fruit = { id: string; name: string }
const fruits: Fruit[] = [
  { id: 'a', name: 'Apple' },
  { id: 'b', name: 'Banana' },
  { id: 'c', name: 'Cherry' },
]

describe('Picker', () => {
  it('Picker filters, moves with arrows and selects with Enter', async () => {
    const onSelect = vi.fn()
    const onClose = vi.fn()
    render(
      <Picker items={fruits} value={fruits[2]} getKey={(f) => f.id} getLabel={(f) => f.name} onSelect={onSelect} onClose={onClose} placeholder="Pick" />,
    )
    const input = screen.getByPlaceholderText('Pick')
    expect(input).toHaveFocus()
    expect(screen.getByRole('option', { name: 'Cherry' })).toHaveAttribute('aria-selected', 'true')
    await userEvent.type(input, 'AN')
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['Banana'])
    await userEvent.clear(input)
    await userEvent.keyboard('{ArrowDown}{ArrowDown}{ArrowUp}{ArrowDown}{Enter}')
    expect(onSelect).toHaveBeenCalledWith(fruits[2])
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalled()
  })

  it('AssigneePicker puts Me first and No assignee last', async () => {
    const onSelect = vi.fn()
    const users = [
      { id: 'u2', name: 'Andrii', disabled: false },
      { id: 'u1', name: 'Olga', disabled: false },
      { id: 'u3', name: 'Gone', disabled: true },
    ]
    render(<AssigneePicker users={users} viewerId="u1" value={null} onSelect={onSelect} onClose={() => {}} />)
    const labels = screen.getAllByRole('option').map((o) => o.lastElementChild?.textContent)
    expect(labels).toEqual(['Me', 'Andrii', 'No assignee'])
    await userEvent.click(screen.getByRole('option', { name: 'No assignee' }))
    expect(onSelect).toHaveBeenCalledWith(null)
    await userEvent.click(screen.getByRole('option', { name: 'Me' }))
    expect(onSelect).toHaveBeenLastCalledWith('u1')
  })

  it('PriorityPicker lists priorities in order', async () => {
    const onSelect = vi.fn()
    render(<PriorityPicker value={2} onSelect={onSelect} onClose={() => {}} />)
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['No priority', 'Urgent', 'High', 'Medium', 'Low'])
    expect(screen.getByRole('option', { name: 'High' })).toHaveAttribute('aria-selected', 'true')
    await userEvent.click(screen.getByRole('option', { name: 'Low' }))
    expect(onSelect).toHaveBeenCalledWith(4)
  })
})
