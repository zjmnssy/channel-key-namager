package i18n

// Message keys of i18n/locales/*.yaml, for text the backend translates itself:
// errors returned to AI clients, emails and notifications. Web console messages
// are not listed here; the web console translates them (see common.Message).

// Errors returned to AI clients
const (
	MsgDatabaseError          = "common.database_error"
	MsgAuthUserBanned         = "auth.user_banned"
	MsgTokenGetInfoFailed     = "token.get_info_failed"
	MsgTokenInvalid           = "token.invalid"
	MsgTokenNotProvided       = "token.not_provided"
	MsgTokenStatusUnavailable = "token.status_unavailable"

	MsgAuthClientIPUnresolved     = "auth.client_ip_unresolved"
	MsgAuthTokenIPNotAllowed      = "auth.token_ip_not_allowed"
	MsgAuthTokenGroupForbidden    = "auth.token_group_forbidden"
	MsgAuthTokenGroupDeprecated   = "auth.token_group_deprecated"
	MsgAuthChannelSelectForbidden = "auth.channel_select_forbidden"

	MsgRateLimitReached      = "rate_limit.reached"
	MsgRateLimitTotalReached = "rate_limit.total_reached"

	MsgDistributorInvalidRequest               = "distributor.invalid_request"
	MsgDistributorInvalidChannelId             = "distributor.invalid_channel_id"
	MsgDistributorChannelDisabled              = "distributor.channel_disabled"
	MsgDistributorTokenNoModelAccess           = "distributor.token_no_model_access"
	MsgDistributorTokenModelForbidden          = "distributor.token_model_forbidden"
	MsgDistributorModelNameRequired            = "distributor.model_name_required"
	MsgDistributorInvalidPlayground            = "distributor.invalid_playground_request"
	MsgDistributorGroupAccessDenied            = "distributor.group_access_denied"
	MsgDistributorGetChannelFailed             = "distributor.get_channel_failed"
	MsgDistributorNoAvailableChannel           = "distributor.no_available_channel"
	MsgDistributorNoAvailableChannelTaskPlugin = "distributor.no_available_channel_task_plugin"
	MsgDistributorInvalidParseModel            = "distributor.invalid_request_parse_model"

	MsgQuotaUserInsufficient             = "quota.user_insufficient"
	MsgQuotaPreConsumeInsufficient       = "quota.pre_consume_insufficient"
	MsgQuotaSubscriptionInsufficient     = "quota.subscription_insufficient"
	MsgRelayModelPriceNotConfigured      = "relay.model_price_not_configured"
	MsgRelayModelPriceNotConfiguredAdmin = "relay.model_price_not_configured_admin"
	MsgRelayTaskUsagePriceUnsupported    = "relay.task_usage_price_unsupported"
	MsgRelaySharedTaskPriceNeedsOverride = "relay.shared_task_price_needs_override"
	MsgRelayTaskPriceNotConfigured       = "relay.task_price_not_configured"
	MsgRelayTaskChannelDisabled          = "relay.task_channel_disabled"
	MsgRelayGroupSaturated               = "relay.group_saturated"
	MsgRelayGroupUpstreamSaturated       = "relay.group_upstream_saturated"
	MsgRelayRetryGetChannelFailed        = "relay.retry_get_channel_failed"
	MsgRelayRetryNoAvailableChannel      = "relay.retry_no_available_channel"
	MsgRelayAccessTokenUnsupported       = "relay.access_token_unsupported"
	MsgRelayRequestRejected              = "relay.request_rejected"
	MsgRelayRequestPluginFailed          = "relay.request_plugin_failed"
	MsgRelayResponseRejected             = "relay.response_rejected"
)

