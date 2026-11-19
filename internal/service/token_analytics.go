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
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/tokenizer"
)

// TokenUsageFilter defines unified filter criteria for both Dashboard UI and Third-Party FinOps API.
type TokenUsageFilter struct {
	Month        string `form:"month" json:"month"`                 // YYYY-MM (default: current month)
	FromDate     string `form:"from_date" json:"from_date"`         // YYYY-MM-DD (optional override)
	ToDate       string `form:"to_date" json:"to_date"`             // YYYY-MM-DD (optional override)
	ProviderName string `form:"provider" json:"provider"`           // e.g. OpenAI, Gemini, DeepSeek
	ModelName    string `form:"model" json:"model"`                 // e.g. gpt-4o-mini
	ModelType    string `form:"model_type" json:"model_type"`       // chat, embedding, rerank, ocr, speech2text, tts
	AppType      string `form:"app_type" json:"app_type"`           // chat, agent, ingestion, retrieval
	AppID        string `form:"app_id" json:"app_id"`               // dialog_id or canvas_id
	Page         int    `form:"page" json:"page"`                   // for logs endpoint
	PageSize     int    `form:"page_size" json:"page_size"`         // for logs endpoint
}

type TokenDashboardKPI struct {
	SelectedMonth             string  `json:"selected_month"`
	TotalCalls                int64   `json:"total_calls"`
	InputTokens               int64   `json:"input_tokens"`
	OutputTokens              int64   `json:"output_tokens"`
	TotalTokens               int64   `json:"total_tokens"`
	AvgLatencyMS              int64   `json:"avg_latency_ms"`
	ActualMTDCostUSD          float64 `json:"actual_mtd_cost_usd"`
	ActualMTDCostVND          float64 `json:"actual_mtd_cost_vnd"`
	PrevMonthTokens           int64   `json:"prev_month_tokens"`
	PrevMonthCostUSD          float64 `json:"prev_month_cost_usd"`
	MoMTokenGrowthPct         float64 `json:"mom_token_growth_pct"`
	MoMCostGrowthPct          float64 `json:"mom_cost_growth_pct"`
	DailyBurnRateUSD          float64 `json:"daily_burn_rate_usd"`
	DailyBurnRateTokens       int64   `json:"daily_burn_rate_tokens"`
	ForecastedMonthEndCostUSD float64 `json:"forecasted_month_end_cost_usd"`
	ForecastedMonthEndCostVND float64 `json:"forecasted_month_end_cost_vnd"`
	ForecastedMonthEndTokens  int64   `json:"forecasted_month_end_tokens"`
	ForecastP90CostUSD        float64 `json:"forecast_p90_cost_usd"`
	ForecastNextMonthCostUSD  float64 `json:"forecast_next_month_cost_usd"`
	MonthlyBudgetUSD          float64 `json:"monthly_budget_usd"`
	AlertThresholdPct         float64 `json:"alert_threshold_pct"`
	BudgetUtilizationPct      float64 `json:"budget_utilization_pct"`
	ForecastedBudgetPct       float64 `json:"forecasted_budget_pct"`
	BudgetStatus              string  `json:"budget_status"` // "normal", "warning", "exceeded"
	EstimatedExhaustionDate   string  `json:"estimated_exhaustion_date"`
	TopProviderName           string  `json:"top_provider_name"`
	TopProviderCostUSD        float64 `json:"top_provider_cost_usd"`
	TopProviderSharePct       float64 `json:"top_provider_share_pct"`
	DisplayCurrency           string  `json:"display_currency"`
	ExchangeRateVND           float64 `json:"exchange_rate_vnd"`
	ElapsedDaysInMonth        int     `json:"elapsed_days_in_month"`
	TotalDaysInMonth          int     `json:"total_days_in_month"`
	RemainingDaysInMonth      int     `json:"remaining_days_in_month"`
}

type DailyTokenCostPoint struct {
	Date                      string  `json:"date"`
	DayOfMonth                int     `json:"day_of_month"`
	IsFuture                  bool    `json:"is_future"`
	Calls                     int64   `json:"calls"`
	InputTokens               int64   `json:"input_tokens"`
	OutputTokens              int64   `json:"output_tokens"`
	TotalTokens               int64   `json:"total_tokens"`
	DailyCostUSD              float64 `json:"daily_cost_usd"`
	CumulativeActualCostUSD   float64 `json:"cumulative_actual_cost_usd"`
	ForecastedDailyCostUSD    float64 `json:"forecasted_daily_cost_usd"`
	CumulativeForecastCostUSD float64 `json:"cumulative_forecast_cost_usd"`
	CumulativeP90CostUSD      float64 `json:"cumulative_p90_cost_usd"`
	MonthlyBudgetLineUSD      float64 `json:"monthly_budget_line_usd"`
}

type MonthlyTokenCostPoint struct {
	Month             string             `json:"month"`
	IsForecast        bool               `json:"is_forecast"`
	Calls             int64              `json:"calls"`
	InputTokens       int64              `json:"input_tokens"`
	OutputTokens      int64              `json:"output_tokens"`
	TotalTokens       int64              `json:"total_tokens"`
	ActualCostUSD     float64            `json:"actual_cost_usd"`
	ForecastedCostUSD float64            `json:"forecasted_cost_usd"`
	ByProviderCostUSD map[string]float64 `json:"by_provider_cost_usd"`
}

type ProviderTokenCostSummary struct {
	ProviderName              string  `json:"provider_name"`
	Calls                     int64   `json:"calls"`
	ModelCount                int     `json:"model_count"`
	InputTokens               int64   `json:"input_tokens"`
	OutputTokens              int64   `json:"output_tokens"`
	TotalTokens               int64   `json:"total_tokens"`
	InputCostUSD              float64 `json:"input_cost_usd"`
	OutputCostUSD             float64 `json:"output_cost_usd"`
	TotalCostUSD              float64 `json:"total_cost_usd"`
	CostSharePct              float64 `json:"cost_share_pct"`
	TokenSharePct             float64 `json:"token_share_pct"`
	DailyBurnRateUSD          float64 `json:"daily_burn_rate_usd"`
	ForecastedMonthEndCostUSD float64 `json:"forecasted_month_end_cost_usd"`
	ForecastP90CostUSD        float64 `json:"forecast_p90_cost_usd"`
	ForecastNextMonthCostUSD  float64 `json:"forecast_next_month_cost_usd"`
	ProviderBudgetUSD         float64 `json:"provider_budget_usd"`
	BudgetUtilizationPct      float64 `json:"budget_utilization_pct"`
	BudgetStatus              string  `json:"budget_status"`
	AvgLatencyMS              int64   `json:"avg_latency_ms"`
}

type ModelTokenCostSummary struct {
	ProviderName              string  `json:"provider_name"`
	ModelName                 string  `json:"model_name"`
	ModelType                 string  `json:"model_type"`
	Calls                     int64   `json:"calls"`
	InputTokens               int64   `json:"input_tokens"`
	OutputTokens              int64   `json:"output_tokens"`
	TotalTokens               int64   `json:"total_tokens"`
	InputPricePer1M           float64 `json:"input_price_per_1m"`
	OutputPricePer1M          float64 `json:"output_price_per_1m"`
	IsCustomPrice             bool    `json:"is_custom_price"`
	InputCostUSD              float64 `json:"input_cost_usd"`
	OutputCostUSD             float64 `json:"output_cost_usd"`
	TotalCostUSD              float64 `json:"total_cost_usd"`
	CostSharePct              float64 `json:"cost_share_pct"`
	DailyBurnRateUSD          float64 `json:"daily_burn_rate_usd"`
	ForecastedMonthEndCostUSD float64 `json:"forecasted_month_end_cost_usd"`
	AvgLatencyMS              int64   `json:"avg_latency_ms"`
}

type AppTokenCostSummary struct {
	AppID        string  `json:"app_id"`
	AppName      string  `json:"app_name"`
	AppType      string  `json:"app_type"`
	Calls        int64   `json:"calls"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	TotalTokens  int64   `json:"total_tokens"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	CostSharePct float64 `json:"cost_share_pct"`
}

type TokenAvailableFilters struct {
	Months     []string `json:"months"`
	Providers  []string `json:"providers"`
	Models     []string `json:"models"`
	ModelTypes []string `json:"model_types"`
	AppTypes   []string `json:"app_types"`
}

