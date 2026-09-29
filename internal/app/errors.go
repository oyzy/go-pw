package app

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Error carries a machine readable code next to the user facing message, so the
// frontend can react to specific failures (for example by focusing the password field
// when the master key is wrong).
type Error struct {
	Code    string
	Message string
}

// Error implements the error interface.
func (e Error) Error() string { return e.Message }

// Errorf builds a coded error.
func Errorf(code, format string, args ...any) error {
	return Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ErrorCode returns the code of a coded error, or an empty string.
func ErrorCode(err error) string {
	var coded Error
	if errors.As(err, &coded) {
		return coded.Code
	}
	return ""
}

// MarshalError is handed to Wails so that Go error messages actually reach the
// frontend. Wails' default marshaller serialises the error value itself
// (json.Marshal(&err)), which yields "{}" for the usual error types and leaves the user
// interface with nothing to display but "[object Object]".
func MarshalError(err error) []byte {
	if err == nil {
		return []byte("null")
	}
	payload := map[string]string{"message": err.Error()}
	if code := ErrorCode(err); code != "" {
		payload["code"] = code
	}
	data, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return []byte(`{"message":"未知错误"}`)
	}
	return data
}

// Error codes used by the services.
const (
	CodeWrongKey     = "wrong-key"
	CodeNoFile       = "file-not-found"
	CodeWrongKeyFile = "wrong-key-file"
	CodeNoVault      = "no-database"
	CodeConflict     = "conflict"
	CodeEmptyField   = "empty-field"
)
