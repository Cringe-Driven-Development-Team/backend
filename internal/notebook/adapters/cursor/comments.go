package cursor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/notebook/usecase"
)

// CommentCodec подписывает курсоры с отдельным назначением ключа.
type CommentCodec struct{ key []byte }

// commentPayload содержит границу и полную привязку стены.
type commentPayload struct {
	Version    int       `json:"v"`
	NotebookID int64     `json:"notebook_id"`
	CellID     string    `json:"cell_id"`
	CreatedAt  time.Time `json:"created_at"`
	ID         int64     `json:"id"`
}

// NewCommentCodec выводит ключ комментариев из секрета приложения.
func NewCommentCodec(secret []byte) *CommentCodec {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("cell-comments-cursor:v1"))
	return &CommentCodec{key: mac.Sum(nil)}
}

// signature вычисляет HMAC сериализованной границы.
func (c *CommentCodec) signature(data []byte) []byte {
	mac := hmac.New(sha256.New, c.key)
	mac.Write(data)
	return mac.Sum(nil)
}

// Encode связывает позицию с конкретной стеной без привязки к посетителю.
func (c *CommentCodec) Encode(notebookID int64, cellID string, position usecase.CommentPosition) (string, error) {
	if usecase.ValidateCommentWall(notebookID, cellID) != nil || position.ID <= 0 || position.CreatedAt.IsZero() || position.CreatedAt.Nanosecond()%1000 != 0 {
		return "", usecase.ErrInvalidCommentList
	}
	data, err := json.Marshal(commentPayload{1, notebookID, cellID, position.CreatedAt.UTC(), position.ID})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data) + "." + base64.RawURLEncoding.EncodeToString(c.signature(data)), nil
}

// Decode проверяет подпись, версию, границу и оба идентификатора стены.
func (c *CommentCodec) Decode(token string, notebookID int64, cellID string) (usecase.CommentPosition, error) {
	invalid := usecase.ErrInvalidCommentList
	if len(token) > 2048 || strings.ContainsAny(token, "\r\n") {
		return usecase.CommentPosition{}, invalid
	}
	raw, sig, ok := strings.Cut(token, ".")
	if !ok {
		return usecase.CommentPosition{}, invalid
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil {
		return usecase.CommentPosition{}, invalid
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(sig)
	if err != nil || !hmac.Equal(signature, c.signature(data)) {
		return usecase.CommentPosition{}, invalid
	}
	var p commentPayload
	if err = json.Unmarshal(data, &p); err != nil || p.Version != 1 || p.NotebookID != notebookID || p.CellID != cellID || usecase.ValidateCommentWall(p.NotebookID, p.CellID) != nil || p.ID <= 0 || p.CreatedAt.IsZero() || p.CreatedAt.Nanosecond()%1000 != 0 {
		return usecase.CommentPosition{}, invalid
	}
	return usecase.CommentPosition{CreatedAt: p.CreatedAt.UTC(), ID: p.ID}, nil
}
