import { useMutation } from '@tanstack/react-query'
import { type ClipboardEvent, type DragEvent, type KeyboardEvent, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { errorMessage, GraphQLRequestError } from '../../api/client'
import { useMembers } from '../../api/hooks'
import { compareNames, i18n } from '../../i18n'
import { Avatar } from '../../ui/Avatar'
import { Picker } from '../../ui/Picker'
import { Popover } from '../../ui/Popover'
import { Markdown } from '../issue-page/Markdown'
import { tooLarge, useMaxUploadMb } from './limits'
import s from './markdown.module.css'
import { escapeText } from './placeholders'
import { type UploadResult, uploadFile } from './upload'

type Props = {
  value: string
  onChange: (value: string) => void
  /** Accessible name of the text field. */
  label: string
  /** Uploads attach to this issue; without it they stay pending. */
  issueId?: string
  /** Ids of the pending uploads made here, for attachmentIds on save. */
  onAttachmentsChange?: (ids: string[]) => void
  /** True while uploads run: saving then would keep a placeholder instead of the file. */
  onBusyChange?: (busy: boolean) => void
  placeholder?: string
  autoFocus?: boolean
  rows?: number
  id?: string
  onKeyDown?: (e: KeyboardEvent<HTMLTextAreaElement>) => void
}

function linkFor(r: UploadResult): string {
  const text = escapeText(r.name)
  return r.inline && r.contentType.startsWith('image/') ? `![${text}](${r.url})` : `[${text}](${r.url})`
}

/**
 * Markdown text field with Write / Preview tabs. Pasted or dropped files
 * upload at once and their Markdown replaces a placeholder at the cursor;
 * typing @ at the start of a word picks a member and inserts a mention.
 */
export function MarkdownEditor({ value, onChange, label, issueId, onAttachmentsChange, onBusyChange, placeholder, autoFocus, rows = 8, id, onKeyDown }: Props) {
  const { t } = useTranslation('issue')
  const [tab, setTab] = useState<'write' | 'preview'>('write')
  const [error, setError] = useState<string | null>(null)
  const [dragging, setDragging] = useState(false)
  const [mentionAt, setMentionAt] = useState<number | null>(null)
  const area = useRef<HTMLTextAreaElement | null>(null)
  // The picker's anchor: state, so the popover renders once the field exists.
  const [areaEl, setAreaEl] = useState<HTMLTextAreaElement | null>(null)
  const caret = useRef<number | null>(null)
  // Uploads finish after later edits: always rewrite the newest text.
  const latest = useRef(value)
  const uploaded = useRef<string[]>([])
  const users = useMembers().data ?? []
  const maxMb = useMaxUploadMb()
  const upload = useMutation({ mutationFn: (file: File) => uploadFile(file, issueId) })
  // Uploads outlive a save or a remount: their results must not reach a later editor.
  const alive = useRef(true)
  const inFlight = useRef(0)
  const onBusy = useRef(onBusyChange)
  useLayoutEffect(() => {
    onBusy.current = onBusyChange
  })
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
      // Closing mid-upload must not leave the parent's save disabled.
      if (inFlight.current > 0) {
        inFlight.current = 0
        onBusy.current?.(false)
      }
    }
  }, [])

  useLayoutEffect(() => {
    latest.current = value
    if (caret.current !== null && area.current) {
      area.current.setSelectionRange(caret.current, caret.current)
      caret.current = null
    }
  }, [value])

  // Once per textarea mount, so typing and inserts never move the caret.
  useLayoutEffect(() => {
    const el = area.current
    if (!autoFocus || !areaEl || !el) return
    const end = el.value.length
    el.setSelectionRange(end, end)
    el.scrollTop = el.scrollHeight
  }, [autoFocus, areaEl])

  const change = (next: string, cursor?: number) => {
    latest.current = next
    if (cursor !== undefined) caret.current = cursor
    onChange(next)
  }

  const addFiles = (files: File[]) => {
    if (files.length === 0) return
    setError(null)
    const ok: File[] = []
    for (const f of files) {
      if (tooLarge(f, maxMb)) {
        setError(i18n.t('errors:reasons.too_large', { maxMb }))
      } else {
        ok.push(f)
      }
    }
    if (ok.length === 0) return
    const placeholders = ok.map((f) => t('editor.uploading', { name: escapeText(f.name) }))
    const text = latest.current
    const at = area.current?.selectionStart ?? text.length
    const insert = placeholders.join('\n')
    change(text.slice(0, at) + insert + text.slice(at), at + insert.length)
    inFlight.current += ok.length
    onBusyChange?.(true)
    const settle = () => {
      inFlight.current -= 1
      if (inFlight.current === 0) onBusyChange?.(false)
    }
    ok.forEach((file, n) => {
      const ph = placeholders[n]
      upload.mutateAsync(file).then(
        (res) => {
          if (!alive.current) return
          settle()
          // A function replacer keeps `$` sequences in file names literal.
          change(latest.current.replace(ph, () => linkFor(res)))
          if (!issueId) {
            uploaded.current = [...uploaded.current, res.id]
            onAttachmentsChange?.(uploaded.current)
          }
        },
        (err: unknown) => {
          if (!alive.current) return
          settle()
          change(latest.current.replace(ph, ''))
          // A lost session sends the user to sign in (SessionWatcher).
          if (!(err instanceof GraphQLRequestError && err.code === 'AUTHENTICATION_ERROR')) setError(errorMessage(err))
        },
      )
    })
  }

  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = Array.from(e.clipboardData?.files ?? [])
    if (files.length === 0) return
    e.preventDefault()
    addFiles(files)
  }

  const hasFiles = (e: DragEvent) => Array.from(e.dataTransfer?.types ?? []).includes('Files')
  const onDrop = (e: DragEvent<HTMLTextAreaElement>) => {
    setDragging(false)
    if (!hasFiles(e)) return
    e.preventDefault()
    addFiles(Array.from(e.dataTransfer.files))
  }

  const onText = (next: string, cursor: number) => {
    const typedAt = next.length === latest.current.length + 1 && next[cursor - 1] === '@' ? cursor - 1 : -1
    change(next)
    if (typedAt >= 0 && (typedAt === 0 || /\s/.test(next[typedAt - 1]))) setMentionAt(typedAt)
  }

  const mention = (user: { id: string; name: string }) => {
    const at = mentionAt
    setMentionAt(null)
    if (at === null || latest.current[at] !== '@') return
    const text = latest.current
    const insert = `[@${escapeText(user.name)}](mention:${user.id}) `
    change(text.slice(0, at) + insert + text.slice(at + 1), at + insert.length)
    area.current?.focus()
  }

  const people = users.filter((u) => !u.disabled).sort((a, b) => compareNames(a.name, b.name))

  return (
    <div className={s.editor}>
      <div className={s.tabs} role="tablist">
        {(['write', 'preview'] as const).map((k) => (
          <button key={k} type="button" role="tab" aria-selected={tab === k} onClick={() => setTab(k)}>
            {t(`editor.${k}`)}
          </button>
        ))}
      </div>
      {tab === 'write' ? (
        <textarea
          ref={(el) => {
            area.current = el
            setAreaEl(el)
          }}
          id={id}
          className={[s.textarea, dragging ? s.dragging : ''].join(' ')}
          aria-label={label}
          autoFocus={autoFocus}
          rows={rows}
          placeholder={placeholder}
          value={value}
          onChange={(e) => onText(e.target.value, e.target.selectionStart)}
          onPaste={onPaste}
          onDragOver={(e) => {
            if (!hasFiles(e)) return
            e.preventDefault()
            setDragging(true)
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={onDrop}
          onKeyDown={onKeyDown}
        />
      ) : value.trim() ? (
        <div className={s.preview}>
          <Markdown source={value} />
        </div>
      ) : (
        <p className={[s.preview, s.muted].join(' ')}>{t('editor.nothingToPreview')}</p>
      )}
      {error && (
        <p role="alert" className={s.error}>
          {error}
        </p>
      )}
      <Popover anchor={areaEl} open={mentionAt !== null} onClose={() => setMentionAt(null)}>
        <Picker
          items={people}
          getKey={(u) => u.id}
          getLabel={(u) => u.name}
          renderItem={(u) => (
            <>
              <Avatar id={u.id} name={u.name} size={18} decorative />
              <span>{u.name}</span>
            </>
          )}
          onSelect={mention}
          onClose={() => setMentionAt(null)}
          placeholder={t('editor.mention')}
        />
      </Popover>
    </div>
  )
}
