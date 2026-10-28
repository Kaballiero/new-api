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
export type SystemInstanceStatus = 'online' | 'stale'

export type SystemInstanceInfo = {
  schema_version?: number
  node?: {
    name?: string
    source?: string
    manually_configured?: boolean
    should_configure_manually?: boolean
    [key: string]: unknown
  }
  role?: {
    is_master?: boolean
    [key: string]: unknown
  }
  runtime?: {
    version?: string
    goos?: string
    goarch?: string
    started_at?: number
    [key: string]: unknown
  }
  host?: {
    hostname?: string
    [key: string]: unknown
  }
  resources?: {
    cpu?: {
      usage_percent?: number
      [key: string]: unknown
    }
    memory?: {
      usage_percent?: number
      [key: string]: unknown
    }
    storage?: {
      total_bytes?: number
      used_bytes?: number
      free_bytes?: number
      used_percent?: number
      [key: string]: unknown
    }
    [key: string]: unknown
  }
  [key: string]: unknown
}

export type SystemInstance = {
  node_name: string
  status: SystemInstanceStatus
  stale_after_seconds: number
  started_at: number
  last_seen_at: number
  info?: SystemInstanceInfo
}

export type SystemInstanceListResponse = {
  success: boolean
  message: string
  data?: SystemInstance[]
}

export type SystemInstanceDeleteResponse = {
  success: boolean
  message: string
  data?: {
    deleted_count: number
  }
}

export type BatchReconciliationReason =
  | 'submission_unknown'
  | 'submission_unknown_overdue'
  | 'provider_window_exceeded'
  | 'terminal_without_cost'
  | 'unsettled_items'
  | 'purchase_unconfirmed'

export type BatchReconciliationDiagnostic = {
  phase?: string
  code?: string
  category?: string
  message?: string
  http_status?: number
  request_id?: string
  alerted_at?: number
}

export type BatchReconciliationJob = {
  id: string
  user_id: number
  token_id: number
  channel_id: number
  model: string
  upstream_model: string
  status: string
  submit_state: string
  billing_status: string
  purchase_status: string
  upstream_id: string
  request_count: number
  result_count: number
  settled_count: number
  reserved_quota: number
  charged_quota: number
  created_at: number
  updated_at: number
  diagnostic?: BatchReconciliationDiagnostic
  reason: BatchReconciliationReason
  can_release: boolean
  can_recover: boolean
}

export type BatchReconciliationListResponse = {
  success?: boolean
  message?: string
  data?: BatchReconciliationJob[]
  has_more?: boolean
}

export type BatchReleaseResponse = {
  success?: boolean
  id?: string
  released_quota?: number
  already_released?: boolean
  status?: 'release_pending'
}

export type BatchRecoverResponse = {
  success?: boolean
  id?: string
}
