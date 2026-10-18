package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/8bit-warrior/Miniray/internal/logger"
	"github.com/8bit-warrior/Miniray/internal/util/crypto"
	"github.com/8bit-warrior/Miniray/internal/web/entity"
	"github.com/8bit-warrior/Miniray/internal/web/global"
	"github.com/8bit-warrior/Miniray/internal/web/middleware"
	"github.com/8bit-warrior/Miniray/internal/web/service"
	"github.com/8bit-warrior/Miniray/internal/web/service/panel"
	"github.com/8bit-warrior/Miniray/internal/web/session"
)

// updateUserForm represents the form for updating user credentials.
type updateUserForm struct {
	OldUsername   string `json:"oldUsername" form:"oldUsername"`
	OldPassword   string `json:"oldPassword" form:"oldPassword"`
	NewUsername   string `json:"newUsername" form:"newUsername"`
	NewPassword   string `json:"newPassword" form:"newPassword"`
	TwoFactorCode string `json:"twoFactorCode" form:"twoFactorCode"`
}

// updateSettingForm carries the persisted settings plus request-scoped fields
// that must never land in the settings table: the 2FA confirmation code and
// the explicit clear flags for redacted secrets (a blank secret alone means
// "unchanged", so clearing needs its own signal — see #5724).
type updateSettingForm struct {
	entity.AllSetting
	TwoFactorCode        string `json:"twoFactorCode" form:"twoFactorCode"`
	ClearLdapPassword bool `json:"clearLdapPassword" form:"clearLdapPassword"`
}

type validateRegexForm struct {
	Regex string `json:"regex" form:"regex"`
}

// SettingController handles settings and user management operations.
type SettingController struct {
	settingService  service.SettingService
	userService     panel.UserService
	panelService    panel.PanelService
	apiTokenService panel.ApiTokenService
	xrayService     service.XrayService
}

// NewSettingController creates a new SettingController and initializes its routes.
func NewSettingController(g *gin.RouterGroup) *SettingController {
	a := &SettingController{}
	a.initRouter(g)
	return a
}

// initRouter sets up the routes for settings management.
func (a *SettingController) initRouter(g *gin.RouterGroup) {
	g = g.Group("/setting")

	g.POST("/all", a.getAllSetting)
	g.POST("/defaultSettings", a.getDefaultSettings)
	g.POST("/factoryDefaults", a.getFactoryDefaults)
	g.POST("/update", a.updateSetting)
	g.POST("/validateRegex", a.validateRegex)
	g.POST("/updateUser", a.updateUser)
	g.POST("/restartPanel", a.restartPanel)
	g.POST("/offlinePanel", a.offlinePanel)
	g.GET("/panelAutostart", a.getPanelAutostart)
	g.POST("/panelAutostart", a.updatePanelAutostart)
	g.GET("/getDefaultJsonConfig", a.getDefaultXrayConfig)
	g.GET("/apiTokens", a.listApiTokens)
	g.POST("/apiTokens/create", a.createApiToken)
	g.POST("/apiTokens/delete/:id", a.deleteApiToken)
	g.POST("/apiTokens/setEnabled/:id", a.setApiTokenEnabled)
}

func (a *SettingController) validateRegex(c *gin.Context) {
	form := &validateRegexForm{}
	if err := c.ShouldBind(form); err != nil {
		pureJsonMsg(c, http.StatusOK, false, err.Error())
		return
	}
	if err := service.ValidateRegex(form.Regex); err != nil {
		pureJsonMsg(c, http.StatusOK, false, err.Error())
		return
	}
	pureJsonMsg(c, http.StatusOK, true, "")
}

// getAllSetting retrieves all current settings as the browser-safe view:
// secret values are redacted and surfaced as has* presence flags instead.
func (a *SettingController) getAllSetting(c *gin.Context) {
	allSetting, err := a.settingService.GetAllSettingView()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.getSettings"), err)
		return
	}
	jsonObj(c, allSetting, nil)
}

