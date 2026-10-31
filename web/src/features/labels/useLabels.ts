import { useQuery } from '@tanstack/react-query'

import { request } from '../../api/client'
import type { LabelFieldsFragment } from '../../gql/graphql'
import { LabelsQuery } from './operations'

export type LabelItem = LabelFieldsFragment
export type LabelGroup = LabelItem & { children: LabelItem[] }

/** Label names and colours as issue rows carry them. */
export type IssueLabel = { id: string; name: string; color: string }

export type LabelCatalog = {
  /** Top-level groups (with children) and free labels, by position. */
  top: LabelGroup[]
  byId: Map<string, LabelItem>
  typeGroupId: string
  defaultTypeId: string | null
  /** Labels of the type group, by position. */
  types: LabelItem[]
  isType: (id: string) => boolean
}

function catalog(d: { labels: LabelGroup[]; labelSettings: { typeGroup: { id: string }; defaultType?: { id: string } | null } }): LabelCatalog {
  const byId = new Map<string, LabelItem>()
  for (const l of d.labels) {
    byId.set(l.id, l)
    for (const c of l.children) byId.set(c.id, c)
  }
  const typeGroupId = d.labelSettings.typeGroup.id
  const types = d.labels.find((l) => l.id === typeGroupId)?.children ?? []
  return {
    top: d.labels,
    byId,
    typeGroupId,
    defaultTypeId: d.labelSettings.defaultType?.id ?? null,
    types,
    isType: (id) => byId.get(id)?.parent?.id === typeGroupId,
  }
}

/** Workspace labels, groups and the type settings (cached for the session). */
export function useLabels() {
  return useQuery({ queryKey: ['labels'], queryFn: () => request(LabelsQuery), select: catalog, staleTime: 60_000 })
}

const presets = ['#95a2b3', '#f2c94c', '#f2994a', '#eb5757', '#bb87fc', '#5e6ad2', '#3a7bd5', '#26b5ce', '#4cb782', '#0f783c']

/** A stable colour for a label created on the fly, picked by its name. */
export function colorFor(name: string): string {
  let h = 0
  for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return presets[h % presets.length]
}
