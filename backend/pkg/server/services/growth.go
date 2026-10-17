package services

import (
	"database/sql"
	"errors"
	"io"
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
	// Warnings names the accounts nearing Telegram's cap on joined groups; joins go on.
	Warnings []growth.JoinWarning `json:"warnings"`
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
	DeadlineAt     string `json:"deadline_at,omitempty"`
	ScheduleID     *int64 `json:"schedule_id,omitempty"`
}

type growthSchedules struct {
	Schedules []growth.ScheduleView `json:"schedules"`
	Total     uint64                `json:"total"`
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

	warnings, err := s.svc.JoinWarnings(c, uid)
	if err != nil {
		logger.FromContext(c).WithError(err).Errorf("error counting joined growth sources")
		response.Error(c, response.ErrInternal, err)
		return
	}

	response.Success(c, http.StatusOK, growthSources{Sources: sources, Total: uint64(len(sources)), Warnings: warnings})
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
		if r.DeadlineAt.Valid {
			run.DeadlineAt = r.DeadlineAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		if r.ScheduleID.Valid {
			run.ScheduleID = &r.ScheduleID.Int64
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
	var flowID int64
	if filter.FlowID != nil {
		flowID = *filter.FlowID
	}
	c.Data(http.StatusOK, "text/csv; charset=utf-8", growth.LeadsCSV(all, s.svc.Language(c, uid, flowID)))
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
	case errors.Is(err, growth.ErrLoginNotFound):
		response.Error(c, response.ErrGrowthLoginNotFound, err)
	case errors.Is(err, growth.ErrLoginUnavailable):
		response.Error(c, response.ErrGrowthLoginUnavailable, err)
	case errors.Is(err, growth.ErrLoginIncomplete):
		response.Error(c, response.ErrGrowthLoginIncomplete, err)
	case errors.Is(err, connectors.ErrBadCode):
		response.Error(c, response.ErrGrowthLoginRejected, err)
	case errors.As(err, new(*connectors.FloodWaitError)):
		response.Error(c, response.ErrGrowthRateLimited, err)
	case errors.Is(err, connectors.ErrUnreachable):
		response.Error(c, response.ErrGrowthUnreachable, err)
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
// @Param json body growthProxyRequest false "a proxy to try before saving it; without one the account's own route is tested"
// @Success 200 {object} response.successResp{data=growth.ProxyCheck} "proxy checked successful"
// @Failure 400 {object} response.errorResp "invalid proxy"
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

	var req growthProxyRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Error(c, response.ErrGrowthInvalidRequest, err)
			return
		}
	}
	candidate := ""
	if req.Proxy != nil {
		candidate = *req.Proxy
	}

	check, err := s.svc.TestAccountProxy(c, uid, accountID, candidate)
	if err != nil {
		accountError(c, err)
		return
	}

	response.Success(c, http.StatusOK, check)
}

type growthProxyRequest struct {
	Proxy *string `json:"proxy"`
}

func accountIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("accountID"), 10, 64)
	if err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return 0, false
	}
	return id, true
}

// EnableAccount is a function to return an account to work
// @Summary Return a disabled account to work: active when it was signed in before, needs_login otherwise
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Param accountID path int true "account id"
// @Success 200 {object} response.successResp{data=growth.AccountView} "account enabled successful"
// @Failure 403 {object} response.errorResp "changing accounts not permitted"
// @Failure 404 {object} response.errorResp "account not found"
// @Failure 500 {object} response.errorResp "internal error on enabling account"
// @Router /growth/accounts/{accountID}/enable [post]
func (s *GrowthService) EnableAccount(c *gin.Context) {
	s.setAccountEnabled(c, true)
}

// DisableAccount is a function to keep an account out of work
// @Summary Keep an account and its session but never pick it for work
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Param accountID path int true "account id"
// @Success 200 {object} response.successResp{data=growth.AccountView} "account disabled successful"
// @Failure 403 {object} response.errorResp "changing accounts not permitted"
// @Failure 404 {object} response.errorResp "account not found"
// @Failure 500 {object} response.errorResp "internal error on disabling account"
// @Router /growth/accounts/{accountID}/disable [post]
func (s *GrowthService) DisableAccount(c *gin.Context) {
	s.setAccountEnabled(c, false)
}