// getDefaultSettings retrieves the default settings based on the host.
func (a *SettingController) getDefaultSettings(c *gin.Context) {
	result, err := a.settingService.GetDefaultSettings(c.Request.Host)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.getSettings"), err)
		return
	}
	jsonObj(c, result, nil)
}

func (a *SettingController) getFactoryDefaults(c *gin.Context) {
	jsonObj(c, a.settingService.GetFactoryDefaults(), nil)
}

// updateSetting updates all settings with the provided data.
func (a *SettingController) updateSetting(c *gin.Context) {
	form, ok := middleware.BindAndValidate[updateSettingForm](c)
	if !ok {
		return
	}
	allSetting := &form.AllSetting
	oldTwoFactor, twoFactorErr := a.settingService.GetTwoFactorEnable()
	oldPanelOutbound, _ := a.settingService.GetPanelOutbound()
	if twoFactorErr == nil && oldTwoFactor {
		// Rebinding the authenticator is the same class of change as turning 2FA
		// off, so both need a current code. Blank still means "unchanged".
		submittedToken := strings.TrimSpace(allSetting.TwoFactorToken)
		storedToken, _ := a.settingService.GetTwoFactorToken()
		if !allSetting.TwoFactorEnable || (submittedToken != "" && submittedToken != storedToken) {
			if err := a.settingService.VerifyTwoFactorCode(form.TwoFactorCode); err != nil {
				jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), err)
				return
			}
		}
	}
	err := a.settingService.UpdateAllSetting(allSetting, service.SecretClears{LdapPassword: form.ClearLdapPassword})
	if err == nil && twoFactorErr == nil && !oldTwoFactor && allSetting.TwoFactorEnable {
		if bumpErr := a.userService.BumpLoginEpoch(); bumpErr != nil {
			err = bumpErr
		}
	}
	if err == nil && form.PanelOutbound != oldPanelOutbound {
		// The egress bridge lives in the generated config; reconcile the
		// running core. One SOCKS inbound plus one routing rule — both
		// hot-appliable, so this normally does not restart Xray.
		if applyErr := a.xrayService.RestartXray(false); applyErr != nil {
			logger.Warning("apply panel outbound change failed:", applyErr)
		}
	}
	jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), err)
}

// updateUser updates the current user's username and password.
func (a *SettingController) updateUser(c *gin.Context) {
	form := &updateUserForm{}
	err := c.ShouldBind(form)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), err)
		return
	}
	user := session.GetLoginUser(c)
	if user.Username != form.OldUsername || !crypto.CheckPasswordHash(user.Password, form.OldPassword) {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifyUserError"), errors.New(I18nWeb(c, "pages.settings.toasts.originalUserPassIncorrect")))
		return
	}
	if form.NewUsername == "" || form.NewPassword == "" {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifyUserError"), errors.New(I18nWeb(c, "pages.settings.toasts.userPassMustBeNotEmpty")))
		return
	}
	if err := a.settingService.VerifyTwoFactorCode(form.TwoFactorCode); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifyUserError"), err)
		return
	}
	err = a.userService.UpdateUser(user.Id, form.NewUsername, form.NewPassword)
	if err == nil {
		user.Username = form.NewUsername
		user.Password, _ = crypto.HashPasswordAsBcrypt(form.NewPassword)
		if saveErr := session.SetLoginUser(c, user); saveErr != nil {
			err = saveErr
		}
	}
	jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifyUser"), err)
}

// restartPanel restarts the panel service after a delay.
func (a *SettingController) restartPanel(c *gin.Context) {
	err := a.panelService.RestartPanel(time.Second * 3)
	jsonMsg(c, I18nWeb(c, "pages.settings.restartPanelSuccess"), err)
}

// getDefaultXrayConfig retrieves the default Xray configuration.
func (a *SettingController) getDefaultXrayConfig(c *gin.Context) {
	defaultJsonConfig, err := a.settingService.GetDefaultXrayConfig()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.getSettings"), err)
		return
	}
	jsonObj(c, defaultJsonConfig, nil)
}

