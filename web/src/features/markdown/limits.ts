import { useQuery } from '@tanstack/react-query'

import { request } from '../../api/client'
import { LimitsQuery } from './operations'

/** Upload size limit in MB; undefined until loaded (the server enforces it anyway). */
export function useMaxUploadMb(): number | undefined {
  return useQuery({ queryKey: ['limits'], queryFn: () => request(LimitsQuery), select: (d) => d.limits.maxUploadMb, staleTime: Infinity }).data
}

export function tooLarge(file: File, maxMb: number | undefined): boolean {
  return maxMb !== undefined && file.size > maxMb * 1024 * 1024
}
