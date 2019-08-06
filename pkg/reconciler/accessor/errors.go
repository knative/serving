package accessor

import "strings"

// Error defines a type of error coming from Accessor.
type Error struct {
	err         error
	errorReason string
}

const (
	// NotOwnResource means the accessor does not own the resource.
	NotOwnResource string = "NotOwned"
)

// NewAccessorError creates a new accessor Error
func NewAccessorError(err error, reason string) Error {
	return Error{
		err:         err,
		errorReason: reason,
	}
}

func (a Error) Error() string {
	return strings.ToLower(string(a.errorReason)) + ": " + a.err.Error()
}

// IsNotOwned returns true if the error is caused by NotOwnResource.
func IsNotOwned(err error) bool {
	accessorError, ok := err.(Error)
	if !ok {
		return false
	}
	return accessorError.errorReason == NotOwnResource
}
