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
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { formatLogQuota, formatTimestampToDate } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'

import { releaseOpenRouterSyncHold, settleOpenRouterSyncCost } from '../../api'
import {
  createOpenRouterSyncDecisionSchema,
  type OpenRouterSyncDecisionValues,
} from '../../lib/openrouter-sync'
import type {
  OpenRouterSyncAdminView,
  OpenRouterSyncOperatorAction,
} from '../../types'
import { DetailRow, DetailSection } from './log-detail-layout'

function OperatorDecisionDialog(props: {
  taskId: string
  action: OpenRouterSyncOperatorAction
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const schema = useMemo(
    () => createOpenRouterSyncDecisionSchema(props.action),
    [props.action]
  )
  const form = useForm<OpenRouterSyncDecisionValues>({
    resolver: zodResolver(schema),
    defaultValues: { cost_usd: '', evidence: '' },
    mode: 'onChange',
  })
  const release = props.action === 'release'
  const mutation = useMutation({
    mutationFn: (values: OpenRouterSyncDecisionValues) =>
      release
        ? releaseOpenRouterSyncHold(props.taskId, values.evidence)
        : settleOpenRouterSyncCost(
            props.taskId,
            values.cost_usd,
            values.evidence
          ),
    onSuccess: (response) => {
      toast.success(
        response.data?.already_applied
          ? t('This decision was already applied')
          : t('Operator decision applied')
      )
      queryClient.invalidateQueries({ queryKey: ['logs'] })
      form.reset()
      props.onOpenChange(false)
    },
    onError: (error) => {
      handleServerError(error, t('Failed to apply the operator decision'))
    },
  })

  return (
    <ConfirmDialog
      open={props.open}
      onOpenChange={(open) => {
        if (!open) form.reset()
        props.onOpenChange(open)
      }}
      title={release ? t('Release hold') : t('Settle with cost')}
      desc={
        release
          ? t(
              'Refund the full held quota. Use only when upstream evidence shows that OpenRouter did not charge this request.'
            )
          : t(
              'Charge the verified OpenRouter cost at the frozen task price basis and refund or charge the difference to the held quota.'
            )
      }
      confirmText={release ? t('Release hold') : t('Settle with cost')}
      destructive={release}
      disabled={!form.formState.isValid}
      isLoading={mutation.isPending}
      handleConfirm={form.handleSubmit((values) => mutation.mutate(values))}
    >
      <Form {...form}>
        <form
          className='space-y-3'
          onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
        >
          {release ? null : (
            <FormField
              control={form.control}
              name='cost_usd'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Upstream cost (USD)')}</FormLabel>
                  <FormControl>
                    <Input inputMode='decimal' autoComplete='off' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          )}
          <FormField
            control={form.control}
            name='evidence'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Evidence')}</FormLabel>
                <FormControl>
                  <Textarea rows={4} maxLength={4096} {...field} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </ConfirmDialog>
  )
}

function formatOperatorDecision(
  decision: NonNullable<OpenRouterSyncAdminView['operator_decision']>
): string {
  const decidedAt = formatTimestampToDate(decision.decided_at, 'seconds')
  return `${decision.action} ${decision.cost_usd} USD · #${decision.operator_id} · ${decidedAt}`
}

interface OpenRouterSyncReconciliationProps {
  taskId: string
  view: OpenRouterSyncAdminView
  isRoot: boolean
}

export function OpenRouterSyncReconciliation(
  props: OpenRouterSyncReconciliationProps
) {
  const { t } = useTranslation()
  const [action, setAction] = useState<OpenRouterSyncOperatorAction | null>(
    null
  )
  const view = props.view
  const lookup = [
    view.last_lookup_class || '-',
    view.last_http_status ? `HTTP ${view.last_http_status}` : null,
  ]
    .filter(Boolean)
    .join(' / ')
  const canRelease = props.isRoot && view.release_allowed
  const canSettle = props.isRoot && view.settle_allowed

  return (
    <DetailSection label={t('OpenRouter Reconciliation')}>
      <DetailRow label={t('Billing state')} value={view.billing_state} mono />
      <DetailRow label={t('Reason')} value={view.reason_code || '-'} mono />
      <DetailRow
        label={t('Result reason')}
        value={view.result_reason || '-'}
        mono
      />
      <DetailRow label={t('Last lookup')} value={lookup} mono />
      <DetailRow
        label={t('Lookup attempts')}
        value={String(view.lookup_attempts)}
        mono
      />
      <DetailRow
        label={t('Held quota')}
        value={formatLogQuota(view.reserved_quota)}
        mono
      />
      <DetailRow
        label={t('Target quota')}
        value={formatLogQuota(view.target_quota)}
        mono
      />
      {view.operator_decision ? (
        <DetailRow
          label={t('Operator decision')}
          value={formatOperatorDecision(view.operator_decision)}
          mono
        />
      ) : null}
      {canRelease || canSettle ? (
        <div className='flex flex-wrap gap-2 pt-1'>
          {canRelease ? (
            <Button
              size='sm'
              variant='destructive'
              onClick={() => setAction('release')}
            >
              {t('Release hold')}
            </Button>
          ) : null}
          {canSettle ? (
            <Button
              size='sm'
              variant='outline'
              onClick={() => setAction('settle')}
            >
              {t('Settle with cost')}
            </Button>
          ) : null}
        </div>
      ) : null}
      {action ? (
        <OperatorDecisionDialog
          key={action}
          taskId={props.taskId}
          action={action}
          open
          onOpenChange={(open) => {
            if (!open) setAction(null)
          }}
        />
      ) : null}
    </DetailSection>
  )
}
