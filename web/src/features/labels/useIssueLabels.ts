import { useMutation, useQueryClient } from '@tanstack/react-query'

import { errorMessage, request } from '../../api/client'
import { useIssueMutation } from '../../lib/issueCache'
import { useToast } from '../../ui/useToast'
import { CreateLabelMutation } from './operations'
import { colorFor, type IssueLabel, type LabelCatalog, type LabelItem } from './useLabels'

type Target = { id: string; type: IssueLabel | null; labels: IssueLabel[] }

const short = (l: LabelItem): IssueLabel => ({ id: l.id, name: l.name, color: l.color })

/** Labels after adding one: a label of an exclusive group replaces its sibling. */
export function withLabel(cat: LabelCatalog, labels: IssueLabel[], add: LabelItem): IssueLabel[] {
  const group = add.parent ? cat.byId.get(add.parent.id) : undefined
  const keep = labels.filter((l) => l.id !== add.id && !(group?.exclusive && cat.byId.get(l.id)?.parent?.id === group.id))
  return [...keep, short(add)]
}

/** Type and label changes of one issue: optimistic, rolled back on error. */
export function useIssueLabels(cat: LabelCatalog | undefined, issue: Target) {
  const qc = useQueryClient()
  const toast = useToast()
  const update = useIssueMutation()
  const create = useMutation({
    mutationFn: (name: string) => request(CreateLabelMutation, { input: { name, color: colorFor(name) } }),
    onError: (err) => toast.show(errorMessage(err), { kind: 'error' }),
    onSettled: () => qc.invalidateQueries({ queryKey: ['labels'] }),
  })

  const toggle = (label: LabelItem, on: boolean) => {
    if (!cat) return
    const labels = on ? withLabel(cat, issue.labels, label) : issue.labels.filter((l) => l.id !== label.id)
    const type = labels.find((l) => cat.isType(l.id)) ?? null
    update.mutate({ id: issue.id, input: on ? { addLabelIds: [label.id] } : { removeLabelIds: [label.id] }, optimistic: { labels, type } })
  }

  const setType = (typeId: string | null) => {
    if (!cat || typeId === (issue.type?.id ?? null)) return
    if (typeId === null) {
      if (issue.type) toggle(cat.byId.get(issue.type.id) ?? { ...issue.type, description: '', isGroup: false, exclusive: false, position: 0, parent: { id: cat.typeGroupId } }, false)
      return
    }
    const label = cat.byId.get(typeId)
    if (label) toggle(label, true)
  }

  const createAndAdd = (name: string) =>
    create.mutate(name, {
      onSuccess: (d) => {
        if (!cat) return
        const l = d.createLabel
        const labels = [...issue.labels, short(l)]
        update.mutate({ id: issue.id, input: { addLabelIds: [l.id] }, optimistic: { labels } })
      },
    })

  return { toggle, setType, createAndAdd }
}
