package onlineconf

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"

	identityv1 "github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/gen/cellestial/identity/v1"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/productconfig"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/httperr"
	"github.com/go-park-mail-ru/2026_2_Cringe_Driven_Development/internal/transport/http/security"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// productHandler читает обе настройки из одного снимка для подтверждённого пользователя.
func productHandler(sessions identityv1.SessionClient, read func(context.Context) (ProductView, error), bffKey, workspaceKey string) http.Handler {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		token := security.BearerToken(r.Header.Values("Authorization"))
		if token == "" {
			unauthenticated(w)
			return
		}
		session, err := sessions.ValidateSession(r.Context(), &identityv1.ValidateSessionRequest{AccessToken: token})
		switch status.Code(err) {
		case codes.Unauthenticated, codes.InvalidArgument:
			unauthenticated(w)
			return
		}
		if err != nil || session.GetUserId() <= 0 {
			httperr.Write(w, http.StatusServiceUnavailable, "dependency_unavailable", "authorization unavailable")
			return
		}
		view, err := read(r.Context())
		result, ok := effectiveSettings(view, session.GetUserId())
		if err != nil || !ok {
			httperr.Write(w, http.StatusServiceUnavailable, "onlineconf_unavailable", "onlineconf snapshot unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})
	bff := security.S2S(bffKey, productconfig.Path)(handler)
	allowed := sha256.Sum256([]byte(workspaceKey))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values(productconfig.WorkspaceKeyHeader)
		if workspaceKey != "" && len(values) == 1 && values[0] != "" {
			hash := sha256.Sum256([]byte(values[0]))
			if subtle.ConstantTimeCompare(hash[:], allowed[:]) == 1 {
				handler.ServeHTTP(w, r)
				return
			}
		}
		bff.ServeHTTP(w, r)
	})
}

// effectiveSettings применяет значения по умолчанию только к свежему снимку.
func effectiveSettings(view ProductView, userID int64) (productconfig.Settings, bool) {
	enabled, state := view.Flag("workspace.notebook_run_all", userID)
	if state == Unavailable {
		return productconfig.Settings{}, false
	}
	limit := 1000
	entry, stateLimit := view.Lookup("bff.notebook_max_cells")
	var value int
	if stateLimit == Found && entry.Type == "config" &&
		json.Unmarshal(entry.Data["value"], &value) == nil && value >= 1 && value <= 1000 {
		limit = value
	}
	return productconfig.Settings{MaxCells: limit, RunAll: enabled || state == Missing}, true
}
