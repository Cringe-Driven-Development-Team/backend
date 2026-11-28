package domain

// ShareRole определяет права адресного читателя.
type ShareRole string

const (
	RoleViewer    ShareRole = "viewer"
	RoleCommenter ShareRole = "commenter"
)

// NotebookReader связывает пользователя с выданной ролью.
type NotebookReader struct {
	Role      ShareRole
	AvatarURL string
	ID        int64
	Login     string
}

// SharingSettings содержит настройки доступа к блокноту.
type SharingSettings struct {
	LinkToken *string
	Readers   []NotebookReader
}

// NotebookAccess содержит только возможности текущего посетителя.
type NotebookAccess struct {
	IsOwner          bool
	CanEdit          bool
	CanManageSharing bool
	CanClone         bool
	CanComment       bool
}

// AccessFor вычисляет права для операций владельца.
func AccessFor(ownerID, userID int64) NotebookAccess {
	return AccessForRole(ownerID, userID, "")
}

// AccessForRole вычисляет права владельца, адресата или посетителя по ссылке.
func AccessForRole(ownerID, userID int64, role ShareRole) NotebookAccess {
	owner := userID > 0 && userID == ownerID
	return NotebookAccess{IsOwner: owner, CanEdit: owner, CanManageSharing: owner, CanClone: owner || (userID > 0 && (role == RoleViewer || role == RoleCommenter)), CanComment: owner || (userID > 0 && role == RoleCommenter)}
}
