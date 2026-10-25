import { PageHeader } from '@/components/page-header';
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb';
import { Button } from '@/components/ui/button';
import { SearchInput } from '@/components/ui/input';
import message from '@/components/ui/message';
import { RAGFlowPagination } from '@/components/ui/ragflow-pagination';
import { DatePickerWithRange } from '@/components/ui/range-picker';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Spin } from '@/components/ui/spin';
import { useNavigatePage } from '@/hooks/logic-hooks/navigate-hooks';
import { IAgentLogMessage } from '@/interfaces/database/agent';
import {
  IFeedbackConfig,
  IReferenceObject,
} from '@/interfaces/database/chat';
import api from '@/utils/api';
import { formatDate } from '@/utils/date';
import { downloadFileFromBlob } from '@/utils/file-util';
import request from '@/utils/request';
import { useQuery } from '@tanstack/react-query';
import dayjs from 'dayjs';
import {
  AlertTriangle,
  BarChart3,
  Bot,
  Code2,
  Download,
  Filter,
  Globe,
  MessageSquare,
  MessageSquareHeart,
  RefreshCw,
  Send,
  Star,
  ThumbsDown,
  ThumbsUp,
} from 'lucide-react';
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { DateRange } from '../../components/originui/calendar/index';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../../components/ui/table';
import { AgentLogDetailModal } from './agent-log-detail-modal';

export interface ICriterionBreakdownStat {
  key: string;
  label: string;
  type: 'csat_5' | 'binary';
  total_count: number;
  positive_count: number;
  negative_count: number;
  avg_score?: number;
}

export interface IFeedbackKPISummary {
  total_sessions: number;
  rated_sessions: number;
  rated_messages: number;
  positive_count: number;
  negative_count: number;
  satisfaction_rate: number;
  avg_csat: number;
  csat_count: number;
  csat_distribution: Record<string, number>;
  by_channel: Record<string, number>;
  by_source_type: Record<string, number>;
  criteria_breakdown: Record<string, ICriterionBreakdownStat>;
}

export interface ISessionFeedbackSummary {
  has_feedback: boolean;
  rated_count: number;
  positive_count: number;
  negative_count: number;
  avg_csat?: number;
  min_csat?: number;
  max_csat?: number;
  channels?: string[];
  latest_comment?: string;
  latest_rated_at?: number;
  failed_criteria?: string[];
  criteria_values?: Record<string, unknown>;
}

export interface IUnifiedFeedbackSessionItem {
  id: string;
  session_id: string;
  name: string;
  source_type: 'chat' | 'agent';
  storage_table: string;
  app_id: string;
  app_name: string;
  user_id: string;
  channel: string;
  round: number;
  thumb_up: number;
  errors?: string | null;
  version_title?: string | null;
  feedback_summary: ISessionFeedbackSummary;
  feedback_config?: IFeedbackConfig;
  messages: IAgentLogMessage[];
  message: IAgentLogMessage[];
  reference: unknown[];
  create_time: number;
  update_time: number;
  create_date: string;
  update_date: string;
}

export interface IFeedbackAppOption {
  id: string;
  name: string;
  source_type: 'chat' | 'agent';
  feedback_config?: IFeedbackConfig;
}

export interface IUnifiedFeedbackAuditResponse {
  kpi: IFeedbackKPISummary;
  total: number;
  page: number;
  page_size: number;
  sessions: IUnifiedFeedbackSessionItem[];
  apps?: IFeedbackAppOption[];
}

interface SessionFeedbackAuditViewProps {
  defaultSourceType: 'chat' | 'agent' | 'all';
  defaultAppId?: string;
  defaultAppName?: string;
}

const getStartOfLast30Days = (): Date => {
  const d = new Date();
  d.setDate(d.getDate() - 30);
  d.setHours(0, 0, 0, 0);
  return d;
};

const getEndOfToday = (): Date => {
  const today = new Date();
  today.setHours(23, 59, 59, 999);
  return today;
};

const normalizeDateParam = (value: Date | undefined, isEnd = false) => {
  if (!value) return undefined;
  const day = dayjs(value);
  return (isEnd ? day.endOf('day') : day.startOf('day')).format(
    'YYYY-MM-DD HH:mm:ss',
  );
};

export const SessionFeedbackAuditView: React.FC<
  SessionFeedbackAuditViewProps
