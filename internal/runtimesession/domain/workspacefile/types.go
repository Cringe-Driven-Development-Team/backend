// Package workspacefile содержит типизированные команды и данные файловой подсистемы.
package workspacefile

import (
	"io"

	"github.com/google/uuid"
)

const (
	ChunkBytes   = 1 << 20
	MaxFileBytes = 3 << 30
)

// Target задаёт проверенную сессию, её файловое поколение и клиента watch.
type Target struct {
	Session uuid.UUID
	Socket  string
}

// Command описывает один запрос управления файлами. Kind определяет допустимые поля.
type Command struct {
	ProtocolVersion int      `json:"protocol_version"`
	Kind            string   `json:"type"`
	RequestID       string   `json:"request_id"`
	SessionID       string   `json:"session_id"`
	Generation      string   `json:"workspace_generation"`
	OperationID     string   `json:"operation_id,omitempty"`
	TransferID      string   `json:"transfer_id,omitempty"`
	Path            string   `json:"path,omitempty"`
	Name            string   `json:"name,omitempty"`
	Version         string   `json:"version,omitempty"`
	Recursive       bool     `json:"recursive,omitempty"`
	Size            *uint64  `json:"size_bytes,omitempty"`
	Hash            *string  `json:"sha256,omitempty"`
	Limit           uint32   `json:"limit,omitempty"`
	Cursor          *string  `json:"cursor,omitempty"`
	Paths           []string `json:"paths,omitempty"`
	After           *uint64  `json:"after,omitempty"`
}

// Error сохраняет код файлового протокола и безопасные подсказки для клиента.
type Error struct {
	Code        string `json:"code"`
	Message     string `json:"message,omitempty"`
	Retryable   bool   `json:"retryable"`
	Resync      bool   `json:"resync_required"`
	OperationID string `json:"operation_id,omitempty"`
	TransferID  string `json:"transfer_id,omitempty"`
}

// Error возвращает стабильный код, который можно передать на HTTP-границу.
func (e *Error) Error() string { return e.Code }

// Entry описывает одно имя в согласованной странице директории.
type Entry struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	Type       string   `json:"type"`
	Size       *uint64  `json:"size_bytes"`
	ModifiedAt string   `json:"modified_at"`
	Version    string   `json:"version"`
	Actions    []string `json:"actions"`
}

// Quota содержит байтовые пределы файловой подсистемы.
type Quota struct {
	Capacity  uint64 `json:"capacity_bytes"`
	Used      uint64 `json:"used_bytes"`
	Available uint64 `json:"available_bytes"`
	Source    string `json:"source"`
}

// Directory содержит страницу директории и курсор её ревизии.
type Directory struct {
	Path       string  `json:"path"`
	Items      []Entry `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Revision   string  `json:"revision"`
	Generation string  `json:"workspace_generation"`
	Quota      Quota   `json:"quota"`
	Seq        uint64  `json:"seq"`
}

// Transfer описывает долговечное состояние загрузки или скачивания.
type Transfer struct {
	ID          string  `json:"transfer_id"`
	Direction   string  `json:"direction"`
	Path        string  `json:"path"`
	Size        uint64  `json:"size_bytes"`
	Offset      uint64  `json:"offset"`
	State       string  `json:"state"`
	Generation  string  `json:"workspace_generation"`
	Hash        *string `json:"sha256"`
	Version     *string `json:"version"`
	ChunkBytes  uint32  `json:"chunk_bytes"`
	CreditBytes uint32  `json:"credit_bytes"`
	Updated     float64 `json:"updated"`
}

// Mutation содержит подтверждение изменения наблюдаемой директории.
type Mutation struct {
	OperationID  string `json:"operation_id,omitempty"`
	Revision     string `json:"revision,omitempty"`
	Resync       bool   `json:"resync_required,omitempty"`
	ResultingSeq uint64 `json:"resulting_seq,omitempty"`
}

// Event описывает одно изменение файлов либо сигнал восстановления подписки.
type Event struct {
	ProtocolVersion int       `json:"protocol_version"`
	SessionID       string    `json:"session_id"`
	Generation      string    `json:"workspace_generation"`
	Seq             uint64    `json:"seq"`
	Type            string    `json:"type"`
	Path            string    `json:"path,omitempty"`
	Directory       string    `json:"directory,omitempty"`
	Revision        string    `json:"revision,omitempty"`
	OldPath         string    `json:"old_path,omitempty"`
	NewPath         string    `json:"new_path,omitempty"`
	Entry           *Entry    `json:"entry,omitempty"`
	Quota           *Quota    `json:"quota,omitempty"`
	Transfer        *Transfer `json:"transfer,omitempty"`
	TransferID      string    `json:"transfer_id,omitempty"`
	Code            string    `json:"code,omitempty"`
	OperationID     string    `json:"operation_id,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	Synchronization string    `json:"synchronization,omitempty"`
	Resync          bool      `json:"resync_required,omitempty"`
}

// Replay описывает целую подтверждённую страницу событий.
type Replay struct {
	Events []Event `json:"events"`
	Seq    uint64  `json:"seq"`
}

// Snapshot обозначает наблюдаемую ревизию директории.
type Snapshot struct {
	Path       string `json:"path"`
	Revision   string `json:"revision"`
	Generation string `json:"workspace_generation"`
}

// Watch содержит ревизии директорий и курсор подписки.
type Watch struct {
	Replay
	Snapshots       []Snapshot `json:"snapshots"`
	Synchronization string     `json:"synchronization"`
}

// Result является результатом одной команды с явной прикладной ошибкой.
type Result struct {
	Directory *Directory
	Transfer  *Transfer
	Mutation  *Mutation
	Watch     *Watch
	Error     *Error
}

// Range представляет один HTTP byte range. Пустые границы обозначают невалидный запрос диапазона.
type Range struct {
	Start, End, Suffix *uint64
}

// Download возвращает подтверждённые метаданные и поток, которым владеет вызывающий.
type Download struct {
	Offset, Length, Total uint64
	Version, Filename     string
	Partial               bool
	Body                  io.ReadCloser
}

// InvalidRange возвращает размер, необходимый для HTTP Content-Range: bytes */size.
type InvalidRange struct{ Total uint64 }

// Error возвращает код неразрешимого диапазона.
func (e *InvalidRange) Error() string { return "invalid_range" }
