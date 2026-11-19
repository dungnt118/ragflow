import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { IFeedbackCriterionConfig } from '@/interfaces/request/chat';
import { Plus, Trash2 } from 'lucide-react';
import { useFieldArray, useFormContext, useWatch } from 'react-hook-form';
import { z } from 'zod';

export const FeedbackCriterionSchema = z.object({
  key: z.string().min(1),
  label: z.string().min(1),
  type: z.enum(['csat_5', 'binary']),
  required: z.boolean().optional(),
  description: z.string().optional(),
});

export const FeedbackTriggerConfigSchema = z.object({
  enabled: z.boolean().optional(),
  rule: z
    .enum(['on_dislike', 'csat_lte', 'criteria_failed', 'batch_count'])
    .optional(),
  csat_threshold: z.coerce.number().min(1).max(5).optional(),
  failed_criteria_keys: z.array(z.string()).optional(),
  batch_count: z.coerce.number().min(1).optional(),
  action_type: z.enum(['agent_webhook', 'mcp_tool', 'webhook']).optional(),
  target_agent_id: z.string().optional(),
  mcp_server_id: z.string().optional(),
  mcp_tool_name: z.string().optional(),
  webhook_url: z.string().optional(),
  webhook_headers: z.record(z.string()).optional(),
});

export const FeedbackConfigSchema = z
  .object({
    enabled: z.boolean().optional(),
    mode: z.enum(['like_dislike', 'multi_criteria']).optional(),
    criteria: z.array(FeedbackCriterionSchema).optional(),
    trigger_config: FeedbackTriggerConfigSchema.nullable().optional(),
  })
  .nullable()
  .optional();

export const DEFAULT_FEEDBACK_CRITERIA: IFeedbackCriterionConfig[] = [
  {
    key: 'content_quality',
    label: 'Chất lượng nội dung phản hồi',
    type: 'csat_5',
    required: true,
  },
  {
    key: 'business_accuracy',
    label: 'Độ chính xác nghiệp vụ / thông tin',
    type: 'binary',
    required: false,
  },
];

type FeedbackConfigFieldsProps = {
  baseName?: string;
};

