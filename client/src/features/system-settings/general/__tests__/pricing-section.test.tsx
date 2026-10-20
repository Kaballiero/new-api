import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { PricingSection } from '../pricing-section'

const mockUseStatus = vi.fn()

vi.mock('@/hooks/use-status', () => ({
  useStatus: () => mockUseStatus(),
}))

vi.mock('../../hooks/use-update-option', () => ({
  useUpdateOption: () => ({ mutateAsync: vi.fn(), isPending: false }),
}))

vi.mock('../../components/form-navigation-guard', () => ({
  FormNavigationGuard: () => null,
}))

const defaultValues = {
  QuotaPerUnit: 500000,
  USDExchangeRate: 7.3,
  DisplayInCurrencyEnabled: true,
  DisplayTokenStatEnabled: false,
  general_setting: {
    quota_display_type: 'USD' as const,
    custom_currency_symbol: '¤',
    custom_currency_exchange_rate: 1,
  },
}

describe('PricingSection billing FX display', () => {
  beforeEach(() => {
    mockUseStatus.mockReturnValue({ loading: false, error: null, status: null })
  })

  it('shows the current RAM billing FX rate separately from the legacy exchange rate', () => {
    mockUseStatus.mockReturnValue({
      loading: false,
      error: null,
      status: { billing_fx: { available: true, usd_rate: 89.5 } },
    })

    render(<PricingSection defaultValues={defaultValues} />)

    expect(screen.getByText('Current Billing FX Rate')).toBeVisible()
    expect(screen.getByDisplayValue('89.5')).toBeDisabled()
    expect(
      screen.getByText(
        'Current RAM rate used for model-cost billing. This does not change the legacy payment or top-up exchange rate.'
      )
    ).toBeVisible()
  })

  it('shows unavailable when the billing FX status is missing or unusable', () => {
    mockUseStatus.mockReturnValue({
      loading: false,
      error: null,
      status: { billing_fx: { available: true } },
    })

    render(<PricingSection defaultValues={defaultValues} />)

    expect(screen.getByDisplayValue('Unavailable')).toBeDisabled()
    expect(screen.queryByDisplayValue('1')).not.toBeInTheDocument()
  })

  it('keeps the billing FX rate visible when quota display uses tokens', () => {
    mockUseStatus.mockReturnValue({
      loading: false,
      error: null,
      status: { billing_fx: { available: true, usd_rate: 89.5 } },
    })

    render(
      <PricingSection
        defaultValues={{
          ...defaultValues,
          general_setting: {
            ...defaultValues.general_setting,
            quota_display_type: 'TOKENS',
          },
        }}
      />
    )

    expect(screen.getByDisplayValue('89.5')).toBeDisabled()
  })
})
