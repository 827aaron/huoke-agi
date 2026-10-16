package services

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"pentagi/pkg/growth"
	"pentagi/pkg/growth/connectors"
	"pentagi/pkg/server/logger"
	"pentagi/pkg/server/response"

	"github.com/gin-gonic/gin"
)

type growthSources struct {
	Sources []growth.SourceView `json:"sources"`
	Total   uint64              `json:"total"`
}

type growthAddSourcesRequest struct {
	Links []string `json:"links" binding:"required,min=1,max=200"`
}

type growthAddSourcesResult struct {
	Results []growth.AddSourceResult `json:"results"`
}

type growthLeads struct {
	Leads []growth.LeadView `json:"leads"`
	Total uint64            `json:"total"`
}

type growthRun struct {
	ID             int64  `json:"id"`
	FlowID         *int64 `json:"flow_id,omitempty"`
	TaskID         *int64 `json:"task_id,omitempty"`
	Target         int    `json:"target"`
	Status         string `json:"status"`
	DeliveredCount int    `json:"delivered_count"`
	NotifyStatus   string `json:"notify_status,omitempty"`
	NotifyError    string `json:"notify_error,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
	FinishedAt     string `json:"finished_at,omitempty"`
}

type growthRuns struct {
	Runs  []growthRun `json:"runs"`
	Total uint64      `json:"total"`
}

// GrowthService serves the customer's source pool, leads and rounds. Every row is
// owned by the authenticated user; flows and tasks are filters only.
type GrowthService struct {
	svc *growth.Service
}

func NewGrowthService(svc *growth.Service) *GrowthService {
	return &GrowthService{svc: svc}
}

// ListSources is a function to return the source pool
// @Summary Retrieve the lead source pool of the current user
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.successResp{data=growthSources} "sources received successful"
// @Failure 403 {object} response.errorResp "getting sources not permitted"
// @Failure 500 {object} response.errorResp "internal error on getting sources"
// @Router /growth/sources/ [get]
func (s *GrowthService) ListSources(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	sources, err := s.svc.ListSources(c, uid)
	if err != nil {
		logger.FromContext(c).WithError(err).Errorf("error listing growth sources")
		response.Error(c, response.ErrInternal, err)
		return
	}

	response.Success(c, http.StatusOK, growthSources{Sources: sources, Total: uint64(len(sources))})
}

// AddSources is a function to add sources to the pool by hand
// @Summary Add Telegram links or web page URLs to the source pool
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param json body growthAddSourcesRequest true "links to add"
// @Success 201 {object} response.successResp{data=growthAddSourcesResult} "sources added successful"
// @Failure 400 {object} response.errorResp "invalid request data"
// @Failure 403 {object} response.errorResp "adding sources not permitted"
// @Failure 500 {object} response.errorResp "internal error on adding sources"
// @Router /growth/sources/ [post]
func (s *GrowthService) AddSources(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	var req growthAddSourcesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.FromContext(c).WithError(err).Errorf("error binding JSON")
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	results, err := s.svc.AddSources(c, growth.Scope{UserID: uid}, req.Links, growth.OriginManual, nil)
	if err != nil {
		logger.FromContext(c).WithError(err).Errorf("error adding growth sources")
		response.Error(c, response.ErrInternal, err)
		return
	}

	response.Success(c, http.StatusCreated, growthAddSourcesResult{Results: results})
}

// DeleteSource is a function to remove a source from the pool
// @Summary Remove a source from the pool; leads and their evidence are kept
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Param sourceID path int true "source id" minimum(0)
// @Success 200 {object} response.successResp "source deleted successful"
// @Failure 400 {object} response.errorResp "invalid source id"
// @Failure 403 {object} response.errorResp "deleting source not permitted"
// @Failure 404 {object} response.errorResp "source not found"
// @Failure 500 {object} response.errorResp "internal error on deleting source"
// @Router /growth/sources/{sourceID} [delete]
func (s *GrowthService) DeleteSource(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	sourceID, err := strconv.ParseInt(c.Param("sourceID"), 10, 64)
	if err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	if err := s.svc.DeleteSource(c, uid, sourceID); err != nil {
		if errors.Is(err, growth.ErrSourceNotFound) {
			response.Error(c, response.ErrGrowthSourceNotFound, err)
			return
		}
		logger.FromContext(c).WithError(err).Errorf("error deleting growth source")
		response.Error(c, response.ErrInternal, err)
		return
	}

	response.Success(c, http.StatusOK, struct{}{})
}

// ListLeads is a function to return leads with their evidence
// @Summary Retrieve leads of the current user with evidence
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Param flow_id query int false "only leads last seen in this flow"
// @Param task_id query int false "only leads last seen in this task"
// @Param run_id query int false "only leads delivered by this round"
// @Param tier query string false "qualified or watchlist"
// @Param delivered query bool false "true for delivered leads, false for undelivered"
// @Param limit query int false "page size, default 100, at most 500"
// @Param offset query int false "rows to skip"
// @Success 200 {object} response.successResp{data=growthLeads} "leads received successful"
// @Failure 400 {object} response.errorResp "invalid query"
// @Failure 403 {object} response.errorResp "getting leads not permitted"
// @Failure 500 {object} response.errorResp "internal error on getting leads"
// @Router /growth/leads/ [get]
func (s *GrowthService) ListLeads(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	filter, err := leadFilter(c)
	if err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	leads, err := s.svc.ListLeads(c, uid, filter)
	if err != nil {
		logger.FromContext(c).WithError(err).Errorf("error listing leads")
		response.Error(c, response.ErrInternal, err)
		return
	}

	response.Success(c, http.StatusOK, growthLeads{Leads: leads, Total: uint64(len(leads))})
}

// GetLead is a function to return one lead
// @Summary Retrieve one lead with its evidence
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Param leadID path int true "lead id" minimum(0)
// @Success 200 {object} response.successResp{data=growth.LeadView} "lead received successful"
// @Failure 400 {object} response.errorResp "invalid lead id"
// @Failure 403 {object} response.errorResp "getting lead not permitted"
// @Failure 404 {object} response.errorResp "lead not found"
// @Failure 500 {object} response.errorResp "internal error on getting lead"
// @Router /growth/leads/{leadID} [get]
func (s *GrowthService) GetLead(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	leadID, err := strconv.ParseInt(c.Param("leadID"), 10, 64)
	if err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	lead, err := s.svc.GetLead(c, uid, leadID)
	if errors.Is(err, sql.ErrNoRows) {
		response.Error(c, response.ErrGrowthLeadNotFound, err)
		return
	}
	if err != nil {
		logger.FromContext(c).WithError(err).Errorf("error getting lead")
		response.Error(c, response.ErrInternal, err)
		return
	}

	response.Success(c, http.StatusOK, lead)
}

// ListRuns is a function to return lead collection rounds
// @Summary Retrieve the lead collection rounds of the current user
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Param flow_id query int false "only rounds of this flow"
// @Success 200 {object} response.successResp{data=growthRuns} "rounds received successful"
// @Failure 400 {object} response.errorResp "invalid query"
// @Failure 403 {object} response.errorResp "getting rounds not permitted"
// @Failure 500 {object} response.errorResp "internal error on getting rounds"
// @Router /growth/runs/ [get]
func (s *GrowthService) ListRuns(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	flowID, err := optionalInt64(c, "flow_id")
	if err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	runs, err := s.svc.ListRuns(c, uid, flowID)
	if err != nil {
		logger.FromContext(c).WithError(err).Errorf("error listing growth rounds")
		response.Error(c, response.ErrInternal, err)
		return
	}

	out := make([]growthRun, 0, len(runs))
	for _, r := range runs {
		run := growthRun{
			ID:             r.ID,
			Target:         int(r.Target),
			Status:         r.Status,
			DeliveredCount: int(r.DeliveredCount),
			NotifyStatus:   r.NotifyStatus,
			NotifyError:    r.NotifyError,
		}
		if r.FlowID.Valid {
			run.FlowID = &r.FlowID.Int64
		}
		if r.TaskID.Valid {
			run.TaskID = &r.TaskID.Int64
		}
		if r.CreatedAt.Valid {
			run.CreatedAt = r.CreatedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		if r.FinishedAt.Valid {
			run.FinishedAt = r.FinishedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		out = append(out, run)
	}

	response.Success(c, http.StatusOK, growthRuns{Runs: out, Total: uint64(len(out))})
}

func leadFilter(c *gin.Context) (growth.LeadFilter, error) {
	var (
		filter growth.LeadFilter
		err    error
	)
	if filter.FlowID, err = optionalInt64(c, "flow_id"); err != nil {
		return filter, err
	}
	if filter.TaskID, err = optionalInt64(c, "task_id"); err != nil {
		return filter, err
	}
	if filter.RunID, err = optionalInt64(c, "run_id"); err != nil {
		return filter, err
	}
	switch tier := c.Query("tier"); tier {
	case "", growth.TierQualified, growth.TierWatchlist:
		filter.Tier = tier
	default:
		return filter, errors.New("tier must be qualified or watchlist")
	}
	if raw := c.Query("delivered"); raw != "" {
		delivered, err := strconv.ParseBool(raw)
		if err != nil {
			return filter, errors.New("delivered must be true or false")
		}
		filter.Delivered = &delivered
	}
	if filter.Limit, err = optionalInt(c, "limit"); err != nil {
		return filter, err
	}
	if filter.Offset, err = optionalInt(c, "offset"); err != nil {
		return filter, err
	}
	return filter, nil
}

func optionalInt64(c *gin.Context, name string) (*int64, error) {
	raw := c.Query(name)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, errors.New(name + " must be an integer")
	}
	return &v, nil
}

func optionalInt(c *gin.Context, name string) (int, error) {
	v, err := optionalInt64(c, name)
	if err != nil || v == nil {
		return 0, err
	}
	return int(*v), nil
}

// exportPageSize is the store's largest page; the export walks pages until one comes back short.
const exportPageSize = 500

// ExportLeads is a function to download leads as CSV
// @Summary Download leads as CSV, one row per lead with its first evidence
// @Tags Growth
// @Produce text/csv
// @Security BearerAuth
// @Param flow_id query int false "only leads last seen in this flow"
// @Param task_id query int false "only leads last seen in this task"
// @Param run_id query int false "only leads delivered by this round"
// @Param tier query string false "qualified or watchlist"
// @Param delivered query bool false "true for delivered leads, false for undelivered"
// @Success 200 {file} file "leads exported successful"
// @Failure 400 {object} response.errorResp "invalid query"
// @Failure 403 {object} response.errorResp "exporting leads not permitted"
// @Failure 500 {object} response.errorResp "internal error on exporting leads"
// @Router /growth/leads/export [get]
func (s *GrowthService) ExportLeads(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	filter, err := leadFilter(c)
	if err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	var all []growth.LeadView
	filter.Limit, filter.Offset = exportPageSize, 0
	for {
		page, err := s.svc.ListLeads(c, uid, filter)
		if err != nil {
			logger.FromContext(c).WithError(err).Errorf("error exporting leads")
			response.Error(c, response.ErrInternal, err)
			return
		}
		all = append(all, page...)
		if len(page) < exportPageSize {
			break
		}
		filter.Offset += exportPageSize
	}

	c.Header("Content-Disposition", `attachment; filename="leads.csv"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", growth.LeadsCSV(all))
}