type TokenDashboardResponse struct {
	KPI               TokenDashboardKPI          `json:"kpi"`
	DailySeries       []DailyTokenCostPoint      `json:"daily_series"`
	MonthlyHistory    []MonthlyTokenCostPoint    `json:"monthly_history"`
	ProviderBreakdown []ProviderTokenCostSummary `json:"provider_breakdown"`
	ModelBreakdown    []ModelTokenCostSummary    `json:"model_breakdown"`
	AppBreakdown      []AppTokenCostSummary      `json:"app_breakdown"`
	AvailableFilters  TokenAvailableFilters      `json:"available_filters"`
}

type PricingCatalogEntry struct {
	ID               string  `json:"id"`
	ProviderName     string  `json:"provider_name"`
	ModelName        string  `json:"model_name"`
	ModelType        string  `json:"model_type"`
	InputPricePer1M  float64 `json:"input_price_per_1m"`
	OutputPricePer1M float64 `json:"output_price_per_1m"`
	Currency         string  `json:"currency"`
	IsCustom         bool    `json:"is_custom"`
	IsConfigured     bool    `json:"is_configured"`
}

type PricingAndBudgetResponse struct {
	PricingList     []PricingCatalogEntry   `json:"pricing_list"`
	Budgets         []entity.LLMBudgetConfig `json:"budgets"`
	GlobalBudget    entity.LLMBudgetConfig  `json:"global_budget"`
	DefaultCatalog  []dao.BuiltinPriceItem  `json:"default_catalog"`
}

type SavePricingRequest struct {
	ProviderName     string  `json:"provider_name" binding:"required"`
	ModelName        string  `json:"model_name" binding:"required"`
	ModelType        string  `json:"model_type"`
	InputPricePer1M  float64 `json:"input_price_per_1m"`
	OutputPricePer1M float64 `json:"output_price_per_1m"`
	RecalculateMonth bool    `json:"recalculate_month"`
}

type SaveBudgetRequest struct {
	ProviderName      string  `json:"provider_name"`
	MonthlyBudgetUSD  float64 `json:"monthly_budget_usd"`
	AlertThresholdPct float64 `json:"alert_threshold_pct"`
	DisplayCurrency   string  `json:"display_currency"`
	ExchangeRateVND   float64 `json:"exchange_rate_vnd"`
}

type TokenAnalyticsService struct {
	usageDAO     *dao.LLMUsageDAO
	writeSem     chan struct{}
	backfillOnce sync.Map
}

var (
	defaultTokenAnalyticsOnce sync.Once
	defaultTokenAnalyticsSvc  *TokenAnalyticsService
)

func init() {
	common.RegisterModelUsageRecorder(func(usage *common.ModelUsage) {
		DefaultTokenAnalyticsService().RecordUsageAsync(usage)
	})
}

func DefaultTokenAnalyticsService() *TokenAnalyticsService {
	defaultTokenAnalyticsOnce.Do(func() {
		defaultTokenAnalyticsSvc = NewTokenAnalyticsService()
	})
	return defaultTokenAnalyticsSvc
}

func NewTokenAnalyticsService() *TokenAnalyticsService {
	return &TokenAnalyticsService{
		usageDAO: dao.NewLLMUsageDAO(),
		writeSem: make(chan struct{}, 16),
	}
}

// RecordUsageAsync persists a completed ModelUsage record in a bounded background goroutine.
func (s *TokenAnalyticsService) RecordUsageAsync(usage *common.ModelUsage) {
	if s == nil || usage == nil || dao.DB == nil {
		return
	}
	cp := *usage
	select {
	case s.writeSem <- struct{}{}:
		go func() {
			defer func() { <-s.writeSem }()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.RecordUsageSync(ctx, dao.DB, &cp); err != nil {
				common.Debug("record llm usage log failed", zap.Error(err))
			}
		}()
	default:
		// Fallback inline with short timeout if concurrency semaphore is saturated
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.RecordUsageSync(ctx, dao.DB, &cp)
	}
}

// RecordUsageSync normalizes, prices, and writes one LLMUsageLog row to db.
func (s *TokenAnalyticsService) RecordUsageSync(ctx context.Context, db *gorm.DB, usage *common.ModelUsage) error {
	if s == nil || db == nil || usage == nil {
		return nil
	}
	now := time.Now().Truncate(time.Second)
	if !usage.StartAt.IsZero() {
		now = usage.StartAt.Truncate(time.Second)
	}
	nowMs := now.UnixMilli()

	providerName := strings.TrimSpace(usage.ProviderName)
	modelName := strings.TrimSpace(usage.ModelName)
	if strings.Contains(modelName, "@") {
		parts := strings.Split(modelName, "@")
		if len(parts) >= 2 {
			if providerName == "" {
				providerName = parts[len(parts)-1]
			}
			modelName = parts[0]
		}
	}
	if providerName == "" {
		providerName = "Unknown"
	}
	if modelName == "" {
		modelName = "unknown"
	}
	modelType := strings.ToLower(strings.TrimSpace(usage.Type))
	if modelType == "" {
		modelType = "chat"
	}
	appType := strings.ToLower(strings.TrimSpace(usage.AppType))
	if appType == "" {
		switch modelType {
		case "embedding", "ocr":
			appType = "ingestion"
		case "rerank":
			appType = "retrieval"
		default:
			appType = "chat"
		}
	}

	tenantID := strings.TrimSpace(usage.TenantID)
	if tenantID == "" {
		tenantID = strings.TrimSpace(usage.UserID)
	}
	if tenantID == "" {
		// Look up first owner tenant if background task did not carry tenant ID
		var t entity.Tenant
		if err := db.WithContext(ctx).Limit(1).First(&t).Error; err == nil && t.ID != "" {
			tenantID = t.ID
		}
	}
	if tenantID == "" {
		return nil
	}

	inTokens := int64(usage.InputTokens)
	outTokens := int64(usage.OutputTokens)
	totalTokens := int64(usage.TotalTokens)
	if totalTokens <= 0 {
		totalTokens = inTokens + outTokens
	}
	if totalTokens <= 0 {
		return nil
	}
	if inTokens <= 0 && outTokens <= 0 {
		outTokens = totalTokens
	}

	inPrice, outPrice := s.usageDAO.ResolveUnitPrice(ctx, db, tenantID, providerName, modelName, modelType)
	inCost, outCost, totalCost := dao.CalculateCostUSD(inTokens, outTokens, inPrice, outPrice)

	logRow := entity.LLMUsageLog{
		ID:             strings.ReplaceAll(uuid.NewString(), "-", ""),
		TenantID:       tenantID,
		UserID:         usage.UserID,
		AppID:          usage.AppID,
		AppName:        usage.AppName,
		AppType:        appType,
		SessionID:      usage.SessionID,
		ProviderName:   providerName,
		InstanceID:     usage.InstanceID,
		ModelName:      modelName,
		ModelType:      modelType,
		InputTokens:    inTokens,
		OutputTokens:   outTokens,
		TotalTokens:    totalTokens,
		InputCostUSD:   inCost,
		OutputCostUSD:  outCost,
		TotalCostUSD:   totalCost,
		ResponseTimeMS: usage.ResponseTimeMS,
		IsEstimated:    usage.IsEstimated,
		YearMonth:      now.Format("2006-01"),
		StatDate:       now.Format("2006-01-02"),
		BaseModel: entity.BaseModel{
			CreateTime: &nowMs,
			CreateDate: &now,
			UpdateTime: &nowMs,
			UpdateDate: &now,
		},
	}
	return db.WithContext(ctx).Create(&logRow).Error
}

