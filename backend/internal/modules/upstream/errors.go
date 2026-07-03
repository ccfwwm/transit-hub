package upstream

type RequestError struct {
	MessageKey string
	Platform   Platform
}

func (e *RequestError) Error() string {
	return e.MessageKey
}

func newRequestError(messageKey string, platform Platform) *RequestError {
	return &RequestError{MessageKey: messageKey, Platform: platform}
}

func errorKey(err error) string {
	if requestErr, ok := err.(*RequestError); ok {
		return requestErr.MessageKey
	}
	return ErrorUnknown
}

