import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { errorMessage, GraphQLRequestError } from '../../api/client'
import { useToast } from '../../ui/useToast'
import { tooLarge, useMaxUploadMb } from '../markdown/limits'
import { uploadFile } from '../markdown/upload'

/** Uploads files to the issue (Attach file, page drop); refreshes the list and the timeline. */
export function useAttach(issueId: string) {
  const { t } = useTranslation('issue')
  const { t: te } = useTranslation('errors')
  const qc = useQueryClient()
  const toast = useToast()
  const maxMb = useMaxUploadMb()
  const upload = useMutation({
    mutationFn: (file: File) => uploadFile(file, issueId),
    onSuccess: (a) => toast.show(t('attachments.uploaded', { name: a.name })),
    onError: (err) => {
      // A lost session sends the user to sign in (SessionWatcher).
      if (!(err instanceof GraphQLRequestError && err.code === 'AUTHENTICATION_ERROR')) toast.show(errorMessage(err), { kind: 'error' })
    },
    onSettled: () => Promise.all([qc.invalidateQueries({ queryKey: ['attachments', issueId] }), qc.invalidateQueries({ queryKey: ['history', issueId] })]),
  })
  return (files: File[]) => {
    const ok = files.filter((f) => !tooLarge(f, maxMb))
    // One message per batch, however many files are over the limit.
    if (ok.length < files.length) toast.show(te('reasons.too_large', { maxMb }), { kind: 'error' })
    ok.forEach((f) => upload.mutate(f))
  }
}
