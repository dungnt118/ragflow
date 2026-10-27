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

package models

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/tokenizer"
)

func prepareModelUsage(ctx context.Context, usage *common.ModelUsage, info *ModelInfo, modelNamePtr *string, modelType string) *common.ModelUsage {
	if usage == nil {
		usage = &common.ModelUsage{}
	}
	if usage.StartAt.IsZero() {
		usage.StartAt = time.Now()
	}
	if usage.Type == "" {
		usage.Type = modelType
	}
	if info != nil {
		if usage.ProviderName == "" {
			usage.ProviderName = info.ProviderName
		}
		if usage.InstanceID == "" {
			usage.InstanceID = info.InstanceID
		}
		if usage.ModelName == "" {
			usage.ModelName = info.Name
		}
		if usage.TenantID == "" {
			usage.TenantID = info.TenantID
		}
		if usage.UserID == "" {
			usage.UserID = info.UserID
		}
	}
	if usage.ModelName == "" && modelNamePtr != nil {
		usage.ModelName = *modelNamePtr
	}
	// Extract bare model name and provider if composite name was passed
	if strings.Contains(usage.ModelName, "@") {
		parts := strings.Split(usage.ModelName, "@")
		if len(parts) >= 2 {
			if usage.ProviderName == "" {
				usage.ProviderName = parts[len(parts)-1]
			}
			usage.ModelName = parts[0]
		}
	}
	if meta, ok := common.ModelUsageMetaFromContext(ctx); ok {
		if usage.TenantID == "" {
			usage.TenantID = meta.TenantID
		}
		if usage.UserID == "" {
			usage.UserID = meta.UserID
		}
		if usage.AppID == "" {
			usage.AppID = meta.AppID
		}
		if usage.AppName == "" {
			usage.AppName = meta.AppName
		}
		if usage.AppType == "" {
			usage.AppType = meta.AppType
		}
		if usage.SessionID == "" {
			usage.SessionID = meta.SessionID
		}
	}
	if usage.AppType == "" {
		switch modelType {
		case "embedding", "ocr":
			usage.AppType = "ingestion"
		case "rerank":
			usage.AppType = "retrieval"
		default:
			usage.AppType = "chat"
		}
	}
	return usage
}

func estimateMessagesTokens(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += 4 // chat message framing overhead
		switch v := msg.Content.(type) {
		case string:
			total += tokenizer.NumTokensFromString(v)
		case []interface{}:
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					if txt, ok := m["text"].(string); ok {
						total += tokenizer.NumTokensFromString(txt)
					}
				}
			}
		default:
			if v != nil {
				total += tokenizer.NumTokensFromString(fmt.Sprintf("%v", v))
			}
		}
	}
	return total
}

func finalizeChatUsage(usage *common.ModelUsage, messages []Message, answerText string, cfgUsage *TokenUsage, respUsage *TokenUsage) {
	if usage == nil {
		return
	}
	if usage.ResponseTimeMS <= 0 && !usage.StartAt.IsZero() {
		usage.ResponseTimeMS = time.Since(usage.StartAt).Milliseconds()
	}
	if usage.TotalTokens <= 0 && respUsage != nil && respUsage.TotalTokens > 0 {
		usage.InputTokens = respUsage.PromptTokens
		usage.OutputTokens = respUsage.CompletionTokens
		usage.TotalTokens = respUsage.TotalTokens
	}
	if usage.TotalTokens <= 0 && cfgUsage != nil && cfgUsage.TotalTokens > 0 {
		usage.InputTokens = cfgUsage.PromptTokens
		usage.OutputTokens = cfgUsage.CompletionTokens
		usage.TotalTokens = cfgUsage.TotalTokens
	}
	if usage.TotalTokens <= 0 && usage.InputTokens <= 0 && usage.OutputTokens <= 0 {
		inTk := estimateMessagesTokens(messages)
		outTk := tokenizer.NumTokensFromString(answerText)
		if inTk > 0 || outTk > 0 {
			usage.InputTokens = inTk
			usage.OutputTokens = outTk
			usage.TotalTokens = inTk + outTk
			usage.IsEstimated = true
		}
	} else if usage.TotalTokens <= 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	} else if usage.InputTokens <= 0 && usage.OutputTokens <= 0 && usage.TotalTokens > 0 {
		// Provider only gave total_tokens; split using message/answer estimate ratio
		inEst := estimateMessagesTokens(messages)
		if inEst > 0 && inEst < usage.TotalTokens {
			usage.InputTokens = inEst
			usage.OutputTokens = usage.TotalTokens - inEst
		} else {
			usage.OutputTokens = usage.TotalTokens
		}
	}
	common.RecordModelUsage(usage)
}

func finalizeEmbeddingUsage(usage *common.ModelUsage, texts []string, embeds []EmbeddingData) {
	if usage == nil {
		return
	}
	if usage.ResponseTimeMS <= 0 && !usage.StartAt.IsZero() {
		usage.ResponseTimeMS = time.Since(usage.StartAt).Milliseconds()
	}
	if usage.TotalTokens <= 0 && usage.InputTokens <= 0 {
		sumEmbedTokens := 0
		for _, e := range embeds {
			sumEmbedTokens += e.TokenCount
		}
		if sumEmbedTokens > 0 {
			usage.InputTokens = sumEmbedTokens
			usage.TotalTokens = sumEmbedTokens
		} else {
			est := 0
			for _, t := range texts {
				est += tokenizer.NumTokensFromString(t)
			}
			if est > 0 {
				usage.InputTokens = est
				usage.TotalTokens = est
				usage.IsEstimated = true
			}
		}
	} else if usage.TotalTokens <= 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	common.RecordModelUsage(usage)
}

func finalizeRerankUsage(usage *common.ModelUsage, req RerankRequest) {
	if usage == nil {
		return
	}
	if usage.ResponseTimeMS <= 0 && !usage.StartAt.IsZero() {
		usage.ResponseTimeMS = time.Since(usage.StartAt).Milliseconds()
	}
	if usage.TotalTokens <= 0 && usage.InputTokens <= 0 {
		est := tokenizer.NumTokensFromString(req.Query)
		for _, doc := range req.Documents {
			est += tokenizer.NumTokensFromString(doc)
		}
		if est > 0 {
			usage.InputTokens = est
			usage.TotalTokens = est
			usage.IsEstimated = true
		}
	} else if usage.TotalTokens <= 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	common.RecordModelUsage(usage)
}

func finalizeOtherUsage(usage *common.ModelUsage, inputText, outputText string, minFallbackTokens int) {
	if usage == nil {
		return
	}
	if usage.ResponseTimeMS <= 0 && !usage.StartAt.IsZero() {
		usage.ResponseTimeMS = time.Since(usage.StartAt).Milliseconds()
	}
	if usage.TotalTokens <= 0 && usage.InputTokens <= 0 && usage.OutputTokens <= 0 {
		inTk := tokenizer.NumTokensFromString(inputText)
		outTk := tokenizer.NumTokensFromString(outputText)
		if inTk == 0 && outTk == 0 && minFallbackTokens > 0 {
			inTk = minFallbackTokens
		}
		if inTk > 0 || outTk > 0 {
			usage.InputTokens = inTk
			usage.OutputTokens = outTk
			usage.TotalTokens = inTk + outTk
			usage.IsEstimated = true
		}
	} else if usage.TotalTokens <= 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	common.RecordModelUsage(usage)
}