type growthAccounts struct {
	Accounts []growth.AccountView `json:"accounts"`
	Total    uint64               `json:"total"`
}

// ListAccounts is a function to return platform accounts with their health
// @Summary Retrieve the platform accounts of the current user with rotation health
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.successResp{data=growthAccounts} "accounts received successful"
// @Failure 403 {object} response.errorResp "getting accounts not permitted"
// @Failure 500 {object} response.errorResp "internal error on getting accounts"
// @Router /growth/accounts/ [get]
func (s *GrowthService) ListAccounts(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	accounts, err := s.svc.ListAccounts(c, uid)
	if err != nil {
		logger.FromContext(c).WithError(err).Errorf("error listing growth accounts")
		response.Error(c, response.ErrInternal, err)
		return
	}

	response.Success(c, http.StatusOK, growthAccounts{Accounts: accounts, Total: uint64(len(accounts))})
}

type growthCreateAccountRequest struct {
	Platform string `json:"platform" binding:"required"`
	Kind     string `json:"kind,omitempty"`
	Label    string `json:"label,omitempty"`
	growth.AccountUpdate
}

func accountError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, growth.ErrAccountNotFound):
		response.Error(c, response.ErrGrowthAccountNotFound, err)
	case errors.Is(err, growth.ErrInvalidSetting):
		response.Error(c, response.ErrGrowthInvalidRequest, err)
	case errors.Is(err, growth.ErrNoSecretKey):
		response.Error(c, response.ErrGrowthNoSecretKey, err)
	default:
		logger.FromContext(c).WithError(err).Errorf("error handling growth account")
		response.Error(c, response.ErrInternal, err)
	}
}

