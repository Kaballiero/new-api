package i18n

// Message keys for i18n translations
// Use these constants instead of hardcoded strings

// Common error messages
const (
	MsgInvalidParams     = "common.invalid_params"
	MsgDatabaseError     = "common.database_error"
	MsgRetryLater        = "common.retry_later"
	MsgGenerateFailed    = "common.generate_failed"
	MsgNotFound          = "common.not_found"
	MsgUnauthorized      = "common.unauthorized"
	MsgForbidden         = "common.forbidden"
	MsgInvalidId         = "common.invalid_id"
	MsgIdEmpty           = "common.id_empty"
	MsgFeatureDisabled   = "common.feature_disabled"
	MsgOperationSuccess  = "common.operation_success"
	MsgOperationFailed   = "common.operation_failed"
	MsgUpdateSuccess     = "common.update_success"
	MsgUpdateFailed      = "common.update_failed"
	MsgCreateSuccess     = "common.create_success"
	MsgCreateFailed      = "common.create_failed"
	MsgDeleteSuccess     = "common.delete_success"
	MsgDeleteFailed      = "common.delete_failed"
	MsgAlreadyExists     = "common.already_exists"
	MsgNameCannotBeEmpty = "common.name_cannot_be_empty"
	MsgBatchTooMany      = "common.batch_too_many"
)

// Auth middleware messages
const (
	MsgAuthNotLoggedIn           = "auth.not_logged_in"
	MsgAuthAccessTokenInvalid    = "auth.access_token_invalid"
	MsgAuthUserInfoInvalid       = "auth.user_info_invalid"
	MsgAuthUserIdNotProvided     = "auth.user_id_not_provided"
	MsgAuthUserIdFormatError     = "auth.user_id_format_error"
	MsgAuthUserIdMismatch        = "auth.user_id_mismatch"
	MsgAuthUserBanned            = "auth.user_banned"
	MsgAuthInsufficientPrivilege = "auth.insufficient_privilege"
)

// Token related messages
const (
	MsgTokenNameTooLong                = "token.name_too_long"
	MsgTokenNameDuplicate              = "token.name_duplicate"
	MsgTokenQuotaNegative              = "token.quota_negative"
	MsgTokenQuotaExceedMax             = "token.quota_exceed_max"
	MsgTokenGenerateFailed             = "token.generate_failed"
	MsgTokenGetInfoFailed              = "token.get_info_failed"
	MsgTokenExpiredCannotEnable        = "token.expired_cannot_enable"
	MsgTokenExhaustedCannotEable       = "token.exhausted_cannot_enable"
	MsgTokenInvalid                    = "token.invalid"
	MsgTokenNotProvided                = "token.not_provided"
	MsgTokenExpired                    = "token.expired"
	MsgTokenExhausted                  = "token.exhausted"
	MsgTokenStatusUnavailable          = "token.status_unavailable"
	MsgTokenDbError                    = "token.db_error"
	MsgTokenNotExists                  = "token.not_exists"
	MsgTokenCountLimit                 = "token.count_limit"
	MsgTokenAutoGroupsTooMany          = "token.auto_groups_too_many"
	MsgTokenAutoGroupsDuplicate        = "token.auto_groups_duplicate"
	MsgTokenAutoGroupsInvalid          = "token.auto_groups_invalid"
	MsgTokenIDRequired                 = "token.id_required"
	MsgTokenSearchConsecutiveWildcards = "token.search_consecutive_wildcards"
	MsgTokenSearchTooManyWildcards     = "token.search_too_many_wildcards"
	MsgTokenSearchKeywordTooShort      = "token.search_keyword_too_short"
	MsgTokenSearchCountFailed          = "token.search_count_failed"
	MsgTokenFuzzySearchLimit           = "token.fuzzy_search_limit"
	MsgTokenSearchFailed               = "token.search_failed"
	MsgTokenListQueryInvalid           = "token.list_query_invalid"
)

// Redemption related messages
const (
	MsgRedemptionNameLength        = "redemption.name_length"
	MsgRedemptionCountPositive     = "redemption.count_positive"
	MsgRedemptionCountMax          = "redemption.count_max"
	MsgRedemptionCreateFailed      = "redemption.create_failed"
	MsgRedemptionInvalid           = "redemption.invalid"
	MsgRedemptionUsed              = "redemption.used"
	MsgRedemptionExpired           = "redemption.expired"
	MsgRedemptionFailed            = "redemption.failed"
	MsgRedemptionNotProvided       = "redemption.not_provided"
	MsgRedemptionExpireTimeInvalid = "redemption.expire_time_invalid"
)

// User related messages
const (
	MsgUserPasswordLoginDisabled     = "user.password_login_disabled"
	MsgUserRegisterDisabled          = "user.register_disabled"
	MsgUserPasswordRegisterDisabled  = "user.password_register_disabled"
	MsgUserUsernameOrPasswordEmpty   = "user.username_or_password_empty"
	MsgUserUsernameOrPasswordError   = "user.username_or_password_error"
	MsgUserEmailOrPasswordEmpty      = "user.email_or_password_empty"
	MsgUserExists                    = "user.exists"
	MsgUserNotExists                 = "user.not_exists"
	MsgUserDisabled                  = "user.disabled"
	MsgUserSessionSaveFailed         = "user.session_save_failed"
	MsgUserRequire2FA                = "user.require_2fa"
	MsgUserEmailVerificationRequired = "user.email_verification_required"
	MsgUserVerificationCodeError     = "user.verification_code_error"
	MsgUserEmailAlreadyTaken         = "user.email_already_taken"
	MsgUserPasswordUnset             = "user.password_unset"
	MsgUserPasswordResetLinkInvalid  = "user.password_reset_link_invalid"
	MsgUserInputInvalid              = "user.input_invalid"
	MsgUserNoPermissionSameLevel     = "user.no_permission_same_level"
	MsgUserNoPermissionHigherLevel   = "user.no_permission_higher_level"
	MsgUserCannotCreateHigherLevel   = "user.cannot_create_higher_level"
	MsgUserCannotDeleteRootUser      = "user.cannot_delete_root_user"
	MsgUserCannotDisableRootUser     = "user.cannot_disable_root_user"
	MsgUserCannotDemoteRootUser      = "user.cannot_demote_root_user"
	MsgUserAlreadyAdmin              = "user.already_admin"
	MsgUserAlreadyCommon             = "user.already_common"
	MsgUserAdminCannotPromote        = "user.admin_cannot_promote"
	MsgUserOriginalPasswordError     = "user.original_password_error"
	MsgUserInviteQuotaInsufficient   = "user.invite_quota_insufficient"
	MsgUserTransferQuotaMinimum      = "user.transfer_quota_minimum"
	MsgUserTransferSuccess           = "user.transfer_success"
	MsgUserTransferFailed            = "user.transfer_failed"
	MsgUserTopUpProcessing           = "user.topup_processing"
	MsgUserRegisterFailed            = "user.register_failed"
	MsgUserDefaultTokenFailed        = "user.default_token_failed"
	MsgUserAffCodeEmpty              = "user.aff_code_empty"
	MsgUserEmailEmpty                = "user.email_empty"
	MsgUserGitHubIdEmpty             = "user.github_id_empty"
	MsgUserDiscordIdEmpty            = "user.discord_id_empty"
	MsgUserOidcIdEmpty               = "user.oidc_id_empty"
	MsgUserWeChatIdEmpty             = "user.wechat_id_empty"
	MsgUserTelegramIdEmpty           = "user.telegram_id_empty"
	MsgUserTelegramNotBound          = "user.telegram_not_bound"
	MsgUserLinuxDOIdEmpty            = "user.linux_do_id_empty"
	MsgUserQuotaChangeZero           = "user.quota_change_zero"
)

