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

package dao

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"ragflow/internal/entity"
)

// BuiltinPriceItem defines a built-in reference price (USD per 1M tokens).
type BuiltinPriceItem struct {
	ProviderName     string  `json:"provider_name"`
	ModelPattern     string  `json:"model_pattern"`
	ModelType        string  `json:"model_type"`
	InputPricePer1M  float64 `json:"input_price_per_1m"`
	OutputPricePer1M float64 `json:"output_price_per_1m"`
}

// DefaultPricingCatalog provides industry-standard USD per 1M token prices for
// popular LLM providers and models. Users can override any entry via LLMPricingConfig.
var DefaultPricingCatalog = []BuiltinPriceItem{
	// OpenAI
	{ProviderName: "OpenAI", ModelPattern: "gpt-4o-mini", ModelType: "chat", InputPricePer1M: 0.15, OutputPricePer1M: 0.60},
	{ProviderName: "OpenAI", ModelPattern: "gpt-4o", ModelType: "chat", InputPricePer1M: 2.50, OutputPricePer1M: 10.00},
	{ProviderName: "OpenAI", ModelPattern: "gpt-4.1-mini", ModelType: "chat", InputPricePer1M: 0.40, OutputPricePer1M: 1.60},
	{ProviderName: "OpenAI", ModelPattern: "gpt-4.1-nano", ModelType: "chat", InputPricePer1M: 0.10, OutputPricePer1M: 0.40},
	{ProviderName: "OpenAI", ModelPattern: "gpt-4.1", ModelType: "chat", InputPricePer1M: 2.00, OutputPricePer1M: 8.00},
	{ProviderName: "OpenAI", ModelPattern: "o3-mini", ModelType: "chat", InputPricePer1M: 1.10, OutputPricePer1M: 4.40},
	{ProviderName: "OpenAI", ModelPattern: "o1", ModelType: "chat", InputPricePer1M: 15.00, OutputPricePer1M: 60.00},
	{ProviderName: "OpenAI", ModelPattern: "gpt-3.5-turbo", ModelType: "chat", InputPricePer1M: 0.50, OutputPricePer1M: 1.50},
	{ProviderName: "OpenAI", ModelPattern: "text-embedding-3-small", ModelType: "embedding", InputPricePer1M: 0.02, OutputPricePer1M: 0.00},
	{ProviderName: "OpenAI", ModelPattern: "text-embedding-3-large", ModelType: "embedding", InputPricePer1M: 0.13, OutputPricePer1M: 0.00},
	{ProviderName: "OpenAI", ModelPattern: "text-embedding-ada-002", ModelType: "embedding", InputPricePer1M: 0.10, OutputPricePer1M: 0.00},

	// Google / Gemini
	{ProviderName: "Gemini", ModelPattern: "gemini-2.5-pro", ModelType: "chat", InputPricePer1M: 1.25, OutputPricePer1M: 10.00},
	{ProviderName: "Gemini", ModelPattern: "gemini-2.5-flash", ModelType: "chat", InputPricePer1M: 0.15, OutputPricePer1M: 0.60},
	{ProviderName: "Gemini", ModelPattern: "gemini-2.0-flash", ModelType: "chat", InputPricePer1M: 0.10, OutputPricePer1M: 0.40},
	{ProviderName: "Gemini", ModelPattern: "gemini-1.5-pro", ModelType: "chat", InputPricePer1M: 1.25, OutputPricePer1M: 5.00},
	{ProviderName: "Gemini", ModelPattern: "gemini-1.5-flash", ModelType: "chat", InputPricePer1M: 0.075, OutputPricePer1M: 0.30},
	{ProviderName: "Gemini", ModelPattern: "text-embedding-004", ModelType: "embedding", InputPricePer1M: 0.025, OutputPricePer1M: 0.00},
	{ProviderName: "Google Cloud", ModelPattern: "gemini-2.5-pro", ModelType: "chat", InputPricePer1M: 1.25, OutputPricePer1M: 10.00},
	{ProviderName: "Google Cloud", ModelPattern: "gemini-2.5-flash", ModelType: "chat", InputPricePer1M: 0.15, OutputPricePer1M: 0.60},

	// Anthropic
	{ProviderName: "Anthropic", ModelPattern: "claude-3-7-sonnet", ModelType: "chat", InputPricePer1M: 3.00, OutputPricePer1M: 15.00},
	{ProviderName: "Anthropic", ModelPattern: "claude-3-5-sonnet", ModelType: "chat", InputPricePer1M: 3.00, OutputPricePer1M: 15.00},
	{ProviderName: "Anthropic", ModelPattern: "claude-3-5-haiku", ModelType: "chat", InputPricePer1M: 0.80, OutputPricePer1M: 4.00},
	{ProviderName: "Anthropic", ModelPattern: "claude-3-opus", ModelType: "chat", InputPricePer1M: 15.00, OutputPricePer1M: 75.00},

	// DeepSeek
	{ProviderName: "DeepSeek", ModelPattern: "deepseek-chat", ModelType: "chat", InputPricePer1M: 0.27, OutputPricePer1M: 1.10},
	{ProviderName: "DeepSeek", ModelPattern: "deepseek-reasoner", ModelType: "chat", InputPricePer1M: 0.55, OutputPricePer1M: 2.19},

	// Tongyi-Qianwen / Aliyun / DashScope
	{ProviderName: "Tongyi-Qianwen", ModelPattern: "qwen-max", ModelType: "chat", InputPricePer1M: 1.60, OutputPricePer1M: 6.40},
	{ProviderName: "Tongyi-Qianwen", ModelPattern: "qwen-plus", ModelType: "chat", InputPricePer1M: 0.40, OutputPricePer1M: 1.20},
	{ProviderName: "Tongyi-Qianwen", ModelPattern: "qwen-turbo", ModelType: "chat", InputPricePer1M: 0.05, OutputPricePer1M: 0.20},
	{ProviderName: "Tongyi-Qianwen", ModelPattern: "qwen3", ModelType: "chat", InputPricePer1M: 0.35, OutputPricePer1M: 1.40},
	{ProviderName: "Tongyi-Qianwen", ModelPattern: "text-embedding", ModelType: "embedding", InputPricePer1M: 0.07, OutputPricePer1M: 0.00},
	{ProviderName: "Tongyi-Qianwen", ModelPattern: "gte-rerank", ModelType: "rerank", InputPricePer1M: 0.10, OutputPricePer1M: 0.00},

	// Zhipu-AI / Moonshot / Mistral / Cohere / Jina
	{ProviderName: "ZHIPU-AI", ModelPattern: "glm-4-plus", ModelType: "chat", InputPricePer1M: 0.70, OutputPricePer1M: 0.70},
	{ProviderName: "ZHIPU-AI", ModelPattern: "glm-4-flash", ModelType: "chat", InputPricePer1M: 0.015, OutputPricePer1M: 0.015},
	{ProviderName: "Moonshot", ModelPattern: "moonshot-v1", ModelType: "chat", InputPricePer1M: 0.80, OutputPricePer1M: 0.80},
	{ProviderName: "Mistral", ModelPattern: "mistral-large", ModelType: "chat", InputPricePer1M: 2.00, OutputPricePer1M: 6.00},
	{ProviderName: "Mistral", ModelPattern: "mistral-ocr", ModelType: "ocr", InputPricePer1M: 1.00, OutputPricePer1M: 1.00},
	{ProviderName: "Cohere", ModelPattern: "rerank", ModelType: "rerank", InputPricePer1M: 1.00, OutputPricePer1M: 0.00},
	{ProviderName: "Cohere", ModelPattern: "embed", ModelType: "embedding", InputPricePer1M: 0.10, OutputPricePer1M: 0.00},
	{ProviderName: "Jina", ModelPattern: "jina-reranker", ModelType: "rerank", InputPricePer1M: 0.05, OutputPricePer1M: 0.00},
	{ProviderName: "Jina", ModelPattern: "jina-embeddings", ModelType: "embedding", InputPricePer1M: 0.02, OutputPricePer1M: 0.00},
}

