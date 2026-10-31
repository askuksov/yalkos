import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useEffect, useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { Dialog } from './Dialog'
import { Menu } from './Menu'
import { Popover } from './Popover'
import { PriorityIcon } from './PriorityIcon'
import { StateIcon, type StateTypeName } from './StateIcon'
import { ToastProvider } from './Toast'
import { useToast } from './useToast'

afterEach(() => vi.useRealTimers())

function DialogHarness() {
  const [open, setOpen] = useState(false)
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Open it
      </button>
      <Dialog open={open} onClose={() => setOpen(false)} title="Hello">
        <input aria-label="inside" autoFocus />
      </Dialog>
    </>
  )
}

describe('ui', () => {
  it('Dialog closes on Escape and restores focus', async () => {
    const user = userEvent.setup()
    render(<DialogHarness />)
    const opener = screen.getByRole('button', { name: 'Open it' })
    await user.click(opener)
    const input = await screen.findByLabelText('inside')
    expect(input).toHaveFocus()
    await user.keyboard('{Escape}')
    expect(screen.queryByLabelText('inside')).not.toBeInTheDocument()
    expect(opener).toHaveFocus()
  })

  it('Popover and Menu hand focus back to their button', async () => {
    const user = userEvent.setup()
    function Harness() {
      const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
      const [open, setOpen] = useState(false)
      const [menu, setMenu] = useState(false)
      const [menuAnchor, setMenuAnchor] = useState<HTMLButtonElement | null>(null)
      return (
        <>
          <button ref={setAnchor} type="button" onClick={() => setOpen(true)}>
            Pick
          </button>
          <Popover anchor={anchor} open={open} onClose={() => setOpen(false)}>
            <input
              aria-label="filter"
              autoFocus
              onKeyDown={(e) => {
                // Like Picker: Enter selects and keeps its default from reaching the button.
                if (e.key === 'Enter') {
                  e.preventDefault()
                  setOpen(false)
                }
              }}
            />
          </Popover>
          <button ref={setMenuAnchor} type="button" onClick={() => setMenu(true)}>
            More
          </button>
          <Menu anchor={menuAnchor} open={menu} onClose={() => setMenu(false)} label="Actions" items={[{ label: 'Do it', onSelect: () => {} }]} />
        </>
      )
    }
    render(<Harness />)
    await user.click(screen.getByRole('button', { name: 'Pick' }))
    expect(screen.getByLabelText('filter')).toHaveFocus()
    await user.keyboard('{Escape}')
    expect(screen.getByRole('button', { name: 'Pick' })).toHaveFocus()
    await user.click(screen.getByRole('button', { name: 'Pick' }))
    await user.keyboard('{Enter}')
    expect(screen.getByRole('button', { name: 'Pick' })).toHaveFocus()
    await user.click(screen.getByRole('button', { name: 'More' }))
    await user.keyboard('{Enter}')
    expect(screen.getByRole('button', { name: 'More' })).toHaveFocus()
  })

  it('error toasts stay, info toasts disappear after 4 s', () => {
    vi.useFakeTimers()
    const grab: { api?: ReturnType<typeof useToast> } = {}
    function Grab() {
      const api = useToast()
      useEffect(() => {
        grab.api = api
      })
      return null
    }
    render(
      <ToastProvider>
        <Grab />
      </ToastProvider>,
    )
    act(() => {
      grab.api?.show('Saved')
      grab.api?.show('Failed', { kind: 'error' })
    })
    expect(screen.getByText('Saved')).toBeInTheDocument()
    act(() => vi.advanceTimersByTime(4100))
    expect(screen.queryByText('Saved')).not.toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('Failed')
  })

  it('shows at most three toasts', () => {
    const grab: { api?: ReturnType<typeof useToast> } = {}
    function Grab() {
      const api = useToast()
      useEffect(() => {
        grab.api = api
      })
      return null
    }
    render(
      <ToastProvider>
        <Grab />
      </ToastProvider>,
    )
    act(() => ['a1', 'a2', 'a3', 'a4'].forEach((m) => grab.api?.show(m)))
    expect(screen.queryByText('a1')).not.toBeInTheDocument()
    expect(screen.getByText('a4')).toBeInTheDocument()
  })

  it('StateIcon renders a distinct shape per type', () => {
    const types: StateTypeName[] = ['BACKLOG', 'UNSTARTED', 'STARTED', 'COMPLETED', 'CANCELED']
    const { container } = render(
      <>
        {types.map((t) => (
          <StateIcon key={t} type={t} color="#888" />
        ))}
      </>,
    )
    const shapes = [...container.querySelectorAll('svg')].map((s) => s.getAttribute('data-shape'))
    expect(new Set(shapes).size).toBe(5)
  })

  it('PriorityIcon is labelled from the catalog', () => {
    render(<PriorityIcon priority={2} />)
    expect(screen.getByRole('img', { name: 'High' })).toBeInTheDocument()
  })
})