func (s *GrowthService) setAccountEnabled(c *gin.Context, enabled bool) {
	uid := int64(c.GetUint64("uid"))
	accountID, ok := accountIDParam(c)
	if !ok {
		return
	}

	view, err := s.svc.SetAccountEnabled(c, uid, accountID, enabled)
	if err != nil {
		accountError(c, err)
		return
	}

	response.Success(c, http.StatusOK, view)
}

// SetAccountProxy is a function to set the proxy of one account
// @Summary Set the account's own proxy; null or empty falls back to the default proxy, else a direct connection
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param accountID path int true "account id"
// @Param json body growthProxyRequest true "http, https, socks5 or socks5h URL, optionally with user:pass@"
// @Success 200 {object} response.successResp{data=growth.AccountView} "proxy saved successful"
// @Failure 400 {object} response.errorResp "invalid proxy"
// @Failure 403 {object} response.errorResp "changing accounts not permitted"
// @Failure 404 {object} response.errorResp "account not found"
// @Failure 503 {object} response.errorResp "GROWTH_SECRET_KEY is not set"
// @Router /growth/accounts/{accountID}/proxy [put]
func (s *GrowthService) SetAccountProxy(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))
	accountID, ok := accountIDParam(c)
	if !ok {
		return
	}
	var req growthProxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}
	proxy := ""
	if req.Proxy != nil {
		proxy = *req.Proxy
	}

	view, err := s.svc.UpdateAccount(c, uid, accountID, growth.AccountUpdate{ProxyURL: &proxy})
	if err != nil {
		accountError(c, err)
		return
	}

	response.Success(c, http.StatusOK, view)
}

type growthTelegramLoginRequest struct {
	Phone     string  `json:"phone" binding:"required"`
	AccountID *int64  `json:"account_id,omitempty"`
	Proxy     *string `json:"proxy,omitempty"`
}

type growthCodeRequest struct {
	Code string `json:"code" binding:"required"`
}

type growthPasswordRequest struct {
	Password string `json:"password" binding:"required"`
}

// StartTelegramLogin is a function to sign a Telegram account in by phone
// @Summary Add or re-sign a Telegram account: Telegram sends a code to the phone
// @Description account_id signs that account in again; without it the phone number becomes a new account. next is code.
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param json body growthTelegramLoginRequest true "phone in international format"
// @Success 201 {object} response.successResp{data=growth.LoginStep} "code sent successful"
// @Failure 400 {object} response.errorResp "invalid phone or proxy"
// @Failure 403 {object} response.errorResp "adding accounts not permitted"
// @Failure 404 {object} response.errorResp "account not found"
// @Failure 429 {object} response.errorResp "Telegram asked to wait"
// @Failure 503 {object} response.errorResp "no Telegram login in this build, or GROWTH_SECRET_KEY is not set"
// @Router /growth/accounts/telegram/login [post]
func (s *GrowthService) StartTelegramLogin(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	var req growthTelegramLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	step, err := s.svc.StartTelegramLogin(c, uid, growth.LoginStart{
		AccountID: req.AccountID, Phone: req.Phone, Proxy: req.Proxy,
	})
	if err != nil {
		accountError(c, err)
		return
	}

	response.Success(c, http.StatusCreated, step)
}

// SubmitTelegramCode is a function to pass on the code Telegram sent
// @Summary Pass on the login code; next is done, or password for an account with two-step verification
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param loginID path string true "login id"
// @Param json body growthCodeRequest true "the code"
// @Success 200 {object} response.successResp{data=growth.LoginStep} "code accepted successful"
// @Failure 400 {object} response.errorResp "the code was rejected; try again"
// @Failure 404 {object} response.errorResp "login not found or expired"
// @Failure 429 {object} response.errorResp "Telegram asked to wait"
// @Router /growth/accounts/telegram/login/{loginID}/code [post]
func (s *GrowthService) SubmitTelegramCode(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	var req growthCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	step, err := s.svc.SubmitTelegramCode(c, uid, c.Param("loginID"), req.Code)
	if err != nil {
		accountError(c, err)
		return
	}

	response.Success(c, http.StatusOK, step)
}