// Quota related messages
const (
	MsgQuotaNegative        = "quota.negative"
	MsgQuotaExceedMax       = "quota.exceed_max"
	MsgQuotaInsufficient    = "quota.insufficient"
	MsgQuotaWarningInvalid  = "quota.warning_invalid"
	MsgQuotaThresholdGtZero = "quota.threshold_gt_zero"
)

// Subscription related messages
const (
	MsgSubscriptionNotEnabled       = "subscription.not_enabled"
	MsgSubscriptionTitleEmpty       = "subscription.title_empty"
	MsgSubscriptionPriceNegative    = "subscription.price_negative"
	MsgSubscriptionPriceMax         = "subscription.price_max"
	MsgSubscriptionPurchaseLimitNeg = "subscription.purchase_limit_negative"
	MsgSubscriptionQuotaNegative    = "subscription.quota_negative"
	MsgSubscriptionGroupNotExists   = "subscription.group_not_exists"
	MsgSubscriptionResetCycleGtZero = "subscription.reset_cycle_gt_zero"
	MsgSubscriptionPurchaseMax      = "subscription.purchase_max"
	MsgSubscriptionInvalidId        = "subscription.invalid_id"
	MsgSubscriptionInvalidUserId    = "subscription.invalid_user_id"
)

// Payment related messages
const (
	MsgPaymentNotConfigured      = "payment.not_configured"
	MsgPaymentMethodNotExists    = "payment.method_not_exists"
	MsgPaymentCallbackError      = "payment.callback_error"
	MsgPaymentCreateFailed       = "payment.create_failed"
	MsgPaymentStartFailed        = "payment.start_failed"
	MsgPaymentAmountTooLow       = "payment.amount_too_low"
	MsgPaymentStripeNotConfig    = "payment.stripe_not_configured"
	MsgPaymentWebhookNotConfig   = "payment.webhook_not_configured"
	MsgPaymentPriceIdNotConfig   = "payment.price_id_not_configured"
	MsgPaymentCreemNotConfig     = "payment.creem_not_configured"
	MsgPaymentComplianceRequired = "payment.compliance_required"
)

// Topup related messages
const (
	MsgTopupNotProvided    = "topup.not_provided"
	MsgTopupOrderNotExists = "topup.order_not_exists"
	MsgTopupOrderStatus    = "topup.order_status"
	MsgTopupFailed         = "topup.failed"
	MsgTopupInvalidQuota   = "topup.invalid_quota"
)

// Channel related messages
const (
	MsgChannelNotExists          = "channel.not_exists"
	MsgChannelIdFormatError      = "channel.id_format_error"
	MsgChannelNoAvailableKey     = "channel.no_available_key"
	MsgChannelGetListFailed      = "channel.get_list_failed"
	MsgChannelGetTagsFailed      = "channel.get_tags_failed"
	MsgChannelGetKeyFailed       = "channel.get_key_failed"
	MsgChannelGetOllamaFailed    = "channel.get_ollama_failed"
	MsgChannelQueryFailed        = "channel.query_failed"
	MsgChannelNoValidUpstream    = "channel.no_valid_upstream"
	MsgChannelUpstreamSaturated  = "channel.upstream_saturated"
	MsgChannelGetAvailableFailed = "channel.get_available_failed"
)

// Model related messages
const (
	MsgModelNameEmpty     = "model.name_empty"
	MsgModelNameExists    = "model.name_exists"
	MsgModelIdMissing     = "model.id_missing"
	MsgModelGetListFailed = "model.get_list_failed"
	MsgModelGetFailed     = "model.get_failed"
	MsgModelResetSuccess  = "model.reset_success"
)

// Vendor related messages
const (
	MsgVendorNameEmpty  = "vendor.name_empty"
	MsgVendorNameExists = "vendor.name_exists"
	MsgVendorIdMissing  = "vendor.id_missing"
)

// Group related messages
const (
	MsgGroupNameTypeEmpty = "group.name_type_empty"
	MsgGroupNameExists    = "group.name_exists"
	MsgGroupIdMissing     = "group.id_missing"
	MsgGroupNotExists     = "group.not_exists"
)

// Pricing/ratio admin endpoints
const (
	MsgPricingMultiplierInvalid = "pricing.multiplier_invalid"
	MsgPricingScopeInvalid      = "pricing.scope_invalid"
	MsgChannelTypeInvalid       = "channel.type_invalid"
)

// Checkin related messages
const (
	MsgCheckinDisabled     = "checkin.disabled"
	MsgCheckinAlreadyToday = "checkin.already_today"
	MsgCheckinFailed       = "checkin.failed"
	MsgCheckinQuotaFailed  = "checkin.quota_failed"
	MsgCheckinSuccess      = "checkin.success"
)

// Passkey related messages
const (
	MsgPasskeyCreateFailed  = "passkey.create_failed"
	MsgPasskeyLoginAbnormal = "passkey.login_abnormal"
	MsgPasskeyUpdateFailed  = "passkey.update_failed"
	MsgPasskeyInvalidUserId = "passkey.invalid_user_id"
	MsgPasskeyVerifyFailed  = "passkey.verify_failed"
)

// 2FA related messages
const (
	MsgTwoFANotEnabled    = "twofa.not_enabled"
	MsgTwoFAUserIdEmpty   = "twofa.user_id_empty"
	MsgTwoFAAlreadyExists = "twofa.already_exists"
	MsgTwoFARecordIdEmpty = "twofa.record_id_empty"
	MsgTwoFACodeInvalid   = "twofa.code_invalid"
)

// Rate limit related messages
const (
	MsgRateLimitReached      = "rate_limit.reached"
	MsgRateLimitTotalReached = "rate_limit.total_reached"
)