type LLMUsageDAO struct {
	priceCacheMu sync.RWMutex
	priceCache   map[string]cachedPriceEntry
}

type cachedPriceEntry struct {
	inputPricePer1M  float64
	outputPricePer1M float64
	expiresAt        time.Time
}

func NewLLMUsageDAO() *LLMUsageDAO {
	return &LLMUsageDAO{
		priceCache: make(map[string]cachedPriceEntry),
	}
}

func (d *LLMUsageDAO) InvalidatePriceCache() {
	if d == nil {
		return
	}
	d.priceCacheMu.Lock()
	d.priceCache = make(map[string]cachedPriceEntry)
	d.priceCacheMu.Unlock()
}

// ResolveUnitPrice returns (inputPricePer1M, outputPricePer1M) in USD for a given
// tenant, provider, model, and modelType.
// Resolution priority:
//  1. Exact tenant override (tenant_id, provider_name, model_name)
//  2. Tenant provider wildcard override (tenant_id, provider_name, "*")
//  3. Built-in DefaultPricingCatalog match (by provider + model substring, or model substring across providers)
//  4. Model-type fallback reference rate so non-free cloud models never silently report $0 unless configured as $0
func (d *LLMUsageDAO) ResolveUnitPrice(ctx context.Context, db *gorm.DB, tenantID, providerName, modelName, modelType string) (float64, float64) {
	normProvider := strings.TrimSpace(providerName)
	normModel := strings.TrimSpace(modelName)
	normType := strings.ToLower(strings.TrimSpace(modelType))
	if normType == "" {
		normType = "chat"
	}

	cacheKey := strings.ToLower(tenantID + "|" + normProvider + "|" + normModel + "|" + normType)
	if d != nil {
		d.priceCacheMu.RLock()
		if entry, ok := d.priceCache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
			d.priceCacheMu.RUnlock()
			return entry.inputPricePer1M, entry.outputPricePer1M
		}
		d.priceCacheMu.RUnlock()
	}

	storeCache := func(inPrice, outPrice float64) (float64, float64) {
		if d != nil {
			d.priceCacheMu.Lock()
			d.priceCache[cacheKey] = cachedPriceEntry{
				inputPricePer1M:  inPrice,
				outputPricePer1M: outPrice,
				expiresAt:        time.Now().Add(30 * time.Second),
			}
			d.priceCacheMu.Unlock()
		}
		return inPrice, outPrice
	}

	if db != nil && tenantID != "" {
		var custom entity.LLMPricingConfig
		err := db.WithContext(ctx).
			Where("tenant_id = ? AND LOWER(provider_name) = LOWER(?) AND LOWER(model_name) = LOWER(?)", tenantID, normProvider, normModel).
			First(&custom).Error
		if err == nil {
			return storeCache(custom.InputPricePer1M, custom.OutputPricePer1M)
		}

		err = db.WithContext(ctx).
			Where("tenant_id = ? AND LOWER(provider_name) = LOWER(?) AND model_name = '*'", tenantID, normProvider).
			First(&custom).Error
		if err == nil {
			return storeCache(custom.InputPricePer1M, custom.OutputPricePer1M)
		}
	}

	inPrice, outPrice, matched := MatchBuiltinPrice(normProvider, normModel, normType)
	if matched {
		return storeCache(inPrice, outPrice)
	}

	return storeCache(inPrice, outPrice)
}

