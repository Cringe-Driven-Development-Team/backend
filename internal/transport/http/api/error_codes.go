// Package api содержит сгенерированные типы и HTTP-интерфейсы контракта Apidog.
package api

// Стабильные имена кодов для прикладного кода. Генератор добавляет префикс типа,
// когда одинаковые значения встречаются и в Error, и в NotebookEditingError.
const (
	CsrfInvalid        = ErrorCodeCsrfInvalid
	Internal           = ErrorCodeInternal
	InvalidCredentials = ErrorCodeInvalidCredentials
	LoginTaken         = ErrorCodeLoginTaken
	NotFound           = ErrorCodeNotFound
	NotImplemented     = ErrorCodeNotImplemented
	Unauthorized       = ErrorCodeUnauthorized
	ValidationError    = ErrorCodeValidationError
)
