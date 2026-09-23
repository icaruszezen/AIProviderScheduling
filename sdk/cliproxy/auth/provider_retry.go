package auth

import (
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func shouldRetrySameCredential(auth *Auth, err error, used int) bool {
	if auth == nil || err == nil {
		return false
	}
	if isStreamFirstTokenTimeout(err) || isRequestStopError(err) || isRequestTerminatedError(err) {
		return false
	}
	count := auth.ProviderRetryCount()
	if count <= 0 || used >= count {
		return false
	}
	return auth.IsProviderRetryableStatus(statusCodeFromError(err))
}

// providerRetryOverridesHardcodedStop reports whether the operator listed this
// error's HTTP status in provider-retry-status-codes. Configured codes take
// priority over hardcoded request-fault and compact-fault classifications.
func providerRetryOverridesHardcodedStop(auth *Auth, err error) bool {
	if auth == nil || err == nil {
		return false
	}
	return auth.IsProviderRetryableStatus(statusCodeFromError(err))
}

func shouldStopOnRequestInvalid(auth *Auth, opts cliproxyexecutor.Options, err error) bool {
	if channelGroupStatusListed(opts, err) {
		return false
	}
	if !isRequestInvalidError(err) {
		return false
	}
	return !providerRetryOverridesHardcodedStop(auth, err)
}

func shouldStopOnRequestOrCompactFault(auth *Auth, opts cliproxyexecutor.Options, err error) bool {
	if channelGroupStatusListed(opts, err) {
		return false
	}
	if providerRetryOverridesHardcodedStop(auth, err) {
		return false
	}
	return isResponsesCompactRequestFaultError(opts, err) || isRequestInvalidError(err)
}
