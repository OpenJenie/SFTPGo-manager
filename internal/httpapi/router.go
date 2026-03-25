package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	httpSwagger "github.com/swaggo/http-swagger/v2"

	"sftpgo-manager/internal/domain"
	"sftpgo-manager/internal/service"
)

// Router wires HTTP handlers to application services.
type Router struct {
	bootstrap *service.BootstrapService
	auth      *service.AuthService
	tenants   *service.TenantService
	external  *service.ExternalAuthService
	uploads   *service.UploadService
}

// New constructs the API router.
func New(
	bootstrap *service.BootstrapService,
	auth *service.AuthService,
	tenants *service.TenantService,
	external *service.ExternalAuthService,
	uploads *service.UploadService,
) http.Handler {
	r := &Router{
		bootstrap: bootstrap,
		auth:      auth,
		tenants:   tenants,
		external:  external,
		uploads:   uploads,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/keys", r.handleBootstrapAPIKey)
	mux.HandleFunc("/api/auth/hook", r.handleExternalAuthHook)
	mux.HandleFunc("/api/events/upload", r.handleUploadHook)
	mux.HandleFunc("/api/tenants", r.requireAPIKey(func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodPost:
			r.handleCreateTenant(w, req)
		case http.MethodGet:
			r.handleListTenants(w, req)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}))
	mux.HandleFunc("/api/tenants/", r.requireAPIKey(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/validate"):
			r.handleValidateTenant(w, req)
		case strings.HasSuffix(req.URL.Path, "/keys"):
			r.handleUpdateTenantKeys(w, req)
		case strings.HasSuffix(req.URL.Path, "/records"):
			r.handleListTenantRecords(w, req)
		default:
			switch req.Method {
			case http.MethodGet:
				r.handleGetTenant(w, req)
			case http.MethodDelete:
				r.handleDeleteTenant(w, req)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			}
		}
	}))
	mux.HandleFunc("/swagger/", httpSwagger.WrapHandler)
	return mux
}

func (r *Router) handleBootstrapAPIKey(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	_ = json.NewDecoder(req.Body).Decode(&body)

	key, meta, err := r.bootstrap.BootstrapAPIKey(req.Context(), body.Label, req.Header.Get("X-Bootstrap-Token"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrBootstrapDisabled):
			writeError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, service.ErrBootstrapForbidden):
			writeError(w, http.StatusForbidden, err.Error())
		case errors.Is(err, service.ErrBootstrapAlreadyCompleted):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         meta.ID,
		"label":      meta.Label,
		"created_at": meta.CreatedAt,
		"key":        key,
	})
}

func (r *Router) handleCreateTenant(w http.ResponseWriter, req *http.Request) {
	var body struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		PublicKey string `json:"public_key"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	tenant, err := r.tenants.CreateTenant(req.Context(), domain.CreateTenantInput{
		Username:  body.Username,
		Password:  body.Password,
		PublicKey: body.PublicKey,
	})
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case strings.Contains(err.Error(), "required"), strings.Contains(err.Error(), "exists"):
			status = http.StatusBadRequest
		case strings.Contains(err.Error(), "sftpgo"):
			status = http.StatusBadGateway
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, tenant)
}

func (r *Router) handleListTenants(w http.ResponseWriter, req *http.Request) {
	tenants, err := r.tenants.ListTenants(req.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tenants == nil {
		tenants = []domain.Tenant{}
	}
	writeJSON(w, http.StatusOK, tenants)
}

func (r *Router) handleGetTenant(w http.ResponseWriter, req *http.Request) {
	id, err := parseID(req.URL.Path, "/api/tenants/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	tenant, err := r.tenants.GetTenant(req.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "tenant not found")
		return
	}
	writeJSON(w, http.StatusOK, tenant)
}

func (r *Router) handleDeleteTenant(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id, err := parseID(req.URL.Path, "/api/tenants/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := r.tenants.DeleteTenant(req.Context(), id); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrTenantNotFound) {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "sftpgo") {
			status = http.StatusBadGateway
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (r *Router) handleValidateTenant(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id, err := parseID(strings.TrimSuffix(req.URL.Path, "/validate"), "/api/tenants/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	result, err := r.tenants.ValidateTenant(req.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "tenant not found")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (r *Router) handleUpdateTenantKeys(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id, err := parseID(strings.TrimSuffix(req.URL.Path, "/keys"), "/api/tenants/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		PublicKey string `json:"public_key"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := r.tenants.UpdateTenantPublicKey(req.Context(), id, body.PublicKey); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrTenantNotFound) {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "required") {
			status = http.StatusBadRequest
		} else if strings.Contains(err.Error(), "sftpgo") {
			status = http.StatusBadGateway
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (r *Router) handleListTenantRecords(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id, err := parseID(strings.TrimSuffix(req.URL.Path, "/records"), "/api/tenants/")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	records, err := r.tenants.ListTenantRecords(req.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "tenant not found")
		return
	}
	if records == nil {
		records = []domain.Record{}
	}
	writeJSON(w, http.StatusOK, records)
}

func (r *Router) handleExternalAuthHook(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body domain.ExternalAuthRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	user, err := r.external.Authenticate(req.Context(), body)
	if err != nil {
		http.Error(w, "", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (r *Router) handleUploadHook(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var event domain.UploadEvent
	if err := json.NewDecoder(req.Body).Decode(&event); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if r.uploads != nil {
		if err := r.uploads.ProcessUploadEvent(context.Background(), event); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (r *Router) requireAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		authHeader := req.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing api key")
			return
		}
		key := strings.TrimPrefix(authHeader, "Bearer ")
		if err := r.auth.ValidateAPIKey(req.Context(), key); err != nil {
			writeError(w, http.StatusUnauthorized, "invalid api key")
			return
		}
		next(w, req)
	}
}

func parseID(path, prefix string) (int64, error) {
	raw := strings.TrimPrefix(path, prefix)
	raw = strings.Split(raw, "/")[0]
	return strconv.ParseInt(raw, 10, 64)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write json: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