// SubmitTelegramPassword is a function to pass on the two-step verification password
// @Summary Pass on the two-step verification password; next is done
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param loginID path string true "login id"
// @Param json body growthPasswordRequest true "the password"
// @Success 200 {object} response.successResp{data=growth.LoginStep} "password accepted successful"
// @Failure 400 {object} response.errorResp "the password was rejected; try again"
// @Failure 404 {object} response.errorResp "login not found or expired"
// @Failure 429 {object} response.errorResp "Telegram asked to wait"
// @Router /growth/accounts/telegram/login/{loginID}/password [post]
func (s *GrowthService) SubmitTelegramPassword(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	var req growthPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	step, err := s.svc.SubmitTelegramPassword(c, uid, c.Param("loginID"), req.Password)
	if err != nil {
		accountError(c, err)
		return
	}

	response.Success(c, http.StatusOK, step)
}

// StartRemoteLogin is a function to open an account's browser for an operator
// @Summary Open a Facebook or Instagram account's browser on the server for an operator to sign in by hand
// @Description account_id signs that account in again; without it a new account of platform is added. view_url is the live view to embed.
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param json body growth.LoginStart true "the account, or the platform of a new one"
// @Success 201 {object} response.successResp{data=growth.RemoteLoginView} "browser opened successful"
// @Failure 400 {object} response.errorResp "invalid request data"
// @Failure 403 {object} response.errorResp "adding accounts not permitted"
// @Failure 404 {object} response.errorResp "account not found"
// @Failure 503 {object} response.errorResp "no remote login for this platform in this build"
// @Router /growth/accounts/remote-login [post]
func (s *GrowthService) StartRemoteLogin(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	var req growth.LoginStart
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}
	req.Phone = ""

	view, err := s.svc.StartRemoteLogin(c, uid, req)
	if err != nil {
		accountError(c, err)
		return
	}

	response.Success(c, http.StatusCreated, view)
}

// FinishRemoteLogin is a function to check the browser the operator signed in
// @Summary Check that the operator signed the browser in; the account turns active
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Param sessionID path string true "remote login session id"
// @Success 200 {object} response.successResp{data=growth.AccountView} "account signed in successful"
// @Failure 404 {object} response.errorResp "login not found or expired"
// @Failure 409 {object} response.errorResp "the browser is not signed in yet; the message says what the page shows"
// @Router /growth/accounts/remote-login/{sessionID}/finish [post]
func (s *GrowthService) FinishRemoteLogin(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	view, err := s.svc.FinishRemoteLogin(c, uid, c.Param("sessionID"))
	if err != nil {
		accountError(c, err)
		return
	}

	response.Success(c, http.StatusOK, view)
}

// CancelRemoteLogin is a function to close the operator's browser view
// @Summary Close the browser view; the account stays as it was and a new one is removed
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Param sessionID path string true "remote login session id"
// @Success 200 {object} response.successResp "login cancelled successful"
// @Failure 404 {object} response.errorResp "login not found or expired"
// @Router /growth/accounts/remote-login/{sessionID} [delete]
func (s *GrowthService) CancelRemoteLogin(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	if err := s.svc.CancelRemoteLogin(c, uid, c.Param("sessionID")); err != nil {
		accountError(c, err)
		return
	}

	response.Success(c, http.StatusOK, nil)
}

type growthLeadsEmailRequest struct {
	FlowID int64  `json:"flow_id" binding:"required,min=1"`
	TaskID *int64 `json:"task_id,omitempty"`
}