// ensureHistoricalBackfill populates llm_usage_log from existing conversation_message and
// api_4_conversation_message rows if the tenant has zero usage logs so far.
func (s *TokenAnalyticsService) ensureHistoricalBackfill(ctx context.Context, db *gorm.DB, tenantID string) {
	if s == nil || db == nil || tenantID == "" {
		return
	}
	if _, loaded := s.backfillOnce.LoadOrStore(tenantID, true); loaded {
		return
	}
	var existingCount int64
	if err := db.WithContext(ctx).Model(&entity.LLMUsageLog{}).Where("tenant_id = ?", tenantID).Count(&existingCount).Error; err != nil || existingCount > 0 {
		return
	}

	// Resolve default provider & model for the tenant
	defaultProvider := "OpenAI"
	defaultModel := "gpt-4o-mini"
	var ut entity.UserTenant
	if err := db.WithContext(ctx).Where("tenant_id = ? AND role = 'owner'", tenantID).First(&ut).Error; err == nil && ut.LLMID != "" {
		mName, _, pName, parseErr := parseModelName(ut.LLMID)
		if parseErr == nil && mName != "" {
			defaultModel = mName
			if pName != "" {
				defaultProvider = pName
			}
		}
	}

	// Map dialog_id -> (name, provider, model)
	type appMeta struct {
		name     string
		provider string
		model    string
	}
	dialogMap := make(map[string]appMeta)
	var dialogs []entity.Chat
	_ = db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&dialogs).Error
	for _, d := range dialogs {
		prov, mdl := defaultProvider, defaultModel
		if d.LLMID != "" {
			mName, _, pName, err := parseModelName(d.LLMID)
			if err == nil && mName != "" {
				mdl = mName
				if pName != "" {
					prov = pName
				}
			}
		}
		dName := d.ID
		if d.Name != nil && strings.TrimSpace(*d.Name) != "" {
			dName = *d.Name
		}
		dialogMap[d.ID] = appMeta{name: dName, provider: prov, model: mdl}
	}

	canvasMap := make(map[string]appMeta)
	var canvases []entity.UserCanvas
	_ = db.WithContext(ctx).Where("user_id = ?", tenantID).Find(&canvases).Error
	for _, c := range canvases {
		cTitle := c.ID
		if c.Title != nil && strings.TrimSpace(*c.Title) != "" {
			cTitle = *c.Title
		}
		canvasMap[c.ID] = appMeta{name: cTitle, provider: defaultProvider, model: defaultModel}
	}

	var batch []entity.LLMUsageLog

	// 1. Backfill internal chat messages (assistant turns)
	var chatSessions []entity.ChatSession
	if len(dialogMap) > 0 {
		dialogIDs := make([]string, 0, len(dialogMap))
		for id := range dialogMap {
			dialogIDs = append(dialogIDs, id)
		}
		_ = db.WithContext(ctx).Select("id, dialog_id, user_id, create_date").Where("dialog_id IN ?", dialogIDs).Limit(200).Find(&chatSessions).Error
	}
	sessionToDialog := make(map[string]string, len(chatSessions))
	sessionToCreateDate := make(map[string]*time.Time, len(chatSessions))
	sessionIDs := make([]string, 0, len(chatSessions))
	for _, sess := range chatSessions {
		sessionToDialog[sess.ID] = sess.DialogID
		sessionToCreateDate[sess.ID] = sess.CreateDate
		sessionIDs = append(sessionIDs, sess.ID)
	}
	if len(sessionIDs) > 0 {
		var msgs []entity.ConversationMessage
		_ = db.WithContext(ctx).Where("conversation_id IN ?", sessionIDs).Order("conversation_id ASC, position ASC").Limit(1000).Find(&msgs).Error
		lastUserTokens := 40
		for _, m := range msgs {
			roleStr := ""
			if m.Role != nil {
				roleStr = *m.Role
			}
			contentStr := ""
			if m.Content != nil {
				contentStr = *m.Content
			}
			if strings.EqualFold(roleStr, "user") {
				tk := tokenizer.NumTokensFromString(contentStr)
				if tk > 0 {
					lastUserTokens = tk + 25
				}
				continue
			}
			if !strings.EqualFold(roleStr, "assistant") {
				continue
			}
			dlgID := sessionToDialog[m.ConversationID]
			meta, ok := dialogMap[dlgID]
			if !ok {
				meta = appMeta{name: "Chat Assistant", provider: defaultProvider, model: defaultModel}
			}
			inTk := int64(lastUserTokens)
			outTk := int64(tokenizer.NumTokensFromString(contentStr))
			if outTk <= 0 {
				outTk = 30
			}
			msgTime := time.Now().Truncate(time.Second)
			if m.CreatedAt != nil && *m.CreatedAt > 0 {
				sec := int64(*m.CreatedAt)
				if sec > 1e12 {
					msgTime = time.UnixMilli(sec).Truncate(time.Second)
				} else {
					msgTime = time.Unix(sec, 0).Truncate(time.Second)
				}
			} else if cd := sessionToCreateDate[m.ConversationID]; cd != nil && !cd.IsZero() {
				msgTime = *cd
			}
			inPrice, outPrice := s.usageDAO.ResolveUnitPrice(ctx, db, tenantID, meta.provider, meta.model, "chat")
			inCost, outCost, totalCost := dao.CalculateCostUSD(inTk, outTk, inPrice, outPrice)
			ms := msgTime.UnixMilli()
			batch = append(batch, entity.LLMUsageLog{
				ID:             strings.ReplaceAll(uuid.NewString(), "-", ""),
				TenantID:       tenantID,
				UserID:         tenantID,
				AppID:          dlgID,
				AppName:        meta.name,
				AppType:        "chat",
				SessionID:      m.ConversationID,
				ProviderName:   meta.provider,
				ModelName:      meta.model,
				ModelType:      "chat",
				InputTokens:    inTk,
				OutputTokens:   outTk,
				TotalTokens:    inTk + outTk,
				InputCostUSD:   inCost,
				OutputCostUSD:  outCost,
				TotalCostUSD:   totalCost,
				ResponseTimeMS: 850,
				IsEstimated:    true,
				YearMonth:      msgTime.Format("2006-01"),
				StatDate:       msgTime.Format("2006-01-02"),
				BaseModel: entity.BaseModel{
					CreateTime: &ms,
					CreateDate: &msgTime,
					UpdateTime: &ms,
					UpdateDate: &msgTime,
				},
			})
		}
	}

	// 2. Backfill API / Agent messages
	var apiConvs []entity.API4Conversation
	_ = db.WithContext(ctx).Select("id, dialog_id, user_id, source, create_date").Where("user_id = ?", tenantID).Limit(200).Find(&apiConvs).Error
	if len(apiConvs) > 0 {
		apiConvMap := make(map[string]entity.API4Conversation, len(apiConvs))
		apiConvIDs := make([]string, 0, len(apiConvs))
		for _, ac := range apiConvs {
			apiConvMap[ac.ID] = ac
			apiConvIDs = append(apiConvIDs, ac.ID)
		}
		var apiMsgs []entity.API4ConversationMessage
		_ = db.WithContext(ctx).Where("conversation_id IN ?", apiConvIDs).Order("conversation_id ASC, position ASC").Limit(1000).Find(&apiMsgs).Error
		lastUserTokens := 50
		for _, m := range apiMsgs {
			roleStr := ""
			if m.Role != nil {
				roleStr = *m.Role
			}
			contentStr := ""
			if m.Content != nil {
				contentStr = *m.Content
			}
			if strings.EqualFold(roleStr, "user") {
				tk := tokenizer.NumTokensFromString(contentStr)
				if tk > 0 {
					lastUserTokens = tk + 30
				}
				continue
			}
			if !strings.EqualFold(roleStr, "assistant") {
				continue
			}
			ac := apiConvMap[m.ConversationID]
			appType := "chat"
			meta := appMeta{name: "Chat / API", provider: defaultProvider, model: defaultModel}
			if ac.Source != nil && strings.EqualFold(*ac.Source, "agent") {
				appType = "agent"
				if cm, ok := canvasMap[ac.DialogID]; ok {
					meta = cm
				} else {
					meta.name = "Agent Workflow"
				}
			} else if dm, ok := dialogMap[ac.DialogID]; ok {
				meta = dm
			}
			inTk := int64(lastUserTokens)
			outTk := int64(tokenizer.NumTokensFromString(contentStr))
			if outTk <= 0 {
				outTk = 35
			}
			msgTime := time.Now().Truncate(time.Second)
			if m.CreatedAt != nil && *m.CreatedAt > 0 {
				sec := int64(*m.CreatedAt)
				if sec > 1e12 {
					msgTime = time.UnixMilli(sec).Truncate(time.Second)
				} else {
					msgTime = time.Unix(sec, 0).Truncate(time.Second)
				}
			} else if ac.CreateDate != nil && !ac.CreateDate.IsZero() {
				msgTime = *ac.CreateDate
			}
			inPrice, outPrice := s.usageDAO.ResolveUnitPrice(ctx, db, tenantID, meta.provider, meta.model, "chat")
			inCost, outCost, totalCost := dao.CalculateCostUSD(inTk, outTk, inPrice, outPrice)
			ms := msgTime.UnixMilli()
			batch = append(batch, entity.LLMUsageLog{
				ID:             strings.ReplaceAll(uuid.NewString(), "-", ""),
				TenantID:       tenantID,
				UserID:         tenantID,
				AppID:          ac.DialogID,
				AppName:        meta.name,
				AppType:        appType,
				SessionID:      m.ConversationID,
				ProviderName:   meta.provider,
				ModelName:      meta.model,
				ModelType:      "chat",
				InputTokens:    inTk,
				OutputTokens:   outTk,
				TotalTokens:    inTk + outTk,
				InputCostUSD:   inCost,
				OutputCostUSD:  outCost,
				TotalCostUSD:   totalCost,
				ResponseTimeMS: 920,
				IsEstimated:    true,
				YearMonth:      msgTime.Format("2006-01"),
				StatDate:       msgTime.Format("2006-01-02"),
				BaseModel: entity.BaseModel{
					CreateTime: &ms,
					CreateDate: &msgTime,
					UpdateTime: &ms,
					UpdateDate: &msgTime,
				},
			})
		}
	}

	if len(batch) > 0 {
		_ = s.usageDAO.InsertBatch(ctx, db, batch)
	}
}

