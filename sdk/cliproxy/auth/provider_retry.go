package auth

func shouldRetrySameCredential(auth *Auth, err error, used int) bool {
	if auth == nil || err == nil {
		return false
	}
	if isRequestInvalidError(err) || isRequestStopError(err) || isRequestTerminatedError(err) {
		return false
	}
	count := auth.ProviderRetryCount()
	if count <= 0 || used >= count {
		return false
	}
	return auth.IsProviderRetryableStatus(statusCodeFromError(err))
}