// MatchBuiltinPrice matches a provider + model against DefaultPricingCatalog,
// falling back to sensible defaults by model family/type.
func MatchBuiltinPrice(providerName, modelName, modelType string) (float64, float64, bool) {
	lowerProv := strings.ToLower(strings.TrimSpace(providerName))
	lowerModel := strings.ToLower(strings.TrimSpace(modelName))
	lowerType := strings.ToLower(strings.TrimSpace(modelType))

	// Strip composite suffix if modelName still contains @instance@provider
	if idx := strings.Index(lowerModel, "@"); idx > 0 {
		lowerModel = lowerModel[:idx]
	}

	// Pass 1: provider + model pattern match
	for _, item := range DefaultPricingCatalog {
		if strings.EqualFold(item.ProviderName, lowerProv) && strings.Contains(lowerModel, strings.ToLower(item.ModelPattern)) {
			return item.InputPricePer1M, item.OutputPricePer1M, true
		}
	}

	// Pass 2: model pattern match across any provider (e.g. OpenAI-API-Compatible / OpenRouter running gpt-4o-mini or deepseek-chat)
	for _, item := range DefaultPricingCatalog {
		if strings.Contains(lowerModel, strings.ToLower(item.ModelPattern)) {
			return item.InputPricePer1M, item.OutputPricePer1M, true
		}
	}

	// Local/self-hosted providers default to $0 unless user sets an internal GPU unit cost
	if lowerProv == "ollama" || lowerProv == "Builtin" || lowerProv == "builtin" || lowerProv == "lm-studio" {
		return 0, 0, true
	}

	// Pass 3: Fallback reference pricing by model type for unrecognized cloud models
	switch lowerType {
	case "embedding":
		return 0.05, 0.00, false
	case "rerank":
		return 0.10, 0.00, false
	case "ocr", "speech2text", "tts":
		return 0.50, 0.50, false
	default:
		return 0.50, 1.50, false
	}
}