> = ({ defaultSourceType, defaultAppId = '', defaultAppName = '' }) => {
  const { t } = useTranslation();
  const {
    navigateToAgents,
    navigateToAgent,
    navigateToChatList,
    navigateToChat,
  } = useNavigatePage();

  // Filter states
  const [sourceType, setSourceType] = useState<'all' | 'chat' | 'agent'>(
    defaultSourceType,
  );
  const [appId, setAppId] = useState<string>(defaultAppId || 'all');
  const [feedbackFilter, setFeedbackFilter] = useState<string>('all'); // 'all' | 'rated' | 'positive' | 'negative' | 'unrated'
  const [csatFilter, setCsatFilter] = useState<string>('all'); // 'all' | 'low' (<=2) | 'mid' (3) | 'high' (>=4)
  const [channelFilter, setChannelFilter] = useState<string>('all'); // 'all' | 'web' | 'widget' | 'telegram'
  const [criterionFilter, setCriterionFilter] = useState<string>('all'); // 'all' | '<key>:failed' | '<key>:all'
  const [keywords, setKeywords] = useState<string>('');
  const [currentDate, setCurrentDate] = useState<DateRange>({
    from: getStartOfLast30Days(),
    to: getEndOfToday(),
  });
  const [sortConfig, setSortConfig] = useState<{
    orderby: string;
    desc: boolean;
  }>({ orderby: 'update_time', desc: true });
  const [pagination, setPagination] = useState<{
    current: number;
    pageSize: number;
  }>({ current: 1, pageSize: 10 });

  useEffect(() => {
    setSourceType(defaultSourceType);
    setAppId(defaultAppId || 'all');
  }, [defaultSourceType, defaultAppId]);

  const queryParams = useMemo(() => {
    const params: Record<string, string | number | boolean> = {
      source_type: sourceType,
      orderby: sortConfig.orderby,
      desc: sortConfig.desc,
      page: pagination.current,
      page_size: pagination.pageSize,
    };
    if (appId && appId !== 'all') {
      params.app_id = appId;
    }
    if (feedbackFilter === 'rated') {
      params.has_feedback = true;
    } else if (feedbackFilter === 'unrated') {
      params.has_feedback = false;
    } else if (feedbackFilter === 'positive') {
      params.has_feedback = true;
      params.thumb_up = true;
    } else if (feedbackFilter === 'negative') {
      params.has_feedback = true;
      params.thumb_up = false;
    }

    if (csatFilter === 'low') {
      params.max_csat = 2;
    } else if (csatFilter === 'mid') {
      params.min_csat = 3;
      params.max_csat = 3;
    } else if (csatFilter === 'high') {
      params.min_csat = 4;
    }

    if (channelFilter && channelFilter !== 'all') {
      params.channel = channelFilter;
    }

    if (criterionFilter && criterionFilter !== 'all') {
      const [cKey, cVal] = criterionFilter.split(':');
      if (cKey) {
        params.criterion_key = cKey;
      }
      if (cVal) {
        params.criterion_value = cVal;
      }
    }

    if (keywords.trim()) {
      params.keywords = keywords.trim();
    }
    const fromStr = normalizeDateParam(currentDate.from, false);
    const toStr = normalizeDateParam(currentDate.to, true);
    if (fromStr) params.from_date = fromStr;
    if (toStr) params.to_date = toStr;

    return params;
  }, [
    sourceType,
    appId,
    feedbackFilter,
    csatFilter,
    channelFilter,
    criterionFilter,
    keywords,
    currentDate,
    sortConfig,
    pagination,
  ]);

  const {
    data: auditData,
    isFetching: loading,
    refetch,
  } = useQuery<IUnifiedFeedbackAuditResponse>({
    queryKey: ['unifiedFeedbackSessions', queryParams],
    queryFn: async () => {
      const { data } = await request.get(api.feedbackSessions, {
        params: queryParams,
      });
      return data?.data;
    },
  });

  const kpi: IFeedbackKPISummary = useMemo(
    () =>
      auditData?.kpi || {
        total_sessions: 0,
        rated_sessions: 0,
        rated_messages: 0,
        positive_count: 0,
        negative_count: 0,
        satisfaction_rate: 0,
        avg_csat: 0,
        csat_count: 0,
        csat_distribution: { '1': 0, '2': 0, '3': 0, '4': 0, '5': 0 },
        by_channel: {},
        by_source_type: { chat: 0, agent: 0 },
        criteria_breakdown: {},
      },
    [auditData?.kpi],
  );

  const sessions = auditData?.sessions || [];
  const total = auditData?.total || 0;
  const appOptions = useMemo(() => {
    const allApps = auditData?.apps || [];
    if (sourceType === 'all') return allApps;
    return allApps.filter((a) => a.source_type === sourceType);
  }, [auditData?.apps, sourceType]);

  const currentSelectedApp = useMemo(() => {
    return (auditData?.apps || []).find((a) => a.id === appId);
  }, [auditData?.apps, appId]);

  const criterionSelectOptions = useMemo(() => {
    const map = new Map<string, string>();
    map.set('content_quality', 'Chất lượng nội dung phản hồi');
    map.set('business_accuracy', 'Độ chính xác nghiệp vụ');
    Object.values(kpi.criteria_breakdown || {}).forEach((stat) => {
      map.set(stat.key, stat.label || stat.key);
    });
    (auditData?.apps || []).forEach((app) => {
      app.feedback_config?.criteria?.forEach((c) => {
        map.set(c.key, c.label || c.key);
      });
    });
    return Array.from(map.entries());
  }, [kpi.criteria_breakdown, auditData?.apps]);

  const handleSort = (key: string) => {
    setSortConfig((prev) => ({
      orderby: key,
      desc: prev.orderby === key ? !prev.desc : true,
    }));
  };

  const handleResetFilters = () => {
    setSourceType(defaultSourceType);
    setAppId(defaultAppId || 'all');
    setFeedbackFilter('all');
    setCsatFilter('all');
    setChannelFilter('all');
    setCriterionFilter('all');
    setKeywords('');
    setCurrentDate({
      from: getStartOfLast30Days(),
      to: getEndOfToday(),
    });
    setPagination({ current: 1, pageSize: pagination.pageSize });
  };

  const [openModal, setOpenModal] = useState(false);
  const [modalData, setModalData] = useState<IUnifiedFeedbackSessionItem>();
  const [showApiModal, setShowApiModal] = useState(false);
  const [exporting, setExporting] = useState(false);

  const showLogDetail = (item: IUnifiedFeedbackSessionItem) => {
    if (item?.messages?.length || item?.message?.length) {
      setModalData(item);
      setOpenModal(true);
    }
  };

  const handleExportCSV = useCallback(async () => {
    try {
      setExporting(true);
      const { data } = await request.get(api.feedbackSessions, {
        params: {
          ...queryParams,
          page: 1,
          page_size: 1000,
        },
      });
      const rowsData: IUnifiedFeedbackSessionItem[] =
        data?.data?.sessions || [];
      if (!rowsData.length) {
        message.warning('Không có dữ liệu phiên để xuất CSV');
        return;
      }

      const headers = [
        'Session ID',
        'Loại nguồn',
        'Tên ứng dụng (Chat/Agent)',
        'Kênh tương tác',
        'User ID',
        'Tiêu đề / Câu hỏi đầu',
        'Số lượt (Round)',
        'Có đánh giá',
        'Số lượt 👍 (Like)',
        'Số lượt 👎 (Dislike)',
        'Điểm CSAT TB (⭐)',
        'Tiêu chí bị đánh giá lỗi',
        'Nhận xét mới nhất',
        'Nội dung hội thoại',
        'Cập nhật lúc',
        'Tạo lúc',
      ];

      const formatConv = (item: IUnifiedFeedbackSessionItem) => {
        return (item.messages || [])
          .map((m) => `${m.role === 'assistant' ? 'AI' : 'User'}: ${m.content ?? ''}`)
          .join('\n');
      };

      const rows = rowsData.map((item) => [
        item.id,
        item.source_type === 'chat' ? 'Chat Assistant' : 'Agent',
        item.app_name,
        item.channel,
        item.user_id,
        item.name,
        item.round,
        item.feedback_summary?.has_feedback ? 'Đã đánh giá' : 'Chưa đánh giá',
        item.feedback_summary?.positive_count ?? 0,
        item.feedback_summary?.negative_count ?? 0,
        item.feedback_summary?.avg_csat ?? '',
        (item.feedback_summary?.failed_criteria || []).join('; '),
        item.feedback_summary?.latest_comment ?? '',
        formatConv(item),
        item.update_date,
        item.create_date,
      ]);

      const csvContent = [
        headers.join(','),
        ...rows.map((row) =>
          row
            .map((cell) => `"${String(cell ?? '').replace(/"/g, '""')}"`)
            .join(','),
        ),
      ].join('\n');

      const BOM = '\uFEFF';
      const blob = new Blob([BOM + csvContent], {
        type: 'text/csv;charset=utf-8;',
      });
      downloadFileFromBlob(
        blob,
        `feedback-sessions-${sourceType}-${new Date().toISOString().split('T')[0]}.csv`,
      );
    } finally {
      setExporting(false);
    }
  }, [queryParams, sourceType]);

  const apiCurlSnippet = useMemo(() => {
    const search = new URLSearchParams();
    Object.entries(queryParams).forEach(([k, v]) => {
      if (v !== undefined && v !== '') {
        search.set(k, String(v));
      }
    });
    const baseUrl = window.location.origin;
    return `curl -X GET "${baseUrl}/api/v1/feedback/sessions?${search.toString()}" \\\n  -H "Authorization: Bearer <YOUR_RAGFLOW_API_KEY>"`;
  }, [queryParams]);

  const renderChannelBadge = (channel: string) => {
    const ch = (channel || 'web').toLowerCase();
    if (ch === 'telegram') {
      return (
        <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-sky-500/15 text-sky-400 border border-sky-500/30">
          <Send className="size-3" /> Telegram
        </span>
      );
    }
    if (ch === 'widget') {
      return (
        <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-purple-500/15 text-purple-400 border border-purple-500/30">
          <Globe className="size-3" /> Widget
        </span>
      );
    }
    return (
      <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium bg-emerald-500/15 text-emerald-400 border border-emerald-500/30">
        <MessageSquare className="size-3" /> Web
      </span>
    );
  };

  const renderFeedbackSummaryCell = (item: IUnifiedFeedbackSessionItem) => {
    const fb = item.feedback_summary;
    if (!fb || !fb.has_feedback) {
      return (
        <span className="text-xs text-text-secondary italic">
          Chưa có đánh giá
        </span>
      );
    }
    return (
      <div className="flex flex-col gap-1 max-w-md">
        <div className="flex flex-wrap items-center gap-1.5">
          {fb.positive_count > 0 && (
            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium bg-emerald-500/15 text-emerald-400 border border-emerald-500/30">
              <ThumbsUp className="size-3" /> {fb.positive_count} Hài lòng
            </span>
          )}
          {fb.negative_count > 0 && (
            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-medium bg-rose-500/15 text-rose-400 border border-rose-500/30">
              <ThumbsDown className="size-3" /> {fb.negative_count} Chưa hài lòng
            </span>
          )}
          {fb.avg_csat !== undefined && fb.avg_csat !== null && (
            <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-xs font-semibold bg-amber-500/15 text-amber-400 border border-amber-500/30">
              <Star className="size-3 fill-amber-400" /> {fb.avg_csat} / 5
            </span>
          )}
          {fb.failed_criteria?.map((cKey) => {
            const label =
              criterionSelectOptions.find(([k]) => k === cKey)?.[1] || cKey;
            return (
              <span
                key={cKey}
                className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[11px] bg-rose-500/10 text-rose-300 border border-rose-500/25"
              >
                <AlertTriangle className="size-2.5" /> {label}
              </span>
            );
          })}
        </div>
        {fb.latest_comment && (
          <div className="text-xs text-text-secondary truncate max-w-sm">
            “{fb.latest_comment}”
          </div>
        )}
      </div>
    );
  };

  const displayAppName =
    currentSelectedApp?.name ||
    defaultAppName ||
    (sourceType === 'chat'
      ? 'Tất cả Chat Assistant'
      : sourceType === 'agent'
        ? 'Tất cả Agent'
        : 'Tất cả Ứng dụng');

  return (
    <div
      className="text-text-primary h-full flex flex-col overflow-auto"
      data-testid="session-feedback-audit-page"
    >
      <PageHeader>
        <Breadcrumb>
          <BreadcrumbList>
            <BreadcrumbItem>
              <BreadcrumbLink
                onClick={
                  defaultSourceType === 'chat'
                    ? navigateToChatList
                    : navigateToAgents
                }
              >
                {defaultSourceType === 'chat' ? 'Chat Assistant' : t('flow.agent')}
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            {defaultAppId && (
              <>
                <BreadcrumbItem>
                  <BreadcrumbLink
                    onClick={
                      defaultSourceType === 'chat'
                        ? navigateToChat(defaultAppId)
                        : navigateToAgent(defaultAppId)
                    }
                  >
                    {displayAppName}
                  </BreadcrumbLink>
                </BreadcrumbItem>
                <BreadcrumbSeparator />
              </>
            )}
            <BreadcrumbItem>
              <BreadcrumbPage>
                Quản trị Hội thoại & Thống kê Đánh giá (CSAT / Feedback)
              </BreadcrumbPage>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>

        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowApiModal((v) => !v)}
            data-testid="toggle-third-party-api-guide"
          >
            <Code2 className="size-4 mr-1.5" />
            API Tích hợp Bên thứ 3
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={handleExportCSV}
            loading={exporting}
            data-testid="export-feedback-csv-btn"
          >
            <Download className="size-4 mr-1.5" />
            Xuất CSV
          </Button>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => refetch()}
            data-testid="refresh-feedback-audit-btn"
          >
            <RefreshCw className="size-4 mr-1.5" />
            Làm mới
          </Button>
        </div>
      </PageHeader>

      <div className="p-5 space-y-5 flex-1">
        {/* Third-party API Guide Collapsible Banner */}
        {showApiModal && (
          <div
            className="rounded-lg border border-sky-500/30 bg-sky-500/5 p-4 space-y-2"
            data-testid="third-party-api-panel"
          >
            <div className="flex items-center justify-between">
              <div className="font-semibold text-sm flex items-center gap-2 text-sky-400">
                <Code2 className="size-4" />
                Chuẩn API Thống nhất cho Bên thứ 3 (REST API v1 - Bearer API Key)
              </div>
              <span className="text-xs text-text-secondary">
                Đồng bộ 100% bộ lọc & cấu trúc dữ liệu với giao diện Quản trị hiện tại
              </span>
            </div>
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-2 text-xs">
              <div className="p-2 rounded bg-bg-card border border-border-button">
                <div className="font-mono text-emerald-400">
                  GET /api/v1/feedback/sessions
                </div>
                <div className="text-text-secondary mt-0.5">
                  Truy vấn hợp nhất trên toàn bộ Chat Assistant & Agent (`source_type=all|chat|agent`, `app_id=...`)
                </div>
              </div>
              <div className="p-2 rounded bg-bg-card border border-border-button">
                <div className="font-mono text-emerald-400">
                  GET /api/v1/chats/:chat_id/feedback-sessions
                </div>
                <div className="text-text-secondary mt-0.5">
                  Truy vấn riêng cho 1 Chat Assistant (gộp cả Web, Telegram & Widget nhúng)
                </div>
              </div>
              <div className="p-2 rounded bg-bg-card border border-border-button">
                <div className="font-mono text-emerald-400">
                  GET /api/v1/agents/:canvas_id/feedback-sessions
                </div>
                <div className="text-text-secondary mt-0.5">
                  Truy vấn riêng cho 1 Agent Canvas
                </div>
              </div>
            </div>
            <pre className="p-3 rounded bg-black/50 text-xs font-mono text-emerald-300 overflow-x-auto">
              {apiCurlSnippet}
            </pre>
          </div>
        )}

        {/* 4 Unified KPI Cards */}
        <div
          className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4"
          data-testid="feedback-kpi-cards"
        >
          {/* KPI Card 1: Total & Rated Sessions */}
          <div
            className="rounded-xl border border-border-button bg-bg-card p-4 shadow-sm flex flex-col justify-between"
            data-testid="kpi-card-sessions"
          >
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-text-secondary uppercase tracking-wider">
                Phiên có đánh giá / Tổng phiên
              </span>
              <MessageSquareHeart className="size-5 text-sky-400" />
            </div>
            <div className="mt-3 flex items-baseline gap-2">
              <span className="text-3xl font-bold">{kpi.rated_sessions}</span>
              <span className="text-sm text-text-secondary">
                / {kpi.total_sessions} phiên
              </span>
            </div>
            <div className="mt-3 flex items-center justify-between text-xs text-text-secondary border-t border-border-button pt-2">
              <span>Lượt phản hồi đã chấm: {kpi.rated_messages}</span>
              <span>
                Chat: {kpi.by_source_type?.chat ?? 0} · Agent:{' '}
                {kpi.by_source_type?.agent ?? 0}
              </span>
            </div>
          </div>

          {/* KPI Card 2: CSAT Score & Star Distribution */}
          <div
            className="rounded-xl border border-border-button bg-bg-card p-4 shadow-sm flex flex-col justify-between"
            data-testid="kpi-card-csat"
          >
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-text-secondary uppercase tracking-wider">
                Điểm CSAT Trung bình (1-5 ⭐)
              </span>
              <Star className="size-5 text-amber-400 fill-amber-400" />
            </div>
            <div className="mt-3 flex items-baseline gap-2">
              <span className="text-3xl font-bold text-amber-400">
                {kpi.csat_count > 0 ? kpi.avg_csat.toFixed(2) : '—'}
              </span>
              <span className="text-sm text-text-secondary">
                / 5.0 ({kpi.csat_count} lượt chấm CSAT)
              </span>
            </div>
            <div className="mt-3 flex items-center gap-1.5 text-[11px] text-text-secondary border-t border-border-button pt-2">
              {['5', '4', '3', '2', '1'].map((star) => (
                <span
                  key={star}
                  className="px-1.5 py-0.5 rounded bg-bg-input border border-border-button"
                >
                  {star}★: {kpi.csat_distribution?.[star] ?? 0}
                </span>
              ))}
            </div>
          </div>

          {/* KPI Card 3: Satisfaction Rate (Like vs Dislike) */}
          <div
            className="rounded-xl border border-border-button bg-bg-card p-4 shadow-sm flex flex-col justify-between"
            data-testid="kpi-card-satisfaction"
          >
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-text-secondary uppercase tracking-wider">
                Tỷ lệ Hài lòng (Like / Dislike)
              </span>
              <BarChart3 className="size-5 text-emerald-400" />
            </div>
            <div className="mt-3 flex items-baseline gap-2">
              <span className="text-3xl font-bold text-emerald-400">
                {kpi.positive_count + kpi.negative_count > 0
                  ? `${kpi.satisfaction_rate}%`
                  : '—'}
              </span>
              <span className="text-xs text-text-secondary">tỷ lệ tích cực</span>
            </div>
            <div className="mt-3 flex items-center justify-between text-xs border-t border-border-button pt-2">
              <span className="inline-flex items-center gap-1 text-emerald-400 font-medium">
                <ThumbsUp className="size-3.5" /> Hài lòng: {kpi.positive_count}
              </span>
              <span className="inline-flex items-center gap-1 text-rose-400 font-medium">
                <ThumbsDown className="size-3.5" /> Chưa hài lòng:{' '}
                {kpi.negative_count}
              </span>
            </div>
          </div>

          {/* KPI Card 4: Channel & Criteria Breakdown */}
          <div
            className="rounded-xl border border-border-button bg-bg-card p-4 shadow-sm flex flex-col justify-between"
            data-testid="kpi-card-criteria"
          >
            <div className="flex items-center justify-between">
              <span className="text-xs font-medium text-text-secondary uppercase tracking-wider">
                Phân bổ Tiêu chí & Kênh
              </span>
              <Filter className="size-5 text-purple-400" />
            </div>
            <div className="mt-2 space-y-1 text-xs">
              {Object.values(kpi.criteria_breakdown || {}).length > 0 ? (
                Object.values(kpi.criteria_breakdown)
                  .slice(0, 2)
                  .map((c) => (
                    <div
                      key={c.key}
                      className="flex items-center justify-between truncate"
                    >
                      <span className="truncate text-text-secondary mr-2">
                        {c.label}:
                      </span>
                      <span className="font-medium shrink-0">
                        {c.type === 'csat_5' && c.avg_score
                          ? `${c.avg_score}★ `
                          : ''}
                        <span className="text-emerald-400">
                          +{c.positive_count}
                        </span>{' '}
                        /{' '}
                        <span className="text-rose-400">
                          -{c.negative_count}
                        </span>
                      </span>
                    </div>
                  ))
              ) : (
                <div className="text-text-secondary italic">
                  Chưa có dữ liệu tiêu chí chi tiết
                </div>
              )}
            </div>
            <div className="mt-2 flex items-center gap-2 text-[11px] text-text-secondary border-t border-border-button pt-2">
              <span>Web: {kpi.by_channel?.web ?? 0}</span>
              <span>·</span>
              <span>Widget: {kpi.by_channel?.widget ?? 0}</span>
              <span>·</span>
              <span>Telegram: {kpi.by_channel?.telegram ?? 0}</span>
            </div>
          </div>
        </div>

        {/* Unified Filter Bar */}
        <div
          className="rounded-xl border border-border-button bg-bg-card p-4 space-y-3"
          data-testid="feedback-filter-bar"
        >
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-6 gap-3">
            {/* 1. Source Type Filter (Chat Assistant vs Agent vs All) */}
            <div className="space-y-1">
              <label className="text-xs text-text-secondary font-medium">
                Loại nguồn (Chat / Agent)
              </label>
              <Select
                value={sourceType}
                onValueChange={(v: 'all' | 'chat' | 'agent') => {
                  setSourceType(v);
                  setAppId('all');
                  setPagination((p) => ({ ...p, current: 1 }));
                }}
              >
                <SelectTrigger data-testid="filter-source-type">
                  <SelectValue placeholder="Chọn loại nguồn" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Tất cả (Chat + Agent)</SelectItem>
                  <SelectItem value="chat">Chat Assistant Session</SelectItem>
                  <SelectItem value="agent">Agent Session</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {/* 2. Specific Chat Assistant / Agent Filter */}
            <div className="space-y-1">
              <label className="text-xs text-text-secondary font-medium">
                Ứng dụng (Chat / Agent)
              </label>
              <Select
                value={appId}
                onValueChange={(v) => {
                  setAppId(v);
                  setPagination((p) => ({ ...p, current: 1 }));
                }}
              >
                <SelectTrigger data-testid="filter-app-id">
                  <SelectValue placeholder="Tất cả ứng dụng" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Tất cả ứng dụng</SelectItem>
                  {appOptions.map((app) => (
                    <SelectItem key={app.id} value={app.id}>
                      [{app.source_type === 'chat' ? 'Chat' : 'Agent'}]{' '}
                      {app.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* 3. Feedback Status Filter */}
            <div className="space-y-1">
              <label className="text-xs text-text-secondary font-medium">
                Trạng thái đánh giá
              </label>
              <Select
                value={feedbackFilter}
                onValueChange={(v) => {
                  setFeedbackFilter(v);
                  setPagination((p) => ({ ...p, current: 1 }));
                }}
              >
                <SelectTrigger data-testid="filter-feedback-status">
                  <SelectValue placeholder="Tất cả trạng thái" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Tất cả phiên</SelectItem>
                  <SelectItem value="rated">Chỉ phiên có đánh giá</SelectItem>
                  <SelectItem value="positive">
                    👍 Đánh giá Tích cực (Like)
                  </SelectItem>
                  <SelectItem value="negative">
                    👎 Đánh giá Tiêu cực (Dislike)
                  </SelectItem>
                  <SelectItem value="unrated">Chưa có đánh giá</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {/* 4. CSAT Score Filter */}
            <div className="space-y-1">
              <label className="text-xs text-text-secondary font-medium">
                Mức điểm CSAT (1-5 ⭐)
              </label>
              <Select
                value={csatFilter}
                onValueChange={(v) => {
                  setCsatFilter(v);
                  setPagination((p) => ({ ...p, current: 1 }));
                }}
              >
                <SelectTrigger data-testid="filter-csat-score">
                  <SelectValue placeholder="Tất cả mức CSAT" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Tất cả mức CSAT</SelectItem>
                  <SelectItem value="low">≤ 2 ⭐ (Không hài lòng)</SelectItem>
                  <SelectItem value="mid">3 ⭐ (Bình thường)</SelectItem>
                  <SelectItem value="high">≥ 4 ⭐ (Hài lòng)</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {/* 5. Channel Filter */}
            <div className="space-y-1">
              <label className="text-xs text-text-secondary font-medium">
                Kênh tương tác
              </label>
              <Select
                value={channelFilter}
                onValueChange={(v) => {
                  setChannelFilter(v);
                  setPagination((p) => ({ ...p, current: 1 }));
                }}
              >
                <SelectTrigger data-testid="filter-channel">
                  <SelectValue placeholder="Tất cả kênh" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Tất cả kênh</SelectItem>
                  <SelectItem value="web">Web nội bộ</SelectItem>
                  <SelectItem value="widget">Widget / Chia sẻ công khai</SelectItem>
                  <SelectItem value="telegram">Telegram Bot</SelectItem>
                </SelectContent>
              </Select>
            </div>

            {/* 6. Criterion Filter */}
            <div className="space-y-1">
              <label className="text-xs text-text-secondary font-medium">
                Lọc theo Tiêu chí nghiệp vụ
              </label>
              <Select
                value={criterionFilter}
                onValueChange={(v) => {
                  setCriterionFilter(v);
                  setPagination((p) => ({ ...p, current: 1 }));
                }}
              >
                <SelectTrigger data-testid="filter-criterion">
                  <SelectValue placeholder="Tất cả tiêu chí" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">Tất cả tiêu chí</SelectItem>
                  {criterionSelectOptions.map(([key, label]) => (
                    <React.Fragment key={key}>
                      <SelectItem value={`${key}:failed`}>
                        ⚠️ Lỗi: {label}
                      </SelectItem>
                      <SelectItem value={`${key}:all`}>
                        Có chấm: {label}
                      </SelectItem>
                    </React.Fragment>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-3 pt-2 border-t border-border-button">
            <div className="flex flex-wrap items-center gap-3">
              <div className="flex items-center gap-2">
                <span className="text-xs text-text-secondary whitespace-nowrap">
                  Từ khóa:
                </span>
                <SearchInput
                  value={keywords}
                  onChange={(e) => setKeywords(e.target.value)}
                  placeholder="Tìm theo ID, nội dung, nhận xét..."
                  className="w-64"
                  data-testid="filter-keywords-input"
                />
              </div>
              <div className="flex items-center gap-2">
                <span className="text-xs text-text-secondary whitespace-nowrap">
                  Thời gian:
                </span>
                <DatePickerWithRange
                  required
                  selected={currentDate}
                  onSelect={(range) =>
                    range?.from &&
                    setCurrentDate({ from: range.from, to: range.to })
                  }
                />
              </div>
            </div>

            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={handleResetFilters}
                data-testid="reset-feedback-filters-btn"
              >
                {t('common.reset')}
              </Button>
            </div>
          </div>
        </div>

        {/* Unified Sessions Table */}
        <div
          className="border border-border-button rounded-xl overflow-hidden bg-bg-card"
          data-testid="feedback-sessions-table"
        >
          <Table rootClassName="max-h-[calc(100vh-380px)]">
            <TableHeader className="sticky top-0 bg-bg-title z-10 shadow-sm">
              <TableRow>
                <TableHead>Nguồn / Ứng dụng</TableHead>
                <TableHead>Kênh</TableHead>
                <TableHead>Câu hỏi / Tiêu đề phiên</TableHead>
                <TableHead
                  className="cursor-pointer hover:bg-muted/50"
                  onClick={() => handleSort('csat_score')}
                >
                  Kết quả Đánh giá (CSAT & Tiêu chí)
                  {sortConfig.orderby === 'csat_score' && (
                    <span className="ml-1">{sortConfig.desc ? '↓' : '↑'}</span>
                  )}
                </TableHead>
                <TableHead
                  className="cursor-pointer hover:bg-muted/50"
                  onClick={() => handleSort('round')}
                >
                  Số lượt
                  {sortConfig.orderby === 'round' && (
                    <span className="ml-1">{sortConfig.desc ? '↓' : '↑'}</span>
                  )}
                </TableHead>
                <TableHead
                  className="cursor-pointer hover:bg-muted/50"
                  onClick={() => handleSort('update_time')}
                >
                  Cập nhật lúc
                  {sortConfig.orderby === 'update_time' && (
                    <span className="ml-1">{sortConfig.desc ? '↓' : '↑'}</span>
                  )}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading && (
                <TableRow>
                  <TableCell colSpan={6} className="h-24 text-center">
                    <Spin size="large">
                      <span className="sr-only">Loading...</span>
                    </Spin>
                  </TableCell>
                </TableRow>
              )}
              {!loading &&
                sessions.map((item) => (
                  <TableRow
                    key={`${item.storage_table}-${item.id}`}
                    className="cursor-pointer hover:bg-muted/30"
                    onClick={() => showLogDetail(item)}
                    data-testid={`feedback-session-row-${item.id}`}
                  >
                    <TableCell>
                      <div className="flex flex-col gap-0.5">
                        <div className="flex items-center gap-1.5 font-medium text-sm">
                          {item.source_type === 'chat' ? (
                            <MessageSquare className="size-3.5 text-sky-400 shrink-0" />
                          ) : (
                            <Bot className="size-3.5 text-amber-400 shrink-0" />
                          )}
                          <span className="truncate max-w-[180px]">
                            {item.app_name}
                          </span>
                        </div>
                        <span className="text-[11px] text-text-secondary font-mono">
                          {item.source_type === 'chat'
                            ? 'Chat Assistant'
                            : 'Agent'}{' '}
                          · {item.id.slice(0, 8)}...
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>{renderChannelBadge(item.channel)}</TableCell>
                    <TableCell>
                      <div className="max-w-xs truncate font-medium text-sm">
                        {item.name ||
                          (item.messages?.length
                            ? item.messages[0]?.content
                            : '—')}
                      </div>
                      {item.user_id && (
                        <div className="text-[11px] text-text-secondary truncate max-w-xs">
                          User: {item.user_id}
                        </div>
                      )}
                    </TableCell>
                    <TableCell>{renderFeedbackSummaryCell(item)}</TableCell>
                    <TableCell>{item.round}</TableCell>
                    <TableCell className="text-xs text-text-secondary whitespace-nowrap">
                      {formatDate(item.update_date)}
                    </TableCell>
                  </TableRow>
                ))}
              {!loading && sessions.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className="h-24 text-center text-text-secondary"
                  >
                    {t('common.noData')}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>

        <div className="flex justify-end">
          <RAGFlowPagination
            current={pagination.current}
            pageSize={pagination.pageSize}
            total={total}
            onChange={(page, pageSize) =>
              setPagination({
                current: page || 1,
                pageSize: pageSize || 10,
              })
            }
          />
        </div>
      </div>

      <AgentLogDetailModal
        isOpen={openModal}
        message={(modalData?.messages || modalData?.message || []) as IAgentLogMessage[]}
        reference={modalData?.reference as unknown as IReferenceObject}
        feedbackConfig={modalData?.feedback_config}
        title={
          modalData
            ? `[${modalData.source_type === 'chat' ? 'Chat' : 'Agent'}: ${modalData.app_name}] ${modalData.name || modalData.id}`
            : ''
        }
        onClose={() => setOpenModal(false)}
      />
    </div>
  );
};

export default SessionFeedbackAuditView;
