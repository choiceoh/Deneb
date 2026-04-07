package coreprotocol

// lookupValidator returns the validator function for a given RPC method name.
func lookupValidator(method string) ValidatorFn {
	switch method {
	// Session methods
	case "sessions.list":
		return validateSessionsListParams
	case "sessions.preview":
		return validateSessionsPreviewParams
	case "sessions.resolve":
		return validateSessionsResolveParams
	case "sessions.create":
		return validateSessionsCreateParams
	case "sessions.send":
		return validateSessionsSendParams
	case "sessions.messages.subscribe":
		return validateSessionsMessagesSubscribeParams
	case "sessions.messages.unsubscribe":
		return validateSessionsMessagesUnsubscribeParams
	case "sessions.abort":
		return validateSessionsAbortParams
	case "sessions.patch":
		return validateSessionsPatchParams
	case "sessions.reset":
		return validateSessionsResetParams
	case "sessions.delete":
		return validateSessionsDeleteParams
	case "sessions.compact":
		return validateSessionsCompactParams
	case "sessions.usage":
		return validateSessionsUsageParams

	// Secrets methods
	case "secrets.resolve":
		return validateSecretsResolveParams
	case "secrets.resolve.result":
		return validateObjectParams
	case "secrets.reload":
		return validateSecretsReloadParams

	// Logs/chat methods
	case "logs.tail":
		return validateLogsTailParams
	case "chat.history":
		return validateChatHistoryParams
	case "chat.send":
		return validateChatSendParams
	case "chat.abort":
		return validateChatAbortParams
	case "chat.inject":
		return validateChatInjectParams
	case "chat.event":
		return validateObjectParams

	// Config methods
	case "config.get":
		return validateConfigGetParams
	case "config.set":
		return validateConfigSetParams
	case "config.apply":
		return validateConfigApplyParams
	case "config.patch":
		return validateConfigPatchParams
	case "config.schema":
		return validateConfigSchemaParams
	case "config.schema.lookup":
		return validateConfigSchemaLookupParams
	case "config.schema.lookup.result":
		return validateObjectParams
	case "update.run":
		return validateUpdateRunParams

	// Telegram methods
	case "telegram.status":
		return validateChannelsStatusParams
	case "telegram.logout":
		return validateChannelsLogoutParams
	case "weblogin.start":
		return validateWebLoginStartParams
	case "weblogin.wait":
		return validateWebLoginWaitParams

	// Agent methods
	case "agent.send":
		return validateSendParams
	case "agent.poll":
		return validatePollParams
	case "agent":
		return validateAgentParams
	case "agent.identity":
		return validateAgentIdentityParams
	case "agent.wait":
		return validateAgentWaitParams
	case "agent.wake":
		return validateWakeParams

	// Agents CRUD
	case "agents.list":
		return validateAgentsListParams
	case "agents.create":
		return validateAgentsCreateParams
	case "agents.update":
		return validateAgentsUpdateParams
	case "agents.delete":
		return validateAgentsDeleteParams
	case "agents.files.list":
		return validateAgentsFilesListParams
	case "agents.files.get":
		return validateAgentsFilesGetParams
	case "agents.files.set":
		return validateAgentsFilesSetParams
	case "models.list":
		return validateModelsListParams
	case "skills.status":
		return validateSkillsStatusParams
	case "skills.bins":
		return validateSkillsBinsParams
	case "skills.install":
		return validateSkillsInstallParams
	case "skills.update":
		return validateSkillsUpdateParams
	case "tools.catalog":
		return validateToolsCatalogParams

	// Cron methods
	case "cron.list":
		return validateCronListParams
	case "cron.status":
		return validateCronStatusParams
	case "cron.add":
		return validateCronAddParams
	case "cron.update":
		return validateCronUpdateParams
	case "cron.remove":
		return validateCronRemoveParams
	case "cron.run":
		return validateCronRunParams
	case "cron.runs":
		return validateCronRunsParams

	// Exec approvals
	case "exec.approvals.get":
		return validateExecApprovalsGetParams
	case "exec.approvals.set":
		return validateExecApprovalsSetParams
	case "exec.approval.request":
		return validateExecApprovalRequestParams
	case "exec.approval.resolve":
		return validateExecApprovalResolveParams
	case "exec.approvals.node.get":
		return validateExecApprovalsNodeGetParams
	case "exec.approvals.node.set":
		return validateExecApprovalsNodeSetParams

	default:
		return nil
	}
}

// validateObjectParams is a generic validator that only requires the value be a JSON object.
func validateObjectParams(value any, path string, errors *[]ValidationError) {
	RequireObject(value, path, errors)
}