type apiTokenCreateForm struct {
	Name      string `json:"name" form:"name"`
	Scope     string `json:"scope" form:"scope"`
	ExpiresAt int64  `json:"expiresAt" form:"expiresAt"`
}

type apiTokenEnabledForm struct {
	Enabled       bool   `json:"enabled" form:"enabled"`
	ExpectedScope string `json:"expectedScope" form:"expectedScope"`
}

type apiTokenScopeForm struct {
	ExpectedScope string `json:"expectedScope" form:"expectedScope"`
}

func (a *SettingController) listApiTokens(c *gin.Context) {
	rows, err := a.apiTokenService.List()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.getSettings"), err)
		return
	}
	jsonObj(c, rows, nil)
}

func (a *SettingController) createApiToken(c *gin.Context) {
	form := &apiTokenCreateForm{}
	if err := c.ShouldBind(form); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), err)
		return
	}
	row, err := a.apiTokenService.Create(form.Name, form.Scope, form.ExpiresAt)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), err)
		return
	}
	jsonObj(c, row, nil)
}

func (a *SettingController) deleteApiToken(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), err)
		return
	}
	form := &apiTokenScopeForm{}
	if bindErr := c.ShouldBind(form); bindErr != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), bindErr)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), a.apiTokenService.DeleteExpectedScope(id, form.ExpectedScope))
}

func (a *SettingController) setApiTokenEnabled(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), err)
		return
	}
	form := &apiTokenEnabledForm{}
	if bindErr := c.ShouldBind(form); bindErr != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), bindErr)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.settings.toasts.modifySettings"), a.apiTokenService.SetEnabledExpectedScope(id, form.ExpectedScope, form.Enabled))
}

// offlinePanel is an authenticated settings API endpoint. The HTTP response is
// sent before the process closes its listener; an independent core is required.
func (a *SettingController) offlinePanel(c *gin.Context) {
	if runtime.GOOS == "linux" && os.Getenv("MINIRAY_SEPARATE_CORE") != "1" {
		pureJsonMsg(c, http.StatusServiceUnavailable, false, "independent core service is required")
		return
	}
	pureJsonMsg(c, http.StatusOK, true, "panel will go offline")
	go func() { time.Sleep(500 * time.Millisecond); global.TriggerPanelOffline() }()
}

type panelAutostartForm struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

func getPanelAutostartState() (bool, error) {
	if runtime.GOOS != "linux" {
		return false, errors.New("systemd panel autostart is supported only on Linux")
	}
	out, err := exec.CommandContext(context.Background(), "systemctl", "is-enabled", "miniray.service").CombinedOutput()
	switch strings.TrimSpace(string(out)) {
	case "enabled", "enabled-runtime", "linked", "linked-runtime":
		return true, nil
	case "disabled", "static", "indirect":
		return false, nil
	default:
		return false, fmt.Errorf("query panel boot state: %w: %s", err, strings.TrimSpace(string(out)))
	}
}

func (a *SettingController) getPanelAutostart(c *gin.Context) {
	enabled, err := getPanelAutostartState()
	jsonObj(c, gin.H{"enabled": enabled}, err)
}

func (a *SettingController) updatePanelAutostart(c *gin.Context) {
	var form panelAutostartForm
	if err := c.ShouldBindJSON(&form); err != nil {
		jsonObj(c, nil, err)
		return
	}
	if runtime.GOOS != "linux" {
		jsonObj(c, nil, errors.New("systemd panel autostart is supported only on Linux"))
		return
	}
	action := "disable"
	if *form.Enabled {
		action = "enable"
	}
	out, err := exec.CommandContext(c.Request.Context(), "systemctl", action, "miniray.service").CombinedOutput()
	if err != nil {
		jsonObj(c, nil, fmt.Errorf("systemctl %s miniray.service: %w: %s", action, err, strings.TrimSpace(string(out))))
		return
	}
	jsonObj(c, gin.H{"enabled": *form.Enabled}, nil)
}