// Setting related messages
const (
	MsgSettingInvalidType      = "setting.invalid_type"
	MsgSettingWebhookEmpty     = "setting.webhook_empty"
	MsgSettingWebhookInvalid   = "setting.webhook_invalid"
	MsgSettingEmailInvalid     = "setting.email_invalid"
	MsgSettingBarkUrlEmpty     = "setting.bark_url_empty"
	MsgSettingBarkUrlInvalid   = "setting.bark_url_invalid"
	MsgSettingGotifyUrlEmpty   = "setting.gotify_url_empty"
	MsgSettingGotifyTokenEmpty = "setting.gotify_token_empty"
	MsgSettingGotifyUrlInvalid = "setting.gotify_url_invalid"
	MsgSettingUrlMustHttp      = "setting.url_must_http"
	MsgSettingSaved            = "setting.saved"
)

// Deployment related messages (io.net)
const (
	MsgDeploymentNotEnabled     = "deployment.not_enabled"
	MsgDeploymentIdRequired     = "deployment.id_required"
	MsgDeploymentContainerIdReq = "deployment.container_id_required"
	MsgDeploymentNameEmpty      = "deployment.name_empty"
	MsgDeploymentNameTaken      = "deployment.name_taken"
	MsgDeploymentHardwareIdReq  = "deployment.hardware_id_required"
	MsgDeploymentHardwareInvId  = "deployment.hardware_invalid_id"
	MsgDeploymentApiKeyRequired = "deployment.api_key_required"
	MsgDeploymentInvalidPayload = "deployment.invalid_payload"
	MsgDeploymentNotFound       = "deployment.not_found"
)

// Performance related messages
const (
	MsgPerfDiskCacheCleared = "performance.disk_cache_cleared"
	MsgPerfStatsReset       = "performance.stats_reset"
	MsgPerfGcExecuted       = "performance.gc_executed"
)

// Ability related messages
const (
	MsgAbilityDbCorrupted   = "ability.db_corrupted"
	MsgAbilityRepairRunning = "ability.repair_running"
)

// OAuth related messages
const (
	MsgOAuthInvalidCode     = "oauth.invalid_code"
	MsgOAuthGetUserErr      = "oauth.get_user_error"
	MsgOAuthAccountUsed     = "oauth.account_used"
	MsgOAuthUnknownProvider = "oauth.unknown_provider"
	MsgOAuthStateInvalid    = "oauth.state_invalid"
	MsgOAuthNotEnabled      = "oauth.not_enabled"
	MsgOAuthUserDeleted     = "oauth.user_deleted"
	MsgOAuthUserBanned      = "oauth.user_banned"
	MsgOAuthBindSuccess     = "oauth.bind_success"
	MsgOAuthAlreadyBound    = "oauth.already_bound"
	MsgOAuthConnectFailed   = "oauth.connect_failed"
	MsgOAuthTokenFailed     = "oauth.token_failed"
	MsgOAuthUserInfoEmpty   = "oauth.user_info_empty"
	MsgOAuthTrustLevelLow   = "oauth.trust_level_low"
)

// Model layer error messages (for translation in controller)
const (
	MsgRedeemFailed          = "redeem.failed"
	MsgCreateDefaultTokenErr = "user.create_default_token_error"
	MsgUuidDuplicate         = "common.uuid_duplicate"
	MsgInvalidInput          = "common.invalid_input"
)

// Distributor related messages
const (
	MsgDistributorInvalidRequest               = "distributor.invalid_request"
	MsgDistributorInvalidChannelId             = "distributor.invalid_channel_id"
	MsgDistributorChannelDisabled              = "distributor.channel_disabled"
	MsgDistributorAffinityChannelDisabled      = "distributor.affinity_channel_disabled"
	MsgDistributorTokenNoModelAccess           = "distributor.token_no_model_access"
	MsgDistributorTokenModelForbidden          = "distributor.token_model_forbidden"
	MsgDistributorModelNameRequired            = "distributor.model_name_required"
	MsgDistributorInvalidPlayground            = "distributor.invalid_playground_request"
	MsgDistributorGroupAccessDenied            = "distributor.group_access_denied"
	MsgDistributorGetChannelFailed             = "distributor.get_channel_failed"
	MsgDistributorNoAvailableChannel           = "distributor.no_available_channel"
	MsgDistributorNoAvailableChannelTaskPlugin = "distributor.no_available_channel_task_plugin"
	MsgDistributorInvalidMidjourney            = "distributor.invalid_midjourney_request"
	MsgDistributorInvalidParseModel            = "distributor.invalid_request_parse_model"
)

// Custom OAuth provider related messages
const (
	MsgCustomOAuthNotFound          = "custom_oauth.not_found"
	MsgCustomOAuthSlugEmpty         = "custom_oauth.slug_empty"
	MsgCustomOAuthSlugExists        = "custom_oauth.slug_exists"
	MsgCustomOAuthNameEmpty         = "custom_oauth.name_empty"
	MsgCustomOAuthHasBindings       = "custom_oauth.has_bindings"
	MsgCustomOAuthBindingNotFound   = "custom_oauth.binding_not_found"
	MsgCustomOAuthProviderIdInvalid = "custom_oauth.provider_id_field_invalid"
)

// Identity related messages
const (
	MsgIdentityUserIDInvalid                   = "identity.user_id_invalid"
	MsgIdentityTwoFAManageForbidden            = "identity.twofa_manage_forbidden"
	MsgIdentityPasskeyNotBound                 = "identity.passkey_not_bound"
	MsgIdentityParamsError                     = "identity.params_error"
	MsgIdentityPasskeyDisabled                 = "identity.passkey_disabled"
	MsgIdentityPasskeyRequestInvalid           = "identity.passkey_request_invalid"
	MsgIdentityWeChatDisabled                  = "identity.wechat_disabled"
	MsgIdentityRequestInvalid                  = "identity.request_invalid"
	MsgIdentityWeChatAlreadyBound              = "identity.wechat_already_bound"
	MsgIdentitySecurityVerificationUnsupported = "identity.security_verification_unsupported"
	MsgIdentityNotLoggedIn                     = "identity.not_logged_in"
	MsgIdentityBackupCodesGenerateFailed       = "identity.backup_codes_generate_failed"
	MsgIdentityBackupCodesSaveFailed           = "identity.backup_codes_save_failed"
	MsgIdentityBindingLoginRequired            = "identity.binding_login_required"
	MsgIdentityProviderIDInvalid               = "identity.provider_id_invalid"
	MsgIdentityAdminTwoFADisabled              = "identity.admin_twofa_disabled"
	MsgIdentityBackupCodesRegenerated          = "identity.backup_codes_regenerated"
	MsgIdentityPasskeyRegistered               = "identity.passkey_registered"
	MsgIdentityPasskeyUnbound                  = "identity.passkey_unbound"
	MsgIdentityPasskeyReset                    = "identity.passkey_reset"
	MsgIdentityTwoFADisabled                   = "identity.twofa_disabled"
	MsgIdentityOAuthUnbound                    = "identity.oauth_unbound"
	MsgIdentityDefaultTokenName                = "identity.default_token_name"
	MsgIdentityVerificationEmailSubject        = "identity.verification_email_subject"
	MsgIdentityVerificationEmailContent        = "identity.verification_email_content"
	MsgIdentityPasswordResetEmailSubject       = "identity.password_reset_email_subject"
	MsgIdentityPasswordResetEmailContent       = "identity.password_reset_email_content"
)

