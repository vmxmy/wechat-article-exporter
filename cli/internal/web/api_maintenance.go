package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/application"
)

const maxMaintenanceCredentialFieldBytes = 16 << 10

// maintenanceRead exposes only the application maintenance facades explicitly
// injected into Options. The browser server never constructs a facade from
// runtime internals, files, or secret stores.
func (server *Server) maintenanceRead(writer http.ResponseWriter, request *http.Request) bool {
	switch request.URL.Path {
	case "/api/v1/settings/credentials":
		service := server.maintenanceService(writer)
		if service == nil {
			return true
		}
		value, err := service.Credentials(request.Context())
		if err != nil {
			server.maintenanceError(writer, err)
			return true
		}
		writeAPI(writer, http.StatusOK, value)
	case "/api/v1/settings/proxies":
		service := server.maintenanceService(writer)
		if service == nil {
			return true
		}
		value, err := service.Proxies(request.Context())
		if err != nil {
			server.maintenanceError(writer, err)
			return true
		}
		writeAPI(writer, http.StatusOK, value)
	case "/api/v1/settings/preferences":
		service := server.maintenanceService(writer)
		if service == nil {
			return true
		}
		value, err := service.Preferences(request.Context())
		if err != nil {
			server.maintenanceError(writer, err)
			return true
		}
		writeAPI(writer, http.StatusOK, value)
	case "/api/v1/maintenance/integrity":
		service := server.storageMaintenanceService(writer)
		if service == nil {
			return true
		}
		value, err := service.CheckIntegrity(request.Context())
		if err != nil {
			server.maintenanceError(writer, err)
			return true
		}
		writeAPI(writer, http.StatusOK, value)
	case "/api/v1/maintenance/diagnostics":
		service := server.storageMaintenanceService(writer)
		if service == nil {
			return true
		}
		value, err := service.Diagnostics(request.Context())
		if err != nil {
			server.maintenanceError(writer, err)
			return true
		}
		writeAPI(writer, http.StatusOK, value)
	default:
		return false
	}
	return true
}

// maintenanceControl is mutation-only and relies on apiMutation for the same
// strict authenticated session, exact loopback Origin, and CSRF validation as
// the rest of the browser control plane.
func (server *Server) maintenanceControl(writer http.ResponseWriter, request *http.Request) bool {
	if request.Method != http.MethodPost && request.Method != http.MethodPatch {
		return false
	}
	switch request.URL.Path {
	case "/api/v1/settings/credentials/validate":
		server.credentialValidate(writer, request)
	case "/api/v1/settings/credentials/import":
		server.credentialImport(writer, request)
	case "/api/v1/settings/credentials/remove":
		server.credentialRemove(writer, request)
	case "/api/v1/settings/proxies":
		server.proxyAdd(writer, request)
	case "/api/v1/settings/proxies/disclosure":
		server.proxyDisclosure(writer, request)
	case "/api/v1/settings/preferences":
		server.preferencesPatch(writer, request)
	case "/api/v1/maintenance/backups":
		server.backupCreate(writer, request)
	case "/api/v1/maintenance/backups/verify":
		server.backupVerify(writer, request)
	case "/api/v1/maintenance/gc/plan":
		server.garbageCollectionPlan(writer, request)
	case "/api/v1/maintenance/gc/apply":
		server.garbageCollectionApply(writer, request)
	default:
		path := strings.TrimPrefix(request.URL.Path, "/api/v1/settings/proxies/")
		if path == request.URL.Path {
			return false
		}
		id, action, found := strings.Cut(path, "/")
		if !found || !validMaintenanceToken(id) {
			return false
		}
		switch action {
		case "remove":
			server.proxyRemove(writer, request, id)
		case "enable":
			server.proxyEnable(writer, request, id, true)
		case "disable":
			server.proxyEnable(writer, request, id, false)
		case "test":
			server.proxyTest(writer, request, id)
		default:
			return false
		}
	}
	return true
}

