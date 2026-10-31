import { PageHeader } from '../app/PageHeader'
import { useCurrentProject } from '../app/projectContext'

export function ProjectIssuesPage() {
  return <PageHeader title={useCurrentProject().name} />
}
