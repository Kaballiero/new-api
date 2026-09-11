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
import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'

export type EffectiveUnitPrice = {
  component: string
  unit: string
  amount_rub: number
}

export type EffectivePricingTier = {
  label?: string
  condition?: {
    kind: string
    unit?: string
    min_value?: number
    max_value?: number
    time_zone?: string
    time_windows?: { start_hour: number; end_hour: number }[]
  }
  unit_prices: EffectiveUnitPrice[]
}

export type EffectiveGroupPricing = {
  using_group: string
  billing_mode: string
  billing_surface: string
  status: string
  is_free: boolean
  tiers: EffectivePricingTier[]
  unit_prices?: EffectiveUnitPrice[]
  formula?: { expression: string; kind: string; output_to_rub_factor: number }
  limitations?: string[]
}

export type EffectivePricingModel = {
  model_name: string
  group_prices: EffectiveGroupPricing[]
}

export type EffectivePricingResponse = {
  user_group: string
  currency: 'RUB'
  fx: { usd_rate: number; publication_version: number; fetched_at: number }
  available_user_groups?: string[]
  data: EffectivePricingModel[]
}

export function useEffectivePricing(
  userGroup?: string,
  adminPreview = false,
  enabled = true
) {
  const user = useAuthStore((state) => state.auth.user)
  return useQuery({
    queryKey: [
      'effective-pricing',
      user?.id,
      user?.group,
      adminPreview,
      userGroup,
    ],
    queryFn: async () => {
      const response = await api.get<{
        success: boolean
        message?: string
        data: EffectivePricingResponse
      }>(
        adminPreview
          ? '/api/option/effective_pricing'
          : '/api/user/effective-pricing',
        {
          params:
            adminPreview && userGroup !== undefined
              ? { user_group: userGroup }
              : undefined,
        }
      )
      return requireServerSuccess(response.data).data
    },
    enabled:
      enabled &&
      Boolean(user) &&
      (!adminPreview || user?.role === ROLE.SUPER_ADMIN),
    staleTime: 0,
    refetchInterval: 60_000,
    retry: false,
    meta: { errorToast: false },
  })
}
