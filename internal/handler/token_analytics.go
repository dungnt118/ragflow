//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

package handler

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/service"
)

func resolveStatsTenantID(ctx context.Context, userID string) string {
	if dao.DB != nil {
		tenants, err := dao.NewUserTenantDAO().GetByUserID(ctx, dao.DB, userID)
		if err == nil && len(tenants) > 0 {
			for _, t := range tenants {
				if strings.EqualFold(t.Role, "owner") && t.TenantID != "" {
					return t.TenantID
				}
			}
			if tenants[0].TenantID != "" {
				return tenants[0].TenantID
			}
		}
	}
	return userID
}

// GetTokenUsageDashboard handles GET /api/v1/system/token-usage/dashboard
func (h *StatsHandler) GetTokenUsageDashboard(c *gin.Context) {
	user, errorCode, errorMessage := GetUser(c)
	if errorCode != common.CodeSuccess {
		common.ErrorWithCode(c, errorCode, errorMessage)
		return
	}

	var filter service.TokenUsageFilter
	_ = c.ShouldBindQuery(&filter)

	ctx := c.Request.Context()
	tenantID := resolveStatsTenantID(ctx, user.ID)
	data, err := service.DefaultTokenAnalyticsService().GetDashboardSummary(ctx, dao.DB, tenantID, filter)
	if err != nil {
		common.ErrorWithCode(c, common.CodeExceptionError, err.Error())
		return
	}
	common.SuccessWithData(c, data, "success")
}

// ListTokenUsageLogs handles GET /api/v1/system/token-usage/logs
func (h *StatsHandler) ListTokenUsageLogs(c *gin.Context) {
	user, errorCode, errorMessage := GetUser(c)
	if errorCode != common.CodeSuccess {
		common.ErrorWithCode(c, errorCode, errorMessage)
		return
	}

	var filter service.TokenUsageFilter
	_ = c.ShouldBindQuery(&filter)

	ctx := c.Request.Context()
	tenantID := resolveStatsTenantID(ctx, user.ID)
	items, total, err := service.DefaultTokenAnalyticsService().ListUsageLogs(ctx, dao.DB, tenantID, filter)
	if err != nil {
		common.ErrorWithCode(c, common.CodeExceptionError, err.Error())
		return
	}
	common.SuccessWithData(c, gin.H{
		"items":     items,
		"total":     total,
		"page":      max(filter.Page, 1),
		"page_size": max(filter.PageSize, 20),
	}, "success")
}

// GetTokenPricingAndBudgets handles GET /api/v1/system/token-usage/pricing
func (h *StatsHandler) GetTokenPricingAndBudgets(c *gin.Context) {
	user, errorCode, errorMessage := GetUser(c)
	if errorCode != common.CodeSuccess {
		common.ErrorWithCode(c, errorCode, errorMessage)
		return
	}

	ctx := c.Request.Context()
	tenantID := resolveStatsTenantID(ctx, user.ID)
	data, err := service.DefaultTokenAnalyticsService().GetPricingAndBudgets(ctx, dao.DB, tenantID)
	if err != nil {
		common.ErrorWithCode(c, common.CodeExceptionError, err.Error())
		return
	}
	common.SuccessWithData(c, data, "success")
}

// SaveTokenPricing handles PUT /api/v1/system/token-usage/pricing
func (h *StatsHandler) SaveTokenPricing(c *gin.Context) {
	user, errorCode, errorMessage := GetUser(c)
	if errorCode != common.CodeSuccess {
		common.ErrorWithCode(c, errorCode, errorMessage)
		return
	}

	var req service.SavePricingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorWithCode(c, common.CodeArgumentError, err.Error())
		return
	}

	ctx := c.Request.Context()
	tenantID := resolveStatsTenantID(ctx, user.ID)
	cfg, err := service.DefaultTokenAnalyticsService().SavePricing(ctx, dao.DB, tenantID, req)
	if err != nil {
		common.ErrorWithCode(c, common.CodeExceptionError, err.Error())
		return
	}
	common.SuccessWithData(c, cfg, "success")
}

// DeleteTokenPricing handles DELETE /api/v1/system/token-usage/pricing/:id
func (h *StatsHandler) DeleteTokenPricing(c *gin.Context) {
	user, errorCode, errorMessage := GetUser(c)
	if errorCode != common.CodeSuccess {
		common.ErrorWithCode(c, errorCode, errorMessage)
		return
	}

	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		common.ErrorWithCode(c, common.CodeArgumentError, "id is required")
		return
	}

	ctx := c.Request.Context()
	tenantID := resolveStatsTenantID(ctx, user.ID)
	if err := service.DefaultTokenAnalyticsService().DeletePricing(ctx, dao.DB, tenantID, id); err != nil {
		common.ErrorWithCode(c, common.CodeExceptionError, err.Error())
		return
	}
	common.SuccessWithData(c, true, "success")
}

// SaveTokenBudget handles PUT /api/v1/system/token-usage/budget
func (h *StatsHandler) SaveTokenBudget(c *gin.Context) {
	user, errorCode, errorMessage := GetUser(c)
	if errorCode != common.CodeSuccess {
		common.ErrorWithCode(c, errorCode, errorMessage)
		return
	}

	var req service.SaveBudgetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorWithCode(c, common.CodeArgumentError, err.Error())
		return
	}

	ctx := c.Request.Context()
	tenantID := resolveStatsTenantID(ctx, user.ID)
	cfg, err := service.DefaultTokenAnalyticsService().SaveBudget(ctx, dao.DB, tenantID, req)
	if err != nil {
		common.ErrorWithCode(c, common.CodeExceptionError, err.Error())
		return
	}
	common.SuccessWithData(c, cfg, "success")
}