// CalculateCostUSD calculates (inputCost, outputCost, totalCost) in USD from token counts and per-1M rates.
func CalculateCostUSD(inputTokens, outputTokens int64, inputPricePer1M, outputPricePer1M float64) (float64, float64, float64) {
	if inputTokens < 0 {
		inputTokens = 0
	}
	if outputTokens < 0 {
		outputTokens = 0
	}
	inCost := (float64(inputTokens) / 1_000_000.0) * inputPricePer1M
	outCost := (float64(outputTokens) / 1_000_000.0) * outputPricePer1M
	return inCost, outCost, inCost + outCost
}

// InsertBatch inserts a slice of LLMUsageLog records.
func (d *LLMUsageDAO) InsertBatch(ctx context.Context, db *gorm.DB, logs []entity.LLMUsageLog) error {
	if db == nil || len(logs) == 0 {
		return nil
	}
	now := time.Now().Truncate(time.Second)
	nowMs := now.UnixMilli()
	for i := range logs {
		if strings.TrimSpace(logs[i].ID) == "" {
			logs[i].ID = strings.ReplaceAll(uuid.NewString(), "-", "")
		}
		if logs[i].CreateTime == nil {
			t := nowMs
			logs[i].CreateTime = &t
		}
		if logs[i].CreateDate == nil {
			dt := now
			logs[i].CreateDate = &dt
		}
		if logs[i].UpdateTime == nil {
			t := nowMs
			logs[i].UpdateTime = &t
		}
		if logs[i].UpdateDate == nil {
			dt := now
			logs[i].UpdateDate = &dt
		}
		if logs[i].YearMonth == "" {
			logs[i].YearMonth = now.Format("2006-01")
		}
		if logs[i].StatDate == "" {
			logs[i].StatDate = now.Format("2006-01-02")
		}
	}
	return db.WithContext(ctx).CreateInBatches(&logs, 100).Error
}

