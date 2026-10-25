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

import { LlmIcon } from '@/components/svg-icon';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import message from '@/components/ui/message';
import { Modal } from '@/components/ui/modal/modal';
import request from '@/utils/request';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  Activity,
  AlertTriangle,
  BarChart3,
  Calendar,
  Code2,
  Coins,
  Copy,
  Cpu,
  DollarSign,
  Download,
  Edit3,
  Filter,
  Layers,
  RefreshCw,
  Settings2,
  TrendingUp,
  Zap,
} from 'lucide-react';
import React, { useCallback, useEffect, useMemo, useState } from 'react';

interface TokenDashboardKPI {
  selected_month: string;
  total_calls: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  avg_latency_ms: number;
  actual_mtd_cost_usd: number;
  actual_mtd_cost_vnd: number;
  prev_month_tokens: number;
  prev_month_cost_usd: number;
  mom_token_growth_pct: number;
  mom_cost_growth_pct: number;
  daily_burn_rate_usd: number;
  daily_burn_rate_tokens: number;
  forecasted_month_end_cost_usd: number;
  forecasted_month_end_cost_vnd: number;
  forecasted_month_end_tokens: number;
  forecast_p90_cost_usd: number;
  forecast_next_month_cost_usd: number;
  monthly_budget_usd: number;
  alert_threshold_pct: number;
  budget_utilization_pct: number;
  forecasted_budget_pct: number;
  budget_status: 'normal' | 'warning' | 'exceeded';
  estimated_exhaustion_date: string;
  top_provider_name: string;
  top_provider_cost_usd: number;
  top_provider_share_pct: number;
  display_currency: string;
  exchange_rate_vnd: number;
  elapsed_days_in_month: number;
  total_days_in_month: number;
  remaining_days_in_month: number;
}

interface DailyTokenCostPoint {
  date: string;
  day_of_month: number;
  is_future: boolean;
  calls: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  daily_cost_usd: number;
  cumulative_actual_cost_usd: number;
  forecasted_daily_cost_usd: number;
  cumulative_forecast_cost_usd: number;
  cumulative_p90_cost_usd: number;
  monthly_budget_line_usd: number;
}

interface MonthlyTokenCostPoint {
  month: string;
  is_forecast: boolean;
  calls: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  actual_cost_usd: number;
  forecasted_cost_usd: number;
  by_provider_cost_usd: Record<string, number>;
}

interface ProviderTokenCostSummary {
  provider_name: string;
  calls: number;
  model_count: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  input_cost_usd: number;
  output_cost_usd: number;
  total_cost_usd: number;
  cost_share_pct: number;
  token_share_pct: number;
  daily_burn_rate_usd: number;
  forecasted_month_end_cost_usd: number;
  forecast_p90_cost_usd: number;
  forecast_next_month_cost_usd: number;
  provider_budget_usd: number;
  budget_utilization_pct: number;
  budget_status: string;
  avg_latency_ms: number;
}

interface ModelTokenCostSummary {
  provider_name: string;
  model_name: string;
  model_type: string;
  calls: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  input_price_per_1m: number;
  output_price_per_1m: number;
  is_custom_price: boolean;
  input_cost_usd: number;
  output_cost_usd: number;
  total_cost_usd: number;
  cost_share_pct: number;
  daily_burn_rate_usd: number;
  forecasted_month_end_cost_usd: number;
  avg_latency_ms: number;
}

interface AppTokenCostSummary {
  app_id: string;
  app_name: string;
  app_type: string;
  calls: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  total_cost_usd: number;
  cost_share_pct: number;
}

interface TokenUsageLogItem {
  id: string;
  provider_name: string;
  model_name: string;
  model_type: string;
  app_name: string;
  app_type: string;
  session_id: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  total_cost_usd: number;
  response_time_ms: number;
  is_estimated: boolean;
  stat_date: string;
  create_date: string;
}

interface PricingCatalogEntry {
  id: string;
  provider_name: string;
  model_name: string;
  model_type: string;
  input_price_per_1m: number;
  output_price_per_1m: number;
  currency: string;
  is_custom: boolean;
  is_configured: boolean;
}

interface TokenDashboardResponse {
  kpi: TokenDashboardKPI;
  daily_series: DailyTokenCostPoint[];
  monthly_history: MonthlyTokenCostPoint[];
  provider_breakdown: ProviderTokenCostSummary[];
  model_breakdown: ModelTokenCostSummary[];
  app_breakdown: AppTokenCostSummary[];
  available_filters: {
    months: string[];
    providers: string[];
    models: string[];
    model_types: string[];
    app_types: string[];
  };
}

const PROVIDER_COLORS = [
  '#10b981',
  '#3b82f6',
  '#f59e0b',
  '#8b5cf6',
  '#ec4899',
  '#06b6d4',
  '#f97316',
];

const formatNumber = (n: number | undefined | null): string => {
  if (n === undefined || n === null || Number.isNaN(n)) return '0';
  return Math.round(n).toLocaleString('en-US');
};