// CreateAccount is a function to add a platform account
// @Summary Add a platform account that a login or a browser profile attaches to
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param json body growthCreateAccountRequest true "platform, kind, label and network identity"
// @Success 201 {object} response.successResp{data=growth.AccountView} "account created successful"
// @Failure 400 {object} response.errorResp "invalid account"
// @Failure 403 {object} response.errorResp "creating accounts not permitted"
// @Failure 503 {object} response.errorResp "GROWTH_SECRET_KEY is not set"
// @Failure 500 {object} response.errorResp "internal error on creating account"
// @Router /growth/accounts/ [post]
func (s *GrowthService) CreateAccount(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	var req growthCreateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	view, err := s.svc.CreateAccount(c, uid, growth.AccountSpec{
		Platform: connectors.Platform(req.Platform),
		Kind:     req.Kind,
		Label:    req.Label,
	}, req.AccountUpdate)
	if err != nil {
		accountError(c, err)
		return
	}

	response.Success(c, http.StatusCreated, view)
}

// UpdateAccount is a function to change a platform account
// @Summary Change the label, status (active or disabled), proxy, fingerprint or external browser of an account
// @Description An absent field keeps its value and an empty string clears it; a value still masked with •••• keeps the stored one.
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param accountID path int true "account id"
// @Param json body growth.AccountUpdate true "fields to change"
// @Success 200 {object} response.successResp{data=growth.AccountView} "account updated successful"
// @Failure 400 {object} response.errorResp "invalid account change"
// @Failure 403 {object} response.errorResp "updating accounts not permitted"
// @Failure 404 {object} response.errorResp "account not found"
// @Failure 503 {object} response.errorResp "GROWTH_SECRET_KEY is not set"
// @Failure 500 {object} response.errorResp "internal error on updating account"
// @Router /growth/accounts/{accountID} [put]
func (s *GrowthService) UpdateAccount(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	accountID, err := strconv.ParseInt(c.Param("accountID"), 10, 64)
	if err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}
	var req growth.AccountUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	view, err := s.svc.UpdateAccount(c, uid, accountID, req)
	if err != nil {
		accountError(c, err)
		return
	}

	response.Success(c, http.StatusOK, view)
}