// Security proof, Turnstile and e-mail rate limit middleware messages
const (
	MsgSecurityProofInvalid               = "security.proof_invalid"
	MsgSecurityProofRequired              = "security.proof_required"
	MsgSecurityProofExpired               = "security.proof_expired"
	MsgSecurityProofScopeMismatch         = "security.proof_scope_mismatch"
	MsgSecurityProofMethodMismatch        = "security.proof_method_mismatch"
	MsgTurnstileTokenEmpty                = "turnstile.token_empty"
	MsgTurnstileVerifyFailed              = "turnstile.verify_failed"
	MsgEmailVerificationRateLimitedWait   = "rate_limit.email_verification_wait"
	MsgEmailVerificationRateLimitedNoWait = "rate_limit.email_verification"
)

// Account messages and persisted account texts
const (
	MsgAccountIDRequired     = "user.id_required"
	MsgAccountUserGroupLabel = "group.user_group_label"
	MsgAccountAutoGroupRatio = "group.auto_ratio_label"
	MsgAccountCheckinLog     = "checkin.log_reward"
	MsgAccountRegisterLog    = "user.log_register_reward"
	MsgAccountInviteeLog     = "user.log_invitee_reward"
	MsgAccountInviterLog     = "user.log_inviter_reward"
)

// Pricing module messages
const (
	MsgPricingRequestInvalid           = "pricing.request_invalid"
	MsgPricingRatioConfigDisabled      = "pricing.ratio_config_disabled"
	MsgPricingUpstreamDataUnrecognized = "pricing.upstream_data_unrecognized"
)

// Channel management messages, notifications and persisted channel texts
const (
	MsgChannelParamsError                = "channel.params_error"
	MsgChannelFetchModelsFailed          = "channel.fetch_models_failed"
	MsgChannelOllamaVersionFailed        = "channel.ollama_version_failed"
	MsgChannelAddModeUnsupported         = "channel.add_mode_unsupported"
	MsgChannelSettingInvalid             = "channel.setting_invalid"
	MsgChannelKeyUnavailable             = "channel.key_unavailable"
	MsgChannelModelNameTooLong           = "channel.model_name_too_long"
	MsgChannelVertexRegionRequired       = "channel.vertex_region_required"
	MsgChannelVertexRegionInvalid        = "channel.vertex_region_invalid"
	MsgChannelVertexRegionDefaultMissing = "channel.vertex_region_default_missing"
	MsgChannelVertexKeysInvalid          = "channel.vertex_keys_invalid"
	MsgChannelVertexKeyEncodeFailed      = "channel.vertex_key_encode_failed"
	MsgChannelVertexKeysEmpty            = "channel.vertex_keys_empty"
	MsgChannelAppendKeysInvalid          = "channel.append_keys_invalid"
	MsgChannelTagRequired                = "channel.tag_required"
	MsgChannelParamOverrideInvalid       = "channel.param_override_invalid"
	MsgChannelHeaderOverrideInvalid      = "channel.header_override_invalid"
	MsgChannelCopySourceFailed           = "channel.copy_source_failed"
	MsgChannelCopyFailed                 = "channel.copy_failed"
	MsgChannelCountTagsFailed            = "channel.count_tags_failed"
	MsgChannelGetTagChannelsFailed       = "channel.get_tag_channels_failed"
	MsgChannelCountFailed                = "channel.count_failed"
	MsgChannelCountTypesFailed           = "channel.count_types_failed"
	MsgChannelNotMultiKey                = "channel.not_multi_key"
	MsgChannelDisableKeyIndexRequired    = "channel.disable_key_index_required"
	MsgChannelEnableKeyIndexRequired     = "channel.enable_key_index_required"
	MsgChannelDeleteKeyIndexRequired     = "channel.delete_key_index_required"
	MsgChannelKeyIndexOutOfRange         = "channel.key_index_out_of_range"
	MsgChannelNoKeysToDisable            = "channel.no_keys_to_disable"
	MsgChannelLastKeyNotDeletable        = "channel.last_key_not_deletable"
	MsgChannelNoAutoDisabledKeys         = "channel.no_auto_disabled_keys"
	MsgChannelMultiKeyActionUnsupported  = "channel.multi_key_action_unsupported"
	MsgChannelKeyDisabled                = "channel.key_disabled"
	MsgChannelKeyEnabled                 = "channel.key_enabled"
	MsgChannelKeysEnabled                = "channel.keys_enabled"
	MsgChannelKeysDisabled               = "channel.keys_disabled"
	MsgChannelKeyDeleted                 = "channel.key_deleted"
	MsgChannelAutoDisabledKeysDeleted    = "channel.auto_disabled_keys_deleted"
	MsgChannelBalanceMultiKey            = "channel.balance_multi_key"
	MsgChannelBalanceNotImplemented      = "channel.balance_not_implemented"
	MsgChannelCodexCredentialInvalid     = "channel.codex_credential_invalid"
	MsgChannelCodexUsageFailed           = "channel.codex_usage_failed"
	MsgChannelCodexResetCreditsFailed    = "channel.codex_reset_credits_failed"
	MsgChannelCodexResetFailed           = "channel.codex_reset_failed"
	MsgChannelCodexRefreshFailed         = "channel.codex_refresh_failed"
	MsgChannelAffinityRuleRequired       = "channel.affinity_rule_required"
	MsgChannelTestTaskBusy               = "channel.test_task_busy"
	MsgChannelModelUpdateTaskBusy        = "channel.model_update_task_busy"

	MsgChannelDisabledSubject            = "channel.disabled_subject"
	MsgChannelDisabledContent            = "channel.disabled_content"
	MsgChannelEnabledNotice              = "channel.enabled_notice"
	MsgChannelTestDoneSubject            = "channel.test_done_subject"
	MsgChannelTestDoneContent            = "channel.test_done_content"
	MsgChannelBalanceExhausted           = "channel.balance_exhausted"
	MsgChannelResponseTimeExceeded       = "channel.response_time_exceeded"
	MsgChannelTestLogName                = "channel.test_log_name"
	MsgChannelModelUpdateSubject         = "channel.model_update_subject"
	MsgChannelModelUpdateSummary         = "channel.model_update_summary"
	MsgChannelModelUpdateChannels        = "channel.model_update_channels"
	MsgChannelModelUpdateChannelsOmitted = "channel.model_update_channels_omitted"
	MsgChannelModelUpdateAddSamples      = "channel.model_update_add_samples"
	MsgChannelModelUpdateRemoveSamples   = "channel.model_update_remove_samples"
	MsgChannelModelUpdateFailedIDs       = "channel.model_update_failed_ids"
	MsgChannelModelUpdateOmitted         = "channel.model_update_omitted"
)

