package domain

// ErrorCode is a stable error identifier safe to expose to the frontend.
type ErrorCode string

const (
	ErrScanFailed         ErrorCode = "SCAN_FAILED"
	ErrCommandTimeout     ErrorCode = "COMMAND_TIMEOUT"
	ErrCommandFailed      ErrorCode = "COMMAND_FAILED"
	ErrUnsupportedSystem  ErrorCode = "UNSUPPORTED_SYSTEM"
	ErrInvalidResult      ErrorCode = "INVALID_RESULT"
	ErrInvalidInput       ErrorCode = "INVALID_INPUT"
	ErrProxyProbeFailed   ErrorCode = "PROXY_PROBE_FAILED"
	ErrInstallUnavailable ErrorCode = "INSTALL_UNAVAILABLE"
	ErrInstallPlanFailed  ErrorCode = "INSTALL_PLAN_FAILED"
	ErrInstallTaskFailed  ErrorCode = "INSTALL_TASK_FAILED"
	ErrProfileFailed      ErrorCode = "PROFILE_FAILED"
	ErrHistoryFailed      ErrorCode = "HISTORY_FAILED"
	ErrRemovalPlanFailed  ErrorCode = "REMOVAL_PLAN_FAILED"
	ErrRemovalTaskFailed  ErrorCode = "REMOVAL_TASK_FAILED"
	ErrRemovalInUse       ErrorCode = "REMOVAL_IN_USE"
	ErrUpdateFailed       ErrorCode = "UPDATE_FAILED"
	ErrUpdateInProgress   ErrorCode = "UPDATE_IN_PROGRESS"
)

// PublicError exposes a stable code and safe message while retaining its cause
// for backend logs and error handling.
type PublicError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	cause   error
}

// NewPublicError constructs a frontend-safe error from a public message and
// optional backend-only cause.
func NewPublicError(code ErrorCode, message string, cause error) *PublicError {
	return &PublicError{
		Code:    code,
		Message: message,
		cause:   cause,
	}
}

// Error returns only the safe code and public message.
func (e *PublicError) Error() string {
	return string(e.Code) + ": " + e.Message
}

// Unwrap returns the backend-only cause for logs and error handling.
func (e *PublicError) Unwrap() error {
	return e.cause
}
