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

package common

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type ModelUsage struct {
	UserID         string    `json:"user_id"`
	UserEmail      string    `json:"user_email"`
	TenantID       string    `json:"tenant_id"`
	TenantEmail    string    `json:"tenant_email"`
	GroupID        string    `json:"group_id"`
	GroupName      string    `json:"group_name"`
	AppID          string    `json:"app_id"`
	AppName        string    `json:"app_name"`
	AppType        string    `json:"app_type"`
	SessionID      string    `json:"session_id"`
	ProviderName   string    `json:"provider_name"`
	InstanceID     string    `json:"instance_id"`
	APIKey         string    `json:"api_key"`
	ModelName      string    `json:"model_name"`
	Type           string    `json:"type"`
	RequestID      string    `json:"request_id"`
	StartAt        time.Time `json:"start_at"`
	ResponseTimeMS int64     `json:"response_time_ms"`
	ErrorMessage   string    `json:"error_message"`

	InputTokens  int  `json:"input_tokens"`
	OutputTokens int  `json:"output_tokens"`
	TotalTokens  int  `json:"total_tokens"`
	IsEstimated  bool `json:"is_estimated"`
	Recorded     bool `json:"-"`
}

func (m *ModelUsage) String() string {
	return fmt.Sprintf("%#v", m)
}

// ModelUsageMeta carries caller context (tenant, app, session) down to model wrappers.
type ModelUsageMeta struct {
	TenantID  string
	UserID    string
	AppID     string
	AppName   string
	AppType   string // "chat", "agent", "ingestion", "retrieval", "system"
	SessionID string
}

type modelUsageMetaKey struct{}

// WithModelUsageMeta attaches ModelUsageMeta to ctx, merging with any existing meta.
func WithModelUsageMeta(ctx context.Context, meta ModelUsageMeta) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if prev, ok := ctx.Value(modelUsageMetaKey{}).(ModelUsageMeta); ok {
		if meta.TenantID == "" {
			meta.TenantID = prev.TenantID
		}
		if meta.UserID == "" {
			meta.UserID = prev.UserID
		}
		if meta.AppID == "" {
			meta.AppID = prev.AppID
		}
		if meta.AppName == "" {
			meta.AppName = prev.AppName
		}
		if meta.AppType == "" {
			meta.AppType = prev.AppType
		}
		if meta.SessionID == "" {
			meta.SessionID = prev.SessionID
		}
	}
	return context.WithValue(ctx, modelUsageMetaKey{}, meta)
}

// ModelUsageMetaFromContext extracts ModelUsageMeta from ctx if present.
func ModelUsageMetaFromContext(ctx context.Context) (ModelUsageMeta, bool) {
	if ctx == nil {
		return ModelUsageMeta{}, false
	}
	meta, ok := ctx.Value(modelUsageMetaKey{}).(ModelUsageMeta)
	return meta, ok
}

// ModelUsageRecorder is a callback invoked whenever a model call completes.
type ModelUsageRecorder func(usage *ModelUsage)

var (
	usageRecorderMu sync.RWMutex
	usageRecorder   ModelUsageRecorder
)

// RegisterModelUsageRecorder registers the global sink for completed LLM calls.
func RegisterModelUsageRecorder(recorder ModelUsageRecorder) {
	usageRecorderMu.Lock()
	usageRecorder = recorder
	usageRecorderMu.Unlock()
}

// RecordModelUsage dispatches usage to the registered recorder at most once per ModelUsage instance.
func RecordModelUsage(usage *ModelUsage) {
	if usage == nil || usage.Recorded {
		return
	}
	if usage.TotalTokens <= 0 && usage.InputTokens <= 0 && usage.OutputTokens <= 0 {
		return
	}
	usage.Recorded = true
	usageRecorderMu.RLock()
	rec := usageRecorder
	usageRecorderMu.RUnlock()
	if rec != nil {
		cp := *usage
		rec(&cp)
	}
}

type OperationLog struct {
	EventTime     time.Time `json:"event_time"`
	UserID        string    `json:"user_id"`
	IPAddress     string    `json:"ip_address"`
	Operation     string    `json:"operation"`
	APIPath       string    `json:"api_path"`
	HTTPMethod    string    `json:"http_method"`
	ResourceType  string    `json:"resource_type"`
	ResourceID    string    `json:"resource_id"`
	ResourceName  string    `json:"resource_name"`
	RequestParams string    `json:"request_params"`
	ErrorCode     uint16    `json:"error_code"`
	Message       string    `json:"message"`
	DurationMS    int64     `json:"duration_ms"`
}
