package onlineconf

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestUpdateValidation проверяет отклонение запроса до обращения к зависимостям.
func TestUpdateValidation(t *testing.T) {
	h := newKeysHandler(nil, nil, nil, nil)
	for _, body := range []string{
		`{"description":"","enabled":true,"rollout_percent":101,"include_user_ids":[],"exclude_user_ids":[]}`,
		`{"description":"","enabled":true,"rollout_percent":25,"include_user_ids":[42],"exclude_user_ids":[42]}`,
		`{"description":""}`,
	} {
		r := httptest.NewRequest("PUT", "/admin/keys/frontend.test", strings.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("body %s: status %d", body, w.Code)
		}
	}
}
