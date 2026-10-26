package security

type TokenParser interface {
	Parse(token string) (int64, error)
}
