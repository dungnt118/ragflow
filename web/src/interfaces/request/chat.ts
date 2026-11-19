export interface IFeedbackCriterionConfig {
  key: string;
  label: string;
  type: 'csat_5' | 'binary';
  required?: boolean;
  description?: string;
}

export interface IFeedbackTriggerConfig {
  enabled?: boolean;
  rule?: 'on_dislike' | 'csat_lte' | 'criteria_failed' | 'batch_count';
  csat_threshold?: number;
  failed_criteria_keys?: string[];
  batch_count?: number;
  action_type?: 'agent_webhook' | 'mcp_tool' | 'webhook';
  target_agent_id?: string;
  mcp_server_id?: string;
  mcp_tool_name?: string;
  webhook_url?: string;
  webhook_headers?: Record<string, string>;
}

export interface IFeedbackConfig {
  enabled?: boolean;
  mode?: 'like_dislike' | 'multi_criteria';
  criteria?: IFeedbackCriterionConfig[];
  trigger_config?: IFeedbackTriggerConfig | null;
}

export interface IFeedbackDetail {
  mode?: 'like_dislike' | 'multi_criteria';
  thumbup?: boolean;
  csat_score?: number;
  criteria?: Record<string, number | boolean>;
  comment?: string;
  channel?: string;
  rater_id?: string;
  rated_at?: number;
}

export interface IFeedbackRequestBody {
  messageId?: string;
  thumbup?: boolean;
  feedback?: string;
  csat_score?: number;
  criteria?: Record<string, number | boolean>;
  channel?: string;
}

export interface IAskRequestBody {
  question: string;
  kb_ids: string[];
  search_id?: string;
}
