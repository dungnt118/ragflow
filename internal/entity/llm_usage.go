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

package entity

// LLMUsageLog records per-call token consumption, latency, and calculated cost
// across all 6 model types (chat, embedding, rerank, ocr, speech2text, tts).
type LLMUsageLog struct {
	ID             string  `gorm:"column:id;size:64;not null;primaryKey" json:"id"`
	TenantID       string  `gorm:"column:tenant_id;size:64;not null;index:idx_llm_usage_tenant_month,priority:1;index:idx_llm_usage_tenant_date,priority:1" json:"tenant_id"`
	UserID         string  `gorm:"column:user_id;size:64;not null;default:''" json:"user_id"`
	AppID          string  `gorm:"column:app_id;size:64;not null;default:'';index:idx_llm_usage_app" json:"app_id"`
	AppName        string  `gorm:"column:app_name;size:128;not null;default:''" json:"app_name"`
	AppType        string  `gorm:"column:app_type;size:32;not null;default:'chat';index:idx_llm_usage_app_type" json:"app_type"`
	SessionID      string  `gorm:"column:session_id;size:64;not null;default:''" json:"session_id"`
	ProviderName   string  `gorm:"column:provider_name;size:64;not null;index:idx_llm_usage_provider_month,priority:1" json:"provider_name"`
	InstanceID     string  `gorm:"column:instance_id;size:64;not null;default:''" json:"instance_id"`
	ModelName      string  `gorm:"column:model_name;size:128;not null;index:idx_llm_usage_model" json:"model_name"`
	ModelType      string  `gorm:"column:model_type;size:32;not null;default:'chat';index:idx_llm_usage_model_type" json:"model_type"`
	InputTokens    int64   `gorm:"column:input_tokens;not null;default:0" json:"input_tokens"`
	OutputTokens   int64   `gorm:"column:output_tokens;not null;default:0" json:"output_tokens"`
	TotalTokens    int64   `gorm:"column:total_tokens;not null;default:0" json:"total_tokens"`
	InputCostUSD   float64 `gorm:"column:input_cost_usd;type:decimal(18,8);not null;default:0" json:"input_cost_usd"`
	OutputCostUSD  float64 `gorm:"column:output_cost_usd;type:decimal(18,8);not null;default:0" json:"output_cost_usd"`
	TotalCostUSD   float64 `gorm:"column:total_cost_usd;type:decimal(18,8);not null;default:0" json:"total_cost_usd"`
	ResponseTimeMS int64   `gorm:"column:response_time_ms;not null;default:0" json:"response_time_ms"`
	IsEstimated    bool    `gorm:"column:is_estimated;default:false" json:"is_estimated"`
	YearMonth      string  `gorm:"column:year_month;size:7;not null;index:idx_llm_usage_tenant_month,priority:2;index:idx_llm_usage_provider_month,priority:2" json:"year_month"`
	StatDate       string  `gorm:"column:stat_date;size:10;not null;index:idx_llm_usage_tenant_date,priority:2" json:"stat_date"`
	BaseModel
}

func (LLMUsageLog) TableName() string {
	return "llm_usage_log"
}

// LLMPricingConfig stores unit pricing (USD per 1M input/output tokens) for a
// specific tenant + provider + model, or tenant="_default_" for system defaults.
type LLMPricingConfig struct {
	ID               string  `gorm:"column:id;size:64;not null;primaryKey" json:"id"`
	TenantID         string  `gorm:"column:tenant_id;size:64;not null;uniqueIndex:idx_llm_pricing_tenant_prov_model,priority:1" json:"tenant_id"`
	ProviderName     string  `gorm:"column:provider_name;size:64;not null;uniqueIndex:idx_llm_pricing_tenant_prov_model,priority:2" json:"provider_name"`
	ModelName        string  `gorm:"column:model_name;size:128;not null;uniqueIndex:idx_llm_pricing_tenant_prov_model,priority:3" json:"model_name"`
	ModelType        string  `gorm:"column:model_type;size:32;not null;default:'chat'" json:"model_type"`
	InputPricePer1M  float64 `gorm:"column:input_price_per_1m;type:decimal(18,6);not null;default:0" json:"input_price_per_1m"`
	OutputPricePer1M float64 `gorm:"column:output_price_per_1m;type:decimal(18,6);not null;default:0" json:"output_price_per_1m"`
	Currency         string  `gorm:"column:currency;size:16;not null;default:'USD'" json:"currency"`
	IsCustom         bool    `gorm:"column:is_custom;default:false" json:"is_custom"`
	BaseModel
}

func (LLMPricingConfig) TableName() string {
	return "llm_pricing_config"
}

// LLMBudgetConfig stores monthly budget limits and display currency preferences
// per tenant (ProviderName="ALL" for global tenant budget, or a specific provider).
type LLMBudgetConfig struct {
	ID                string  `gorm:"column:id;size:64;not null;primaryKey" json:"id"`
	TenantID          string  `gorm:"column:tenant_id;size:64;not null;uniqueIndex:idx_llm_budget_tenant_prov,priority:1" json:"tenant_id"`
	ProviderName      string  `gorm:"column:provider_name;size:64;not null;default:'ALL';uniqueIndex:idx_llm_budget_tenant_prov,priority:2" json:"provider_name"`
	MonthlyBudgetUSD  float64 `gorm:"column:monthly_budget_usd;type:decimal(18,4);not null;default:100" json:"monthly_budget_usd"`
	AlertThresholdPct float64 `gorm:"column:alert_threshold_pct;type:decimal(6,2);not null;default:80" json:"alert_threshold_pct"`
	DisplayCurrency   string  `gorm:"column:display_currency;size:16;not null;default:'USD'" json:"display_currency"`
	ExchangeRateVND   float64 `gorm:"column:exchange_rate_vnd;type:decimal(18,2);not null;default:25500" json:"exchange_rate_vnd"`
	BaseModel
}

func (LLMBudgetConfig) TableName() string {
	return "llm_budget_config"
}