const (
	MsgGatewayIPInvalid        = "gateway.ip_invalid"
	MsgGatewayIPDenied         = "gateway.ip_denied"
	MsgGatewayGroupForbidden   = "gateway.group_forbidden"
	MsgGatewayGroupDeprecated  = "gateway.group_deprecated"
	MsgGatewayChannelForbidden = "gateway.channel_forbidden"
)

const (
	MsgBillingWalletInsufficient       = "billing.wallet_insufficient"
	MsgBillingWalletBelowReserve       = "billing.wallet_below_reserve"
	MsgBillingSubscriptionInsufficient = "billing.subscription_insufficient"
	MsgBillingQuotaNegative            = "billing.quota_negative"
	MsgBillingWalletWarning            = "billing.wallet_warning"
	MsgBillingSubscriptionWarning      = "billing.subscription_warning"
	MsgBillingQuotaWarningBrief        = "billing.quota_warning_brief"
	MsgBillingQuotaWarningText         = "billing.quota_warning_text"
	MsgBillingQuotaWarningHTML         = "billing.quota_warning_html"
)

// Usage read-side messages.
const (
	MsgUsageDeprecated            = "usage.deprecated"
	MsgUsageInvalidToken          = "usage.invalid_token"
	MsgUsageQueryFailed           = "usage.query_failed"
	MsgUsageStatsFailed           = "usage.stats_failed"
	MsgUsageSpanExceeded          = "usage.span_exceeded"
	MsgUsageInvalidStart          = "usage.invalid_start"
	MsgUsageInvalidEnd            = "usage.invalid_end"
	MsgUsageInvalidRange          = "usage.invalid_range"
	MsgUsageInvalidTokenID        = "usage.invalid_token_id"
	MsgUsagePositiveTime          = "usage.positive_time"
	MsgUsageGranularity           = "usage.granularity"
	MsgUsageMonthLimit            = "usage.month_limit"
	MsgUsageAggregateRequestSize  = "usage.aggregate_request_size"
	MsgUsageAggregateJSON         = "usage.aggregate_json"
	MsgUsageAggregateNotFound     = "usage.aggregate_not_found"
	MsgUsageAggregateResponseSize = "usage.aggregate_response_size"
	MsgUsageAggregateOverflow     = "usage.aggregate_overflow"
	MsgUsageAggregateUserCount    = "usage.aggregate_user_count"
	MsgUsageAggregateUserIDs      = "usage.aggregate_user_ids"
	MsgUsageAggregateTimes        = "usage.aggregate_times"
	MsgUsageAggregateGranularity  = "usage.aggregate_granularity"
	MsgUsageAggregateMonths       = "usage.aggregate_months"
	MsgUsageAggregateGroup        = "usage.aggregate_group"
	MsgUsageRankingPeriod         = "usage.ranking_period"
)

// Audit query validation messages.
const (
	MsgAuditInvalidPagination = "audit.invalid_pagination"
	MsgAuditInvalidFilters    = "audit.invalid_filters"
	MsgAuditInvalidTimeRange  = "audit.invalid_time_range"
	MsgAuditInvalidResult     = "audit.invalid_result"
)

const (
	MsgRedemptionIDEmpty       = "redemption.id_empty"
	MsgRedemptionQuotaPositive = "redemption.quota_positive"
	MsgRedemptionWalletQuota   = "redemption.wallet_quota"
	MsgRedemptionQuotaAmount   = "redemption.quota_amount"
	MsgRedemptionQuotaTokens   = "redemption.quota_tokens"
	MsgRedemptionCreditLog     = "redemption.credit_log"
)

// Payments compatibility messages; translated separately from the structural extraction.
const (
	MsgPaymentsInvalidParameters = "payments.invalid_parameters"
	MsgPaymentsGroup             = "payments.group"
	MsgPaymentsLowAmount         = "payments.low_amount"
	MsgPaymentsUser              = "payments.user"
	MsgPaymentsChannel           = "payments.channel"
	MsgPaymentsCheckout          = "payments.checkout"
	MsgPaymentsInsert            = "payments.insert"
	MsgPaymentsQuotaRange        = "payments.quota_range"
	MsgPaymentsQuotaPositive     = "payments.quota_positive"
	MsgPaymentsAmount            = "payments.amount"
	MsgPaymentsMethod            = "payments.method"
	MsgPaymentsEpayConfig        = "payments.epay_config"
	MsgPaymentsStripeMaximum     = "payments.stripe_maximum"
	MsgPaymentsSuccessURL        = "payments.success_url"
	MsgPaymentsCancelURL         = "payments.cancel_url"
	MsgPaymentsProductRequired   = "payments.product_required"
	MsgPaymentsProductsConfig    = "payments.products_config"
	MsgPaymentsProductMissing    = "payments.product_missing"
	MsgPaymentsWaffoDisabled     = "payments.waffo_disabled"
	MsgPaymentsPayMethod         = "payments.pay_method"
	MsgPaymentsPaymentConfig     = "payments.payment_config"
	MsgPaymentsPancakeConfig     = "payments.pancake_config"
	MsgPaymentsCredentials       = "payments.credentials"
	MsgPaymentsSaveConfig        = "payments.save_config"
	MsgPaymentsCatalog           = "payments.catalog"
	MsgPaymentsSearch            = "payments.search"
	MsgPaymentsTradeNo           = "payments.trade_no"
	MsgPaymentsPaymentTradeNo    = "payments.payment_trade_no"
	MsgPaymentsOrderMissing      = "payments.order_missing"
	MsgPaymentsManualStatus      = "payments.manual_status"
	MsgPaymentsOrderStatus       = "payments.order_status"
	MsgPaymentsRecharge          = "payments.recharge"
	MsgPaymentsCompliance        = "payments.compliance"
	MsgPaymentsInvalidQuota      = "payments.invalid_quota"
	MsgPaymentsWalletLimit       = "payments.wallet_limit"
	MsgPaymentsProviderMismatch  = "payments.provider_mismatch"
	MsgPaymentsNotFound          = "payments.not_found"
	MsgPaymentsNotPending        = "payments.not_pending"
	MsgPaymentsSessionOnly       = "payments.session_only"
	MsgPaymentsMinimum           = "payments.minimum"
	MsgPaymentsMaximum           = "payments.maximum"
)

// Creem request-body read failure keeps its original compatibility bytes.
const MsgPaymentsReadQuery = "payments.read_query"

// Payment receipt suffixes follow the beneficiary language.
const (
	MsgPaymentsQuotaAmount = "payments.quota_amount"
	MsgPaymentsQuotaTokens = "payments.quota_tokens"
)

