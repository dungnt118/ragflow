/*
 *  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

import { FeedbackConfigSchema } from '@/components/feedback-config-fields';
import { FeedbackSummaryBadge } from '@/components/feedback-summary-badge';
import { buildChatSettingFormSchema } from '@/pages/next-chats/chat/app-settings/use-chat-setting-schema';
import { render, screen } from '@testing-library/react';

const tStub = ((key: string) => key) as any;

describe('FeedbackConfigSchema & ChatSettingSchema (INV-01 / AC-01 / AC-04)', () => {
  it('preserves feedback_config in prompt_config when parsing Chat Settings', () => {
    const schema = buildChatSettingFormSchema(tStub);
    const input = {
      name: 'Trợ lý QLTN',
      description: 'Bot hỗ trợ nghiệp vụ',
      icon: '',
      language: 'Vietnamese',
      dataset_ids: ['kb-1'],
      prompt_config: {
        system: 'Bạn là trợ lý. {knowledge}',
        prologue: 'Xin chào!',
        empty_response: '',
        quote: true,
        keyword: false,
        tts: false,
        parameters: [],
        reasoning: false,
        cross_languages: [],
        toc_enhance: false,
        use_kg: false,
        refine_multiturn: true,
        feedback_config: {
          enabled: true,
          mode: 'multi_criteria' as const,
          criteria: [
            {
              key: 'content_quality',
              label: 'Chất lượng nội dung phản hồi',
              type: 'csat_5' as const,
              required: true,
            },
            {
              key: 'business_accuracy',
              label: 'Độ chính xác nghiệp vụ',
              type: 'binary' as const,
              required: true,
            },
          ],
          trigger_config: {
            enabled: true,
            on_dislike: true,
            csat_lte: 2,
            criteria_failed: ['business_accuracy'],
            batch_count: 1,
            action_type: 'agent_webhook' as const,
            target_agent_id: 'agent-audit-01',
          },
        },
      },
      similarity_threshold: 0.2,
      keywords_similarity_weight: 0.3,
      rerank_candidates_count: 64,
      top_n: 8,
      top_k: 1024,
      llm_id: 'gpt-4o',
      llm_setting: {
        temperature: 0.1,
        top_p: 0.3,
        presence_penalty: 0.4,
        frequency_penalty: 0.7,
        max_tokens: 512,
      },
      rerank_id: '',
    };

    const parsed = schema.parse(input);
    expect(parsed.prompt_config.feedback_config).toBeDefined();
    expect(parsed.prompt_config.feedback_config?.enabled).toBe(true);
    expect(parsed.prompt_config.feedback_config?.mode).toBe('multi_criteria');
    expect(parsed.prompt_config.feedback_config?.criteria).toHaveLength(2);
    expect(
      parsed.prompt_config.feedback_config?.trigger_config?.target_agent_id,
    ).toBe('agent-audit-01');
  });

  it('validates FeedbackConfigSchema defaults and constraints', () => {
    const parsed = FeedbackConfigSchema.parse({
      enabled: true,
      mode: 'like_dislike',
    });
    expect(parsed?.enabled).toBe(true);
    expect(parsed?.mode).toBe('like_dislike');
  });
});

describe('FeedbackSummaryBadge (AC-03 / REQ-02)', () => {
  it('renders nothing when no feedback or rating exists', () => {
    const { container } = render(<FeedbackSummaryBadge />);
    expect(container.firstChild).toBeNull();
  });

  it('renders thumbup, CSAT score, criteria pills, channel and comment', () => {
    render(
      <FeedbackSummaryBadge
        thumbup={false}
        feedback="Thiếu hướng dẫn xuất kho"
        feedbackDetail={{
          csat_score: 2,
          criteria: {
            content_quality: 2,
            business_accuracy: false,
          },
          channel: 'telegram',
        }}
        feedbackConfig={{
          enabled: true,
          mode: 'multi_criteria',
          criteria: [
            {
              key: 'content_quality',
              label: 'Chất lượng nội dung',
              type: 'csat_5',
              required: true,
            },
            {
              key: 'business_accuracy',
              label: 'Độ chính xác nghiệp vụ',
              type: 'binary',
              required: true,
            },
          ],
        }}
      />,
    );

    expect(screen.getByText('Chưa hài lòng')).toBeTruthy();
    expect(screen.getByText('CSAT 2/5')).toBeTruthy();
    expect(screen.getByText('Chất lượng nội dung:')).toBeTruthy();
    expect(screen.getByText('2/5 ⭐')).toBeTruthy();
    expect(screen.getByText('Độ chính xác nghiệp vụ:')).toBeTruthy();
    expect(screen.getByText('Chưa chuẩn')).toBeTruthy();
    expect(screen.getByText('telegram')).toBeTruthy();
    expect(screen.getByText('Thiếu hướng dẫn xuất kho')).toBeTruthy();
  });
});