// EmailLeads is a function to mail the leads of a round on demand
// @Summary Mail a round's lead list to the notification addresses, as the email of a finished round
// @Description With task_id: the round's delivered leads, or its qualified leads while none are delivered. Without: the flow's delivered leads, else its qualified ones.
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param json body growthLeadsEmailRequest true "the flow and optionally the round's task"
// @Success 200 {object} response.successResp{data=growthTestEmailResult} "email sent successful"
// @Failure 400 {object} response.errorResp "invalid request, no SMTP server configured, or no leads to send"
// @Failure 403 {object} response.errorResp "sending email not permitted"
// @Failure 502 {object} response.errorResp "the email could not be sent"
// @Router /growth/leads/email [post]
func (s *GrowthService) EmailLeads(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	var req growthLeadsEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	to, err := s.svc.EmailLeads(c, uid, req.FlowID, req.TaskID)
	switch {
	case errors.Is(err, growth.ErrNoSMTP), errors.Is(err, growth.ErrNoLeadsToSend):
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	case err != nil:
		response.Error(c, response.ErrGrowthEmailFailed, err)
		return
	}

	response.Success(c, http.StatusOK, growthTestEmailResult{Sent: true, To: to})
}

// ListProfiles is a function to return the business profile of the current user
// @Summary Retrieve the business profile every task of the current user starts from
// @Description A user has at most one profile, so the list is empty or holds one.
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.successResp{data=[]growth.ProfileView} "profiles received successful"
// @Failure 403 {object} response.errorResp "getting the profile not permitted"
// @Failure 500 {object} response.errorResp "internal error on getting the profile"
// @Router /growth/profiles [get]
func (s *GrowthService) ListProfiles(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	profiles, err := s.svc.ListProfiles(c, uid)
	if err != nil {
		logger.FromContext(c).WithError(err).Errorf("error getting business profile")
		response.Error(c, response.ErrInternal, err)
		return
	}

	response.Success(c, http.StatusOK, profiles)
}

// ListProfilePresets is a function to return the starting profiles
// @Summary Retrieve the starting profiles, one per kind of customer, to copy into the profile form
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.successResp{data=[]growth.ProfilePreset} "presets received successful"
// @Failure 403 {object} response.errorResp "getting the presets not permitted"
// @Router /growth/profiles/presets [get]
func (s *GrowthService) ListProfilePresets(c *gin.Context) {
	response.Success(c, http.StatusOK, growth.ProfilePresets())
}

// CreateProfile is a function to store the business profile
// @Summary Save the business profile; new tasks start with it
// @Description Saving when a profile exists replaces it. knowledge says whether the lists reached the knowledge store: seeded, or skipped with the reason.
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param json body growth.Profile true "business profile"
// @Success 201 {object} response.successResp{data=growth.ProfileView} "profile saved successful"
// @Failure 400 {object} response.errorResp "invalid profile"
// @Failure 403 {object} response.errorResp "saving the profile not permitted"
// @Failure 500 {object} response.errorResp "internal error on saving the profile"
// @Router /growth/profiles [post]
func (s *GrowthService) CreateProfile(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	var req growth.Profile
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	saved, err := s.svc.SaveProfile(c, uid, req)
	if err != nil {
		profileError(c, err)
		return
	}

	response.Success(c, http.StatusCreated, saved)
}

// UpdateProfile is a function to change the business profile
// @Summary Change the business profile; new tasks start with it
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param profileID path int true "profile id" minimum(0)
// @Param json body growth.Profile true "business profile"
// @Success 200 {object} response.successResp{data=growth.ProfileView} "profile saved successful"
// @Failure 400 {object} response.errorResp "invalid profile"
// @Failure 403 {object} response.errorResp "saving the profile not permitted"
// @Failure 404 {object} response.errorResp "profile not found"
// @Failure 500 {object} response.errorResp "internal error on saving the profile"
// @Router /growth/profiles/{profileID} [put]
func (s *GrowthService) UpdateProfile(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	id, err := strconv.ParseInt(c.Param("profileID"), 10, 64)
	if err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}
	var req growth.Profile
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	saved, err := s.svc.UpdateProfile(c, uid, id, req)
	if err != nil {
		profileError(c, err)
		return
	}

	response.Success(c, http.StatusOK, saved)
}