const (
	MsgSubscriptionsInvalidUserID     = "subscriptions.invalid_user_id"
	MsgSubscriptionsInvalidParameters = "subscriptions.invalid_parameters"
)

const (
	MsgSubscriptionsInvalidID             = "subscriptions.invalid_id"
	MsgSubscriptionsTitleRequired         = "subscriptions.title_required"
	MsgSubscriptionsNegativePrice         = "subscriptions.negative_price"
	MsgSubscriptionsHighPrice             = "subscriptions.high_price"
	MsgSubscriptionsNegativePurchaseLimit = "subscriptions.negative_purchase_limit"
	MsgSubscriptionsNegativeTotal         = "subscriptions.negative_total"
	MsgSubscriptionsUpgradeGroupMissing   = "subscriptions.upgrade_group_missing"
	MsgSubscriptionsDowngradeGroupMissing = "subscriptions.downgrade_group_missing"
	MsgSubscriptionsInvalidResetSeconds   = "subscriptions.invalid_reset_seconds"
)

const (
	MsgSubscriptionsPurchaseLimit         = "subscriptions.purchase_limit"
	MsgSubscriptionsNoActivePlan          = "subscriptions.no_active_plan"
	MsgSubscriptionsPlanDisabled          = "subscriptions.plan_disabled"
	MsgSubscriptionsBalanceNegativePrice  = "subscriptions.balance_negative_price"
	MsgSubscriptionsBalanceDisallowed     = "subscriptions.balance_disallowed"
	MsgSubscriptionsInsufficientBalance   = "subscriptions.insufficient_balance"
	MsgSubscriptionsInvalidQuotaUnit      = "subscriptions.invalid_quota_unit"
	MsgSubscriptionsInvalidSubscriptionID = "subscriptions.invalid_subscription_id"
	MsgSubscriptionsGroupUpgrade          = "subscriptions.group_upgrade"
	MsgSubscriptionsGroupDowngrade        = "subscriptions.group_downgrade"
)

const (
	MsgSubscriptionsStripePriceMissing    = "subscriptions.stripe_price_missing"
	MsgSubscriptionsStripeConfig          = "subscriptions.stripe_config"
	MsgSubscriptionsStripeWebhook         = "subscriptions.stripe_webhook"
	MsgSubscriptionsCreemProductMissing   = "subscriptions.creem_product_missing"
	MsgSubscriptionsCreemWebhook          = "subscriptions.creem_webhook"
	MsgSubscriptionsPancakeProductMissing = "subscriptions.pancake_product_missing"
	MsgSubscriptionsPancakeConfig         = "subscriptions.pancake_config"
	MsgSubscriptionsUserMissing           = "subscriptions.user_missing"
	MsgSubscriptionsPriceLow              = "subscriptions.price_low"
	MsgSubscriptionsPayMethod             = "subscriptions.pay_method"
	MsgSubscriptionsCallbackConfig        = "subscriptions.callback_config"
	MsgSubscriptionsEpayConfig            = "subscriptions.epay_config"
	MsgSubscriptionsCreateOrder           = "subscriptions.create_order"
	MsgSubscriptionsCheckout              = "subscriptions.checkout"
)

const (
	MsgSubscriptionsProductName    = "subscriptions.product_name"
	MsgSubscriptionsProductAmount  = "subscriptions.product_amount"
	MsgSubscriptionsPancakeBinding = "subscriptions.pancake_binding"
	MsgSubscriptionsCreateProduct  = "subscriptions.create_product"
	MsgSubscriptionsProductCatalog = "subscriptions.product_catalog"
)

const (
	MsgSubscriptionsReceiptPurchase = "subscriptions.receipt_purchase"
	MsgSubscriptionsReceiptBalance  = "subscriptions.receipt_balance"
	MsgSubscriptionsResetAudit      = "subscriptions.reset_audit"
)

const MsgSubscriptionsReadQuery = "subscriptions.read_query"

// Async-task content endpoints.
const (
	MsgTasksQueryTaskFailed               = "tasks.query_task_failed"
	MsgTasksTaskNotFound                  = "tasks.task_not_found"
	MsgTasksArtifactNotFound              = "tasks.artifact_not_found"
	MsgTasksArtifactPreviewUnavailable    = "tasks.artifact_preview_unavailable"
	MsgTasksArtifactPreviewFailed         = "tasks.artifact_preview_failed"
	MsgTasksArtifactFetchFailed           = "tasks.artifact_fetch_failed"
	MsgTasksArtifactsNotReady             = "tasks.artifacts_not_ready"
	MsgTasksArtifactStorageUnavailable    = "tasks.artifact_storage_unavailable"
	MsgTasksArtifactURLFailed             = "tasks.artifact_url_failed"
	MsgTasksArtifactContentUnavailable    = "tasks.artifact_content_unavailable"
	MsgTasksArtifactContentFailed         = "tasks.artifact_content_failed"
	MsgTasksArtifactGone                  = "tasks.artifact_gone"
	MsgTasksArtifactRequestMissing        = "tasks.artifact_request_missing"
	MsgTasksArtifactRequestRejected       = "tasks.artifact_request_rejected"
	MsgTasksArtifactDecodeFailed          = "tasks.artifact_decode_failed"
	MsgTasksArtifactLoopRejected          = "tasks.artifact_loop_rejected"
	MsgTasksArtifactMethodRejected        = "tasks.artifact_method_rejected"
	MsgTasksArtifactBodyRejected          = "tasks.artifact_body_rejected"
	MsgTasksArtifactPublicRequestRejected = "tasks.artifact_public_request_rejected"
	MsgTasksArtifactChannelUnavailable    = "tasks.artifact_channel_unavailable"
	MsgTasksArtifactProxyClientFailed     = "tasks.artifact_proxy_client_failed"
	MsgTasksArtifactRequestFailed         = "tasks.artifact_request_failed"
	MsgTasksArtifactHeadersRejected       = "tasks.artifact_headers_rejected"
	MsgTasksArtifactRedirectRejected      = "tasks.artifact_redirect_rejected"
	MsgTasksArtifactTimeout               = "tasks.artifact_timeout"
	MsgTasksArtifactAuthenticationFailed  = "tasks.artifact_authentication_failed"
	MsgTasksArtifactBusy                  = "tasks.artifact_busy"
	MsgTasksTaskIDRequired                = "tasks.task_id_required"
	MsgTasksTaskNotCompleted              = "tasks.task_not_completed"
	MsgTasksArtifactUpstreamStatus        = "tasks.artifact_upstream_status"
)

