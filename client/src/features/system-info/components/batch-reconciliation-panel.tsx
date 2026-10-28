/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { RefreshCw, Scale } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { formatQuota } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { listBatchReconciliation, releaseUnsubmittedBatch } from '../api'
import type { BatchReconciliationJob } from '../types'
import { BatchReconciliationTable } from './batch-reconciliation-table'
import { BatchRecoverDialog } from './batch-recover-dialog'
import { BatchReleaseDialog } from './batch-release-dialog'

const RECONCILIATION_LIMIT = 50
const QUERY_KEY = ['system-info', 'batch-reconciliation']

export function BatchReconciliationPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [target, setTarget] = useState<BatchReconciliationJob | null>(null)
  const [recoverTarget, setRecoverTarget] =
    useState<BatchReconciliationJob | null>(null)
  const jobsQuery = useQuery({
    queryKey: QUERY_KEY,
    queryFn: async () => {
      const res = await listBatchReconciliation(RECONCILIATION_LIMIT)
      if (!res.success || !Array.isArray(res.data)) {
        throw createServerError(
          res,
          t('We could not load Batch reconciliation.')
        )
      }
      return { jobs: res.data, hasMore: res.has_more === true }
    },
    staleTime: 30 * 1000,
    retry: false,
  })
  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: QUERY_KEY })
  const releaseMutation = useMutation({
    mutationFn: async (input: { id: string; evidence: string }) => {
      const res = await releaseUnsubmittedBatch(input.id, input.evidence)
      if (res.data.success === false) {
        throw createServerError(res.data, t('Release failed'))
      }
      return res
    },
    onSuccess: async (res) => {
      if (res.status === 202) {
        toast.info(t('Release pending cache confirmation'))
      } else if (res.data.already_released) {
        toast.success(t('Hold was already released'))
      } else {
        toast.success(
          t('Released hold of {{quota}}', {
            quota: formatQuota(res.data.released_quota ?? 0),
          })
        )
      }
      setTarget(null)
      await invalidate()
    },
    onError: (error) => {
      handleServerError(error, t('Release failed'))
      void invalidate()
    },
  })

  const jobs = jobsQuery.data?.jobs ?? []
  const loading = jobsQuery.isLoading
  const refreshing = jobsQuery.isFetching && !loading

  return (
    <section className='bg-card overflow-hidden rounded-lg border shadow-xs'>
      <div className='flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5'>
        <div className='flex min-w-0 items-center gap-2'>
          <span className='bg-muted text-muted-foreground inline-flex size-7 items-center justify-center rounded-md'>
            <Scale className='size-4' aria-hidden='true' />
          </span>
          <div className='min-w-0'>
            <h3 className='text-sm font-semibold'>
              {t('Batch Reconciliation')}
            </h3>
            <p className='text-muted-foreground mt-0.5 text-xs'>
              {t(
                'Batches whose provider identity or settlement needs an operator. Holds stay reserved until resolved.'
              )}
            </p>
          </div>
        </div>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => void jobsQuery.refetch()}
          disabled={jobsQuery.isFetching}
          aria-label={t('Refresh')}
        >
          <RefreshCw
            data-icon='inline-start'
            className={cn('size-3.5', refreshing && 'animate-spin')}
            aria-hidden='true'
          />
          {refreshing ? t('Refreshing...') : t('Refresh')}
        </Button>
      </div>

      <div aria-busy={jobsQuery.isFetching}>
        {loading && (
          <div className='space-y-2 p-4 sm:p-5'>
            <Skeleton className='h-9 w-full rounded-md' />
            <Skeleton className='h-9 w-full rounded-md' />
            <Skeleton className='h-9 w-full rounded-md' />
          </div>
        )}
        {!loading && jobsQuery.isError && (
          <ErrorState
            title={t('We could not load Batch reconciliation.')}
            description={
              jobsQuery.error instanceof Error
                ? jobsQuery.error.message
                : undefined
            }
            onRetry={() => {
              void jobsQuery.refetch()
            }}
            className='min-h-[220px]'
          />
        )}
        {!loading && !jobsQuery.isError && jobs.length === 0 && (
          <EmptyState
            icon={Scale}
            title={t('No Batches need reconciliation.')}
            className='min-h-[200px]'
          />
        )}
        {!loading && !jobsQuery.isError && jobs.length > 0 && (
          <div className='space-y-2 p-4 sm:p-5'>
            <BatchReconciliationTable
              jobs={jobs}
              releasingId={
                releaseMutation.isPending
                  ? (releaseMutation.variables?.id ?? null)
                  : null
              }
              onRelease={setTarget}
              onRecover={setRecoverTarget}
            />
            {jobsQuery.data?.hasMore && (
              <p className='text-muted-foreground text-xs'>
                {t('Showing the newest {{count}} Batches.', {
                  count: RECONCILIATION_LIMIT,
                })}
              </p>
            )}
          </div>
        )}
      </div>

      <BatchReleaseDialog
        key={target?.id ?? 'closed'}
        job={target}
        isLoading={releaseMutation.isPending}
        onOpenChange={(open) => {
          if (!open && !releaseMutation.isPending) setTarget(null)
        }}
        onConfirm={(evidence) => {
          if (target) releaseMutation.mutate({ id: target.id, evidence })
        }}
      />
      <BatchRecoverDialog
        key={`recover-${recoverTarget?.id ?? 'closed'}`}
        job={recoverTarget}
        onClose={() => setRecoverTarget(null)}
        onSettled={invalidate}
      />
    </section>
  )
}
