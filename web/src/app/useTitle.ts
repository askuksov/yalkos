import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

/** Sets the document title to "<title> · Yalkos". */
export function useTitle(title: string | undefined) {
  const { t } = useTranslation()
  useEffect(() => {
    document.title = title ? t('app.documentTitle', { title }) : t('app.name')
  }, [title, t])
}
