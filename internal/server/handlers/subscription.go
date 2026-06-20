package handlers

/*
Lodestar — subscription HTTP handlers and route registration.

User routes (any authenticated user):
  GET  /api/v1/subscription/plans    — list enabled plans (public to logged-in users)
  GET  /api/v1/subscription/self     — get own active subscription
  POST /api/v1/subscription/purchase — purchase with balance

Admin routes (subscriptions:write):
  GET    /api/v1/subscription/admin/plans         — list all plans (including disabled)
  POST   /api/v1/subscription/admin/plans/create   — create plan
  POST   /api/v1/subscription/admin/plans/update    — update plan
  DELETE /api/v1/subscription/admin/plans/delete/:id — delete plan
  POST   /api/v1/subscription/admin/bind            — bind subscription to user
  GET    /api/v1/subscription/admin/subscriptions    — list all user subscriptions
*/

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	subop "github.com/gypg/lodestar/internal/op/subscription"
	"github.com/gypg/lodestar/internal/server/auth"
	"github.com/gypg/lodestar/internal/server/middleware"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/server/router"
)

func init() {
	// User routes — any authenticated user
	router.NewGroupRouter("/api/v1/subscription").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/plans", http.MethodGet).
				Handle(listPlans),
		).
		AddRoute(
			router.NewRoute("/self", http.MethodGet).
				Handle(getSelfSubscription),
		).
		AddRoute(
			router.NewRoute("/purchase", http.MethodPost).
				Handle(purchaseWithBalance),
		)

	// Admin routes — require subscriptions:write
	router.NewGroupRouter("/api/v1/subscription/admin").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		Use(middleware.RequirePermission(auth.PermSubscriptionsWrite)).
		AddRoute(
			router.NewRoute("/plans", http.MethodGet).
				Handle(adminListPlans),
		).
		AddRoute(
			router.NewRoute("/plans/create", http.MethodPost).
				Handle(adminCreatePlan),
		).
		AddRoute(
			router.NewRoute("/plans/update", http.MethodPost).
				Handle(adminUpdatePlan),
		).
		AddRoute(
			router.NewRoute("/plans/delete/:id", http.MethodDelete).
				Handle(adminDeletePlan),
		).
		AddRoute(
			router.NewRoute("/bind", http.MethodPost).
				Handle(adminBindSubscription),
		).
		AddRoute(
			router.NewRoute("/subscriptions", http.MethodGet).
				Handle(adminListSubscriptions),
		)
}

// --- User handlers ---

func listPlans(c *gin.Context) {
	plans, err := subop.ListPlans(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, plans)
}

func getSelfSubscription(c *gin.Context) {
	userID := uint(c.GetInt("user_id"))
	sub, err := subop.GetUserSubscription(userID, c.Request.Context())
	if err != nil {
		// No active subscription is not an error — return nil
		resp.Success(c, nil)
		return
	}
	resp.Success(c, sub)
}

func purchaseWithBalance(c *gin.Context) {
	var req struct {
		PlanID int `json:"plan_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.PlanID <= 0 {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	userID := uint(c.GetInt("user_id"))
	if err := subop.PurchaseWithBalance(userID, req.PlanID, c.Request.Context()); err != nil {
		if err == subop.ErrInsufficientBalance {
			resp.Error(c, http.StatusBadRequest, "insufficient balance")
			return
		}
		if err == subop.ErrPlanNotFound || err == subop.ErrPlanDisabled {
			resp.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

// --- Admin handlers ---

func adminListPlans(c *gin.Context) {
	plans, err := subop.ListAllPlans(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, plans)
}

func adminCreatePlan(c *gin.Context) {
	var plan model.SubscriptionPlan
	if err := c.ShouldBindJSON(&plan); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	if plan.Name == "" {
		resp.Error(c, http.StatusBadRequest, "name is required")
		return
	}
	if plan.Price < 0 {
		resp.Error(c, http.StatusBadRequest, "price cannot be negative")
		return
	}
	if plan.Currency == "" {
		plan.Currency = "USD"
	}
	if plan.DurationType == "" {
		plan.DurationType = model.SubDurationMonth
	}
	if plan.DurationDays <= 0 {
		plan.DurationDays = 30
	}
	plan.ID = 0 // ensure new record
	if err := subop.CreatePlan(&plan, c.Request.Context()); err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, plan)
}

func adminUpdatePlan(c *gin.Context) {
	var req struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Desc    string `json:"description"`
		Price   *float64 `json:"price"`
		Currency string `json:"currency"`
		DurationType string `json:"duration_type"`
		DurationDays *int   `json:"duration_days"`
		CustomDurationS *int64 `json:"custom_duration_s"`
		QuotaAmount *float64 `json:"quota_amount"`
		Enabled  *bool  `json:"enabled"`
		SortOrder *int  `json:"sort_order"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ID <= 0 {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	updates := make(map[string]any)
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Desc != "" {
		updates["description"] = req.Desc
	}
	if req.Price != nil {
		if *req.Price < 0 {
			resp.Error(c, http.StatusBadRequest, "price must be non-negative")
			return
		}
		updates["price"] = *req.Price
	}
	if req.Currency != "" {
		updates["currency"] = req.Currency
	}
	if req.DurationType != "" {
		updates["duration_type"] = req.DurationType
	}
	if req.DurationDays != nil {
		updates["duration_days"] = *req.DurationDays
	}
	if req.CustomDurationS != nil {
		updates["custom_duration_s"] = *req.CustomDurationS
	}
	if req.QuotaAmount != nil {
		updates["quota_amount"] = *req.QuotaAmount
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.SortOrder != nil {
		updates["sort_order"] = *req.SortOrder
	}
	if len(updates) == 0 {
		resp.Error(c, http.StatusBadRequest, "no fields to update")
		return
	}
	if err := subop.UpdatePlan(req.ID, updates, c.Request.Context()); err != nil {
		if err == subop.ErrPlanNotFound {
			resp.Error(c, http.StatusNotFound, err.Error())
			return
		}
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func adminDeletePlan(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	if err := subop.DeletePlan(id, c.Request.Context()); err != nil {
		if err == subop.ErrPlanNotFound {
			resp.Error(c, http.StatusNotFound, err.Error())
			return
		}
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func adminBindSubscription(c *gin.Context) {
	var req struct {
		UserID uint `json:"user_id"`
		PlanID int  `json:"plan_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.UserID == 0 || req.PlanID <= 0 {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	if err := subop.AdminBindSubscription(req.UserID, req.PlanID, c.Request.Context()); err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func adminListSubscriptions(c *gin.Context) {
	subs, err := subop.ListAllUserSubscriptions(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, subs)
}