// TestAccountProxy is a function to check the route of an account
// @Summary Reach the internet the way the account's traffic goes and report the latency or the error
// @Description source is account, global or direct; ok false carries the error.
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Param accountID path int true "account id"
// @Success 200 {object} response.successResp{data=growth.ProxyCheck} "proxy checked successful"
// @Failure 403 {object} response.errorResp "testing accounts not permitted"
// @Failure 404 {object} response.errorResp "account not found"
// @Failure 500 {object} response.errorResp "internal error on testing proxy"
// @Router /growth/accounts/{accountID}/test-proxy [post]
func (s *GrowthService) TestAccountProxy(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	accountID, err := strconv.ParseInt(c.Param("accountID"), 10, 64)
	if err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	check, err := s.svc.TestAccountProxy(c, uid, accountID)
	if err != nil {
		accountError(c, err)
		return
	}

	response.Success(c, http.StatusOK, check)
}

type growthSettings struct {
	Settings []growth.SettingView `json:"settings"`
}

type growthSaveSettingRequest struct {
	Provider string            `json:"provider,omitempty"`
	Fields   map[string]string `json:"fields" binding:"required"`
}

type growthTestEmailRequest struct {
	To string `json:"to,omitempty"`
}

type growthTestEmailResult struct {
	Sent bool     `json:"sent"`
	To   []string `json:"to"`
}

func settingError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, growth.ErrInvalidSetting):
		response.Error(c, response.ErrGrowthInvalidRequest, err)
	case errors.Is(err, growth.ErrNoSecretKey):
		response.Error(c, response.ErrGrowthNoSecretKey, err)
	case errors.Is(err, growth.ErrSettingNotStored):
		response.Error(c, response.ErrGrowthSettingNotFound, err)
	default:
		logger.FromContext(c).WithError(err).Errorf("error handling growth setting")
		response.Error(c, response.ErrInternal, err)
	}
}

