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
import { useMutation } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'

import { recoverBatch } from '../api'
import {
  MAX_BATCH_UPSTREAM_ID_LENGTH,
  batchRecoverSchema,
  getBatchRecoverErrorMessage,
  type BatchRecoverFormValues,
} from '../lib/batch-recover'
import type { BatchReconciliationJob } from '../types'

type BatchRecoverDialogProps = {
  job: BatchReconciliationJob | null
  onClose: () => void
  /** Refreshes the queue after any attempt, since the Batch may have changed. */
  onSettled: () => Promise<unknown>
}

export function BatchRecoverDialog(props: BatchRecoverDialogProps) {
  const { t } = useTranslation()
  const form = useForm<BatchRecoverFormValues>({
    resolver: zodResolver(batchRecoverSchema),
    defaultValues: { upstreamId: '' },
    mode: 'onChange',
  })
  const recoverMutation = useMutation({
    mutationFn: async (input: { id: string; upstreamId: string }) => {
      const res = await recoverBatch(input.id, input.upstreamId)
      if (res.success !== true) {
        throw createServerError(res, t('Recovery failed'))
      }
      return res
    },
    onSuccess: async () => {
      toast.success(t('Batch recovered; polling resumes with actual cost'))
      props.onClose()
      await props.onSettled()
    },
    onError: (error) => {
      const known = getBatchRecoverErrorMessage(error)
      handleServerError(
        error,
        t('Recovery failed'),
        known ? { title: t(known) } : undefined
      )
      void props.onSettled()
    },
  })

  return (
    <ConfirmDialog
      open={props.job !== null}
      onOpenChange={(open) => {
        if (!open && !recoverMutation.isPending) props.onClose()
      }}
      title={t('Recover Batch by ID')}
      desc={t(
        'Bind Batch {{id}} to the provider Batch you found. The hold is then settled by actual cost.',
        { id: props.job?.id ?? '' }
      )}
      confirmText={t('Recover')}
      disabled={!form.formState.isValid}
      isLoading={recoverMutation.isPending}
      handleConfirm={() =>
        void form.handleSubmit((values) => {
          if (props.job) {
            recoverMutation.mutate({
              id: props.job.id,
              upstreamId: values.upstreamId,
            })
          }
        })()
      }
    >
      <Alert>
        <AlertDescription>
          {t(
            'Use only a Batch that exists in the provider dashboard. Ownership is verified against the requests of this Batch; if it cannot be verified, nothing changes.'
          )}
        </AlertDescription>
      </Alert>
      <Form {...form}>
        <FormField
          control={form.control}
          name='upstreamId'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Provider Batch ID')}</FormLabel>
              <FormControl>
                <Input
                  autoComplete='off'
                  spellCheck={false}
                  maxLength={MAX_BATCH_UPSTREAM_ID_LENGTH}
                  required
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'For example batch_abc123, msgbatch_abc123 or batches/abc123.'
                )}{' '}
                {t('For Gemini use the full name, e.g. batches/abc123')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </Form>
    </ConfirmDialog>
  )
}