export function FeedbackConfigFields({
  baseName = 'prompt_config.feedback_config',
}: FeedbackConfigFieldsProps) {
  const form = useFormContext();
  const fieldPath = (sub: string) => (baseName ? `${baseName}.${sub}` : sub);

  const enabled = useWatch({
    control: form.control,
    name: fieldPath('enabled'),
  });
  const mode =
    useWatch({
      control: form.control,
      name: fieldPath('mode'),
    }) || 'like_dislike';
  const triggerEnabled = useWatch({
    control: form.control,
    name: fieldPath('trigger_config.enabled'),
  });
  const triggerRule =
    useWatch({
      control: form.control,
      name: fieldPath('trigger_config.rule'),
    }) || 'on_dislike';
  const triggerAction =
    useWatch({
      control: form.control,
      name: fieldPath('trigger_config.action_type'),
    }) || 'agent_webhook';

  const { fields, append, remove, replace } = useFieldArray({
    control: form.control,
    name: fieldPath('criteria'),
  });

  const handleModeChange = (nextMode: 'like_dislike' | 'multi_criteria') => {
    form.setValue(fieldPath('mode'), nextMode, { shouldDirty: true });
    if (nextMode === 'multi_criteria' && fields.length === 0) {
      replace(DEFAULT_FEEDBACK_CRITERIA);
    }
  };

  return (
    <div
      className="space-y-4 rounded-lg border border-border-button p-4"
      data-testid="feedback-config-section"
    >
      <div className="flex items-center justify-between">
        <div>
          <div className="text-sm font-medium text-text-primary">
            Đánh giá chất lượng phản hồi (Feedback & CSAT)
          </div>
          <div className="text-xs text-text-secondary">
            Hiển thị trên Web, Embedded Widget và Telegram Bot
          </div>
        </div>
        <Switch
          checked={Boolean(enabled)}
          onCheckedChange={(checked) => {
            form.setValue(fieldPath('enabled'), checked, { shouldDirty: true });
            if (checked && !form.getValues(fieldPath('mode'))) {
              form.setValue(fieldPath('mode'), 'like_dislike', {
                shouldDirty: true,
              });
            }
          }}
          data-testid="feedback-config-enabled-switch"
        />
      </div>

      {enabled && (
        <div className="space-y-4 pt-2 border-t border-border-button">
          <div className="space-y-1.5">
            <label className="text-xs font-medium text-text-secondary">
              Chế độ đánh giá
            </label>
            <Select
              value={mode}
              onValueChange={(val) =>
                handleModeChange(val as 'like_dislike' | 'multi_criteria')
              }
            >
              <SelectTrigger data-testid="feedback-config-mode-select">
                <SelectValue placeholder="Chọn chế độ đánh giá" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="like_dislike">
                  Like / Dislike truyền thống
                </SelectItem>
                <SelectItem value="multi_criteria">
                  CSAT 5 mức & Đa tiêu chí tùy chỉnh
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          {mode === 'multi_criteria' && (
            <div className="space-y-3 rounded-md bg-bg-card p-3 border border-border-button">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium text-text-primary">
                  Danh sách tiêu chí đánh giá ({fields.length})
                </span>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-7 text-xs gap-1"
                  onClick={() =>
                    append({
                      key: `criterion_${fields.length + 1}`,
                      label: `Tiêu chí ${fields.length + 1}`,
                      type: 'csat_5',
                      required: false,
                    })
                  }
                  data-testid="feedback-add-criterion-btn"
                >
                  <Plus className="size-3.5" />
                  Thêm tiêu chí
                </Button>
              </div>

              <div className="space-y-2.5">
                {fields.map((item, idx) => (
                  <div
                    key={item.id}
                    className="grid grid-cols-12 gap-2 items-center rounded border border-border-button p-2"
                  >
                    <div className="col-span-5">
                      <Input
                        placeholder="Tên tiêu chí (VD: Chất lượng nội dung)"
                        className="h-8 text-xs"
                        {...form.register(fieldPath(`criteria.${idx}.label`))}
                      />
                    </div>
                    <div className="col-span-3">
                      <Input
                        placeholder="Mã (key)"
                        className="h-8 text-xs font-mono"
                        {...form.register(fieldPath(`criteria.${idx}.key`))}
                      />
                    </div>
                    <div className="col-span-3">
                      <Select
                        value={
                          form.watch(fieldPath(`criteria.${idx}.type`)) ||
                          'csat_5'
                        }
                        onValueChange={(v) =>
                          form.setValue(fieldPath(`criteria.${idx}.type`), v, {
                            shouldDirty: true,
                          })
                        }
                      >
                        <SelectTrigger className="h-8 text-xs">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="csat_5">CSAT 5 mức (1-5⭐)</SelectItem>
                          <SelectItem value="binary">
                            Like/Dislike (Đúng/Sai)
                          </SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="col-span-1 flex justify-end">
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        onClick={() => remove(idx)}
                        disabled={fields.length <= 1}
                      >
                        <Trash2 className="size-3.5 text-state-error" />
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          <div className="space-y-3 pt-2 border-t border-border-button">
            <div className="flex items-center justify-between">
              <div>
                <div className="text-xs font-medium text-text-primary">
                  Trigger tự động xử lý phản hồi tiêu cực
                </div>
                <div className="text-[11px] text-text-secondary">
                  Tự động kích hoạt Agent Workflow, MCP Tool hoặc Webhook
                </div>
              </div>
              <Switch
                checked={Boolean(triggerEnabled)}
                onCheckedChange={(checked) => {
                  form.setValue(fieldPath('trigger_config.enabled'), checked, {
                    shouldDirty: true,
                  });
                  if (checked) {
                    if (!form.getValues(fieldPath('trigger_config.rule'))) {
                      form.setValue(
                        fieldPath('trigger_config.rule'),
                        'on_dislike',
                        { shouldDirty: true },
                      );
                    }
                    if (
                      !form.getValues(fieldPath('trigger_config.action_type'))
                    ) {
                      form.setValue(
                        fieldPath('trigger_config.action_type'),
                        'agent_webhook',
                        { shouldDirty: true },
                      );
                    }
                  }
                }}
                data-testid="feedback-trigger-enabled-switch"
              />
            </div>

            {triggerEnabled && (
              <div className="space-y-3 rounded-md bg-bg-card p-3 border border-border-button">
                <div className="grid grid-cols-2 gap-2">
                  <div className="space-y-1">
                    <label className="text-xs text-text-secondary">
                      Quy tắc kích hoạt
                    </label>
                    <Select
                      value={triggerRule}
                      onValueChange={(v) =>
                        form.setValue(fieldPath('trigger_config.rule'), v, {
                          shouldDirty: true,
                        })
                      }
                    >
                      <SelectTrigger className="h-8 text-xs">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="on_dislike">
                          Khi gặp Dislike / Đánh giá tiêu cực
                        </SelectItem>
                        <SelectItem value="csat_lte">
                          Khi điểm CSAT ≤ ngưỡng
                        </SelectItem>
                        <SelectItem value="criteria_failed">
                          Khi có tiêu chí bị đánh trượt
                        </SelectItem>
                        <SelectItem value="batch_count">
                          Theo lô N đánh giá tiêu cực
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  <div className="space-y-1">
                    <label className="text-xs text-text-secondary">
                      Loại tác vụ kích hoạt
                    </label>
                    <Select
                      value={triggerAction}
                      onValueChange={(v) =>
                        form.setValue(
                          fieldPath('trigger_config.action_type'),
                          v,
                          { shouldDirty: true },
                        )
                      }
                    >
                      <SelectTrigger className="h-8 text-xs">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="agent_webhook">
                          Agent Workflow nội bộ
                        </SelectItem>
                        <SelectItem value="mcp_tool">
                          Gọi MCP Tool bên ngoài
                        </SelectItem>
                        <SelectItem value="webhook">
                          HTTP Webhook bên ngoài
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>

                {triggerRule === 'csat_lte' && (
                  <div className="space-y-1">
                    <label className="text-xs text-text-secondary">
                      Ngưỡng điểm CSAT tối đa kích hoạt (1 - 5)
                    </label>
                    <Input
                      type="number"
                      min={1}
                      max={5}
                      defaultValue={2}
                      className="h-8 text-xs"
                      {...form.register(
                        fieldPath('trigger_config.csat_threshold'),
                      )}
                    />
                  </div>
                )}

                {triggerRule === 'batch_count' && (
                  <div className="space-y-1">
                    <label className="text-xs text-text-secondary">
                      Số lượng đánh giá tiêu cực tích lũy (Batch Count)
                    </label>
                    <Input
                      type="number"
                      min={1}
                      defaultValue={3}
                      className="h-8 text-xs"
                      {...form.register(
                        fieldPath('trigger_config.batch_count'),
                      )}
                    />
                  </div>
                )}

                {triggerAction === 'agent_webhook' && (
                  <div className="space-y-1">
                    <label className="text-xs text-text-secondary">
                      Target Agent ID (Agent xử lý bổ sung tài liệu)
                    </label>
                    <Input
                      placeholder="Nhập ID của Agent Workflow..."
                      className="h-8 text-xs font-mono"
                      {...form.register(
                        fieldPath('trigger_config.target_agent_id'),
                      )}
                    />
                  </div>
                )}

                {triggerAction === 'mcp_tool' && (
                  <div className="grid grid-cols-2 gap-2">
                    <div className="space-y-1">
                      <label className="text-xs text-text-secondary">
                        MCP Server ID
                      </label>
                      <Input
                        placeholder="ID máy chủ MCP..."
                        className="h-8 text-xs font-mono"
                        {...form.register(
                          fieldPath('trigger_config.mcp_server_id'),
                        )}
                      />
                    </div>
                    <div className="space-y-1">
                      <label className="text-xs text-text-secondary">
                        Tên MCP Tool
                      </label>
                      <Input
                        placeholder="Tên hàm tool..."
                        className="h-8 text-xs font-mono"
                        {...form.register(
                          fieldPath('trigger_config.mcp_tool_name'),
                        )}
                      />
                    </div>
                  </div>
                )}

                {triggerAction === 'webhook' && (
                  <div className="space-y-1">
                    <label className="text-xs text-text-secondary">
                      Webhook URL (POST JSON)
                    </label>
                    <Input
                      placeholder="https://example.com/api/feedback-hook"
                      className="h-8 text-xs font-mono"
                      {...form.register(
                        fieldPath('trigger_config.webhook_url'),
                      )}
                    />
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