// UpsertPricingConfig saves or updates a custom pricing entry for a tenant.
func (d *LLMUsageDAO) UpsertPricingConfig(ctx context.Context, db *gorm.DB, cfg *entity.LLMPricingConfig) error {
	if db == nil || cfg == nil {
		return nil
	}
	now := time.Now().Truncate(time.Second)
	nowMs := now.UnixMilli()

	var existing entity.LLMPricingConfig
	err := db.WithContext(ctx).
		Where("tenant_id = ? AND provider_name = ? AND model_name = ?", cfg.TenantID, cfg.ProviderName, cfg.ModelName).
		First(&existing).Error
	if err == nil {
		existing.ModelType = cfg.ModelType
		existing.InputPricePer1M = cfg.InputPricePer1M
		existing.OutputPricePer1M = cfg.OutputPricePer1M
		if cfg.Currency != "" {
			existing.Currency = cfg.Currency
		}
		existing.IsCustom = true
		existing.UpdateTime = &nowMs
		existing.UpdateDate = &now
		err = db.WithContext(ctx).Save(&existing).Error
		*cfg = existing
		d.InvalidatePriceCache()
		return err
	}

	if strings.TrimSpace(cfg.ID) == "" {
		cfg.ID = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	if cfg.Currency == "" {
		cfg.Currency = "USD"
	}
	cfg.IsCustom = true
	cfg.CreateTime = &nowMs
	cfg.CreateDate = &now
	cfg.UpdateTime = &nowMs
	cfg.UpdateDate = &now
	err = db.WithContext(ctx).Create(cfg).Error
	d.InvalidatePriceCache()
	return err
}

// DeleteCustomPricing deletes a custom pricing config by ID for a tenant.
func (d *LLMUsageDAO) DeleteCustomPricing(ctx context.Context, db *gorm.DB, tenantID, id string) error {
	if db == nil {
		return nil
	}
	err := db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&entity.LLMPricingConfig{}).Error
	d.InvalidatePriceCache()
	return err
}

// ListCustomPricing returns all custom pricing rows for a tenant.
func (d *LLMUsageDAO) ListCustomPricing(ctx context.Context, db *gorm.DB, tenantID string) ([]entity.LLMPricingConfig, error) {
	if db == nil {
		return nil, nil
	}
	var list []entity.LLMPricingConfig
	err := db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("provider_name ASC, model_name ASC").
		Find(&list).Error
	return list, err
}

// UpsertBudgetConfig saves or updates a monthly budget configuration for a tenant.
func (d *LLMUsageDAO) UpsertBudgetConfig(ctx context.Context, db *gorm.DB, cfg *entity.LLMBudgetConfig) error {
	if db == nil || cfg == nil {
		return nil
	}
	now := time.Now().Truncate(time.Second)
	nowMs := now.UnixMilli()
	if strings.TrimSpace(cfg.ProviderName) == "" {
		cfg.ProviderName = "ALL"
	}
	if cfg.DisplayCurrency == "" {
		cfg.DisplayCurrency = "USD"
	}
	if cfg.ExchangeRateVND <= 0 {
		cfg.ExchangeRateVND = 25500
	}
	if cfg.AlertThresholdPct <= 0 {
		cfg.AlertThresholdPct = 80
	}

	var existing entity.LLMBudgetConfig
	err := db.WithContext(ctx).
		Where("tenant_id = ? AND provider_name = ?", cfg.TenantID, cfg.ProviderName).
		First(&existing).Error
	if err == nil {
		existing.MonthlyBudgetUSD = cfg.MonthlyBudgetUSD
		existing.AlertThresholdPct = cfg.AlertThresholdPct
		existing.DisplayCurrency = cfg.DisplayCurrency
		existing.ExchangeRateVND = cfg.ExchangeRateVND
		existing.UpdateTime = &nowMs
		existing.UpdateDate = &now
		err = db.WithContext(ctx).Save(&existing).Error
		*cfg = existing
		return err
	}

	if strings.TrimSpace(cfg.ID) == "" {
		cfg.ID = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	cfg.CreateTime = &nowMs
	cfg.CreateDate = &now
	cfg.UpdateTime = &nowMs
	cfg.UpdateDate = &now
	return db.WithContext(ctx).Create(cfg).Error
}

// ListBudgetConfigs returns all budget configs for a tenant.
func (d *LLMUsageDAO) ListBudgetConfigs(ctx context.Context, db *gorm.DB, tenantID string) ([]entity.LLMBudgetConfig, error) {
	if db == nil {
		return nil, nil
	}
	var list []entity.LLMBudgetConfig
	err := db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("provider_name ASC").
		Find(&list).Error
	return list, err
}
