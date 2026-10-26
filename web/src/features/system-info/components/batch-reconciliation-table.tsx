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
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { toIntlLocale } from '@/i18n/languages'
import {
  formatQuota,
  formatTimestampRelative,
  formatTimestampToDate,
} from '@/lib/format'

import type {
  BatchReconciliationJob,
  BatchReconciliationReason,
} from '../types'

const REASON_LABEL: Record<BatchReconciliationReason, string> = {
  submission_unknown: 'Creation outcome unknown',
  submission_unknown_overdue: 'Creation outcome unknown for over 48 hours',
  provider_window_exceeded: 'Provider window exceeded',
  terminal_without_cost: 'Finished without provider cost',
  unsettled_items: 'Unsettled requests',
  purchase_unconfirmed: 'Purchase cost unconfirmed',
}

const REASON_VARIANT: Record<
  BatchReconciliationReason,
  'destructive' | 'warning' | 'secondary'
> = {
  submission_unknown: 'warning',
  submission_unknown_overdue: 'destructive',
  provider_window_exceeded: 'destructive',
  terminal_without_cost: 'destructive',
  unsettled_items: 'warning',
  purchase_unconfirmed: 'secondary',
}

type BatchReconciliationTableProps = {
  jobs: BatchReconciliationJob[]
  releasingId: string | null
  onRelease: (job: BatchReconciliationJob) => void
}

export function BatchReconciliationTable(props: BatchReconciliationTableProps) {
  const { t, i18n } = useTranslation()

  return (
    <div className='overflow-x-auto rounded-md border'>
      <Table className='min-w-[1000px]'>
        <TableHeader>
          <TableRow className='bg-muted/40 hover:bg-muted/40'>
            <TableHead className='h-9 px-4 text-xs'>{t('Batch')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Reason')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Status')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Requests')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Held quota')}</TableHead>
            <TableHead className='h-9 text-xs'>{t('Created')}</TableHead>
            <TableHead className='h-9 pr-4 text-right text-xs'>
              {t('Actions')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.jobs.map((job) => (
            <TableRow key={job.id} className='hover:bg-muted/30'>
              <TableCell className='px-4 py-3 align-middle'>
                <div className='font-mono text-xs'>{job.id}</div>
                <div className='text-muted-foreground text-xs'>
                  {job.model} · {t('User')} {job.user_id} · {t('Channel')}{' '}
                  {job.channel_id}
                </div>
              </TableCell>
              <TableCell className='py-3 align-middle'>
                <Badge variant={REASON_VARIANT[job.reason] ?? 'secondary'}>
                  {t(REASON_LABEL[job.reason] ?? job.reason)}
                </Badge>
                {job.diagnostic?.code && (
                  <div
                    className='text-muted-foreground mt-1 max-w-[220px] truncate font-mono text-[11px]'
                    title={job.diagnostic.message}
                  >
                    {job.diagnostic.code}
                  </div>
                )}
              </TableCell>
              <TableCell className='text-muted-foreground py-3 align-middle font-mono text-[11px]'>
                <div>{job.status}</div>
                <div>{job.submit_state}</div>
                <div>{job.billing_status}</div>
              </TableCell>
              <TableCell className='py-3 align-middle text-xs tabular-nums'>
                {job.settled_count}/{job.result_count}/{job.request_count}
              </TableCell>
              <TableCell className='py-3 align-middle text-xs tabular-nums'>
                {formatQuota(job.reserved_quota)}
              </TableCell>
              <TableCell
                className='text-muted-foreground py-3 align-middle text-xs whitespace-nowrap'
                title={formatTimestampToDate(job.created_at)}
              >
                {formatTimestampRelative(
                  job.created_at,
                  'seconds',
                  toIntlLocale(i18n.language)
                )}
              </TableCell>
              <TableCell className='py-3 pr-4 text-right align-middle'>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={!job.can_release || props.releasingId === job.id}
                  onClick={() => props.onRelease(job)}
                >
                  {t('Release hold')}
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