// Persisted async-task messages use the recipient language.
const (
	MsgTasksAction            = "tasks.log_action"
	MsgTasksPerCall           = "tasks.log_per_call"
	MsgTasksParameters        = "tasks.log_parameters"
	MsgTasksMJPrice           = "tasks.mj_price"
	MsgTasksMJPriceID         = "tasks.mj_price_id"
	MsgTasksTieredSettlement  = "tasks.tiered_settlement"
	MsgTasksAdaptorAdjustment = "tasks.adaptor_adjustment"
	MsgTasksTokenSettlement   = "tasks.token_settlement"
	MsgTasksTimeout           = "tasks.timeout"
	MsgTasksLegacyTimeout     = "tasks.legacy_timeout"
	MsgTasksMJTimeout         = "tasks.mj_timeout"
	MsgTasksMJFailure         = "tasks.mj_failure"
)

// Batch response and recipient log messages.
const (
	MsgBatchBatchBusy                           = "batch.batch_busy"
	MsgBatchBatchNotReconciled                  = "batch.batch_not_reconciled"
	MsgBatchBatchQuotaOverflow                  = "batch.batch_quota_overflow"
	MsgBatchBatchTooLarge                       = "batch.batch_too_large"
	MsgBatchBatchWorkerUnavailable              = "batch.batch_worker_unavailable"
	MsgBatchChannelSnapshotUnavailable          = "batch.channel_snapshot_unavailable"
	MsgBatchChannelUnavailable                  = "batch.channel_unavailable"
	MsgBatchIdempotencyConflict                 = "batch.idempotency_conflict"
	MsgBatchInvalidCursor                       = "batch.invalid_cursor"
	MsgBatchInvalidCustomId                     = "batch.invalid_custom_id"
	MsgBatchInvalidEnvelope                     = "batch.invalid_envelope"
	MsgBatchInvalidIdempotencyKey               = "batch.invalid_idempotency_key"
	MsgBatchInvalidLimit                        = "batch.invalid_limit"
	MsgBatchInvalidPurchaseCost                 = "batch.invalid_purchase_cost"
	MsgBatchInvalidPurchaseEvidence             = "batch.invalid_purchase_evidence"
	MsgBatchInvalidRecoveryIdentity             = "batch.invalid_recovery_identity"
	MsgBatchMalformedEnvelope                   = "batch.malformed_envelope"
	MsgBatchNotFound                            = "batch.not_found"
	MsgBatchPricingUnavailable                  = "batch.pricing_unavailable"
	MsgBatchPurchaseReconciliationConflict      = "batch.purchase_reconciliation_conflict"
	MsgBatchRecoveryUnverified                  = "batch.recovery_unverified"
	MsgBatchResultsExpired                      = "batch.results_expired"
	MsgBatchResultsPending                      = "batch.results_pending"
	MsgBatchStorageCancellationUnavailable      = "batch.storage_cancellation_unavailable"
	MsgBatchStorageDeletionUnavailable          = "batch.storage_deletion_unavailable"
	MsgBatchStorageListUnavailable              = "batch.storage_list_unavailable"
	MsgBatchStorageLookupUnavailable            = "batch.storage_lookup_unavailable"
	MsgBatchStorageReservationUnavailable       = "batch.storage_reservation_unavailable"
	MsgBatchSubmissionPending                   = "batch.submission_pending"
	MsgBatchInvalidItem                         = "batch.invalid_item"
	MsgBatchItemBodyInvalid                     = "batch.item_body_invalid"
	MsgBatchItemModelMismatch                   = "batch.item_model_mismatch"
	MsgBatchItemStreamingUnsupported            = "batch.item_streaming_unsupported"
	MsgBatchItemCountInvalid                    = "batch.item_count_invalid"
	MsgBatchOutputLimitUnsupported              = "batch.output_limit_unsupported"
	MsgBatchOutputLimitOutOfRange               = "batch.output_limit_out_of_range"
	MsgBatchUseGenerationConfig                 = "batch.use_generation_config"
	MsgBatchGenerationConfigInvalid             = "batch.generation_config_invalid"
	MsgBatchUseMaxOutputTokens                  = "batch.use_max_output_tokens"
	MsgBatchUseCandidateCount                   = "batch.use_candidate_count"
	MsgBatchMaxOutputTokensInvalid              = "batch.max_output_tokens_invalid"
	MsgBatchCandidateCountInvalid               = "batch.candidate_count_invalid"
	MsgBatchOutputLimitRequired                 = "batch.output_limit_required"
	MsgBatchBillingExpressionRequired           = "batch.billing_expression_required"
	MsgBatchBillingExpressionHeadersUnsupported = "batch.billing_expression_headers_unsupported"
	MsgBatchBillingExpressionInvalid            = "batch.billing_expression_invalid"
	MsgBatchOpenRouterAdaptorUnsupported        = "batch.open_router_adaptor_unsupported"
	MsgBatchInsufficientQuota                   = "batch.insufficient_quota"
	MsgBatchUnsupportedOperation                = "batch.unsupported_operation"
	MsgBatchFailed                              = "batch.failed"
	MsgBatchCreationRejected                    = "batch.creation_rejected"
	MsgBatchRequestLog                          = "batch.request_log"
	MsgBatchAggregateLog                        = "batch.aggregate_log"
)

// Task-plugin management and protocol lookup messages.
const (
	MsgTaskPluginSourceTooLarge      = "taskplugins.source_too_large"
	MsgTaskPluginSourceHash          = "taskplugins.source_hash"
	MsgTaskPluginPluginNotFound      = "taskplugins.not_found"
	MsgTaskPluginVersionNotFound     = "taskplugins.version_not_found"
	MsgTaskPluginDeleteFactory       = "taskplugins.delete_factory"
	MsgTaskPluginMarketplaceName     = "taskplugins.marketplace_name"
	MsgTaskPluginMarketplaceURL      = "taskplugins.marketplace_url"
	MsgTaskPluginSourceConflict      = "taskplugins.source_conflict"
	MsgTaskPluginInUse               = "taskplugins.in_use"
	MsgTaskPluginEnabledRequired     = "taskplugins.enabled_required"
	MsgTaskPluginResponseNotFound    = "taskplugins.response_not_found"
	MsgTaskPluginOriginInvalid       = "taskplugins.origin_invalid"
	MsgTaskPluginOriginMissing       = "taskplugins.origin_missing"
	MsgTaskPluginOriginPlatform      = "taskplugins.origin_platform"
	MsgTaskPluginOriginChannel       = "taskplugins.origin_channel"
	MsgTaskPluginOriginDisabled      = "taskplugins.origin_disabled"
	MsgTaskPluginProtocolFailed      = "taskplugins.protocol_failed"
	MsgTaskPluginProtocolUnavailable = "taskplugins.protocol_unavailable"
	MsgTaskPluginObservationsLimit   = "taskplugins.observations_limit"
	MsgTaskPluginAuthentication      = "taskplugins.authentication"
)

