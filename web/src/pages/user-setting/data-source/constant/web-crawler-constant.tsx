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

import { FormFieldType } from '@/components/dynamic-form';
import { TFunction } from 'i18next';

export const webCrawlerConstant = (t: TFunction) => [
  {
    label: t('setting.webCrawlerStartUrl'),
    name: 'config.start_url',
    type: FormFieldType.Text,
    required: true,
    placeholder: 'https://cms.demego.vn/',
    tooltip: t('setting.webCrawlerStartUrlTip'),
  },
  {
    label: t('setting.webCrawlerCrawlMode'),
    name: 'config.crawl_mode',
    type: FormFieldType.Select,
    required: false,
    options: [
      {
        label: t('setting.webCrawlerModeAuto'),
        value: 'auto',
      },
      {
        label: t('setting.webCrawlerModeHtmlLinks'),
        value: 'html_links',
      },
      {
        label: t('setting.webCrawlerModeSsrSpa'),
        value: 'ssr_spa',
      },
      {
        label: t('setting.webCrawlerModeSitemapHybrid'),
        value: 'sitemap_hybrid',
      },
    ],
    tooltip: t('setting.webCrawlerCrawlModeTip'),
  },
  {
    label: t('setting.webCrawlerExtractStrategy'),
    name: 'config.extract_strategy',
    type: FormFieldType.Select,
    required: false,
    options: [
      {
        label: t('setting.webCrawlerExtractClean'),
        value: 'article_clean',
      },
      {
        label: t('setting.webCrawlerExtractFull'),
        value: 'full_page',
      },
    ],
    tooltip: t('setting.webCrawlerExtractStrategyTip'),
  },
  {
    label: t('setting.webCrawlerMaxDepth'),
    name: 'config.max_depth',
    type: FormFieldType.Number,
    required: false,
    tooltip: t('setting.webCrawlerMaxDepthTip'),
    validation: {
      min: 0,
      max: 10,
    },
  },
  {
    label: t('setting.webCrawlerMaxPages'),
    name: 'config.max_pages',
    type: FormFieldType.Number,
    required: false,
    tooltip: t('setting.webCrawlerMaxPagesTip'),
    validation: {
      min: 1,
      max: 5000,
    },
  },
  {
    label: t('setting.webCrawlerSameDomainOnly'),
    name: 'config.same_domain_only',
    type: FormFieldType.Switch,
    required: false,
    defaultValue: true,
    tooltip: t('setting.webCrawlerSameDomainOnlyTip'),
  },
  {
    label: t('setting.webCrawlerUrlFilter'),
    name: 'config.url_filter',
    type: FormFieldType.Text,
    required: false,
    placeholder: '^https://cms\\.demego\\.vn/.*',
    tooltip: t('setting.webCrawlerUrlFilterTip'),
  },
  {
    label: t('setting.webCrawlerExcludeUrlFilter'),
    name: 'config.exclude_url_filter',
    type: FormFieldType.Text,
    required: false,
    placeholder: '/(login|register|admin|cart)',
    tooltip: t('setting.webCrawlerExcludeUrlFilterTip'),
  },
  {
    label: t('setting.dataSourceFieldFollowPdfLinks'),
    name: 'config.follow_pdf_links',
    type: FormFieldType.Switch,
    required: false,
    defaultValue: false,
    tooltip: t('setting.sitemapFollowPdfLinksTip'),
  },
  {
    label: t('setting.dataSourceFieldRestrictPdfToDomain'),
    name: 'config.restrict_pdf_to_domain',
    type: FormFieldType.Switch,
    required: false,
    defaultValue: true,
    shouldRender: (formValues: any) =>
      formValues?.config?.follow_pdf_links === true,
    tooltip: t('setting.sitemapRestrictPdfToDomainTip'),
  },
  {
    label: t('setting.webCrawlerCrawlDelayMs'),
    name: 'config.crawl_delay_ms',
    type: FormFieldType.Number,
    required: false,
    tooltip: t('setting.webCrawlerCrawlDelayMsTip'),
    validation: {
      min: 0,
      max: 10000,
    },
  },
  {
    label: t('setting.dataSourceFieldUserAgent'),
    name: 'config.user_agent',
    type: FormFieldType.Text,
    required: false,
    placeholder: 'RAGFlow-WebCrawler/1.0',
    tooltip: t('setting.sitemapUserAgentTip'),
  },
  {
    label: t('setting.dataSourceFieldBatchSize'),
    name: 'config.batch_size',
    type: FormFieldType.Number,
    required: false,
    tooltip: t('setting.sitemapBatchSizeTip'),
    validation: {
      min: 1,
      message: t('setting.dataSourceValidationMinOne', {
        label: t('setting.dataSourceFieldBatchSize'),
      }),
    },
  },
];