func toInt64(v interface{}) (int64, bool) {
	switch val := v.(type) {
	case int:
		return int64(val), true
	case int64:
		return val, true
	case float64:
		return int64(val), true
	default:
		return 0, false
	}
}

// GetDashboardSummary computes the full FinOps dashboard metrics, provider/model breakdowns,
// daily actual + forecasted curves, and monthly projections.
func (s *TokenAnalyticsService) GetDashboardSummary(ctx context.Context, db *gorm.DB, tenantID string, filter TokenUsageFilter) (*TokenDashboardResponse, error) {
	if db == nil {
		db = dao.DB
	}
	s.ensureHistoricalBackfill(ctx, db, tenantID)

	now := time.Now()
	selectedMonth := strings.TrimSpace(filter.Month)
	if selectedMonth == "" || len(selectedMonth) != 7 {
		selectedMonth = now.Format("2006-01")
	}

	monthStart, err := time.ParseInLocation("2006-01", selectedMonth, now.Location())
	if err != nil {
		monthStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		selectedMonth = monthStart.Format("2006-01")
	}
	nextMonthStart := monthStart.AddDate(0, 1, 0)
	monthEnd := nextMonthStart.AddDate(0, 0, -1)
	totalDaysInMonth := monthEnd.Day()

	elapsedDays := totalDaysInMonth
	isCurrentMonth := selectedMonth == now.Format("2006-01")
	isFutureMonth := selectedMonth > now.Format("2006-01")
	if isCurrentMonth {
		elapsedDays = now.Day()
		if elapsedDays < 1 {
			elapsedDays = 1
		}
	} else if isFutureMonth {
		elapsedDays = 0
	}
	remainingDays := totalDaysInMonth - elapsedDays
	if remainingDays < 0 {
		remainingDays = 0
	}

	// Load tenant budgets & currency config
	budgets, _ := s.usageDAO.ListBudgetConfigs(ctx, db, tenantID)
	globalBudget := entity.LLMBudgetConfig{
		TenantID:          tenantID,
		ProviderName:      "ALL",
		MonthlyBudgetUSD:  100.0,
		AlertThresholdPct: 80.0,
		DisplayCurrency:   "USD",
		ExchangeRateVND:   25500.0,
	}
	providerBudgetMap := make(map[string]entity.LLMBudgetConfig)
	for _, b := range budgets {
		if strings.EqualFold(b.ProviderName, "ALL") || b.ProviderName == "" {
			globalBudget = b
			if globalBudget.ExchangeRateVND <= 0 {
				globalBudget.ExchangeRateVND = 25500.0
			}
			if globalBudget.AlertThresholdPct <= 0 {
				globalBudget.AlertThresholdPct = 80.0
			}
		} else {
			providerBudgetMap[strings.ToLower(b.ProviderName)] = b
		}
	}

	// Query logs for the selected month (or custom date range)
	baseQuery := func(q *gorm.DB) *gorm.DB {
		q = q.Where("tenant_id = ?", tenantID)
		if filter.ProviderName != "" && !strings.EqualFold(filter.ProviderName, "all") {
			q = q.Where("LOWER(provider_name) = LOWER(?)", strings.TrimSpace(filter.ProviderName))
		}
		if filter.ModelName != "" && !strings.EqualFold(filter.ModelName, "all") {
			q = q.Where("LOWER(model_name) = LOWER(?)", strings.TrimSpace(filter.ModelName))
		}
		if filter.ModelType != "" && !strings.EqualFold(filter.ModelType, "all") {
			q = q.Where("LOWER(model_type) = LOWER(?)", strings.TrimSpace(filter.ModelType))
		}
		if filter.AppType != "" && !strings.EqualFold(filter.AppType, "all") {
			q = q.Where("LOWER(app_type) = LOWER(?)", strings.TrimSpace(filter.AppType))
		}
		if filter.AppID != "" && !strings.EqualFold(filter.AppID, "all") {
			q = q.Where("app_id = ?", strings.TrimSpace(filter.AppID))
		}
		return q
	}

	monthQuery := baseQuery(db.WithContext(ctx).Model(&entity.LLMUsageLog{}))
	if filter.FromDate != "" && filter.ToDate != "" {
		monthQuery = monthQuery.Where("stat_date >= ? AND stat_date <= ?", filter.FromDate[:10], filter.ToDate[:10])
	} else {
		monthQuery = monthQuery.Where("`year_month` = ?", selectedMonth)
	}

	var monthLogs []entity.LLMUsageLog
	if err := monthQuery.Order("stat_date ASC, create_time ASC").Find(&monthLogs).Error; err != nil {
		return nil, err
	}

	// Also query previous month for MoM comparison
	prevMonthStr := monthStart.AddDate(0, -1, 0).Format("2006-01")
	var prevMonthAgg struct {
		TotalTokens  int64
		TotalCostUSD float64
	}
	_ = baseQuery(db.WithContext(ctx).Model(&entity.LLMUsageLog{})).
		Select("COALESCE(SUM(total_tokens), 0) AS total_tokens, COALESCE(SUM(total_cost_usd), 0) AS total_cost_usd").
		Where("`year_month` = ?", prevMonthStr).
		Scan(&prevMonthAgg).Error

	// Aggregate selected month logs
	var (
		totalCalls     int64
		inputTokens    int64
		outputTokens   int64
		totalTokens    int64
		totalLatencyMS int64
		actualCostUSD  float64
	)

	type dayAgg struct {
		calls        int64
		inputTokens  int64
		outputTokens int64
		totalTokens  int64
		costUSD      float64
	}
	dailyMap := make(map[string]*dayAgg)

	type provAgg struct {
		providerName string
		calls        int64
		models       map[string]struct{}
		inputTokens  int64
		outputTokens int64
		totalTokens  int64
		inputCost    float64
		outputCost   float64
		totalCost    float64
		latencySum   int64
		dailyCosts   map[int]float64 // dayOfMonth -> cost
	}
	provMap := make(map[string]*provAgg)

	type modelKey struct {
		provider  string
		model     string
		modelType string
	}
	type mdlAgg struct {
		key          modelKey
		calls        int64
		inputTokens  int64
		outputTokens int64
		totalTokens  int64
		inputCost    float64
		outputCost   float64
		totalCost    float64
		latencySum   int64
		dailyCosts   map[int]float64
	}
	modelMap := make(map[modelKey]*mdlAgg)

	type appKey struct {
		appType string
		appID   string
	}
	type appAgg struct {
		appID        string
		appName      string
		appType      string
		calls        int64
		inputTokens  int64
		outputTokens int64
		totalTokens  int64
		totalCost    float64
	}
	appMap := make(map[appKey]*appAgg)

	for _, row := range monthLogs {
		totalCalls++
		inputTokens += row.InputTokens
		outputTokens += row.OutputTokens
		totalTokens += row.TotalTokens
		totalLatencyMS += row.ResponseTimeMS
		actualCostUSD += row.TotalCostUSD

		dAgg, ok := dailyMap[row.StatDate]
		if !ok {
			dAgg = &dayAgg{}
			dailyMap[row.StatDate] = dAgg
		}
		dAgg.calls++
		dAgg.inputTokens += row.InputTokens
		dAgg.outputTokens += row.OutputTokens
		dAgg.totalTokens += row.TotalTokens
		dAgg.costUSD += row.TotalCostUSD

		dayNum := 1
		if len(row.StatDate) >= 10 {
			if parsed, pErr := time.Parse("2006-01-02", row.StatDate[:10]); pErr == nil {
				dayNum = parsed.Day()
			}
		}

		pKey := strings.ToLower(row.ProviderName)
		pAgg, ok := provMap[pKey]
		if !ok {
			pAgg = &provAgg{
				providerName: row.ProviderName,
				models:       make(map[string]struct{}),
				dailyCosts:   make(map[int]float64),
			}
			provMap[pKey] = pAgg
		}
		pAgg.calls++
		pAgg.models[strings.ToLower(row.ModelName)] = struct{}{}
		pAgg.inputTokens += row.InputTokens
		pAgg.outputTokens += row.OutputTokens
		pAgg.totalTokens += row.TotalTokens
		pAgg.inputCost += row.InputCostUSD
		pAgg.outputCost += row.OutputCostUSD
		pAgg.totalCost += row.TotalCostUSD
		pAgg.latencySum += row.ResponseTimeMS
		pAgg.dailyCosts[dayNum] += row.TotalCostUSD

		mKey := modelKey{
			provider:  row.ProviderName,
			model:     row.ModelName,
			modelType: row.ModelType,
		}
		mAgg, ok := modelMap[mKey]
		if !ok {
			mAgg = &mdlAgg{
				key:        mKey,
				dailyCosts: make(map[int]float64),
			}
			modelMap[mKey] = mAgg
		}
		mAgg.calls++
		mAgg.inputTokens += row.InputTokens
		mAgg.outputTokens += row.OutputTokens
		mAgg.totalTokens += row.TotalTokens
		mAgg.inputCost += row.InputCostUSD
		mAgg.outputCost += row.OutputCostUSD
		mAgg.totalCost += row.TotalCostUSD
		mAgg.latencySum += row.ResponseTimeMS
		mAgg.dailyCosts[dayNum] += row.TotalCostUSD

		aKey := appKey{appType: row.AppType, appID: row.AppID}
		aAgg, ok := appMap[aKey]
		if !ok {
			aName := row.AppName
			if aName == "" {
				if row.AppType == "agent" {
					aName = "Agent Workflow"
				} else if row.AppType == "ingestion" {
					aName = "Document Ingestion"
				} else {
					aName = "Chat Assistant"
				}
			}
			aAgg = &appAgg{
				appID:   row.AppID,
				appName: aName,
				appType: row.AppType,
			}
			appMap[aKey] = aAgg
		}
		aAgg.calls++
		aAgg.inputTokens += row.InputTokens
		aAgg.outputTokens += row.OutputTokens
		aAgg.totalTokens += row.TotalTokens
		aAgg.totalCost += row.TotalCostUSD
	}

	// Forecasting helper: computes EWMA daily burn rate and standard deviation across elapsed days
	computeForecast := func(dailyCosts map[int]float64, mtdCost float64) (burnRate, forecastedMonthEnd, forecastP90, forecastNextMonth float64) {
		if elapsedDays <= 0 {
			return 0, mtdCost, mtdCost, 0
		}
		if mtdCost <= 0 {
			return 0, 0, 0, 0
		}
		mtdAvg := mtdCost / float64(elapsedDays)

		// Compute 7-day recent window average & EWMA over active window
		windowDays := 7
		if elapsedDays < windowDays {
			windowDays = elapsedDays
		}
		recentSum := 0.0
		activeDaysCount := 0
		for d := elapsedDays - windowDays + 1; d <= elapsedDays; d++ {
			c := dailyCosts[d]
			recentSum += c
			if c > 0 {
				activeDaysCount++
			}
		}
		recentAvg := recentSum / float64(windowDays)

		// If usage just started today (or only 1-2 active days in the month so far),
		// avoid dividing a single day's test usage by 25 empty prior days without weighting today's velocity
		if activeDaysCount > 0 && elapsedDays > 3 && len(dailyCosts) <= 2 {
			activeAvg := mtdCost / float64(len(dailyCosts))
			burnRate = 0.55*activeAvg + 0.45*mtdAvg
		} else {
			burnRate = 0.65*recentAvg + 0.35*mtdAvg
		}

		// Standard deviation for P90 confidence interval
		var varianceSum float64
		for d := 1; d <= elapsedDays; d++ {
			diff := dailyCosts[d] - mtdAvg
			varianceSum += diff * diff
		}
		stdDev := math.Sqrt(varianceSum / float64(max(elapsedDays, 1)))
		if stdDev < burnRate*0.15 {
			stdDev = burnRate * 0.15
		}

		forecastedMonthEnd = mtdCost + burnRate*float64(remainingDays)
		forecastP90 = forecastedMonthEnd + 1.28*stdDev*math.Sqrt(float64(max(remainingDays, 1)))
		daysNextMonth := nextMonthStart.AddDate(0, 1, -1).Day()
		forecastNextMonth = burnRate * float64(daysNextMonth)
		return burnRate, forecastedMonthEnd, forecastP90, forecastNextMonth
	}

	globalDailyCosts := make(map[int]float64, elapsedDays)
	for d := 1; d <= totalDaysInMonth; d++ {
		dateStr := fmt.Sprintf("%s-%02d", selectedMonth, d)
		if da, ok := dailyMap[dateStr]; ok {
			globalDailyCosts[d] = da.costUSD
		}
	}

	dailyBurnUSD, forecastedMonthEndUSD, forecastP90USD, forecastNextMonthUSD := computeForecast(globalDailyCosts, actualCostUSD)
	var dailyBurnTokens int64
	if elapsedDays > 0 {
		if len(dailyMap) > 0 && len(dailyMap) <= 2 && elapsedDays > 3 {
			dailyBurnTokens = int64(0.55*(float64(totalTokens)/float64(len(dailyMap))) + 0.45*(float64(totalTokens)/float64(elapsedDays)))
		} else {
			dailyBurnTokens = totalTokens / int64(elapsedDays)
		}
	}
	forecastedMonthEndTokens := totalTokens + dailyBurnTokens*int64(remainingDays)

	// Build DailySeries (1..totalDaysInMonth)
	dailySeries := make([]DailyTokenCostPoint, 0, totalDaysInMonth)
	var runningActual float64
	var runningForecast float64
	stdDevDaily := 0.0
	if remainingDays > 0 && forecastP90USD > forecastedMonthEndUSD {
		stdDevDaily = (forecastP90USD - forecastedMonthEndUSD) / (1.28 * math.Sqrt(float64(remainingDays)))
	}
	futureDayIdx := 0
	for d := 1; d <= totalDaysInMonth; d++ {
		dateStr := fmt.Sprintf("%s-%02d", selectedMonth, d)
		isFuture := isCurrentMonth && d > elapsedDays
		da := dailyMap[dateStr]
		var calls, inTk, outTk, totTk int64
		var dCost float64
		if da != nil {
			calls = da.calls
			inTk = da.inputTokens
			outTk = da.outputTokens
			totTk = da.totalTokens
			dCost = da.costUSD
		}

		pt := DailyTokenCostPoint{
			Date:                 dateStr,
			DayOfMonth:           d,
			IsFuture:             isFuture,
			Calls:                calls,
			InputTokens:          inTk,
			OutputTokens:         outTk,
			TotalTokens:          totTk,
			DailyCostUSD:         roundFloat(dCost, 6),
			MonthlyBudgetLineUSD: roundFloat(globalBudget.MonthlyBudgetUSD, 4),
		}
		if !isFuture {
			runningActual += dCost
			runningForecast = runningActual
			pt.CumulativeActualCostUSD = roundFloat(runningActual, 6)
			if d == elapsedDays {
				pt.CumulativeForecastCostUSD = roundFloat(runningForecast, 6)
				pt.CumulativeP90CostUSD = roundFloat(runningForecast, 6)
			}
		} else {
			futureDayIdx++
			runningForecast += dailyBurnUSD
			p90 := runningForecast + 1.28*stdDevDaily*math.Sqrt(float64(futureDayIdx))
			pt.ForecastedDailyCostUSD = roundFloat(dailyBurnUSD, 6)
			pt.CumulativeForecastCostUSD = roundFloat(runningForecast, 6)
			pt.CumulativeP90CostUSD = roundFloat(p90, 6)
		}
		dailySeries = append(dailySeries, pt)
	}

	// Build ProviderBreakdown
	providerBreakdown := make([]ProviderTokenCostSummary, 0, len(provMap))
	for _, p := range provMap {
		pBurn, pForecastEnd, pP90, pNext := computeForecast(p.dailyCosts, p.totalCost)
		var costShare, tokenShare float64
		if actualCostUSD > 0 {
			costShare = (p.totalCost / actualCostUSD) * 100.0
		}
		if totalTokens > 0 {
			tokenShare = (float64(p.totalTokens) / float64(totalTokens)) * 100.0
		}
		pBudget := 0.0
		pAlertPct := globalBudget.AlertThresholdPct
		if pb, ok := providerBudgetMap[strings.ToLower(p.providerName)]; ok {
			pBudget = pb.MonthlyBudgetUSD
			if pb.AlertThresholdPct > 0 {
				pAlertPct = pb.AlertThresholdPct
			}
		}
		var pBudgetUtil float64
		pStatus := "normal"
		if pBudget > 0 {
			pBudgetUtil = (p.totalCost / pBudget) * 100.0
			if p.totalCost >= pBudget || pForecastEnd >= pBudget {
				pStatus = "exceeded"
			} else if pBudgetUtil >= pAlertPct || (pForecastEnd/pBudget)*100.0 >= pAlertPct {
				pStatus = "warning"
			}
		}
		var avgLat int64
		if p.calls > 0 {
			avgLat = p.latencySum / p.calls
		}
		providerBreakdown = append(providerBreakdown, ProviderTokenCostSummary{
			ProviderName:              p.providerName,
			Calls:                     p.calls,
			ModelCount:                len(p.models),
			InputTokens:               p.inputTokens,
			OutputTokens:              p.outputTokens,
			TotalTokens:               p.totalTokens,
			InputCostUSD:              roundFloat(p.inputCost, 6),
			OutputCostUSD:             roundFloat(p.outputCost, 6),
			TotalCostUSD:              roundFloat(p.totalCost, 6),
			CostSharePct:              roundFloat(costShare, 2),
			TokenSharePct:             roundFloat(tokenShare, 2),
			DailyBurnRateUSD:          roundFloat(pBurn, 6),
			ForecastedMonthEndCostUSD: roundFloat(pForecastEnd, 6),
			ForecastP90CostUSD:        roundFloat(pP90, 6),
			ForecastNextMonthCostUSD:  roundFloat(pNext, 6),
			ProviderBudgetUSD:         roundFloat(pBudget, 4),
			BudgetUtilizationPct:      roundFloat(pBudgetUtil, 2),
			BudgetStatus:              pStatus,
			AvgLatencyMS:              avgLat,
		})
	}
	sort.Slice(providerBreakdown, func(i, j int) bool {
		if providerBreakdown[i].TotalCostUSD != providerBreakdown[j].TotalCostUSD {
			return providerBreakdown[i].TotalCostUSD > providerBreakdown[j].TotalCostUSD
		}
		return providerBreakdown[i].TotalTokens > providerBreakdown[j].TotalTokens
	})

	// Build ModelBreakdown
	customPrices, _ := s.usageDAO.ListCustomPricing(ctx, db, tenantID)
	customPriceMap := make(map[string]entity.LLMPricingConfig, len(customPrices))
	for _, cp := range customPrices {
		customPriceMap[strings.ToLower(cp.ProviderName+"|"+cp.ModelName)] = cp
	}

	modelBreakdown := make([]ModelTokenCostSummary, 0, len(modelMap))
	for _, m := range modelMap {
		mBurn, mForecastEnd, _, _ := computeForecast(m.dailyCosts, m.totalCost)
		inPrice, outPrice := s.usageDAO.ResolveUnitPrice(ctx, db, tenantID, m.key.provider, m.key.model, m.key.modelType)
		_, isCustom := customPriceMap[strings.ToLower(m.key.provider+"|"+m.key.model)]
		var costShare float64
		if actualCostUSD > 0 {
			costShare = (m.totalCost / actualCostUSD) * 100.0
		}
		var avgLat int64
		if m.calls > 0 {
			avgLat = m.latencySum / m.calls
		}
		modelBreakdown = append(modelBreakdown, ModelTokenCostSummary{
			ProviderName:              m.key.provider,
			ModelName:                 m.key.model,
			ModelType:                 m.key.modelType,
			Calls:                     m.calls,
			InputTokens:               m.inputTokens,
			OutputTokens:              m.outputTokens,
			TotalTokens:               m.totalTokens,
			InputPricePer1M:           inPrice,
			OutputPricePer1M:          outPrice,
			IsCustomPrice:             isCustom,
			InputCostUSD:              roundFloat(m.inputCost, 6),
			OutputCostUSD:             roundFloat(m.outputCost, 6),
			TotalCostUSD:              roundFloat(m.totalCost, 6),
			CostSharePct:              roundFloat(costShare, 2),
			DailyBurnRateUSD:          roundFloat(mBurn, 6),
			ForecastedMonthEndCostUSD: roundFloat(mForecastEnd, 6),
			AvgLatencyMS:              avgLat,
		})
	}
	sort.Slice(modelBreakdown, func(i, j int) bool {
		if modelBreakdown[i].TotalCostUSD != modelBreakdown[j].TotalCostUSD {
			return modelBreakdown[i].TotalCostUSD > modelBreakdown[j].TotalCostUSD
		}
		return modelBreakdown[i].TotalTokens > modelBreakdown[j].TotalTokens
	})

	// Build AppBreakdown
	appBreakdown := make([]AppTokenCostSummary, 0, len(appMap))
	for _, a := range appMap {
		var costShare float64
		if actualCostUSD > 0 {
			costShare = (a.totalCost / actualCostUSD) * 100.0
		}
		appBreakdown = append(appBreakdown, AppTokenCostSummary{
			AppID:        a.appID,
			AppName:      a.appName,
			AppType:      a.appType,
			Calls:        a.calls,
			InputTokens:  a.inputTokens,
			OutputTokens: a.outputTokens,
			TotalTokens:  a.totalTokens,
			TotalCostUSD: roundFloat(a.totalCost, 6),
			CostSharePct: roundFloat(costShare, 2),
		})
	}
	sort.Slice(appBreakdown, func(i, j int) bool {
		return appBreakdown[i].TotalCostUSD > appBreakdown[j].TotalCostUSD
	})

	// Build MonthlyHistory (last 5 months + selected month + next month forecast)
	monthlyHistory := s.buildMonthlyHistory(ctx, db, tenantID, monthStart, forecastedMonthEndUSD, forecastNextMonthUSD, providerBreakdown)

	// Compute KPI summary fields
	var avgLatMS int64
	if totalCalls > 0 {
		avgLatMS = totalLatencyMS / totalCalls
	}
	var momTokenPct, momCostPct float64
	if prevMonthAgg.TotalTokens > 0 {
		momTokenPct = ((float64(totalTokens) - float64(prevMonthAgg.TotalTokens)) / float64(prevMonthAgg.TotalTokens)) * 100.0
	}
	if prevMonthAgg.TotalCostUSD > 0 {
		momCostPct = ((actualCostUSD - prevMonthAgg.TotalCostUSD) / prevMonthAgg.TotalCostUSD) * 100.0
	}

	var budgetUtilPct, forecastBudgetPct float64
	budgetStatus := "normal"
	exhaustionDate := ""
	if globalBudget.MonthlyBudgetUSD > 0 {
		budgetUtilPct = (actualCostUSD / globalBudget.MonthlyBudgetUSD) * 100.0
		forecastBudgetPct = (forecastedMonthEndUSD / globalBudget.MonthlyBudgetUSD) * 100.0
		if actualCostUSD >= globalBudget.MonthlyBudgetUSD || forecastedMonthEndUSD >= globalBudget.MonthlyBudgetUSD {
			budgetStatus = "exceeded"
		} else if budgetUtilPct >= globalBudget.AlertThresholdPct || forecastBudgetPct >= globalBudget.AlertThresholdPct {
			budgetStatus = "warning"
		}
		if dailyBurnUSD > 0 && actualCostUSD < globalBudget.MonthlyBudgetUSD {
			daysToExhaust := int(math.Ceil((globalBudget.MonthlyBudgetUSD - actualCostUSD) / dailyBurnUSD))
			if daysToExhaust <= remainingDays {
				exhaustionDate = time.Date(monthStart.Year(), monthStart.Month(), elapsedDays, 0, 0, 0, 0, now.Location()).
					AddDate(0, 0, daysToExhaust).Format("2006-01-02")
			}
		} else if actualCostUSD >= globalBudget.MonthlyBudgetUSD {
			exhaustionDate = now.Format("2006-01-02")
		}
	}

	topProvName := "-"
	topProvCost := 0.0
	topProvShare := 0.0
	if len(providerBreakdown) > 0 {
		topProvName = providerBreakdown[0].ProviderName
		topProvCost = providerBreakdown[0].TotalCostUSD
		topProvShare = providerBreakdown[0].CostSharePct
	}

	availFilters := s.buildAvailableFilters(ctx, db, tenantID, selectedMonth)

	return &TokenDashboardResponse{
		KPI: TokenDashboardKPI{
			SelectedMonth:             selectedMonth,
			TotalCalls:                totalCalls,
			InputTokens:               inputTokens,
			OutputTokens:              outputTokens,
			TotalTokens:               totalTokens,
			AvgLatencyMS:              avgLatMS,
			ActualMTDCostUSD:          roundFloat(actualCostUSD, 6),
			ActualMTDCostVND:          roundFloat(actualCostUSD*globalBudget.ExchangeRateVND, 0),
			PrevMonthTokens:           prevMonthAgg.TotalTokens,
			PrevMonthCostUSD:          roundFloat(prevMonthAgg.TotalCostUSD, 6),
			MoMTokenGrowthPct:         roundFloat(momTokenPct, 2),
			MoMCostGrowthPct:          roundFloat(momCostPct, 2),
			DailyBurnRateUSD:          roundFloat(dailyBurnUSD, 6),
			DailyBurnRateTokens:       dailyBurnTokens,
			ForecastedMonthEndCostUSD: roundFloat(forecastedMonthEndUSD, 6),
			ForecastedMonthEndCostVND: roundFloat(forecastedMonthEndUSD*globalBudget.ExchangeRateVND, 0),
			ForecastedMonthEndTokens:  forecastedMonthEndTokens,
			ForecastP90CostUSD:        roundFloat(forecastP90USD, 6),
			ForecastNextMonthCostUSD:  roundFloat(forecastNextMonthUSD, 6),
			MonthlyBudgetUSD:          roundFloat(globalBudget.MonthlyBudgetUSD, 4),
			AlertThresholdPct:         roundFloat(globalBudget.AlertThresholdPct, 2),
			BudgetUtilizationPct:      roundFloat(budgetUtilPct, 2),
			ForecastedBudgetPct:       roundFloat(forecastBudgetPct, 2),
			BudgetStatus:              budgetStatus,
			EstimatedExhaustionDate:   exhaustionDate,
			TopProviderName:           topProvName,
			TopProviderCostUSD:        roundFloat(topProvCost, 6),
			TopProviderSharePct:       roundFloat(topProvShare, 2),
			DisplayCurrency:           globalBudget.DisplayCurrency,
			ExchangeRateVND:           globalBudget.ExchangeRateVND,
			ElapsedDaysInMonth:        elapsedDays,
			TotalDaysInMonth:          totalDaysInMonth,
			RemainingDaysInMonth:      remainingDays,
		},
		DailySeries:       dailySeries,
		MonthlyHistory:    monthlyHistory,
		ProviderBreakdown: providerBreakdown,
		ModelBreakdown:    modelBreakdown,
		AppBreakdown:      appBreakdown,
		AvailableFilters:  availFilters,
	}, nil
}