// Emails and the name of the token created at registration, in the
// recipient's language
const (
	MsgUserDefaultTokenName = "user.default_token_name"

	MsgEmailVerificationSubject  = "email.verification.subject"
	MsgEmailVerificationGreeting = "email.verification.greeting"
	MsgEmailVerificationCode     = "email.verification.code"
	MsgEmailVerificationValidity = "email.verification.validity"

	MsgEmailPasswordResetSubject      = "email.password_reset.subject"
	MsgEmailPasswordResetGreeting     = "email.password_reset.greeting"
	MsgEmailPasswordResetLink         = "email.password_reset.link"
	MsgEmailPasswordResetLinkFallback = "email.password_reset.link_fallback"
	MsgEmailPasswordResetValidity     = "email.password_reset.validity"

	MsgEmailBindingSubject     = "email.binding.subject"
	MsgEmailBindingNewIntro    = "email.binding.new_intro"
	MsgEmailBindingOldIntro    = "email.binding.old_intro"
	MsgEmailBindingCode        = "email.binding.code"
	MsgEmailBindingNewValidity = "email.binding.new_validity"
	MsgEmailBindingOldValidity = "email.binding.old_validity"

	MsgEmailSecurityNoticeSubject = "email.security_notice.subject"
	MsgEmailSecurityNoticeChanged = "email.security_notice.changed"
	MsgEmailSecurityNoticeAdvice  = "email.security_notice.advice"

	MsgSecurityEventPasswordUpdated      = "email.security_event.password_updated"
	MsgSecurityEventEmailChangeRequested = "email.security_event.email_change_requested"
	MsgSecurityEventEmailChanged         = "email.security_event.email_changed"
	MsgSecurityEventEmailConfirmed       = "email.security_event.email_confirmed"
	MsgSecurityEventLoginLinked          = "email.security_event.login_linked"
	MsgSecurityEventLoginUnlinked        = "email.security_event.login_unlinked"
	MsgSecurityEventWeChatLinked         = "email.security_event.wechat_linked"
)

// Low-quota notices, in the recipient's saved language
const (
	MsgNotifyQuotaLowTitle             = "notify.quota_low_title"
	MsgNotifySubscriptionQuotaLowTitle = "notify.subscription_quota_low_title"
	MsgNotifyQuotaLowBark              = "notify.quota_low_bark"
	MsgNotifyQuotaLowGotify            = "notify.quota_low_gotify"
	MsgNotifyQuotaLowEmail             = "notify.quota_low_email"
)

// Channel notices sent to administrators who receive them and to model update
// watchers
const (
	MsgChannelNotifyDisabledSubject  = "channel.notify_disabled_subject"
	MsgChannelNotifyDisabledContent  = "channel.notify_disabled_content"
	MsgChannelNotifyEnabledSubject   = "channel.notify_enabled_subject"
	MsgChannelNotifyEnabledContent   = "channel.notify_enabled_content"
	MsgChannelNotifyScheduledSubject = "channel.notify_scheduled_subject"
	MsgChannelTestCompletedSubject   = "channel.test_completed_subject"
	MsgChannelTestCompletedContent   = "channel.test_completed_content"
	MsgChannelResponseTimeExceeded   = "channel.response_time_exceeded"

	MsgChannelUpstreamUpdateNotifySubject   = "channel.upstream_update_notify_subject"
	MsgChannelUpstreamUpdateSummary         = "channel.upstream_update_summary"
	MsgChannelUpstreamUpdateChangedChannels = "channel.upstream_update_changed_channels"
	MsgChannelUpstreamUpdateMoreChannels    = "channel.upstream_update_more_channels"
	MsgChannelUpstreamUpdateAddedModels     = "channel.upstream_update_added_models"
	MsgChannelUpstreamUpdateRemovedModels   = "channel.upstream_update_removed_models"
	MsgChannelUpstreamUpdateFailedChannels  = "channel.upstream_update_failed_channels"
	MsgChannelUpstreamUpdateMoreOmitted     = "channel.upstream_update_more_omitted"
)

// Receivable bill export: the header and the status and failure texts written into the
// downloaded xlsx, which reaches the reader without passing through the web console
const (
	MsgReceivableBillExportBillDate         = "receivable_bill.export.bill_date"
	MsgReceivableBillExportSiteName         = "receivable_bill.export.site_name"
	MsgReceivableBillExportAmount           = "receivable_bill.export.amount"
	MsgReceivableBillExportDiscount         = "receivable_bill.export.receivable_discount"
	MsgReceivableBillExportReceivableAmount = "receivable_bill.export.receivable_amount"
	MsgReceivableBillExportStatus           = "receivable_bill.export.status"
	MsgReceivableBillExportFailureReason    = "receivable_bill.export.failure_reason"
	MsgReceivableBillExportSyncedAt         = "receivable_bill.export.synced_at"
	MsgReceivableBillStatusSuccess          = "receivable_bill.status.success"
	MsgReceivableBillStatusRetrying         = "receivable_bill.status.retrying"
	MsgReceivableBillStatusFailed           = "receivable_bill.status.failed"
	MsgReceivableBillErrorRequestFailed     = "receivable_bill.error.request_failed"
	MsgReceivableBillErrorNoValidQuota      = "receivable_bill.error.no_valid_quota"
	MsgReceivableBillErrorSiteDeleted       = "receivable_bill.error.site_deleted"
)