const currentYearMonth = (): string => {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`;
};

const TokenCostDashboardPage: React.FC = () => {
  const [selectedMonth, setSelectedMonth] = useState<string>(currentYearMonth());
  const [providerFilter, setProviderFilter] = useState<string>('all');
  const [modelTypeFilter, setModelTypeFilter] = useState<string>('all');
  const [appTypeFilter, setAppTypeFilter] = useState<string>('all');
  const [currency, setCurrency] = useState<'USD' | 'VND'>('USD');
  const [chartMode, setChartMode] = useState<'daily' | 'monthly'>('daily');
  const [activeTab, setActiveTab] = useState<
    'providers' | 'models' | 'apps' | 'logs'
  >('providers');

  const [loading, setLoading] = useState<boolean>(false);
  const [dashboard, setDashboard] = useState<TokenDashboardResponse | null>(
    null,
  );
  const [logs, setLogs] = useState<TokenUsageLogItem[]>([]);
  const [logsTotal, setLogsTotal] = useState<number>(0);

  // Pricing & Budget modal state
  const [pricingModalOpen, setPricingModalOpen] = useState<boolean>(false);
  const [apiModalOpen, setApiModalOpen] = useState<boolean>(false);
  const [pricingEntries, setPricingEntries] = useState<PricingCatalogEntry[]>(
    [],
  );
  const [budgetForm, setBudgetForm] = useState({
    monthly_budget_usd: 100,
    alert_threshold_pct: 80,
    display_currency: 'USD',
    exchange_rate_vnd: 25500,
  });
  const [priceForm, setPriceForm] = useState({
    provider_name: 'OpenAI',
    model_name: 'gpt-4o-mini',
    model_type: 'chat',
    input_price_per_1m: 0.15,
    output_price_per_1m: 0.6,
    recalculate_month: true,
  });
  const [savingConfig, setSavingConfig] = useState<boolean>(false);

  const exchangeRate = dashboard?.kpi?.exchange_rate_vnd || 25500;

  const formatMoney = useCallback(
    (usd: number | undefined | null, digits = 4): string => {
      const val = Number(usd || 0);
      if (currency === 'VND') {
        const vnd = Math.round(val * exchangeRate);
        return `${vnd.toLocaleString('vi-VN')} ₫`;
      }
      if (val > 0 && val < 0.01 && digits < 4) {
        return `$${val.toFixed(4)}`;
      }
      return `$${val.toFixed(digits)}`;
    },
    [currency, exchangeRate],
  );

  const fetchDashboard = useCallback(async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams();
      if (selectedMonth) params.set('month', selectedMonth);
      if (providerFilter !== 'all') params.set('provider', providerFilter);
      if (modelTypeFilter !== 'all') params.set('model_type', modelTypeFilter);
      if (appTypeFilter !== 'all') params.set('app_type', appTypeFilter);

      const { data: res } = await request.get(
        `/api/v1/system/token-usage/dashboard?${params.toString()}`,
      );
      if (res?.code === 0 && res?.data) {
        setDashboard(res.data);
      }
    } catch (err) {
      // handled by request interceptor
    } finally {
      setLoading(false);
    }
  }, [selectedMonth, providerFilter, modelTypeFilter, appTypeFilter]);

  const fetchLogs = useCallback(async () => {
    try {
      const params = new URLSearchParams();
      if (selectedMonth) params.set('month', selectedMonth);
      if (providerFilter !== 'all') params.set('provider', providerFilter);
      if (modelTypeFilter !== 'all') params.set('model_type', modelTypeFilter);
      if (appTypeFilter !== 'all') params.set('app_type', appTypeFilter);
      params.set('page', '1');
      params.set('page_size', '50');

      const { data: res } = await request.get(
        `/api/v1/system/token-usage/logs?${params.toString()}`,
      );
      if (res?.code === 0 && res?.data) {
        setLogs(res.data.items || []);
        setLogsTotal(res.data.total || 0);
      }
    } catch {
      // ignore
    }
  }, [selectedMonth, providerFilter, modelTypeFilter, appTypeFilter]);

  const fetchPricingConfig = useCallback(async () => {
    try {
      const { data: res } = await request.get(
        '/api/v1/system/token-usage/pricing',
      );
      if (res?.code === 0 && res?.data) {
        setPricingEntries(res.data.pricing_list || []);
        if (res.data.global_budget) {
          setBudgetForm({
            monthly_budget_usd:
              res.data.global_budget.monthly_budget_usd ?? 100,
            alert_threshold_pct:
              res.data.global_budget.alert_threshold_pct ?? 80,
            display_currency: res.data.global_budget.display_currency || 'USD',
            exchange_rate_vnd:
              res.data.global_budget.exchange_rate_vnd || 25500,
          });
        }
      }
    } catch {
      // ignore
    }
  }, []);

  useEffect(() => {
    fetchDashboard();
    fetchLogs();
  }, [fetchDashboard, fetchLogs]);

  const handleOpenPricingModal = useCallback(
    (prefill?: {
      provider_name: string;
      model_name: string;
      model_type: string;
      input_price_per_1m: number;
      output_price_per_1m: number;
    }) => {
      if (prefill) {
        setPriceForm({
          provider_name: prefill.provider_name,
          model_name: prefill.model_name,
          model_type: prefill.model_type || 'chat',
          input_price_per_1m: prefill.input_price_per_1m,
          output_price_per_1m: prefill.output_price_per_1m,
          recalculate_month: true,
        });
      }
      fetchPricingConfig();
      setPricingModalOpen(true);
    },
    [fetchPricingConfig],
  );

  const handleSaveBudget = async () => {
    setSavingConfig(true);
    try {
      const { data: res } = await request.put(
        '/api/v1/system/token-usage/budget',
        {
          data: {
            provider_name: 'ALL',
            ...budgetForm,
          },
        },
      );
      if (res?.code === 0) {
        message.success('Đã lưu cấu hình hạn mức ngân sách tháng');
        fetchDashboard();
      }
    } finally {
      setSavingConfig(false);
    }
  };

  const handleSavePricing = async () => {
    if (!priceForm.provider_name.trim() || !priceForm.model_name.trim()) {
      message.error('Vui lòng nhập tên Provider và Model');
      return;
    }
    setSavingConfig(true);
    try {
      const { data: res } = await request.put(
        '/api/v1/system/token-usage/pricing',
        {
          data: priceForm,
        },
      );
      if (res?.code === 0) {
        message.success(
          `Đã cập nhật đơn giá cho ${priceForm.provider_name} / ${priceForm.model_name}`,
        );
        fetchPricingConfig();
        fetchDashboard();
        fetchLogs();
      }
    } finally {
      setSavingConfig(false);
    }
  };

  const handleExportCSV = () => {
    if (!dashboard) return;
    const rows: string[] = [
      'Month,Provider,Model,Type,Calls,InputTokens,OutputTokens,TotalTokens,InputPricePer1M_USD,OutputPricePer1M_USD,ActualCost_USD,DailyBurnRate_USD,ForecastedMonthEnd_USD',
    ];
    for (const m of dashboard.model_breakdown || []) {
      rows.push(
        [
          dashboard.kpi.selected_month,
          `"${m.provider_name}"`,
          `"${m.model_name}"`,
          m.model_type,
          m.calls,
          m.input_tokens,
          m.output_tokens,
          m.total_tokens,
          m.input_price_per_1m,
          m.output_price_per_1m,
          m.total_cost_usd,
          m.daily_burn_rate_usd,
          m.forecasted_month_end_cost_usd,
        ].join(','),
      );
    }
    const blob = new Blob(['\uFEFF' + rows.join('\n')], {
      type: 'text/csv;charset=utf-8;',
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `ragflow_token_cost_${selectedMonth}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const kpi = dashboard?.kpi;
  const dailySeries = dashboard?.daily_series || [];
  const monthlyHistory = dashboard?.monthly_history || [];
  const providers = dashboard?.provider_breakdown || [];
  const models = dashboard?.model_breakdown || [];
  const apps = dashboard?.app_breakdown || [];

  // Compute SVG chart geometry for Daily Cumulative Actual + Forecasted Trajectory
  const svgChart = useMemo(() => {
    const width = 760;
    const height = 230;
    const padLeft = 58;
    const padRight = 24;
    const padTop = 20;
    const padBottom = 32;
    const innerW = width - padLeft - padRight;
    const innerH = height - padTop - padBottom;

    if (dailySeries.length === 0) {
      return null;
    }

    let maxY = 0.01;
    for (const pt of dailySeries) {
      maxY = Math.max(
        maxY,
        pt.cumulative_actual_cost_usd || 0,
        pt.cumulative_forecast_cost_usd || 0,
        pt.cumulative_p90_cost_usd || 0,
      );
    }
    if (kpi && kpi.monthly_budget_usd > 0) {
      maxY = Math.max(maxY, kpi.monthly_budget_usd * 1.05);
    } else {
      maxY = maxY * 1.15;
    }

    const xForIndex = (idx: number) => {
      if (dailySeries.length <= 1) return padLeft;
      return padLeft + (idx / (dailySeries.length - 1)) * innerW;
    };
    const yForVal = (val: number) => {
      const clamped = Math.max(0, Math.min(val, maxY));
      return padTop + innerH - (clamped / maxY) * innerH;
    };

    const actualPoints: string[] = [];
    const forecastPoints: string[] = [];
    const p90Points: string[] = [];

    dailySeries.forEach((pt, idx) => {
      const x = xForIndex(idx);
      if (!pt.is_future) {
        const y = yForVal(pt.cumulative_actual_cost_usd);
        actualPoints.push(`${x.toFixed(1)},${y.toFixed(1)}`);
        if (pt.cumulative_forecast_cost_usd > 0) {
          forecastPoints.push(`${x.toFixed(1)},${y.toFixed(1)}`);
          p90Points.push(`${x.toFixed(1)},${y.toFixed(1)}`);
        }
      } else {
        const yf = yForVal(pt.cumulative_forecast_cost_usd);
        const yp90 = yForVal(pt.cumulative_p90_cost_usd);
        forecastPoints.push(`${x.toFixed(1)},${yf.toFixed(1)}`);
        p90Points.push(`${x.toFixed(1)},${yp90.toFixed(1)}`);
      }
    });

    const budgetY =
      kpi && kpi.monthly_budget_usd > 0
        ? yForVal(kpi.monthly_budget_usd)
        : null;

    return {
      width,
      height,
      padLeft,
      padRight,
      padTop,
      padBottom,
      innerW,
      innerH,
      maxY,
      actualPolyline: actualPoints.join(' '),
      forecastPolyline: forecastPoints.join(' '),
      p90Polyline: p90Points.join(' '),
      budgetY,
      xForIndex,
      yForVal,
    };
  }, [dailySeries, kpi]);

  const apiCurlSnippet = useMemo(() => {
    const origin =
      typeof window !== 'undefined'
        ? window.location.origin
        : 'http://localhost';
    const params = new URLSearchParams();
    params.set('month', selectedMonth);
    if (providerFilter !== 'all') params.set('provider', providerFilter);
    if (modelTypeFilter !== 'all') params.set('model_type', modelTypeFilter);
    if (appTypeFilter !== 'all') params.set('app_type', appTypeFilter);
    return `curl -X GET "${origin}/api/v1/system/token-usage/dashboard?${params.toString()}" \\\n  -H "Authorization: Bearer <YOUR_API_KEY_OR_JWT>"`;
  }, [selectedMonth, providerFilter, modelTypeFilter, appTypeFilter]);

  return (
    <section
      className="flex flex-col w-full h-full overflow-y-auto px-6 py-5 gap-5 text-text-primary"
      data-testid="token-cost-dashboard-page"
    >
      {/* Top Header */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-border-button pb-4">
        <div className="flex items-center gap-3">
          <div className="p-2.5 rounded-lg bg-accent-primary/10 text-accent-primary">
            <BarChart3 className="size-6" />
          </div>
          <div>
            <h1 className="text-xl font-semibold text-text-primary flex items-center gap-2">
              Quản Lý Token Tiêu Thụ & Dự Báo Chi Phí (FinOps)
              <Badge variant="secondary" className="text-xs font-normal">
                Tháng {selectedMonth}
              </Badge>
            </h1>
            <p className="text-xs text-text-secondary mt-0.5">
              Theo dõi thời gian thực lượng Input/Output Token, chi phí phát
              sinh và dự phóng cuối tháng theo từng LLM Provider & Model
            </p>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {/* Currency Switcher */}
          <div
            className="inline-flex rounded-md border border-border-button bg-bg-input p-0.5"
            data-testid="currency-switcher"
          >
            <button
              type="button"
              onClick={() => setCurrency('USD')}
              className={`px-2.5 py-1 text-xs font-medium rounded transition-colors ${
                currency === 'USD'
                  ? 'bg-bg-card text-text-primary shadow-sm'
                  : 'text-text-secondary hover:text-text-primary'
              }`}
            >
              USD ($)
            </button>
            <button
              type="button"
              onClick={() => setCurrency('VND')}
              className={`px-2.5 py-1 text-xs font-medium rounded transition-colors ${
                currency === 'VND'
                  ? 'bg-bg-card text-text-primary shadow-sm'
                  : 'text-text-secondary hover:text-text-primary'
              }`}
            >
              VNĐ (₫)
            </button>
          </div>

          <Button
            variant="outline"
            size="sm"
            onClick={() => handleOpenPricingModal()}
            data-testid="btn-open-pricing-modal"
            className="gap-1.5"
          >
            <Settings2 className="size-4" />
            Đơn giá & Ngân sách
          </Button>

          <Button
            variant="outline"
            size="sm"
            onClick={() => setApiModalOpen(true)}
            data-testid="btn-open-finops-api-modal"
            className="gap-1.5"
          >
            <Code2 className="size-4" />
            API Tích hợp Bên thứ 3
          </Button>

          <Button
            variant="outline"
            size="sm"
            onClick={handleExportCSV}
            data-testid="btn-export-token-csv"
            className="gap-1.5"
          >
            <Download className="size-4" />
            Xuất CSV
          </Button>

          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              fetchDashboard();
              fetchLogs();
            }}
            data-testid="btn-refresh-token-dashboard"
            className="gap-1.5"
          >
            <RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />
          </Button>
        </div>
      </div>

      {/* Unified Filter Bar */}
      <div
        className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-3 bg-bg-card border border-border-button rounded-lg p-3.5"
        data-testid="token-usage-filter-bar"
      >
        <div className="flex flex-col gap-1.5">
          <label className="text-xs text-text-secondary flex items-center gap-1">
            <Calendar className="size-3.5" /> Tháng báo cáo
          </label>
          <Select
            value={selectedMonth}
            onValueChange={(val) => setSelectedMonth(val)}
          >
            <SelectTrigger
              data-testid="filter-select-month"
              className="h-9 w-full bg-bg-input border-border-button text-text-primary"
            >
              <SelectValue placeholder="Chọn tháng" />
            </SelectTrigger>
            <SelectContent className="bg-bg-card border-border-button text-text-primary">
              {(
                dashboard?.available_filters?.months || [currentYearMonth()]
              ).map((m) => (
                <SelectItem
                  key={m}
                  value={m}
                  className="text-text-primary hover:bg-border-button focus:bg-border-button focus:text-text-primary cursor-pointer"
                >
                  Tháng {m}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="flex flex-col gap-1.5">
          <label className="text-xs text-text-secondary flex items-center gap-1">
            <Cpu className="size-3.5" /> Nhà cung cấp (LLM Provider)
          </label>
          <Select
            value={providerFilter}
            onValueChange={(val) => setProviderFilter(val)}
          >
            <SelectTrigger
              data-testid="filter-select-provider"
              className="h-9 w-full bg-bg-input border-border-button text-text-primary"
            >
              <SelectValue placeholder="Tất cả LLM Providers" />
            </SelectTrigger>
            <SelectContent className="bg-bg-card border-border-button text-text-primary">
              <SelectItem
                value="all"
                className="text-text-primary hover:bg-border-button focus:bg-border-button focus:text-text-primary cursor-pointer"
              >
                Tất cả LLM Providers
              </SelectItem>
              {(dashboard?.available_filters?.providers || []).map((p) => (
                <SelectItem
                  key={p}
                  value={p}
                  className="text-text-primary hover:bg-border-button focus:bg-border-button focus:text-text-primary cursor-pointer"
                >
                  {p}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="flex flex-col gap-1.5">
          <label className="text-xs text-text-secondary flex items-center gap-1">
            <Layers className="size-3.5" /> Loại mô hình (Model Type)
          </label>
          <Select
            value={modelTypeFilter}
            onValueChange={(val) => setModelTypeFilter(val)}
          >
            <SelectTrigger
              data-testid="filter-select-model-type"
              className="h-9 w-full bg-bg-input border-border-button text-text-primary"
            >
              <SelectValue placeholder="Tất cả loại mô hình" />
            </SelectTrigger>
            <SelectContent className="bg-bg-card border-border-button text-text-primary">
              <SelectItem
                value="all"
                className="text-text-primary hover:bg-border-button focus:bg-border-button focus:text-text-primary cursor-pointer"
              >
                Tất cả loại mô hình
              </SelectItem>
              <SelectItem
                value="chat"
                className="text-text-primary hover:bg-border-button focus:bg-border-button focus:text-text-primary cursor-pointer"
              >
                Chat / Reasoning LLM
              </SelectItem>
              <SelectItem
                value="embedding"
                className="text-text-primary hover:bg-border-button focus:bg-border-button focus:text-text-primary cursor-pointer"
              >
                Embedding (Vector hóa)
              </SelectItem>
              <SelectItem
                value="rerank"
                className="text-text-primary hover:bg-border-button focus:bg-border-button focus:text-text-primary cursor-pointer"
              >
                Rerank (Xếp hạng lại)
              </SelectItem>
              <SelectItem
                value="ocr"
                className="text-text-primary hover:bg-border-button focus:bg-border-button focus:text-text-primary cursor-pointer"
              >
                OCR / Image2Text
              </SelectItem>
              <SelectItem
                value="speech2text"
                className="text-text-primary hover:bg-border-button focus:bg-border-button focus:text-text-primary cursor-pointer"
              >
                ASR / Speech2Text
              </SelectItem>
              <SelectItem
                value="tts"
                className="text-text-primary hover:bg-border-button focus:bg-border-button focus:text-text-primary cursor-pointer"
              >
                TTS / Text2Speech
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div className="flex flex-col gap-1.5">
          <label className="text-xs text-text-secondary flex items-center gap-1">
            <Filter className="size-3.5" /> Nguồn tác vụ (Operation Source)
          </label>
          <Select
            value={appTypeFilter}
            onValueChange={(val) => setAppTypeFilter(val)}
          >
            <SelectTrigger
              data-testid="filter-select-app-type"
              className="h-9 w-full bg-bg-input border-border-button text-text-primary"
            >
              <SelectValue placeholder="Tất cả nguồn tác vụ" />
            </SelectTrigger>
            <SelectContent className="bg-bg-card border-border-button text-text-primary">
              <SelectItem
                value="all"
                className="text-text-primary hover:bg-border-button focus:bg-border-button focus:text-text-primary cursor-pointer"
              >
                Tất cả nguồn tác vụ
              </SelectItem>
              <SelectItem
                value="chat"
                className="text-text-primary hover:bg-border-button focus:bg-border-button focus:text-text-primary cursor-pointer"
              >
                Chat Assistant
              </SelectItem>
              <SelectItem
                value="agent"
                className="text-text-primary hover:bg-border-button focus:bg-border-button focus:text-text-primary cursor-pointer"
              >
                Agent Workflow
              </SelectItem>
              <SelectItem
                value="ingestion"
                className="text-text-primary hover:bg-border-button focus:bg-border-button focus:text-text-primary cursor-pointer"
              >
                Document Ingestion
              </SelectItem>
              <SelectItem
                value="retrieval"
                className="text-text-primary hover:bg-border-button focus:bg-border-button focus:text-text-primary cursor-pointer"
              >
                Retrieval & Rerank
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div className="flex flex-col justify-end">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setSelectedMonth(currentYearMonth());
              setProviderFilter('all');
              setModelTypeFilter('all');
              setAppTypeFilter('all');
            }}
            data-testid="btn-reset-token-filters"
            className="h-9 border border-border-button text-text-primary hover:bg-border-button"
          >
            Đặt lại bộ lọc
          </Button>
        </div>
      </div>

      {/* 4 Executive KPI Cards */}
      <div
        className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4"
        data-testid="token-kpi-cards"
      >
        {/* KPI 1: Total Tokens */}
        <Card className="border-border-button bg-bg-card">
          <CardHeader className="pb-2 flex flex-row items-center justify-between space-y-0">
            <CardTitle className="text-xs font-medium text-text-secondary">
              TỔNG TOKEN TIÊU THỤ TRONG THÁNG
            </CardTitle>
            <Zap className="size-4 text-amber-400" />
          </CardHeader>
          <CardContent>
            <div
              className="text-2xl font-bold text-text-primary"
              data-testid="kpi-total-tokens"
            >
              {formatNumber(kpi?.total_tokens)}
            </div>
            <div className="mt-2 flex items-center justify-between text-xs text-text-secondary">
              <span>
                Input:{' '}
                <strong className="text-text-primary">
                  {formatNumber(kpi?.input_tokens)}
                </strong>
              </span>
              <span>
                Output:{' '}
                <strong className="text-text-primary">
                  {formatNumber(kpi?.output_tokens)}
                </strong>
              </span>
            </div>
            <div className="mt-1.5 flex items-center justify-between text-xs text-text-secondary border-t border-border-button/60 pt-1.5">
              <span>Tổng lượt gọi: {formatNumber(kpi?.total_calls)} calls</span>
              <span>Độ trễ TB: {formatNumber(kpi?.avg_latency_ms)} ms</span>
            </div>
          </CardContent>
        </Card>

        {/* KPI 2: Actual MTD Cost */}
        <Card className="border-border-button bg-bg-card">
          <CardHeader className="pb-2 flex flex-row items-center justify-between space-y-0">
            <CardTitle className="text-xs font-medium text-text-secondary">
              CHI PHÍ THỰC TẾ LŨY KẾ (MTD COST)
            </CardTitle>
            <DollarSign className="size-4 text-emerald-400" />
          </CardHeader>
          <CardContent>
            <div
              className="text-2xl font-bold text-emerald-400"
              data-testid="kpi-actual-cost"
            >
              {formatMoney(kpi?.actual_mtd_cost_usd, 4)}
            </div>
            <div className="mt-2">
              <div className="flex justify-between text-xs text-text-secondary mb-1">
                <span>
                  Ngân sách tháng:{' '}
                  {formatMoney(kpi?.monthly_budget_usd || 100, 2)}
                </span>
                <span>{(kpi?.budget_utilization_pct || 0).toFixed(1)}%</span>
              </div>
              <div className="w-full h-1.5 bg-bg-input rounded-full overflow-hidden">
                <div
                  className={`h-full rounded-full transition-all ${
                    kpi?.budget_status === 'exceeded'
                      ? 'bg-red-500'
                      : kpi?.budget_status === 'warning'
                        ? 'bg-amber-500'
                        : 'bg-emerald-500'
                  }`}
                  style={{
                    width: `${Math.min(100, kpi?.budget_utilization_pct || 0)}%`,
                  }}
                />
              </div>
            </div>
            <div className="mt-1.5 flex items-center justify-between text-xs text-text-secondary pt-1">
              <span>
                Đã qua {kpi?.elapsed_days_in_month || 0}/
                {kpi?.total_days_in_month || 30} ngày
              </span>
              <span>
                Tháng trước: {formatMoney(kpi?.prev_month_cost_usd, 4)}
              </span>
            </div>
          </CardContent>
        </Card>

        {/* KPI 3: Forecasted Month-End Cost */}
        <Card className="border-border-button bg-bg-card">
          <CardHeader className="pb-2 flex flex-row items-center justify-between space-y-0">
            <CardTitle className="text-xs font-medium text-text-secondary">
              DỰ BÁO CHI PHÍ CUỐI THÁNG (FORECAST)
            </CardTitle>
            <TrendingUp className="size-4 text-sky-400" />
          </CardHeader>
          <CardContent>
            <div className="flex items-baseline gap-2">
              <div
                className="text-2xl font-bold text-sky-400"
                data-testid="kpi-forecast-cost"
              >
                {formatMoney(kpi?.forecasted_month_end_cost_usd, 4)}
              </div>
              {kpi?.budget_status === 'exceeded' ? (
                <Badge variant="destructive" className="text-[10px]">
                  Dự kiến vượt ngân sách
                </Badge>
              ) : kpi?.budget_status === 'warning' ? (
                <Badge variant="secondary" className="text-[10px] text-amber-400">
                  Sắp chạm ngưỡng
                </Badge>
              ) : (
                <Badge
                  variant="secondary"
                  className="text-[10px] text-emerald-400"
                >
                  Trong hạn mức
                </Badge>
              )}
            </div>
            <div className="mt-2 flex items-center justify-between text-xs text-text-secondary">
              <span>
                Ngưỡng thận trọng (P90):{' '}
                <strong className="text-text-primary">
                  {formatMoney(kpi?.forecast_p90_cost_usd, 4)}
                </strong>
              </span>
            </div>
            <div className="mt-1.5 flex items-center justify-between text-xs text-text-secondary border-t border-border-button/60 pt-1.5">
              <span>
                Dự báo tháng kế tiếp:{' '}
                <strong className="text-text-primary">
                  {formatMoney(kpi?.forecast_next_month_cost_usd, 4)}
                </strong>
              </span>
              {kpi?.estimated_exhaustion_date && (
                <span className="text-red-400 flex items-center gap-0.5">
                  <AlertTriangle className="size-3" /> Chạm trần:{' '}
                  {kpi.estimated_exhaustion_date}
                </span>
              )}
            </div>
          </CardContent>
        </Card>

        {/* KPI 4: Daily Burn Rate & Top Provider */}
        <Card className="border-border-button bg-bg-card">
          <CardHeader className="pb-2 flex flex-row items-center justify-between space-y-0">
            <CardTitle className="text-xs font-medium text-text-secondary">
              TỐC ĐỘ ĐỐT / NGÀY & TOP PROVIDER
            </CardTitle>
            <Activity className="size-4 text-purple-400" />
          </CardHeader>
          <CardContent>
            <div
              className="text-2xl font-bold text-text-primary"
              data-testid="kpi-burn-rate"
            >
              {formatMoney(kpi?.daily_burn_rate_usd, 4)}{' '}
              <span className="text-xs font-normal text-text-secondary">
                / ngày
              </span>
            </div>
            <div className="mt-2 flex items-center justify-between text-xs text-text-secondary">
              <span>
                Trung bình:{' '}
                <strong className="text-text-primary">
                  {formatNumber(kpi?.daily_burn_rate_tokens)} tokens/ngày
                </strong>
              </span>
            </div>
            <div className="mt-1.5 flex items-center justify-between text-xs text-text-secondary border-t border-border-button/60 pt-1.5">
              <span className="truncate">
                Top Provider:{' '}
                <strong
                  className="text-text-primary"
                  data-testid="kpi-top-provider"
                >
                  {kpi?.top_provider_name || '-'}
                </strong>
              </span>
              <span className="text-emerald-400 font-medium">
                {(kpi?.top_provider_share_pct || 0).toFixed(1)}% chi phí
              </span>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Charts Row: Left = Cumulative Trajectory & Forecast, Right = Provider Breakdown */}
      <div
        className="grid grid-cols-1 lg:grid-cols-12 gap-4"
        data-testid="token-charts-section"
      >
        {/* Chart 1: Actual vs Forecasted Trajectory */}
        <Card className="lg:col-span-7 border-border-button bg-bg-card">
          <CardHeader className="pb-2 flex flex-row flex-wrap items-center justify-between gap-2">
            <div>
              <CardTitle className="text-sm font-semibold text-text-primary">
                Biểu Đồ Chi Phí Lũy Kế & Đường Dự Báo Cuối Tháng
              </CardTitle>
              <p className="text-xs text-text-secondary">
                Đường liền: Chi phí thực tế lũy kế (MTD) • Đường nét đứt: Dự
                phóng EWMA đến cuối tháng & P90
              </p>
            </div>
            <div className="inline-flex rounded-md border border-border-button bg-bg-input p-0.5">
              <button
                type="button"
                onClick={() => setChartMode('daily')}
                className={`px-2.5 py-1 text-xs rounded ${
                  chartMode === 'daily'
                    ? 'bg-bg-card text-text-primary font-medium'
                    : 'text-text-secondary'
                }`}
              >
                Theo Ngày ({selectedMonth})
              </button>
              <button
                type="button"
                onClick={() => setChartMode('monthly')}
                className={`px-2.5 py-1 text-xs rounded ${
                  chartMode === 'monthly'
                    ? 'bg-bg-card text-text-primary font-medium'
                    : 'text-text-secondary'
                }`}
              >
                Xu hướng 6 Tháng & Tháng tới
              </button>
            </div>
          </CardHeader>
          <CardContent>
            {chartMode === 'daily' && svgChart ? (
              <div className="w-full overflow-x-auto">
                <svg
                  viewBox={`0 0 ${svgChart.width} ${svgChart.height}`}
                  className="w-full h-56"
                  data-testid="daily-forecast-svg-chart"
                >
                  {/* Horizontal Grid Lines */}
                  {[0, 0.25, 0.5, 0.75, 1].map((ratio) => {
                    const y = svgChart.padTop + svgChart.innerH * (1 - ratio);
                    const val = svgChart.maxY * ratio;
                    return (
                      <g key={ratio}>
                        <line
                          x1={svgChart.padLeft}
                          y1={y}
                          x2={svgChart.width - svgChart.padRight}
                          y2={y}
                          stroke="currentColor"
                          className="text-border-button/50"
                          strokeDasharray="3 3"
                        />
                        <text
                          x={svgChart.padLeft - 6}
                          y={y + 4}
                          textAnchor="end"
                          className="fill-text-secondary text-[10px]"
                        >
                          {formatMoney(val, 2)}
                        </text>
                      </g>
                    );
                  })}

                  {/* Budget Limit Line */}
                  {svgChart.budgetY !== null && (
                    <g>
                      <line
                        x1={svgChart.padLeft}
                        y1={svgChart.budgetY}
                        x2={svgChart.width - svgChart.padRight}
                        y2={svgChart.budgetY}
                        stroke="#ef4444"
                        strokeWidth="1.5"
                        strokeDasharray="6 4"
                      />
                      <text
                        x={svgChart.width - svgChart.padRight - 4}
                        y={svgChart.budgetY - 5}
                        textAnchor="end"
                        fill="#ef4444"
                        className="text-[10px] font-medium"
                      >
                        Trần ngân sách ({formatMoney(kpi?.monthly_budget_usd, 2)})
                      </text>
                    </g>
                  )}

                  {/* P90 Upper Bound Dashed Line */}
                  {svgChart.p90Polyline && (
                    <polyline
                      fill="none"
                      stroke="#f59e0b"
                      strokeWidth="1.5"
                      strokeDasharray="2 4"
                      points={svgChart.p90Polyline}
                    />
                  )}

                  {/* Forecasted Cumulative Trajectory Dashed Line */}
                  {svgChart.forecastPolyline && (
                    <polyline
                      fill="none"
                      stroke="#38bdf8"
                      strokeWidth="2.2"
                      strokeDasharray="6 4"
                      points={svgChart.forecastPolyline}
                    />
                  )}

                  {/* Actual Cumulative Cost Solid Line */}
                  {svgChart.actualPolyline && (
                    <polyline
                      fill="none"
                      stroke="#10b981"
                      strokeWidth="2.8"
                      points={svgChart.actualPolyline}
                    />
                  )}

                  {/* X-Axis Day Labels */}
                  {dailySeries.map((pt, idx) => {
                    if (
                      idx !== 0 &&
                      idx !== dailySeries.length - 1 &&
                      pt.day_of_month % 5 !== 0
                    ) {
                      return null;
                    }
                    const x = svgChart.xForIndex(idx);
                    return (
                      <text
                        key={pt.date}
                        x={x}
                        y={svgChart.height - 8}
                        textAnchor="middle"
                        className="fill-text-secondary text-[10px]"
                      >
                        {pt.date.slice(5)}
                      </text>
                    );
                  })}
                </svg>

                <div className="flex flex-wrap items-center justify-center gap-5 text-xs text-text-secondary mt-1">
                  <span className="flex items-center gap-1.5">
                    <span className="w-3 h-0.5 bg-emerald-500 inline-block" />
                    Chi phí thực tế lũy kế (Actual MTD)
                  </span>
                  <span className="flex items-center gap-1.5">
                    <span className="w-3 h-0.5 bg-sky-400 inline-block border-b border-dashed" />
                    Quỹ đạo dự báo cuối tháng (Forecast P50)
                  </span>
                  <span className="flex items-center gap-1.5">
                    <span className="w-3 h-0.5 bg-amber-500 inline-block" />
                    Biên độ thận trọng (P90 Upper Bound)
                  </span>
                </div>
              </div>
            ) : (
              /* Monthly History + Next Month Bar View */
              <div
                className="flex flex-col gap-2.5 py-2"
                data-testid="monthly-history-chart"
              >
                {monthlyHistory.map((m) => {
                  const maxMonthly = Math.max(
                    0.001,
                    ...monthlyHistory.map((x) =>
                      Math.max(x.actual_cost_usd, x.forecasted_cost_usd),
                    ),
                  );
                  const displayCost = m.is_forecast
                    ? m.forecasted_cost_usd
                    : m.actual_cost_usd;
                  const pct = Math.min(100, (displayCost / maxMonthly) * 100);
                  return (
                    <div key={m.month} className="flex items-center gap-3">
                      <div className="w-28 text-xs font-medium text-text-primary flex items-center gap-1">
                        <span>{m.month}</span>
                        {m.is_forecast && (
                          <Badge
                            variant="secondary"
                            className="text-[10px] px-1 py-0 text-sky-400"
                          >
                            Dự báo
                          </Badge>
                        )}
                      </div>
                      <div className="flex-1 h-5 bg-bg-input rounded overflow-hidden relative">
                        <div
                          className={`h-full transition-all ${
                            m.is_forecast
                              ? 'bg-sky-500/70 border-r-2 border-dashed border-sky-300'
                              : 'bg-emerald-500/80'
                          }`}
                          style={{ width: `${Math.max(pct, 2)}%` }}
                        />
                      </div>
                      <div className="w-36 text-right text-xs font-semibold text-text-primary">
                        {formatMoney(displayCost, 4)}
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </CardContent>
        </Card>

        {/* Chart 2: Provider Breakdown Visual */}
        <Card className="lg:col-span-5 border-border-button bg-bg-card">
          <CardHeader className="pb-2">
            <CardTitle className="text-sm font-semibold text-text-primary">
              Phân Bổ Chi Phí & Dự Báo Theo Từng LLM Provider
            </CardTitle>
            <p className="text-xs text-text-secondary">
              So sánh tỷ trọng tiêu thụ và dự phóng cuối tháng giữa các nhà cung
              cấp
            </p>
          </CardHeader>
          <CardContent>
            {providers.length === 0 ? (
              <div className="h-48 flex items-center justify-center text-xs text-text-secondary">
                Chưa có dữ liệu gọi LLM trong tháng {selectedMonth}
              </div>
            ) : (
              <div className="flex flex-col gap-3.5">
                {providers.map((p, idx) => {
                  const color = PROVIDER_COLORS[idx % PROVIDER_COLORS.length];
                  return (
                    <div
                      key={p.provider_name}
                      className="flex flex-col gap-1.5 p-2.5 rounded-lg bg-bg-input/50 border border-border-button/60"
                      data-testid={`provider-card-${p.provider_name}`}
                    >
                      <div className="flex items-center justify-between">
                        <div className="flex items-center gap-2">
                          <LlmIcon
                            name={p.provider_name}
                            width={20}
                            height={20}
                            imgClass="size-5"
                          />
                          <span className="text-sm font-semibold text-text-primary">
                            {p.provider_name}
                          </span>
                          <Badge variant="secondary" className="text-[10px]">
                            {p.model_count} models • {formatNumber(p.calls)}{' '}
                            calls
                          </Badge>
                        </div>
                        <div className="text-right">
                          <span className="text-sm font-bold text-emerald-400">
                            {formatMoney(p.total_cost_usd, 4)}
                          </span>
                          <span className="text-xs text-text-secondary ml-1.5">
                            ({p.cost_share_pct.toFixed(1)}%)
                          </span>
                        </div>
                      </div>

                      {/* Share Progress Bar */}
                      <div className="w-full h-2 bg-bg-base rounded-full overflow-hidden">
                        <div
                          className="h-full rounded-full"
                          style={{
                            width: `${Math.max(p.cost_share_pct, 3)}%`,
                            backgroundColor: color,
                          }}
                        />
                      </div>

                      <div className="flex flex-wrap items-center justify-between text-[11px] text-text-secondary pt-0.5">
                        <span>
                          Tokens:{' '}
                          <strong className="text-text-primary">
                            {formatNumber(p.total_tokens)}
                          </strong>{' '}
                          (In: {formatNumber(p.input_tokens)} / Out:{' '}
                          {formatNumber(p.output_tokens)})
                        </span>
                        <span className="text-sky-400 font-medium">
                          Dự báo cuối tháng:{' '}
                          {formatMoney(p.forecasted_month_end_cost_usd, 4)}
                        </span>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Detailed Breakdown Tabs: Providers / Models / Apps / Audit Logs */}
      <Card className="border-border-button bg-bg-card">
        <CardHeader className="pb-3 border-b border-border-button">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => setActiveTab('providers')}
                data-testid="tab-provider-breakdown"
                className={`px-3 py-1.5 rounded-md text-xs font-medium transition-colors ${
                  activeTab === 'providers'
                    ? 'bg-accent-primary text-white'
                    : 'bg-bg-input text-text-secondary hover:text-text-primary'
                }`}
              >
                Theo LLM Provider ({providers.length})
              </button>
              <button
                type="button"
                onClick={() => setActiveTab('models')}
                data-testid="tab-model-breakdown"
                className={`px-3 py-1.5 rounded-md text-xs font-medium transition-colors ${
                  activeTab === 'models'
                    ? 'bg-accent-primary text-white'
                    : 'bg-bg-input text-text-secondary hover:text-text-primary'
                }`}
              >
                Theo từng Model & Đơn giá ({models.length})
              </button>
              <button
                type="button"
                onClick={() => setActiveTab('apps')}
                data-testid="tab-app-breakdown"
                className={`px-3 py-1.5 rounded-md text-xs font-medium transition-colors ${
                  activeTab === 'apps'
                    ? 'bg-accent-primary text-white'
                    : 'bg-bg-input text-text-secondary hover:text-text-primary'
                }`}
              >
                Theo Chat Assistant / Agent ({apps.length})
              </button>
              <button
                type="button"
                onClick={() => setActiveTab('logs')}
                data-testid="tab-usage-logs"
                className={`px-3 py-1.5 rounded-md text-xs font-medium transition-colors ${
                  activeTab === 'logs'
                    ? 'bg-accent-primary text-white'
                    : 'bg-bg-input text-text-secondary hover:text-text-primary'
                }`}
              >
                Sổ cái Chi tiết Lượt gọi ({formatNumber(logsTotal)})
              </button>
            </div>
          </div>
        </CardHeader>

        <CardContent className="pt-4 overflow-x-auto">
          {activeTab === 'providers' && (
            <table
              className="w-full text-left border-collapse text-xs"
              data-testid="table-provider-breakdown"
            >
              <thead>
                <tr className="border-b border-border-button text-text-secondary">
                  <th className="py-2.5 px-3">LLM Provider</th>
                  <th className="py-2.5 px-3 text-right">Số Model</th>
                  <th className="py-2.5 px-3 text-right">Lượt gọi</th>
                  <th className="py-2.5 px-3 text-right">Input Tokens</th>
                  <th className="py-2.5 px-3 text-right">Output Tokens</th>
                  <th className="py-2.5 px-3 text-right">Tổng Tokens</th>
                  <th className="py-2.5 px-3 text-right">
                    Chi phí Thực tế (MTD)
                  </th>
                  <th className="py-2.5 px-3 text-right">Tốc độ đốt / ngày</th>
                  <th className="py-2.5 px-3 text-right">
                    Dự báo Cuối tháng (P50)
                  </th>
                  <th className="py-2.5 px-3 text-right">Ngưỡng P90</th>
                  <th className="py-2.5 px-3 text-right">Dự báo Tháng tới</th>
                  <th className="py-2.5 px-3 text-right">Tỷ trọng</th>
                </tr>
              </thead>
              <tbody>
                {providers.map((p) => (
                  <tr
                    key={p.provider_name}
                    className="border-b border-border-button/50 hover:bg-bg-input/40"
                  >
                    <td className="py-2.5 px-3 font-medium text-text-primary flex items-center gap-2">
                      <LlmIcon
                        name={p.provider_name}
                        width={18}
                        height={18}
                        imgClass="size-4"
                      />
                      {p.provider_name}
                    </td>
                    <td className="py-2.5 px-3 text-right">{p.model_count}</td>
                    <td className="py-2.5 px-3 text-right">
                      {formatNumber(p.calls)}
                    </td>
                    <td className="py-2.5 px-3 text-right">
                      {formatNumber(p.input_tokens)}
                    </td>
                    <td className="py-2.5 px-3 text-right">
                      {formatNumber(p.output_tokens)}
                    </td>
                    <td className="py-2.5 px-3 text-right font-semibold">
                      {formatNumber(p.total_tokens)}
                    </td>
                    <td className="py-2.5 px-3 text-right font-semibold text-emerald-400">
                      {formatMoney(p.total_cost_usd, 4)}
                    </td>
                    <td className="py-2.5 px-3 text-right">
                      {formatMoney(p.daily_burn_rate_usd, 4)}/ngày
                    </td>
                    <td className="py-2.5 px-3 text-right font-semibold text-sky-400">
                      {formatMoney(p.forecasted_month_end_cost_usd, 4)}
                    </td>
                    <td className="py-2.5 px-3 text-right text-amber-400">
                      {formatMoney(p.forecast_p90_cost_usd, 4)}
                    </td>
                    <td className="py-2.5 px-3 text-right">
                      {formatMoney(p.forecast_next_month_cost_usd, 4)}
                    </td>
                    <td className="py-2.5 px-3 text-right">
                      {p.cost_share_pct.toFixed(1)}%
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          {activeTab === 'models' && (
            <table
              className="w-full text-left border-collapse text-xs"
              data-testid="table-model-breakdown"
            >
              <thead>
                <tr className="border-b border-border-button text-text-secondary">
                  <th className="py-2.5 px-3">Provider</th>
                  <th className="py-2.5 px-3">Model</th>
                  <th className="py-2.5 px-3">Loại</th>
                  <th className="py-2.5 px-3 text-right">Lượt gọi</th>
                  <th className="py-2.5 px-3 text-right">Input Tokens</th>
                  <th className="py-2.5 px-3 text-right">Output Tokens</th>
                  <th className="py-2.5 px-3 text-right">
                    Đơn giá In ($/1M)
                  </th>
                  <th className="py-2.5 px-3 text-right">
                    Đơn giá Out ($/1M)
                  </th>
                  <th className="py-2.5 px-3 text-right">Chi phí MTD</th>
                  <th className="py-2.5 px-3 text-right">
                    Dự báo Cuối tháng
                  </th>
                  <th className="py-2.5 px-3 text-center">Đơn giá</th>
                </tr>
              </thead>
              <tbody>
                {models.map((m) => (
                  <tr
                    key={`${m.provider_name}-${m.model_name}-${m.model_type}`}
                    className="border-b border-border-button/50 hover:bg-bg-input/40"
                  >
                    <td className="py-2.5 px-3 font-medium">
                      {m.provider_name}
                    </td>
                    <td className="py-2.5 px-3 font-mono text-text-primary">
                      {m.model_name}
                    </td>
                    <td className="py-2.5 px-3">
                      <Badge variant="secondary" className="text-[10px]">
                        {m.model_type}
                      </Badge>
                    </td>
                    <td className="py-2.5 px-3 text-right">
                      {formatNumber(m.calls)}
                    </td>
                    <td className="py-2.5 px-3 text-right">
                      {formatNumber(m.input_tokens)}
                    </td>
                    <td className="py-2.5 px-3 text-right">
                      {formatNumber(m.output_tokens)}
                    </td>
                    <td className="py-2.5 px-3 text-right">
                      ${m.input_price_per_1m.toFixed(3)}
                    </td>
                    <td className="py-2.5 px-3 text-right">
                      ${m.output_price_per_1m.toFixed(3)}
                    </td>
                    <td className="py-2.5 px-3 text-right font-semibold text-emerald-400">
                      {formatMoney(m.total_cost_usd, 4)}
                    </td>
                    <td className="py-2.5 px-3 text-right font-semibold text-sky-400">
                      {formatMoney(m.forecasted_month_end_cost_usd, 4)}
                    </td>
                    <td className="py-2.5 px-3 text-center">
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-7 px-2 text-xs gap-1"
                        onClick={() =>
                          handleOpenPricingModal({
                            provider_name: m.provider_name,
                            model_name: m.model_name,
                            model_type: m.model_type,
                            input_price_per_1m: m.input_price_per_1m,
                            output_price_per_1m: m.output_price_per_1m,
                          })
                        }
                      >
                        <Edit3 className="size-3" /> Sửa giá
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          {activeTab === 'apps' && (
            <table
              className="w-full text-left border-collapse text-xs"
              data-testid="table-app-breakdown"
            >
              <thead>
                <tr className="border-b border-border-button text-text-secondary">
                  <th className="py-2.5 px-3">Tên Ứng dụng / Nguồn</th>
                  <th className="py-2.5 px-3">Phân loại</th>
                  <th className="py-2.5 px-3 text-right">Số lượt gọi</th>
                  <th className="py-2.5 px-3 text-right">Input Tokens</th>
                  <th className="py-2.5 px-3 text-right">Output Tokens</th>
                  <th className="py-2.5 px-3 text-right">Tổng Tokens</th>
                  <th className="py-2.5 px-3 text-right">Chi phí phát sinh</th>
                  <th className="py-2.5 px-3 text-right">% Tỷ trọng</th>
                </tr>
              </thead>
              <tbody>
                {apps.map((a) => (
                  <tr
                    key={`${a.app_type}-${a.app_id}`}
                    className="border-b border-border-button/50 hover:bg-bg-input/40"
                  >
                    <td className="py-2.5 px-3 font-medium text-text-primary">
                      {a.app_name || a.app_id || 'Hệ thống'}
                    </td>
                    <td className="py-2.5 px-3">
                      <Badge variant="secondary" className="text-[10px]">
                        {a.app_type}
                      </Badge>
                    </td>
                    <td className="py-2.5 px-3 text-right">
                      {formatNumber(a.calls)}
                    </td>
                    <td className="py-2.5 px-3 text-right">
                      {formatNumber(a.input_tokens)}
                    </td>
                    <td className="py-2.5 px-3 text-right">
                      {formatNumber(a.output_tokens)}
                    </td>
                    <td className="py-2.5 px-3 text-right font-semibold">
                      {formatNumber(a.total_tokens)}
                    </td>
                    <td className="py-2.5 px-3 text-right font-semibold text-emerald-400">
                      {formatMoney(a.total_cost_usd, 4)}
                    </td>
                    <td className="py-2.5 px-3 text-right">
                      {a.cost_share_pct.toFixed(1)}%
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          {activeTab === 'logs' && (
            <table
              className="w-full text-left border-collapse text-xs"
              data-testid="table-usage-logs"
            >
              <thead>
                <tr className="border-b border-border-button text-text-secondary">
                  <th className="py-2.5 px-3">Thời gian</th>
                  <th className="py-2.5 px-3">Provider</th>
                  <th className="py-2.5 px-3">Model</th>
                  <th className="py-2.5 px-3">Loại</th>
                  <th className="py-2.5 px-3">Ứng dụng</th>
                  <th className="py-2.5 px-3 text-right">Input</th>
                  <th className="py-2.5 px-3 text-right">Output</th>
                  <th className="py-2.5 px-3 text-right">Tổng Token</th>
                  <th className="py-2.5 px-3 text-right">Chi phí</th>
                  <th className="py-2.5 px-3 text-right">Độ trễ</th>
                </tr>
              </thead>
              <tbody>
                {logs.map((l) => (
                  <tr
                    key={l.id}
                    className="border-b border-border-button/50 hover:bg-bg-input/40"
                  >
                    <td className="py-2 px-3 text-text-secondary">
                      {l.create_date
                        ? new Date(l.create_date).toLocaleString('vi-VN')
                        : l.stat_date}
                    </td>
                    <td className="py-2 px-3 font-medium">{l.provider_name}</td>
                    <td className="py-2 px-3 font-mono">{l.model_name}</td>
                    <td className="py-2 px-3">
                      <Badge variant="secondary" className="text-[10px]">
                        {l.model_type}
                      </Badge>
                    </td>
                    <td className="py-2 px-3">
                      {l.app_name || l.app_type || '-'}
                    </td>
                    <td className="py-2 px-3 text-right">
                      {formatNumber(l.input_tokens)}
                    </td>
                    <td className="py-2 px-3 text-right">
                      {formatNumber(l.output_tokens)}
                    </td>
                    <td className="py-2 px-3 text-right font-semibold">
                      {formatNumber(l.total_tokens)}
                    </td>
                    <td className="py-2 px-3 text-right text-emerald-400 font-medium">
                      {formatMoney(l.total_cost_usd, 6)}
                    </td>
                    <td className="py-2 px-3 text-right">
                      {formatNumber(l.response_time_ms)} ms
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>

      {/* Modal 1: Pricing & Monthly Budget Settings */}
      <Modal
        open={pricingModalOpen}
        onOpenChange={setPricingModalOpen}
        title="Cấu Hình Đơn Giá Token & Hạn Mức Ngân Sách Tháng"
        className="max-w-3xl"
        showfooter={false}
      >
        <div
          className="flex flex-col gap-5 text-sm text-text-primary max-h-[75vh] overflow-y-auto pr-1"
          data-testid="pricing-budget-modal"
        >
          {/* Section A: Monthly Budget & Exchange Rate */}
          <div className="p-4 rounded-lg border border-border-button bg-bg-input/40 flex flex-col gap-3">
            <h3 className="font-semibold text-sm flex items-center gap-2">
              <Coins className="size-4 text-emerald-400" />
              1. Hạn Mức Ngân Sách Tháng & Tỷ Giá Quy Đổi
            </h3>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
              <div>
                <label className="text-xs text-text-secondary block mb-1">
                  Ngân sách mục tiêu tháng (USD)
                </label>
                <Input
                  type="number"
                  step="1"
                  value={budgetForm.monthly_budget_usd}
                  onChange={(e) =>
                    setBudgetForm((prev) => ({
                      ...prev,
                      monthly_budget_usd: Number(e.target.value),
                    }))
                  }
                  data-testid="input-monthly-budget-usd"
                />
              </div>
              <div>
                <label className="text-xs text-text-secondary block mb-1">
                  Ngưỡng cảnh báo sớm (%)
                </label>
                <Input
                  type="number"
                  step="5"
                  value={budgetForm.alert_threshold_pct}
                  onChange={(e) =>
                    setBudgetForm((prev) => ({
                      ...prev,
                      alert_threshold_pct: Number(e.target.value),
                    }))
                  }
                />
              </div>
              <div>
                <label className="text-xs text-text-secondary block mb-1">
                  Tỷ giá USD → VNĐ
                </label>
                <Input
                  type="number"
                  step="100"
                  value={budgetForm.exchange_rate_vnd}
                  onChange={(e) =>
                    setBudgetForm((prev) => ({
                      ...prev,
                      exchange_rate_vnd: Number(e.target.value),
                    }))
                  }
                />
              </div>
            </div>
            <div className="flex justify-end">
              <Button
                size="sm"
                onClick={handleSaveBudget}
                disabled={savingConfig}
                data-testid="btn-save-budget"
              >
                Lưu Hạn Mức Ngân Sách
              </Button>
            </div>
          </div>

          {/* Section B: Add/Override Unit Price per Provider & Model */}
          <div className="p-4 rounded-lg border border-border-button bg-bg-input/40 flex flex-col gap-3">
            <h3 className="font-semibold text-sm flex items-center gap-2">
              <DollarSign className="size-4 text-sky-400" />
              2. Cập Nhật Đơn Giá Theo LLM Provider & Model (USD / 1M Tokens)
            </h3>
            <div className="grid grid-cols-1 sm:grid-cols-5 gap-2.5">
              <div>
                <label className="text-xs text-text-secondary block mb-1">
                  LLM Provider
                </label>
                <Input
                  value={priceForm.provider_name}
                  onChange={(e) =>
                    setPriceForm((prev) => ({
                      ...prev,
                      provider_name: e.target.value,
                    }))
                  }
                  placeholder="VD: OpenAI, Gemini..."
                  data-testid="input-price-provider"
                />
              </div>
              <div>
                <label className="text-xs text-text-secondary block mb-1">
                  Tên Model (hoặc *)
                </label>
                <Input
                  value={priceForm.model_name}
                  onChange={(e) =>
                    setPriceForm((prev) => ({
                      ...prev,
                      model_name: e.target.value,
                    }))
                  }
                  placeholder="VD: gpt-4o-mini"
                  data-testid="input-price-model"
                />
              </div>
              <div>
                <label className="text-xs text-text-secondary block mb-1">
                  Loại Model
                </label>
                <select
                  value={priceForm.model_type}
                  onChange={(e) =>
                    setPriceForm((prev) => ({
                      ...prev,
                      model_type: e.target.value,
                    }))
                  }
                  className="h-9 w-full rounded-md border border-border-button bg-bg-input px-2 text-xs"
                >
                  <option value="chat">chat</option>
                  <option value="embedding">embedding</option>
                  <option value="rerank">rerank</option>
                  <option value="ocr">ocr</option>
                </select>
              </div>
              <div>
                <label className="text-xs text-text-secondary block mb-1">
                  Giá Input ($/1M)
                </label>
                <Input
                  type="number"
                  step="0.01"
                  value={priceForm.input_price_per_1m}
                  onChange={(e) =>
                    setPriceForm((prev) => ({
                      ...prev,
                      input_price_per_1m: Number(e.target.value),
                    }))
                  }
                  data-testid="input-price-in"
                />
              </div>
              <div>
                <label className="text-xs text-text-secondary block mb-1">
                  Giá Output ($/1M)
                </label>
                <Input
                  type="number"
                  step="0.01"
                  value={priceForm.output_price_per_1m}
                  onChange={(e) =>
                    setPriceForm((prev) => ({
                      ...prev,
                      output_price_per_1m: Number(e.target.value),
                    }))
                  }
                  data-testid="input-price-out"
                />
              </div>
            </div>

            <div className="flex items-center justify-between pt-1">
              <label className="flex items-center gap-2 text-xs text-text-secondary cursor-pointer">
                <input
                  type="checkbox"
                  checked={priceForm.recalculate_month}
                  onChange={(e) =>
                    setPriceForm((prev) => ({
                      ...prev,
                      recalculate_month: e.target.checked,
                    }))
                  }
                />
                Áp dụng và tính toán lại ngay cho các lượt gọi của Model này
                trong tháng
              </label>
              <Button
                size="sm"
                onClick={handleSavePricing}
                disabled={savingConfig}
                data-testid="btn-save-pricing"
              >
                Lưu Đơn Giá Model
              </Button>
            </div>
          </div>

          {/* Section C: Reference Table of Configured & Default Prices */}
          <div className="flex flex-col gap-2">
            <h4 className="text-xs font-semibold text-text-secondary uppercase">
              Bảng Đơn Giá Áp Dụng Hiện Tại ({pricingEntries.length} models)
            </h4>
            <div className="max-h-56 overflow-y-auto border border-border-button rounded-md">
              <table className="w-full text-left text-xs">
                <thead className="bg-bg-input sticky top-0">
                  <tr className="border-b border-border-button text-text-secondary">
                    <th className="py-2 px-3">Provider</th>
                    <th className="py-2 px-3">Model</th>
                    <th className="py-2 px-3">Loại</th>
                    <th className="py-2 px-3 text-right">Input ($/1M)</th>
                    <th className="py-2 px-3 text-right">Output ($/1M)</th>
                    <th className="py-2 px-3 text-center">Nguồn</th>
                  </tr>
                </thead>
                <tbody>
                  {pricingEntries.slice(0, 40).map((pe, idx) => (
                    <tr
                      key={`${pe.provider_name}-${pe.model_name}-${idx}`}
                      className="border-b border-border-button/40 hover:bg-bg-input/30 cursor-pointer"
                      onClick={() =>
                        setPriceForm({
                          provider_name: pe.provider_name,
                          model_name: pe.model_name,
                          model_type: pe.model_type,
                          input_price_per_1m: pe.input_price_per_1m,
                          output_price_per_1m: pe.output_price_per_1m,
                          recalculate_month: true,
                        })
                      }
                    >
                      <td className="py-1.5 px-3 font-medium">
                        {pe.provider_name}
                      </td>
                      <td className="py-1.5 px-3 font-mono">{pe.model_name}</td>
                      <td className="py-1.5 px-3">{pe.model_type}</td>
                      <td className="py-1.5 px-3 text-right">
                        ${pe.input_price_per_1m.toFixed(3)}
                      </td>
                      <td className="py-1.5 px-3 text-right">
                        ${pe.output_price_per_1m.toFixed(3)}
                      </td>
                      <td className="py-1.5 px-3 text-center">
                        {pe.is_custom ? (
                          <Badge
                            variant="secondary"
                            className="text-[10px] text-emerald-400"
                          >
                            Custom
                          </Badge>
                        ) : (
                          <span className="text-[10px] text-text-secondary">
                            Mặc định
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </Modal>

      {/* Modal 2: Third-Party API Integration Guide */}
      <Modal
        open={apiModalOpen}
        onOpenChange={setApiModalOpen}
        title="Tài Liệu REST API Báo Cáo Token & Dự Báo Chi Phí (Bên Thứ 3)"
        className="max-w-2xl"
        showfooter={false}
      >
        <div
          className="flex flex-col gap-4 text-xs text-text-primary"
          data-testid="finops-api-modal"
        >
          <p className="text-text-secondary">
            Hệ thống cung cấp bộ REST API v1 đồng nhất với các bộ lọc trên
            Dashboard để tích hợp trực tiếp vào ERP, hệ thống kế toán hoặc công
            cụ FinOps bên thứ 3:
          </p>

          <div className="p-3 rounded-md bg-bg-input border border-border-button flex flex-col gap-2">
            <div className="flex items-center justify-between">
              <span className="font-semibold text-emerald-400">
                1. Lấy KPI Tổng hợp, Phân rã theo Provider & Dự báo Chi phí
                Tháng
              </span>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 px-2 gap-1"
                onClick={() => {
                  navigator.clipboard.writeText(apiCurlSnippet);
                  message.success('Đã sao chép lệnh cURL');
                }}
              >
                <Copy className="size-3.5" /> Copy cURL
              </Button>
            </div>
            <pre className="p-2.5 rounded bg-bg-base overflow-x-auto font-mono text-[11px] text-text-primary">
              {apiCurlSnippet}
            </pre>
          </div>

          <div className="p-3 rounded-md bg-bg-input border border-border-button flex flex-col gap-1.5">
            <span className="font-semibold text-sky-400">
              2. Các Endpoint Quản trị Đơn giá, Ngân sách & Sổ cái Chi tiết
            </span>
            <ul className="list-disc list-inside space-y-1 text-text-secondary font-mono text-[11px]">
              <li>
                GET /api/v1/system/token-usage/logs?month={selectedMonth}
                &provider=OpenAI&page=1&page_size=50
              </li>
              <li>GET /api/v1/system/token-usage/pricing</li>
              <li>
                PUT /api/v1/system/token-usage/pricing (Body: provider_name,
                model_name, input_price_per_1m, output_price_per_1m)
              </li>
              <li>
                PUT /api/v1/system/token-usage/budget (Body: provider_name,
                monthly_budget_usd, alert_threshold_pct)
              </li>
            </ul>
          </div>
        </div>
      </Modal>
    </section>
  );
};

export default TokenCostDashboardPage;
