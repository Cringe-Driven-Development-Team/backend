package domain

import "testing"

func TestAccessForRole(t *testing.T) {
	for _, tc := range []struct {
		role ShareRole
		user int64
		want NotebookAccess
	}{
		{"", 1, NotebookAccess{IsOwner: true, CanEdit: true, CanManageSharing: true, CanComment: true, CanClone: true}},
		{RoleViewer, 2, NotebookAccess{CanClone: true}},
		{RoleCommenter, 2, NotebookAccess{CanComment: true, CanClone: true}},
		{"", 2, NotebookAccess{}},
	} {
		if got := AccessForRole(1, tc.user, tc.role); got != tc.want {
			t.Errorf("user %d role %q: got %+v, want %+v", tc.user, tc.role, got, tc.want)
		}
	}
}
