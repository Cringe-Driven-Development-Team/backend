package httptransport

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/api"
)

func TestLegacyMissingRefreshCookieRemainsUnauthorized(t *testing.T) {
	handler, signer, _, _, _ := csrfRouter(t)
	for _, operation := range []string{"refresh", "logout"} {
		req := csrfRequest(http.MethodPost, BaseURL+"/auth/"+operation, signer.Anonymous(), "", "")
		rec := serveCSRF(handler, req)
		var response api.Error
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusUnauthorized || response.Code != api.Unauthorized {
			t.Fatalf("%s: status=%d code=%s", operation, rec.Code, response.Code)
		}
	}
}
