package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rakhisyudha/lab/internal/platform/httpx"
	"github.com/rakhisyudha/lab/internal/platform/httpx/apperr"
	"github.com/rakhisyudha/lab/internal/platform/pagination"
)

// Handler mengekspos endpoint autentikasi dan admin auth.
type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

type registerRequest struct {
	Code        string `json:"code" binding:"required"`
	Email       string `json:"email" binding:"required,email"`
	Password    string `json:"password" binding:"required"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type mePatchRequest struct {
	DisplayName     *string `json:"display_name"`
	PreferredLocale *string `json:"preferred_locale"`
}

type passwordRequest struct {
	NewPassword string `json:"new_password" binding:"required"`
}

type createInvitationRequest struct {
	Email *string `json:"email"`
	Role  string  `json:"role" binding:"required,oneof=user admin"`
}

type userPatchRequest struct {
	IsActive *bool   `json:"is_active"`
	Role     *string `json:"role"`
}

func (h *Handler) Register(c *gin.Context) {
	var req registerRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	pair, err := h.service.Register(c.Request.Context(), req.Code, req.Email, req.Password, req.DisplayName)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, pair)
}

func (h *Handler) Login(c *gin.Context) {
	var req loginRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	pair, err := h.service.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, pair)
}

func (h *Handler) Refresh(c *gin.Context) {
	var req refreshRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	pair, err := h.service.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, pair)
}

func (h *Handler) Logout(c *gin.Context) {
	var req refreshRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.service.Logout(c.Request.Context(), req.RefreshToken); err != nil {
		httpx.Fail(c, err)
		return
	}
	noContent(c)
}

func (h *Handler) Me(c *gin.Context) {
	p, _ := PrincipalFrom(c.Request.Context())
	id, err := uuid.Parse(p.UserID)
	if err != nil {
		httpx.Fail(c, apperr.New(apperr.CodeUnauthenticated))
		return
	}
	view, err := h.service.Me(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h *Handler) PatchMe(c *gin.Context) {
	var req mePatchRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	p, _ := PrincipalFrom(c.Request.Context())
	id, err := uuid.Parse(p.UserID)
	if err != nil {
		httpx.Fail(c, apperr.New(apperr.CodeUnauthenticated))
		return
	}
	view, err := h.service.UpdateMe(c.Request.Context(), id, req.DisplayName, req.PreferredLocale)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h *Handler) ChangePassword(c *gin.Context) {
	var req passwordRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	p, _ := PrincipalFrom(c.Request.Context())
	id, err := uuid.Parse(p.UserID)
	if err != nil {
		httpx.Fail(c, apperr.New(apperr.CodeUnauthenticated))
		return
	}
	if err := h.service.ChangePassword(c.Request.Context(), id, req.NewPassword); err != nil {
		httpx.Fail(c, err)
		return
	}
	noContent(c)
}

func (h *Handler) CreateInvitation(c *gin.Context) {
	var req createInvitationRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	p, _ := PrincipalFrom(c.Request.Context())
	actor, err := uuid.Parse(p.UserID)
	if err != nil {
		httpx.Fail(c, apperr.New(apperr.CodeUnauthenticated))
		return
	}
	created, err := h.service.CreateInvitation(c.Request.Context(), actor, req.Email, req.Role)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (h *Handler) ListInvitations(c *gin.Context) {
	req, err := pagination.ParseOffsetQuery(pagination.KindAdmin, c.Query("limit"), c.Query("offset"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items, total, err := h.service.ListInvitations(c.Request.Context(), req.Limit, req.Offset)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, pagination.NewOffsetPage(items, req, total))
}

func (h *Handler) RevokeInvitation(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
		return
	}
	if err := h.service.RevokeInvitation(c.Request.Context(), id); err != nil {
		httpx.Fail(c, err)
		return
	}
	noContent(c)
}

func (h *Handler) ListUsers(c *gin.Context) {
	req, err := pagination.ParseOffsetQuery(pagination.KindAdmin, c.Query("limit"), c.Query("offset"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items, total, err := h.service.ListUsers(c.Request.Context(), req.Limit, req.Offset)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, pagination.NewOffsetPage(items, req, total))
}

func (h *Handler) PatchUser(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, apperr.Validation(apperr.FieldError{Field: "id", Reason: "uuid"}))
		return
	}
	var req userPatchRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		httpx.Fail(c, err)
		return
	}
	view, err := h.service.UpdateUser(c.Request.Context(), id, req.IsActive, req.Role)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

// RegisterRoutes memasang endpoint auth di grup yang sudah terautentikasi
// maupun yang publik. Grup `api` adalah /api/v1.
func (h *Handler) RegisterRoutes(api *gin.RouterGroup, mw *Middleware) {
	authGroup := api.Group("/auth")
	{
		authGroup.POST("/register", h.Register)
		authGroup.POST("/login", h.Login)
		authGroup.POST("/refresh", h.Refresh)
		authGroup.POST("/logout", h.Logout)
	}

	me := api.Group("/me")
	me.Use(mw.Authenticate())
	{
		me.GET("", h.Me)
		me.PATCH("", h.PatchMe)
		me.POST("/password", h.ChangePassword)
	}

	admin := api.Group("/admin")
	admin.Use(mw.Authenticate(), RequireRole("admin"))
	{
		admin.POST("/invitations", h.CreateInvitation)
		admin.GET("/invitations", h.ListInvitations)
		admin.DELETE("/invitations/:id", h.RevokeInvitation)
		admin.GET("/users", h.ListUsers)
		admin.PATCH("/users/:id", h.PatchUser)
	}
}
