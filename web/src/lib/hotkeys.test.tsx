import { fireEvent, render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { useEscape, useHotkey } from './hotkeys'

function Hot({ k, fn, allowInInputs }: { k: string; fn: (e: KeyboardEvent) => void; allowInInputs?: boolean }) {
  useHotkey(k, fn, { allowInInputs })
  return (
    <div>
      <input aria-label="field" />
      <textarea aria-label="area" />
      <div aria-label="rich" contentEditable suppressContentEditableWarning />
    </div>
  )
}

function Esc({ fn, enabled = true }: { fn: () => void; enabled?: boolean }) {
  useEscape(fn, enabled)
  return null
}

describe('hotkeys', () => {
  it('fires on the key outside inputs', () => {
    const fn = vi.fn()
    render(<Hot k="c" fn={fn} />)
    fireEvent.keyDown(document.body, { key: 'c', code: 'KeyC' })
    expect(fn).toHaveBeenCalledTimes(1)
    fireEvent.keyDown(document.body, { key: 'C', code: 'KeyC', shiftKey: true })
    fireEvent.keyDown(document.body, { key: 'c', code: 'KeyC', metaKey: true })
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('ignores hotkeys in editable targets', () => {
    const fn = vi.fn()
    const { getByLabelText } = render(<Hot k="c" fn={fn} />)
    for (const label of ['field', 'area', 'rich']) {
      fireEvent.keyDown(getByLabelText(label), { key: 'c', code: 'KeyC' })
    }
    expect(fn).not.toHaveBeenCalled()
  })

  it('does not fire while a dialog is open', () => {
    const fn = vi.fn()
    render(
      <>
        <Hot k="c" fn={fn} />
        <dialog open />
      </>,
    )
    fireEvent.keyDown(document.body, { key: 'c', code: 'KeyC' })
    expect(fn).not.toHaveBeenCalled()
  })

  it('matches physical keys on a Russian layout', () => {
    const fn = vi.fn()
    render(<Hot k="c" fn={fn} />)
    fireEvent.keyDown(document.body, { key: 'с', code: 'KeyC' })
    expect(fn).toHaveBeenCalledTimes(1)
    fireEvent.keyDown(document.body, { key: 'c', code: 'KeyX' })
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('mod+k works inside inputs when allowed', () => {
    const fn = vi.fn()
    const { getByLabelText } = render(<Hot k="mod+k" fn={fn} allowInInputs />)
    // jsdom's platform is not macOS: mod is Ctrl.
    fireEvent.keyDown(getByLabelText('field'), { key: 'k', code: 'KeyK', ctrlKey: true })
    expect(fn).toHaveBeenCalledTimes(1)
    fireEvent.keyDown(getByLabelText('field'), { key: 'k', code: 'KeyK' })
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('matches shift+/ for ?', () => {
    const fn = vi.fn()
    render(<Hot k="?" fn={fn} />)
    fireEvent.keyDown(document.body, { key: ',', code: 'Slash', shiftKey: true })
    expect(fn).toHaveBeenCalledTimes(1)
    fireEvent.keyDown(document.body, { key: '/', code: 'Slash' })
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('Escape goes to the topmost handler only', () => {
    const first = vi.fn()
    const second = vi.fn()
    const { rerender } = render(
      <>
        <Esc fn={first} />
        <Esc fn={second} />
      </>,
    )
    fireEvent.keyDown(document.body, { key: 'Escape', code: 'Escape' })
    expect(second).toHaveBeenCalledTimes(1)
    expect(first).not.toHaveBeenCalled()
    rerender(
      <>
        <Esc fn={first} />
        <Esc fn={second} enabled={false} />
      </>,
    )
    fireEvent.keyDown(document.body, { key: 'Escape', code: 'Escape' })
    expect(first).toHaveBeenCalledTimes(1)
  })
})