func (server *Server) credentialValidate(writer http.ResponseWriter, request *http.Request) {
	if !server.maintenanceMutation(writer, request, http.MethodPost) {
		return
	}
	var input credentialImportInput
	if err := decodeControl(request, &input); err != nil || !validCredentialImport(input) {
		server.invalidMaintenanceInput(writer)
		return
	}
	value, err := server.maintenanceService(writer).ValidateCredential(request.Context(), credentialImportRequest(input))
	if err != nil {
		server.maintenanceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}

func (server *Server) credentialImport(writer http.ResponseWriter, request *http.Request) {
	if !server.maintenanceMutation(writer, request, http.MethodPost) {
		return
	}
	var input credentialImportInput
	if err := decodeControl(request, &input); err != nil || !validCredentialImport(input) {
		server.invalidMaintenanceInput(writer)
		return
	}
	value, err := server.maintenanceService(writer).ImportCredential(request.Context(), credentialImportRequest(input))
	if err != nil {
		server.maintenanceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusCreated, value)
}

func (server *Server) credentialRemove(writer http.ResponseWriter, request *http.Request) {
	if !server.maintenanceMutation(writer, request, http.MethodPost) {
		return
	}
	var input struct {
		ID           string `json:"id"`
		Confirmation string `json:"confirm"`
	}
	if err := decodeControl(request, &input); err != nil || !validMaintenanceToken(input.ID) {
		server.invalidMaintenanceInput(writer)
		return
	}
	if input.Confirmation != "remove-credential:"+input.ID {
		server.maintenanceConfirmationRequired(writer)
		return
	}
	if err := server.maintenanceService(writer).RemoveCredential(request.Context(), input.ID); err != nil {
		server.maintenanceError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) proxyAdd(writer http.ResponseWriter, request *http.Request) {
	if !server.maintenanceMutation(writer, request, http.MethodPost) {
		return
	}
	var input proxyAddInput
	if err := decodeControl(request, &input); err != nil {
		server.invalidMaintenanceInput(writer)
		return
	}
	value, err := server.maintenanceService(writer).AddProxy(request.Context(), input.request())
	if err != nil {
		server.maintenanceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusCreated, value)
}

func (server *Server) proxyDisclosure(writer http.ResponseWriter, request *http.Request) {
	if !server.maintenanceMutation(writer, request, http.MethodPost) {
		return
	}
	var input proxyAddInput
	if err := decodeControl(request, &input); err != nil {
		server.invalidMaintenanceInput(writer)
		return
	}
	value, err := server.maintenanceService(writer).ProxyDisclosure(input.request())
	if err != nil {
		server.maintenanceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}

type credentialImportInput struct {
	Nickname    string     `json:"nickname"`
	Biz         string     `json:"biz"`
	UIN         string     `json:"uin"`
	Key         string     `json:"key"`
	PassTicket  string     `json:"passTicket"`
	WapSID2     string     `json:"wapSid2"`
	AppMsgToken string     `json:"appMsgToken"`
	Cookie      string     `json:"cookie"`
	ExpiresAt   *time.Time `json:"expiresAt"`
}

func credentialImportRequest(input credentialImportInput) application.CredentialImportRequest {
	return application.CredentialImportRequest{Nickname: input.Nickname, Biz: input.Biz, UIN: input.UIN, Key: input.Key, PassTicket: input.PassTicket, WapSID2: input.WapSID2, AppMsgToken: input.AppMsgToken, Cookie: input.Cookie, ExpiresAt: timeValue(input.ExpiresAt)}
}

type proxyAddInput struct {
	Name          string                          `json:"name"`
	Endpoint      string                          `json:"endpoint"`
	Authorization string                          `json:"authorization"`
	Trust         application.ProxyTrust          `json:"trust"`
	Classes       []application.ProxyRequestClass `json:"classes"`
	Priority      int                             `json:"priority"`
	Confirmation  string                          `json:"confirm"`
}

func (input proxyAddInput) request() application.ProxyAddRequest {
	return application.ProxyAddRequest{Name: input.Name, Endpoint: input.Endpoint, Authorization: input.Authorization, Trust: input.Trust, Classes: input.Classes, Priority: input.Priority, Confirmation: input.Confirmation}
}

func (server *Server) proxyRemove(writer http.ResponseWriter, request *http.Request, id string) {
	if !server.maintenanceMutation(writer, request, http.MethodPost) {
		return
	}
	var input struct {
		Confirmation string `json:"confirm"`
	}
	if err := decodeControl(request, &input); err != nil {
		server.invalidMaintenanceInput(writer)
		return
	}
	if input.Confirmation != "remove-proxy:"+id {
		server.maintenanceConfirmationRequired(writer)
		return
	}
	value, err := server.maintenanceService(writer).RemoveProxy(request.Context(), id)
	if err != nil {
		server.maintenanceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}
func (server *Server) proxyEnable(writer http.ResponseWriter, request *http.Request, id string, enabled bool) {
	if !server.maintenanceMutation(writer, request, http.MethodPost) {
		return
	}
	var value application.ProxyRoute
	var err error
	if enabled {
		value, err = server.maintenanceService(writer).EnableProxy(request.Context(), id)
	} else {
		value, err = server.maintenanceService(writer).DisableProxy(request.Context(), id)
	}
	if err != nil {
		server.maintenanceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}
func (server *Server) proxyTest(writer http.ResponseWriter, request *http.Request, id string) {
	if !server.maintenanceMutation(writer, request, http.MethodPost) {
		return
	}
	value, err := server.maintenanceService(writer).TestProxy(request.Context(), id)
	if err != nil {
		server.maintenanceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}

func (server *Server) preferencesPatch(writer http.ResponseWriter, request *http.Request) {
	if !server.maintenanceMutation(writer, request, http.MethodPatch) {
		return
	}
	var input application.PreferencesPatch
	if err := decodeControl(request, &input); err != nil {
		server.invalidMaintenanceInput(writer)
		return
	}
	value, err := server.maintenanceService(writer).PatchPreferences(request.Context(), input)
	if err != nil {
		server.maintenanceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}
func (server *Server) backupCreate(writer http.ResponseWriter, request *http.Request) {
	if !server.storageMaintenanceMutation(writer, request, http.MethodPost) {
		return
	}
	value, err := server.storageMaintenanceService(writer).CreateBackup(request.Context())
	if err != nil {
		server.maintenanceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusCreated, value)
}
func (server *Server) backupVerify(writer http.ResponseWriter, request *http.Request) {
	if !server.storageMaintenanceMutation(writer, request, http.MethodPost) {
		return
	}
	var input struct {
		BackupID string `json:"backupId"`
	}
	if err := decodeControl(request, &input); err != nil || !validMaintenanceToken(input.BackupID) {
		server.invalidMaintenanceInput(writer)
		return
	}
	value, err := server.storageMaintenanceService(writer).VerifyBackup(request.Context(), input.BackupID)
	if err != nil {
		server.maintenanceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}
func (server *Server) garbageCollectionPlan(writer http.ResponseWriter, request *http.Request) {
	if !server.storageMaintenanceMutation(writer, request, http.MethodPost) {
		return
	}
	var input struct{}
	if err := decodeControl(request, &input); err != nil {
		server.invalidMaintenanceInput(writer)
		return
	}
	value, err := server.storageMaintenanceService(writer).PlanGarbageCollection(request.Context())
	if err != nil {
		server.maintenanceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}
func (server *Server) garbageCollectionApply(writer http.ResponseWriter, request *http.Request) {
	if !server.storageMaintenanceMutation(writer, request, http.MethodPost) {
		return
	}
	var input application.GarbageCollectionApplyRequest
	if err := decodeControl(request, &input); err != nil || !validMaintenanceToken(input.PlanID) || !validMaintenanceToken(input.Confirmation) {
		server.invalidMaintenanceInput(writer)
		return
	}
	value, err := server.storageMaintenanceService(writer).ApplyGarbageCollection(request.Context(), input)
	if err != nil {
		server.maintenanceError(writer, err)
		return
	}
	writeAPI(writer, http.StatusOK, value)
}

func (server *Server) maintenanceService(writer http.ResponseWriter) *application.MaintenanceService {
	if server.maintenance == nil {
		server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "workspace maintenance capability is not available")
		return nil
	}
	return server.maintenance
}
func (server *Server) storageMaintenanceService(writer http.ResponseWriter) *application.MaintenanceStorageService {
	if server.storageMaintenance == nil {
		server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "workspace maintenance capability is not available")
		return nil
	}
	return server.storageMaintenance
}
func (server *Server) maintenanceMutation(writer http.ResponseWriter, request *http.Request, method string) bool {
	if !server.apiMutation(writer, request, method) {
		return false
	}
	return server.maintenanceService(writer) != nil
}
func (server *Server) storageMaintenanceMutation(writer http.ResponseWriter, request *http.Request, method string) bool {
	if !server.apiMutation(writer, request, method) {
		return false
	}
	return server.storageMaintenanceService(writer) != nil
}
func (server *Server) invalidMaintenanceInput(writer http.ResponseWriter) {
	server.apiError(writer, http.StatusBadRequest, "invalid_argument", "maintenance request is invalid")
}
func (server *Server) maintenanceError(writer http.ResponseWriter, err error) {
	if errors.Is(err, application.ErrUnavailable) {
		server.apiError(writer, http.StatusServiceUnavailable, "unavailable", "workspace maintenance capability is not available")
		return
	}
	if errors.Is(err, application.ErrMaintenanceConfirmationRequired) {
		server.maintenanceConfirmationRequired(writer)
		return
	}
	server.apiError(writer, http.StatusBadRequest, "invalid_argument", "maintenance request is invalid")
}
func (server *Server) maintenanceConfirmationRequired(writer http.ResponseWriter) {
	server.apiError(writer, http.StatusBadRequest, "confirmation_required", "maintenance confirmation is invalid or expired")
}

func validMaintenanceToken(value string) bool {
	if len(value) == 0 || len(value) > 256 {
		return false
	}
	for index, character := range value {
		if index == 0 && !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9')) {
			return false
		}
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("._:-", character)) {
			return false
		}
	}
	return true
}
func validCredentialImport(input credentialImportInput) bool {
	for _, value := range []string{input.Nickname, input.Biz, input.UIN, input.Key, input.PassTicket, input.WapSID2, input.AppMsgToken, input.Cookie} {
		if len(value) > maxMaintenanceCredentialFieldBytes {
			return false
		}
	}
	return true
}
func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
