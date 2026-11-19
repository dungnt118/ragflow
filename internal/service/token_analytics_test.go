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

package service

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
)

func setupTokenAnalyticsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite memory db: %v", err)
	}
	if err := db.AutoMigrate(
		&entity.Tenant{},
		&entity.UserTenant{},
		&entity.TenantLLM{},
		&entity.Chat{},
		&entity.ChatSession{},
		&entity.ConversationMessage{},
		&entity.API4Conversation{},
		&entity.API4ConversationMessage{},
		&entity.UserCanvas{},
		&entity.LLMUsageLog{},
		&entity.LLMPricingConfig{},
		&entity.LLMBudgetConfig{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestTokenAnalyticsService_RecordPricingAndMonthlyForecast(t *testing.T) {
	db := setupTokenAnalyticsTestDB(t)
	ctx := context.Background()
	svc := NewTokenAnalyticsService()
	tenantID := "tenant_finops_01"

	now := time.Now()
	monthStr := now.Format("2006-01")
	todayStr := now.Format("2006-01-02")

	inP, outP, matched := dao.MatchBuiltinPrice("OpenAI", "gpt-4o", "chat")
	if !matched || inP != 2.50 || outP != 10.00 {
		t.Fatalf("unexpected builtin price for gpt-4o: in=%.2f out=%.2f matched=%v", inP, outP, matched)
	}

	// 1. Record OpenAI gpt-4o chat call (100,000 input + 50,000 output tokens)
	// Builtin price for gpt-4o: $2.50 / 1M input ($0.25) + $10.00 / 1M output ($0.50) = $0.75
	err := svc.RecordUsageSync(ctx, db, &common.ModelUsage{
		TenantID:       tenantID,
		UserID:         tenantID,
		AppID:          "dlg_sales",
		AppName:        "Trợ lý Bán hàng",
		AppType:        "chat",
		SessionID:      "sess_1",
		ProviderName:   "OpenAI",
		ModelName:      "gpt-4o",
		Type:           "chat",
		InputTokens:    100_000,
		OutputTokens:   50_000,
		TotalTokens:    150_000,
		ResponseTimeMS: 1200,
		StartAt:        now,
	})
	if err != nil {
		t.Fatalf("RecordUsageSync OpenAI failed: %v", err)
	}

	// 2. Record Gemini gemini-2.5-flash agent call (200,000 input + 100,000 output tokens)
	// Builtin price for gemini-2.5-flash: $0.15 / 1M input ($0.03) + $0.60 / 1M output ($0.06) = $0.09
	err = svc.RecordUsageSync(ctx, db, &common.ModelUsage{
		TenantID:       tenantID,
		UserID:         tenantID,
		AppID:          "canvas_support",
		AppName:        "Agent CSKH",
		AppType:        "agent",
		SessionID:      "sess_2",
		ProviderName:   "Gemini",
		ModelName:      "gemini-2.5-flash",
		Type:           "chat",
		InputTokens:    200_000,
		OutputTokens:   100_000,
		TotalTokens:    300_000,
		ResponseTimeMS: 800,
		StartAt:        now,
	})
	if err != nil {
		t.Fatalf("RecordUsageSync Gemini failed: %v", err)
	}

	// 3. Record DeepSeek embedding/ingestion call
	err = svc.RecordUsageSync(ctx, db, &common.ModelUsage{
		TenantID:       tenantID,
		UserID:         tenantID,
		AppID:          "kb_docs",
		AppName:        "Kho Tài Liệu",
		AppType:        "ingestion",
		ProviderName:   "OpenAI",
		ModelName:      "text-embedding-3-small",
		Type:           "embedding",
		InputTokens:    500_000,
		OutputTokens:   0,
		TotalTokens:    500_000,
		ResponseTimeMS: 300,
		StartAt:        now,
	})
	if err != nil {
		t.Fatalf("RecordUsageSync Embedding failed: %v", err)
	}

	// Set monthly budget to $10.00
	_, err = svc.SaveBudget(ctx, db, tenantID, SaveBudgetRequest{
		ProviderName:      "ALL",
		MonthlyBudgetUSD:  10.0,
		AlertThresholdPct: 80.0,
		DisplayCurrency:   "USD",
		ExchangeRateVND:   25500,
	})
	if err != nil {
		t.Fatalf("SaveBudget failed: %v", err)
	}

	// 4. Query Dashboard Summary for current month
	dash, err := svc.GetDashboardSummary(ctx, db, tenantID, TokenUsageFilter{
		Month: monthStr,
	})
	if err != nil {
		t.Fatalf("GetDashboardSummary failed: %v", err)
	}

	if dash.KPI.TotalCalls != 3 {
		t.Fatalf("expected 3 total calls, got %d", dash.KPI.TotalCalls)
	}
	if dash.KPI.TotalTokens != 950_000 {
		t.Fatalf("expected 950000 total tokens, got %d", dash.KPI.TotalTokens)
	}
	// Expected MTD cost: 0.75 + 0.09 + 0.01 = $0.85
	if dash.KPI.ActualMTDCostUSD < 0.849 || dash.KPI.ActualMTDCostUSD > 0.851 {
		t.Fatalf("expected ActualMTDCostUSD ~0.85, got %.6f", dash.KPI.ActualMTDCostUSD)
	}
	if dash.KPI.DailyBurnRateUSD <= 0 {
		t.Fatalf("expected positive DailyBurnRateUSD, got %.6f", dash.KPI.DailyBurnRateUSD)
	}
	if dash.KPI.ForecastedMonthEndCostUSD < dash.KPI.ActualMTDCostUSD {
		t.Fatalf("expected ForecastedMonthEndCostUSD (%.6f) >= ActualMTDCostUSD (%.6f)",
			dash.KPI.ForecastedMonthEndCostUSD, dash.KPI.ActualMTDCostUSD)
	}
	if len(dash.ProviderBreakdown) != 2 {
		t.Fatalf("expected 2 providers in ProviderBreakdown, got %d", len(dash.ProviderBreakdown))
	}
	if dash.ProviderBreakdown[0].ProviderName != "OpenAI" {
		t.Fatalf("expected top provider OpenAI, got %s", dash.ProviderBreakdown[0].ProviderName)
	}
	if len(dash.ModelBreakdown) != 3 {
		t.Fatalf("expected 3 models in ModelBreakdown, got %d", len(dash.ModelBreakdown))
	}
	if len(dash.DailySeries) < 28 {
		t.Fatalf("expected full month DailySeries (>=28 days), got %d", len(dash.DailySeries))
	}

	// Verify today's point in DailySeries
	foundToday := false
	for _, pt := range dash.DailySeries {
		if pt.Date == todayStr {
			foundToday = true
			if pt.TotalTokens != 950_000 {
				t.Fatalf("expected today tokens 950000, got %d", pt.TotalTokens)
			}
		}
	}
	if !foundToday {
		t.Fatalf("today %s not found in DailySeries", todayStr)
	}

	// 5. Override pricing for gpt-4o and recalculate month
	_, err = svc.SavePricing(ctx, db, tenantID, SavePricingRequest{
		ProviderName:     "OpenAI",
		ModelName:        "gpt-4o",
		ModelType:        "chat",
		InputPricePer1M:  5.00,
		OutputPricePer1M: 20.00,
		RecalculateMonth: true,
	})
	if err != nil {
		t.Fatalf("SavePricing failed: %v", err)
	}

	dashAfter, err := svc.GetDashboardSummary(ctx, db, tenantID, TokenUsageFilter{
		Month:        monthStr,
		ProviderName: "OpenAI",
	})
	if err != nil {
		t.Fatalf("GetDashboardSummary after re-price failed: %v", err)
	}
	// gpt-4o cost doubled from $0.75 to $1.50, plus $0.01 embedding = $1.51
	if dashAfter.KPI.ActualMTDCostUSD < 1.509 || dashAfter.KPI.ActualMTDCostUSD > 1.511 {
		t.Fatalf("expected recalculated OpenAI cost ~1.51, got %.6f", dashAfter.KPI.ActualMTDCostUSD)
	}
}