func profileError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, growth.ErrInvalidProfile):
		response.Error(c, response.ErrGrowthInvalidRequest, err)
	case errors.Is(err, growth.ErrNoProfile):
		response.Error(c, response.ErrGrowthProfileNotFound, err)
	default:
		logger.FromContext(c).WithError(err).Errorf("error saving business profile")
		response.Error(c, response.ErrInternal, err)
	}
}

type growthSettings struct {
	Settings []growth.SettingView `json:"settings"`
}

type growthSaveSettingRequest struct {
	Provider string            `json:"provider,omitempty"`
	Fields   map[string]string `json:"fields" binding:"required"`
}

// An empty field is the saved setting's, else the .env one's; server_url is the llm
// kind's endpoint and api_url the screening kind's.
type growthListModelsRequest struct {
	Provider  string `json:"provider,omitempty"`
	APIKey    string `json:"api_key,omitempty"`
	APIURL    string `json:"api_url,omitempty"`
	ServerURL string `json:"server_url,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
}

type growthModels struct {
	Models []growth.ModelOption `json:"models"`
}

type growthTestEmailRequest struct {
	To string `json:"to,omitempty"`
	// Language names the language of the message, as the agents name it ("Chinese");
	// empty takes the language of the user's newest task.
	Language string `json:"language,omitempty" example:"Chinese"`
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
// @Param kind path string true "llm, embedding, telegram, youtube, web_search, email or proxy"
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
// @Param kind path string true "llm, embedding, telegram, youtube, web_search, email or proxy"
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
// @Param kind path string true "llm, embedding, telegram, youtube, web_search, email or proxy"
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

// ListModels is a function to fetch the models a provider's endpoint serves
// @Summary List the models of a provider's endpoint, for a choice before saving
// @Description For screening through OpenRouter only typesafe/ models are listed. A 502 means the list could not be fetched and the model is typed by name.
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param kind path string true "llm or screening"
// @Param json body growthListModelsRequest false "provider, key and endpoint; empty fields are the saved ones"
// @Success 200 {object} response.successResp{data=growthModels} "models received successful"
// @Failure 400 {object} response.errorResp "invalid provider, endpoint or missing key"
// @Failure 403 {object} response.errorResp "listing models not permitted"
// @Failure 502 {object} response.errorResp "the endpoint did not return its models"
// @Router /growth/settings/{kind}/models [post]
func (s *GrowthService) ListModels(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	var req growthListModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}
	values := map[string]string{"api_key": req.APIKey, "api_url": req.APIURL, "server_url": req.ServerURL, "protocol": req.Protocol}
	if values["server_url"] == "" {
		values["server_url"] = req.APIURL
	}

	models, err := s.svc.ListModels(c, uid, c.Param("kind"), req.Provider, values)
	if errors.Is(err, growth.ErrModelList) {
		response.Error(c, response.ErrGrowthModelList, err)
		return
	}
	if err != nil {
		settingError(c, err)
		return
	}

	response.Success(c, http.StatusOK, growthModels{Models: models})
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

	to, err := s.svc.SendTestEmail(c, uid, req.To, req.Language)
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

// ListSchedules is a function to return the schedules of the current user
// @Summary Retrieve the schedules that repeat the user's tasks
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Param flow_id query int false "only schedules repeating this flow"
// @Success 200 {object} response.successResp{data=growthSchedules} "schedules received successful"
// @Failure 400 {object} response.errorResp "invalid query"
// @Failure 403 {object} response.errorResp "getting schedules not permitted"
// @Failure 500 {object} response.errorResp "internal error on getting schedules"
// @Router /growth/schedules [get]
func (s *GrowthService) ListSchedules(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	flowID, err := optionalInt64(c, "flow_id")
	if err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	out, err := s.svc.ListSchedules(c, uid, flowID)
	if err != nil {
		logger.FromContext(c).WithError(err).Errorf("error listing growth schedules")
		response.Error(c, response.ErrInternal, err)
		return
	}

	response.Success(c, http.StatusOK, growthSchedules{Schedules: out, Total: uint64(len(out))})
}

// CreateSchedule is a function to repeat a task on a schedule
// @Summary Repeat a task on the chosen days; each start opens a new task delivered by the end time
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param json body growth.ScheduleInput true "schedule"
// @Success 201 {object} response.successResp{data=growth.ScheduleView} "schedule saved successful"
// @Failure 400 {object} response.errorResp "invalid schedule"
// @Failure 403 {object} response.errorResp "saving the schedule not permitted"
// @Failure 404 {object} response.errorResp "task not found"
// @Failure 500 {object} response.errorResp "internal error on saving the schedule"
// @Router /growth/schedules [post]
func (s *GrowthService) CreateSchedule(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	var req growth.ScheduleInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	saved, err := s.svc.CreateSchedule(c, uid, req)
	if err != nil {
		scheduleError(c, err)
		return
	}

	response.Success(c, http.StatusCreated, saved)
}

// UpdateSchedule is a function to change a schedule
// @Summary Change a schedule; flow_id is ignored, an empty input keeps the saved one
// @Tags Growth
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param scheduleID path int true "schedule id" minimum(0)
// @Param json body growth.ScheduleInput true "schedule"
// @Success 200 {object} response.successResp{data=growth.ScheduleView} "schedule saved successful"
// @Failure 400 {object} response.errorResp "invalid schedule"
// @Failure 403 {object} response.errorResp "saving the schedule not permitted"
// @Failure 404 {object} response.errorResp "schedule not found"
// @Failure 500 {object} response.errorResp "internal error on saving the schedule"
// @Router /growth/schedules/{scheduleID} [put]
func (s *GrowthService) UpdateSchedule(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	id, err := strconv.ParseInt(c.Param("scheduleID"), 10, 64)
	if err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}
	var req growth.ScheduleInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	saved, err := s.svc.UpdateSchedule(c, uid, id, req)
	if err != nil {
		scheduleError(c, err)
		return
	}

	response.Success(c, http.StatusOK, saved)
}

// DeleteSchedule is a function to remove a schedule
// @Summary Remove a schedule; the tasks it started are kept
// @Tags Growth
// @Produce json
// @Security BearerAuth
// @Param scheduleID path int true "schedule id" minimum(0)
// @Success 200 {object} response.successResp "schedule deleted successful"
// @Failure 400 {object} response.errorResp "invalid schedule id"
// @Failure 403 {object} response.errorResp "deleting the schedule not permitted"
// @Failure 404 {object} response.errorResp "schedule not found"
// @Failure 500 {object} response.errorResp "internal error on deleting the schedule"
// @Router /growth/schedules/{scheduleID} [delete]
func (s *GrowthService) DeleteSchedule(c *gin.Context) {
	uid := int64(c.GetUint64("uid"))

	id, err := strconv.ParseInt(c.Param("scheduleID"), 10, 64)
	if err != nil {
		response.Error(c, response.ErrGrowthInvalidRequest, err)
		return
	}

	if err := s.svc.DeleteSchedule(c, uid, id); err != nil {
		scheduleError(c, err)
		return
	}

	response.Success(c, http.StatusOK, struct{}{})
}

func scheduleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, growth.ErrInvalidSchedule):
		response.Error(c, response.ErrGrowthInvalidRequest, err)
	case errors.Is(err, growth.ErrScheduleNotFound):
		response.Error(c, response.ErrGrowthScheduleNotFound, err)
	case errors.Is(err, growth.ErrFlowNotFound):
		response.Error(c, response.ErrGrowthFlowNotFound, err)
	default:
		logger.FromContext(c).WithError(err).Errorf("error saving growth schedule")
		response.Error(c, response.ErrInternal, err)
	}
}