func (s *TokenAnalyticsService) buildMonthlyHistory(
	ctx context.Context,
	db *gorm.DB,
	tenantID string,
	selectedMonthStart time.Time,
	selectedMonthForecastUSD float64,
	nextMonthForecastUSD float64,
	providerBreakdown []ProviderTokenCostSummary,
) []MonthlyTokenCostPoint {
	type mRow struct {
		YearMonth    string
		ProviderName string
		Calls        int64
		InputTokens  int64
		OutputTokens int64
		TotalTokens  int64
		TotalCostUSD float64
	}
	startMonth := selectedMonthStart.AddDate(0, -5, 0).Format("2006-01")
	endMonth := selectedMonthStart.Format("2006-01")

	var rows []mRow
	_ = db.WithContext(ctx).Model(&entity.LLMUsageLog{}).
		Select("`year_month`, provider_name, COUNT(*) AS calls, COALESCE(SUM(input_tokens), 0) AS input_tokens, COALESCE(SUM(output_tokens), 0) AS output_tokens, COALESCE(SUM(total_tokens), 0) AS total_tokens, COALESCE(SUM(total_cost_usd), 0) AS total_cost_usd").
		Where("tenant_id = ? AND `year_month` >= ? AND `year_month` <= ?", tenantID, startMonth, endMonth).
		Group("`year_month`, provider_name").
		Find(&rows).Error

	byMonth := make(map[string]*MonthlyTokenCostPoint)
	for i := -5; i <= 0; i++ {
		mStr := selectedMonthStart.AddDate(0, i, 0).Format("2006-01")
		byMonth[mStr] = &MonthlyTokenCostPoint{
			Month:             mStr,
			IsForecast:        false,
			ByProviderCostUSD: make(map[string]float64),
		}
	}
	for _, r := range rows {
		pt, ok := byMonth[r.YearMonth]
		if !ok {
			continue
		}
		pt.Calls += r.Calls
		pt.InputTokens += r.InputTokens
		pt.OutputTokens += r.OutputTokens
		pt.TotalTokens += r.TotalTokens
		pt.ActualCostUSD += r.TotalCostUSD
		pt.ByProviderCostUSD[r.ProviderName] = roundFloat(pt.ByProviderCostUSD[r.ProviderName]+r.TotalCostUSD, 6)
	}

	out := make([]MonthlyTokenCostPoint, 0, 7)
	for i := -5; i <= 0; i++ {
		mStr := selectedMonthStart.AddDate(0, i, 0).Format("2006-01")
		pt := byMonth[mStr]
		pt.ActualCostUSD = roundFloat(pt.ActualCostUSD, 6)
		if i == 0 {
			pt.ForecastedCostUSD = roundFloat(selectedMonthForecastUSD, 6)
		} else {
			pt.ForecastedCostUSD = pt.ActualCostUSD
		}
		out = append(out, *pt)
	}

	// Append Next Month Forecast point
	nextMonthStr := selectedMonthStart.AddDate(0, 1, 0).Format("2006-01")
	nextByProv := make(map[string]float64)
	for _, pb := range providerBreakdown {
		nextByProv[pb.ProviderName] = roundFloat(pb.ForecastNextMonthCostUSD, 6)
	}
	out = append(out, MonthlyTokenCostPoint{
		Month:             nextMonthStr,
		IsForecast:        true,
		ForecastedCostUSD: roundFloat(nextMonthForecastUSD, 6),
		ByProviderCostUSD: nextByProv,
	})
	return out
}

