// Package observability предоставляет контекст запроса и безопасное структурированное логирование.
package observability

import "context"

// requestIDKey отделяет ключ контекста от ключей других пакетов.
type requestIDKey struct{}

// WithRequestID привязывает идентификатор id к контексту ctx.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID возвращает идентификатор запроса из ctx или пустую строку.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// ValidRequestID допускает от 1 до 64 ASCII-символов из общего алфавита HTTP и RPC.
func ValidRequestID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for i := range len(id) {
		c := id[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}