// Payable bill export: the header and the texts written into the downloaded xlsx files
const (
	MsgPayableBillExportTaskID         = "payable_bill.export.task_id"
	MsgPayableBillExportBillDate       = "payable_bill.export.bill_date"
	MsgPayableBillExportSiteID         = "payable_bill.export.site_id"
	MsgPayableBillExportSiteName       = "payable_bill.export.site_name"
	MsgPayableBillExportDeltaAmount    = "payable_bill.export.delta_amount"
	MsgPayableBillExportUsedAmount     = "payable_bill.export.used_amount"
	MsgPayableBillExportDeltaNote      = "payable_bill.export.delta_note"
	MsgPayableBillExportProvider       = "payable_bill.export.provider"
	MsgPayableBillExportProviderSource = "payable_bill.export.provider_source"
	MsgPayableBillExportSyncedAt       = "payable_bill.export.synced_at"
	MsgPayableBillExportCounted        = "payable_bill.export.counted"
	MsgPayableBillExportAmount         = "payable_bill.export.amount"
	MsgPayableBillExportDiscount       = "payable_bill.export.discount"
	MsgPayableBillExportPayableAmount  = "payable_bill.export.payable_amount"
	MsgPayableBillExportNote           = "payable_bill.export.note"
	MsgPayableBillDeltaFirstSync       = "payable_bill.delta.first_sync"
	MsgPayableBillDeltaNewChannel      = "payable_bill.delta.new_channel"
	MsgPayableBillDeltaRollback        = "payable_bill.delta.rollback"
	MsgPayableBillSourceManual         = "payable_bill.source.manual"
	MsgPayableBillSourceParsed         = "payable_bill.source.parsed"
	MsgPayableBillSourceInherited      = "payable_bill.source.inherited"
	MsgPayableBillSourceUnknown        = "payable_bill.source.unknown"
	MsgPayableBillSourceRule           = "payable_bill.source.rule"
	MsgPayableBillDiscountMixed        = "payable_bill.discount.mixed"
	MsgPayableBillDiscountMissing      = "payable_bill.discount.missing"
	MsgPayableBillPayableIncomplete    = "payable_bill.payable.incomplete"
	MsgPayableBillProviderUnknown      = "payable_bill.provider.unknown"
	MsgPayableBillCountedYes           = "payable_bill.counted.yes"
	MsgPayableBillCountedNo            = "payable_bill.counted.no"

	// Channel usage export: the platform channel in the site channel's push marker, and the site channel as pulled
	MsgPayableBillExportLocalChannelID          = "payable_bill.export.local_channel_id"
	MsgPayableBillExportLocalChannelName        = "payable_bill.export.local_channel_name"
	MsgPayableBillExportRemoteChannelID         = "payable_bill.export.remote_channel_id"
	MsgPayableBillExportRemoteChannelName       = "payable_bill.export.remote_channel_name"
	MsgPayableBillExportRemoteChannelStatus     = "payable_bill.export.remote_channel_status"
	MsgPayableBillExportRemoteChannelStatusType = "payable_bill.export.remote_channel_status_type"
	MsgPayableBillExportRemoteChannelGroup      = "payable_bill.export.remote_channel_group"
	MsgPayableBillLocalChannelMissing           = "payable_bill.local_channel.missing"
	MsgPayableBillChannelEnabled                = "payable_bill.channel_status.enabled"
	MsgPayableBillChannelDisabled               = "payable_bill.channel_status.disabled"
	MsgPayableBillChannelStatusUnknown          = "payable_bill.channel_status.unknown"
	MsgPayableBillChannelManuallyDisabled       = "payable_bill.channel_status_type.manually_disabled"
	MsgPayableBillChannelAutoDisabled           = "payable_bill.channel_status_type.auto_disabled"
)