func (s *TokenAnalyticsService) buildAvailableFilters(ctx context.Context, db *gorm.DB, tenantID, selectedMonth string) TokenAvailableFilters {
	now := time.Now()
	monthSet := map[string]struct{}{
		now.Format("2006-01"):                   {},
		now.AddDate(0, -1, 0).Format("2006-01"): {},
		now.AddDate(0, -2, 0).Format("2006-01"): {},
		selectedMonth:                           {},
	}
	var dbMonths []string
	_ = db.WithContext(ctx).Model(&entity.LLMUsageLog{}).Where("tenant_id = ?", tenantID).Distinct("`year_month`").Pluck("`year_month`", &dbMonths).Error
	for _, m := range dbMonths {
		if m != "" {
			monthSet[m] = struct{}{}
		}
	}
	months := make([]string, 0, len(monthSet))
	for m := range monthSet {
		months = append(months, m)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(months)))

	var providers []string
	_ = db.WithContext(ctx).Model(&entity.LLMUsageLog{}).Where("tenant_id = ?", tenantID).Distinct("provider_name").Pluck("provider_name", &providers).Error
	sort.Strings(providers)

	var models []string
	_ = db.WithContext(ctx).Model(&entity.LLMUsageLog{}).Where("tenant_id = ?", tenantID).Distinct("model_name").Pluck("model_name", &models).Error
	sort.Strings(models)

	return TokenAvailableFilters{
		Months:     months,
		Providers:  providers,
		Models:     models,
		ModelTypes: []string{"chat", "embedding", "rerank", "ocr", "speech2text", "tts"},
		AppTypes:   []string{"chat", "agent", "ingestion", "retrieval"},
	}
}