// ListSettings is a function to return the credentials of the current user, masked
// @Summary Retrieve the credentials of the current user with secrets masked
// @Description Every kind is listed; source says whether its value comes from the database, .env or nowhere.
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.successResp{data=growthSettings} "settings received successful"
// @Failure 403 {object} response.errorResp "getting settings not permitted"
// @Failure 500 {object} response.errorResp "internal error on getting settings"
// @Router /growth/settings/ [get]
func (s *GrowthService) ListSettings(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	settings, err := s.svc.ListSettings(c, uid)
	if err != nil {
		settingError(c, err)
		return
	}

	response.Success(c, http.StatusOK, growthSettings{Settings: settings})
}

// SaveSetting is a function to store one kind of credential, encrypted
// @Summary Save the credential of one kind; an empty field keeps the saved value
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param kind path string true "llm, telegram, youtube, web_search, email or proxy"
// @Param json body growthSaveSettingRequest true "provider and fields"
// @Success 200 {object} response.successResp{data=growth.SettingView} "setting saved successful"
// @Failure 400 {object} response.errorResp "invalid setting"
// @Failure 403 {object} response.errorResp "saving settings not permitted"
// @Failure 503 {object} response.errorResp "GROWTH_SECRET_KEY is not set"
// @Failure 500 {object} response.errorResp "internal error on saving setting"
// @Router /growth/settings/{kind} [put]
func (s *GrowthService) SaveSetting(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	var req growthSaveSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	view, err := s.svc.SaveSetting(c, uid, c.Param("kind"), req.Provider, req.Fields)
	if err != nil {
		settingError(c, err)
		return
	}

	response.Success(c, http.StatusOK, view)
}

// DeleteSetting is a function to remove a saved credential
// @Summary Remove the saved credential of one kind, so .env applies again
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Param kind path string true "llm, telegram, youtube, web_search, email or proxy"
// @Success 200 {object} response.successResp "setting deleted successful"
// @Failure 403 {object} response.errorResp "deleting settings not permitted"
// @Failure 404 {object} response.errorResp "no saved setting of this kind"
// @Failure 500 {object} response.errorResp "internal error on deleting setting"
// @Router /growth/settings/{kind} [delete]
func (s *GrowthService) DeleteSetting(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	if err := s.svc.DeleteSetting(c, uid, c.Param("kind")); err != nil {
		settingError(c, err)
		return
	}

	response.Success(c, http.StatusOK, struct{}{})
}

// TestSetting is a function to check a credential against its service
// @Summary Check the credential of one kind against its service and record the verdict
// @Description status is connected or invalid; untested means the service has no check without a login.
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Param kind path string true "llm, telegram, youtube, web_search, email or proxy"
// @Success 200 {object} response.successResp{data=growth.SettingView} "setting tested successful"
// @Failure 400 {object} response.errorResp "unknown kind"
// @Failure 403 {object} response.errorResp "testing settings not permitted"
// @Failure 500 {object} response.errorResp "internal error on testing setting"
// @Router /growth/settings/{kind}/test [post]
func (s *GrowthService) TestSetting(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	view, err := s.svc.TestSetting(c, uid, c.Param("kind"))
	if err != nil {
		settingError(c, err)
		return
	}

	response.Success(c, http.StatusOK, view)
}

// SendTestEmail is a function to send a test message with the email setting
// @Summary Send a test message through the configured SMTP server
// @Description Without to, it goes to the notify_emails of the email setting, or else to the login address.
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param json body growthTestEmailRequest false "recipients, comma separated"
// @Success 200 {object} response.successResp{data=growthTestEmailResult} "test email sent successful"
// @Failure 400 {object} response.errorResp "invalid recipients"
// @Failure 403 {object} response.errorResp "sending email not permitted"
// @Failure 502 {object} response.errorResp "the SMTP server refused or could not be reached"
// @Router /growth/settings/test-email [post]
func (s *GrowthService) SendTestEmail(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	var req growthTestEmailRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Error(c, response.ErrGrowthInvalidRequest, err)
			return
		}
	}

	to, err := s.svc.SendTestEmail(c, uid, req.To)
	if err != nil {
		if errors.Is(err, growth.ErrInvalidSetting) {
			response.Error(c, response.ErrGrowthInvalidRequest, err)
			return
		}
		response.Error(c, response.ErrGrowthEmailFailed, err)
		return
	}

	response.Success(c, http.StatusOK, growthTestEmailResult{Sent: true, To: to})
}