// General settings editor messages.
const (
	MsgSettingsInvalidParameters    = "settings.invalid_parameters"
	MsgSettingsComplianceField      = "settings.compliance_field"
	MsgSettingsRetiredTheme         = "settings.retired_theme"
	MsgSettingsGitHub               = "settings.github"
	MsgSettingsDiscord              = "settings.discord"
	MsgSettingsOIDC                 = "settings.oidc"
	MsgSettingsLinuxDO              = "settings.linuxdo"
	MsgSettingsEmailDomain          = "settings.email_domain"
	MsgSettingsWeChat               = "settings.wechat"
	MsgSettingsTurnstile            = "settings.turnstile"
	MsgSettingsTelegram             = "settings.telegram"
	MsgSettingsImageRatio           = "settings.image_ratio"
	MsgSettingsAudioRatio           = "settings.audio_ratio"
	MsgSettingsAudioCompletionRatio = "settings.audio_completion_ratio"
	MsgSettingsCreateCacheRatio     = "settings.create_cache_ratio"
	MsgSettingsBatchObject          = "settings.batch_object"
	MsgSettingsBatchExpression      = "settings.batch_expression"
	MsgSettingsExpressionObject     = "settings.expression_object"
	MsgSettingsModelExpression      = "settings.model_expression"
)

// System setup and maintenance messages.
const (
	MsgSystemSetupComplete       = "system.setup_complete"
	MsgSystemInvalidSetup        = "system.invalid_setup"
	MsgSystemUsernameLength      = "system.username_length"
	MsgSystemPasswordMismatch    = "system.password_mismatch"
	MsgSystemSystemError         = "system.system_error"
	MsgSystemCreateRoot          = "system.create_root"
	MsgSystemSaveSelfMode        = "system.save_self_mode"
	MsgSystemSaveDemoMode        = "system.save_demo_mode"
	MsgSystemInitialize          = "system.initialize"
	MsgSystemInitialized         = "system.initialized"
	MsgSystemDatabaseFailed      = "system.database_failed"
	MsgSystemRunning             = "system.running"
	MsgSystemTargetRequired      = "system.target_required"
	MsgSystemTypeRequired        = "system.type_required"
	MsgSystemTaskIDRequired      = "system.task_id_required"
	MsgSystemTaskNotFound        = "system.task_not_found"
	MsgSystemNodeRequired        = "system.node_required"
	MsgSystemInstanceNotStale    = "system.instance_not_stale"
	MsgSystemCacheCleared        = "system.cache_cleared"
	MsgSystemStatsReset          = "system.stats_reset"
	MsgSystemGCExecuted          = "system.g_c_executed"
	MsgSystemInvalidMode         = "system.invalid_mode"
	MsgSystemInvalidValue        = "system.invalid_value"
	MsgSystemLogDirectoryMissing = "system.log_directory_missing"
	MsgSystemPartialDelete       = "system.partial_delete"
	MsgSystemModelRequired       = "system.model_required"
)

// Literal response compatibility for migrated scenarios.
const (
	MsgCatalogModelNameEmpty              = "catalog.model_name_empty"
	MsgCatalogModelNameExists             = "catalog.model_name_exists"
	MsgCatalogVendorIDMissing             = "catalog.vendor_id_missing"
	MsgCatalogVendorNameEmpty             = "catalog.vendor_name_empty"
	MsgCatalogVendorNameExists            = "catalog.vendor_name_exists"
	MsgAccountsCheckinDisabled            = "accounts.checkin_disabled"
	MsgAccountsCheckinAlreadyToday        = "accounts.checkin_already_today"
	MsgAccountsCheckinFailed              = "accounts.checkin_failed"
	MsgAccountsCheckinQuotaFailed         = "accounts.checkin_quota_failed"
	MsgAccountsInviteQuotaInsufficient    = "accounts.invite_quota_insufficient"
	MsgPricingChannelQueryFailed          = "pricing.channel_query_failed"
	MsgPricingNoValidUpstream             = "pricing.no_valid_upstream"
	MsgChannelMultiKeyNotFound            = "channel.multi_key_not_found"
	MsgIdentityWeChatAccountDeleted       = "identity.wechat_account_deleted"
	MsgIdentityWeChatRegistrationDisabled = "identity.wechat_registration_disabled"
	MsgIdentityWeChatAccountBanned        = "identity.wechat_account_banned"
	MsgIdentityWeChatCodeMissing          = "identity.wechat_code_missing"
	MsgIdentityWeChatCodeInvalid          = "identity.wechat_code_invalid"
	MsgIdentityPasskeyCreateFailed        = "identity.passkey_create_failed"
	MsgIdentityPasskeyLoginAbnormal       = "identity.passkey_login_abnormal"
	MsgIdentityPasskeyAccountDisabled     = "identity.passkey_account_disabled"
	MsgIdentityPasskeyUserIDInvalid       = "identity.passkey_user_id_invalid"
	MsgIdentityTwoFANotEnabled            = "identity.twofa_not_enabled"
)

const MsgChannelModelFetchResponseFailed = "channel.model_fetch_response_failed"

const MsgChannelOllamaVersionResponseFailed = "channel.ollama_version_response_failed"

const MsgAccountsTransferQuotaMinimum = "accounts.transfer_quota_minimum"

const MsgChannelTagsResponseFailed = "channel.tags_response_failed"

const MsgChannelListResponseFailed = "channel.list_response_failed"

// Deployment and prefill management messages.
const (
	MsgDeploymentManageNotConfigured       = "deployment.management.not_configured"
	MsgDeploymentManageNameEmpty           = "deployment.management.name_empty"
	MsgDeploymentManageNameUnavailable     = "deployment.management.name_unavailable"
	MsgDeploymentManageIDRequired          = "deployment.management.deployment_id_required"
	MsgDeploymentManageContainerIDRequired = "deployment.management.container_id_required"
	MsgDeploymentManageInvalidPayload      = "deployment.management.invalid_payload"
	MsgDeploymentManageAPIKeyRequired      = "deployment.management.api_key_required"
	MsgDeploymentManageValidateKey         = "deployment.management.validate_key"
	MsgDeploymentManageTerminated          = "deployment.management.terminated"
	MsgDeploymentManageCreated             = "deployment.management.created"
	MsgDeploymentManageNameParameter       = "deployment.management.name_parameter"
	MsgDeploymentManageContainerParameter  = "deployment.management.container_parameter"
	MsgDeploymentManageHardwareParameter   = "deployment.management.hardware_parameter"
	MsgDeploymentManageInvalidHardware     = "deployment.management.invalid_hardware"
	MsgDeploymentManageContainerNotFound   = "deployment.management.container_not_found"
	MsgPrefillNameType                     = "catalog.management.prefill_name_type"
	MsgPrefillDuplicate                    = "catalog.management.prefill_duplicate"
	MsgPrefillIDRequired                   = "catalog.management.prefill_id_required"
)