// ListUsageLogs returns paginated individual LLM usage records for auditing.
func (s *TokenAnalyticsService) ListUsageLogs(ctx context.Context, db *gorm.DB, tenantID string, filter TokenUsageFilter) ([]entity.LLMUsageLog, int64, error) {
	if db == nil {
		db = dao.DB
	}
	s.ensureHistoricalBackfill(ctx, db, tenantID)

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize < 1 {
		pageSize = 20
	} else if pageSize > 200 {
		pageSize = 200
	}

	q := db.WithContext(ctx).Model(&entity.LLMUsageLog{}).Where("tenant_id = ?", tenantID)
	if filter.Month != "" && !strings.EqualFold(filter.Month, "all") {
		q = q.Where("`year_month` = ?", strings.TrimSpace(filter.Month))
	}
	if filter.FromDate != "" && filter.ToDate != "" {
		q = q.Where("stat_date >= ? AND stat_date <= ?", filter.FromDate[:10], filter.ToDate[:10])
	}
	if filter.ProviderName != "" && !strings.EqualFold(filter.ProviderName, "all") {
		q = q.Where("LOWER(provider_name) = LOWER(?)", strings.TrimSpace(filter.ProviderName))
	}
	if filter.ModelName != "" && !strings.EqualFold(filter.ModelName, "all") {
		q = q.Where("LOWER(model_name) = LOWER(?)", strings.TrimSpace(filter.ModelName))
	}
	if filter.ModelType != "" && !strings.EqualFold(filter.ModelType, "all") {
		q = q.Where("LOWER(model_type) = LOWER(?)", strings.TrimSpace(filter.ModelType))
	}
	if filter.AppType != "" && !strings.EqualFold(filter.AppType, "all") {
		q = q.Where("LOWER(app_type) = LOWER(?)", strings.TrimSpace(filter.AppType))
	}
	if filter.AppID != "" && !strings.EqualFold(filter.AppID, "all") {
		q = q.Where("app_id = ?", strings.TrimSpace(filter.AppID))
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []entity.LLMUsageLog
	err := q.Order("create_time DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

// GetPricingAndBudgets returns the merged pricing table (custom overrides + tenant configured models + built-in catalog) and budgets.
func (s *TokenAnalyticsService) GetPricingAndBudgets(ctx context.Context, db *gorm.DB, tenantID string) (*PricingAndBudgetResponse, error) {
	if db == nil {
		db = dao.DB
	}
	customs, err := s.usageDAO.ListCustomPricing(ctx, db, tenantID)
	if err != nil {
		return nil, err
	}
	customMap := make(map[string]entity.LLMPricingConfig, len(customs))
	entries := make([]PricingCatalogEntry, 0, len(customs)+32)
	seen := make(map[string]bool)

	for _, c := range customs {
		key := strings.ToLower(c.ProviderName + "|" + c.ModelName)
		customMap[key] = c
		seen[key] = true
		entries = append(entries, PricingCatalogEntry{
			ID:               c.ID,
			ProviderName:     c.ProviderName,
			ModelName:        c.ModelName,
			ModelType:        c.ModelType,
			InputPricePer1M:  c.InputPricePer1M,
			OutputPricePer1M: c.OutputPricePer1M,
			Currency:         c.Currency,
			IsCustom:         true,
			IsConfigured:     true,
		})
	}

	// Include models from tenant_llm and llm_usage_log so all active models appear at the top
	var tenantLLMs []entity.TenantLLM
	_ = db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&tenantLLMs).Error
	for _, tl := range tenantLLMs {
		if tl.LLMName == nil || *tl.LLMName == "" {
			continue
		}
		key := strings.ToLower(tl.LLMFactory + "|" + *tl.LLMName)
		if seen[key] {
			continue
		}
		seen[key] = true
		mType := "chat"
		if tl.ModelType != nil && *tl.ModelType != "" {
			mType = *tl.ModelType
		}
		inP, outP := s.usageDAO.ResolveUnitPrice(ctx, db, tenantID, tl.LLMFactory, *tl.LLMName, mType)
		entries = append(entries, PricingCatalogEntry{
			ProviderName:     tl.LLMFactory,
			ModelName:        *tl.LLMName,
			ModelType:        mType,
			InputPricePer1M:  inP,
			OutputPricePer1M: outP,
			Currency:         "USD",
			IsCustom:         false,
			IsConfigured:     true,
		})
	}

	// Also include built-in catalog items
	for _, item := range dao.DefaultPricingCatalog {
		key := strings.ToLower(item.ProviderName + "|" + item.ModelPattern)
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, PricingCatalogEntry{
			ProviderName:     item.ProviderName,
			ModelName:        item.ModelPattern,
			ModelType:        item.ModelType,
			InputPricePer1M:  item.InputPricePer1M,
			OutputPricePer1M: item.OutputPricePer1M,
			Currency:         "USD",
			IsCustom:         false,
			IsConfigured:     false,
		})
	}

	budgets, _ := s.usageDAO.ListBudgetConfigs(ctx, db, tenantID)
	globalBudget := entity.LLMBudgetConfig{
		TenantID:          tenantID,
		ProviderName:      "ALL",
		MonthlyBudgetUSD:  100.0,
		AlertThresholdPct: 80.0,
		DisplayCurrency:   "USD",
		ExchangeRateVND:   25500.0,
	}
	for _, b := range budgets {
		if strings.EqualFold(b.ProviderName, "ALL") || b.ProviderName == "" {
			globalBudget = b
			break
		}
	}

	return &PricingAndBudgetResponse{
		PricingList:    entries,
		Budgets:        budgets,
		GlobalBudget:   globalBudget,
		DefaultCatalog: dao.DefaultPricingCatalog,
	}, nil
}

// SavePricing saves a custom price override and optionally recalculates historical logs for the current month.
func (s *TokenAnalyticsService) SavePricing(ctx context.Context, db *gorm.DB, tenantID string, req SavePricingRequest) (*entity.LLMPricingConfig, error) {
	if db == nil {
		db = dao.DB
	}
	mType := strings.ToLower(strings.TrimSpace(req.ModelType))
	if mType == "" {
		mType = "chat"
	}
	cfg := &entity.LLMPricingConfig{
		TenantID:         tenantID,
		ProviderName:     strings.TrimSpace(req.ProviderName),
		ModelName:        strings.TrimSpace(req.ModelName),
		ModelType:        mType,
		InputPricePer1M:  req.InputPricePer1M,
		OutputPricePer1M: req.OutputPricePer1M,
		Currency:         "USD",
		IsCustom:         true,
	}
	if err := s.usageDAO.UpsertPricingConfig(ctx, db, cfg); err != nil {
		return nil, err
	}

	if req.RecalculateMonth {
		q := db.WithContext(ctx).Model(&entity.LLMUsageLog{}).
			Where("tenant_id = ? AND LOWER(provider_name) = LOWER(?)", tenantID, cfg.ProviderName)
		if cfg.ModelName != "*" {
			q = q.Where("LOWER(model_name) = LOWER(?)", cfg.ModelName)
		}
		var logs []entity.LLMUsageLog
		if err := q.Find(&logs).Error; err == nil {
			for _, l := range logs {
				inC, outC, totC := dao.CalculateCostUSD(l.InputTokens, l.OutputTokens, cfg.InputPricePer1M, cfg.OutputPricePer1M)
				_ = db.WithContext(ctx).Model(&entity.LLMUsageLog{}).
					Where("id = ?", l.ID).
					Updates(map[string]interface{}{
						"input_cost_usd":  inC,
						"output_cost_usd": outC,
						"total_cost_usd":  totC,
					}).Error
			}
		}
	}
	return cfg, nil
}

// DeletePricing removes a custom pricing entry.
func (s *TokenAnalyticsService) DeletePricing(ctx context.Context, db *gorm.DB, tenantID, id string) error {
	if db == nil {
		db = dao.DB
	}
	return s.usageDAO.DeleteCustomPricing(ctx, db, tenantID, id)
}

// SaveBudget saves a monthly budget configuration for the tenant.
func (s *TokenAnalyticsService) SaveBudget(ctx context.Context, db *gorm.DB, tenantID string, req SaveBudgetRequest) (*entity.LLMBudgetConfig, error) {
	if db == nil {
		db = dao.DB
	}
	prov := strings.TrimSpace(req.ProviderName)
	if prov == "" {
		prov = "ALL"
	}
	cfg := &entity.LLMBudgetConfig{
		TenantID:          tenantID,
		ProviderName:      prov,
		MonthlyBudgetUSD:  req.MonthlyBudgetUSD,
		AlertThresholdPct: req.AlertThresholdPct,
		DisplayCurrency:   req.DisplayCurrency,
		ExchangeRateVND:   req.ExchangeRateVND,
	}
	if err := s.usageDAO.UpsertBudgetConfig(ctx, db, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func roundFloat(val float64, precision int) float64 {
	pow := math.Pow(10, float64(precision))
	return math.Round(val*pow) / pow
}
