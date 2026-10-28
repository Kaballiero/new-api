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
import { useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

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
import { Textarea } from '@/components/ui/textarea'
import { formatQuota } from '@/lib/format'

import {
  MAX_BATCH_RELEASE_EVIDENCE_LENGTH,
  batchReleaseSchema,
  type BatchReleaseFormValues,
} from '../lib/batch-release'
import type { BatchReconciliationJob } from '../types'

type BatchReleaseDialogProps = {
  job: BatchReconciliationJob | null
  isLoading: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: (evidence: string) => void
}

export function BatchReleaseDialog(props: BatchReleaseDialogProps) {
  const { t } = useTranslation()
  const form = useForm<BatchReleaseFormValues>({
    resolver: zodResolver(batchReleaseSchema),
    defaultValues: { evidence: '' },
  })
  const evidence = useWatch({ control: form.control, name: 'evidence' })

  return (
    <ConfirmDialog
      open={props.job !== null}
      onOpenChange={props.onOpenChange}
      title={t('Release Batch hold')}
      desc={t(
        'Return the {{quota}} hold of Batch {{id}} to the user. No charge is made.',
        {
          quota: formatQuota(props.job?.reserved_quota ?? 0),
          id: props.job?.id ?? '',
        }
      )}
      confirmText={t('Release hold')}
      destructive
      disabled={evidence.trim() === ''}
      isLoading={props.isLoading}
      handleConfirm={() =>
        void form.handleSubmit((values) => props.onConfirm(values.evidence))()
      }
    >
      <Alert variant='destructive'>
        <AlertDescription>
          {t(
            'Release only after confirming with the provider that no Batch exists for this creation. If the provider did create it, its cost will not be recovered.'
          )}
        </AlertDescription>
      </Alert>
      <Form {...form}>
        <FormField
          control={form.control}
          name='evidence'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Provider evidence')}</FormLabel>
              <FormControl>
                <Textarea
                  maxLength={MAX_BATCH_RELEASE_EVIDENCE_LENGTH}
                  required
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t(
                  'Describe how you verified that the provider has no Batch. Stored only in the root audit log.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </Form>
    </ConfirmDialog>
  )
}
