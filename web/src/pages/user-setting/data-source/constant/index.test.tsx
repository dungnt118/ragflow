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

import type { TFunction } from 'i18next';
import { FormFieldType } from '@/components/dynamic-form';
import { buildGithubActionsWorkflowYaml } from './github-workflow-template';
import {
  DataSourceFormDefaultValues,
  DataSourceKey,
  generateDataSourceInfo,
  getDataSourceFieldsWithExtras,
} from './index';

const translate = ((key: string) => key) as TFunction;

describe('Xquik data source', () => {
  it('registers its catalog entry and defaults', () => {
    const info = generateDataSourceInfo(translate)[DataSourceKey.XQUIK];
    const defaults = DataSourceFormDefaultValues[DataSourceKey.XQUIK];

    expect(info.name).toBe('Xquik');
    expect(info.description).toBe('setting.xquikDescription');
    expect(defaults).toEqual({
      name: '',
      source: 'xquik',
      config: {
        query: '',
        query_type: 'Latest',
        page_size: 100,
        max_pages: 10,
        batch_size: 32,
        request_delay: 0.5,
        credentials: { xquik_api_key: '' },
      },
    });
  });

  it('keeps the API key secret and bounds page usage', () => {
    const fields = getDataSourceFieldsWithExtras(
      translate,
      DataSourceKey.XQUIK,
    ) as Array<{
      name: string;
      type?: FormFieldType;
      required?: boolean;
      validation?: { min?: number; max?: number };
    }>;
    const apiKey = fields.find(
      (field) => field.name === 'config.credentials.xquik_api_key',
    );
    const pageSize = fields.find((field) => field.name === 'config.page_size');
    const maxPages = fields.find((field) => field.name === 'config.max_pages');

    expect(apiKey?.type).toBe(FormFieldType.Password);
    expect(apiKey?.required).toBe(true);
    expect(pageSize?.validation).toMatchObject({ min: 1, max: 10000 });
    expect(maxPages?.validation).toMatchObject({ min: 1, max: 1000 });
  });
});

describe('Sitemap data source', () => {
  it('registers its catalog entry and defaults', () => {
    const info = generateDataSourceInfo(translate)[DataSourceKey.SITEMAP];
    const defaults = DataSourceFormDefaultValues[DataSourceKey.SITEMAP];

    expect(info.name).toBe('Sitemap');
    expect(info.description).toBe('setting.sitemapDescription');
    expect(defaults).toEqual({
      name: '',
      source: 'sitemap',
      refresh_freq: 360,
      config: {
        sitemap_url: '',
        url_filter: '',
        follow_pdf_links: false,
        restrict_pdf_to_domain: true,
        user_agent: '',
        batch_size: 10,
      },
    });
  });

  it('requires the sitemap URL and only shows the domain restriction when following PDFs', () => {
    const fields = getDataSourceFieldsWithExtras(
      translate,
      DataSourceKey.SITEMAP,
    ) as Array<{
      name: string;
      type?: FormFieldType;
      required?: boolean;
      validation?: { min?: number };
      shouldRender?: (values: any) => boolean;
    }>;
    const sitemapUrl = fields.find((f) => f.name === 'config.sitemap_url');
    const restrict = fields.find(
      (f) => f.name === 'config.restrict_pdf_to_domain',
    );
    const batchSize = fields.find((f) => f.name === 'config.batch_size');

    expect(sitemapUrl?.required).toBe(true);
    expect(restrict?.type).toBe(FormFieldType.Switch);
    expect(
      restrict?.shouldRender?.({ config: { follow_pdf_links: false } }),
    ).toBe(false);
    expect(
      restrict?.shouldRender?.({ config: { follow_pdf_links: true } }),
    ).toBe(true);
    expect(batchSize?.validation?.min).toBe(1);
  });
});

describe('Web Crawler data source', () => {
  it('registers its catalog entry and defaults', () => {
    const info = generateDataSourceInfo(translate)[DataSourceKey.WEB_CRAWLER];
    const defaults = DataSourceFormDefaultValues[DataSourceKey.WEB_CRAWLER];

    expect(info.name).toBe('Web Crawler (Spider)');
    expect(info.description).toBe('setting.webCrawlerDescription');
    expect(defaults).toEqual({
      name: '',
      source: 'web_crawler',
      refresh_freq: 360,
      config: {
        start_url: '',
        crawl_mode: 'auto',
        extract_strategy: 'article_clean',
        max_depth: 2,
        max_pages: 300,
        same_domain_only: true,
        url_filter: '',
        exclude_url_filter: '',
        follow_pdf_links: false,
        restrict_pdf_to_domain: true,
        crawl_delay_ms: 100,
        user_agent: '',
        batch_size: 10,
      },
    });
  });

  it('exposes crawl mode and extraction strategy selectors with bounded depth and pages', () => {
    const fields = getDataSourceFieldsWithExtras(
      translate,
      DataSourceKey.WEB_CRAWLER,
    ) as Array<{
      name: string;
      type?: FormFieldType;
      required?: boolean;
      options?: Array<{ label: string; value: string }>;
      validation?: { min?: number; max?: number };
    }>;
    const startUrl = fields.find((f) => f.name === 'config.start_url');
    const crawlMode = fields.find((f) => f.name === 'config.crawl_mode');
    const extractStrategy = fields.find(
      (f) => f.name === 'config.extract_strategy',
    );
    const maxDepth = fields.find((f) => f.name === 'config.max_depth');
    const maxPages = fields.find((f) => f.name === 'config.max_pages');

    expect(startUrl?.required).toBe(true);
    expect(crawlMode?.type).toBe(FormFieldType.Select);
    expect(crawlMode?.options?.map((o) => o.value)).toEqual([
      'auto',
      'html_links',
      'ssr_spa',
      'sitemap_hybrid',
    ]);
    expect(extractStrategy?.type).toBe(FormFieldType.Select);
    expect(extractStrategy?.options?.map((o) => o.value)).toEqual([
      'article_clean',
      'full_page',
    ]);
    expect(maxDepth?.validation).toMatchObject({ min: 0, max: 10 });
    expect(maxPages?.validation).toMatchObject({ min: 1, max: 5000 });
  });
});

describe('Google Drive data source', () => {
  it('lets a folder-only connector leave My Drive emails empty', () => {
    const fields = getDataSourceFieldsWithExtras(
      translate,
      DataSourceKey.GOOGLE_DRIVE,
    ) as Array<{ name: string; required?: boolean }>;
    const myDriveEmails = fields.find(
      (field) => field.name === 'config.my_drive_emails',
    );
    const sharedFolderUrls = fields.find(
      (field) => field.name === 'config.shared_folder_urls',
    );

    expect(myDriveEmails?.required).toBe(false);
    expect(sharedFolderUrls?.required).toBe(true);
  });
});

describe.each([
  ['MySQL', DataSourceKey.MYSQL],
  ['PostgreSQL', DataSourceKey.POSTGRESQL],
])('%s data source', (_label, key) => {
  it('exposes a bounded batch size for large table syncs', () => {
    const fields = getDataSourceFieldsWithExtras(translate, key) as Array<{
      name: string;
      validation?: { min?: number };
    }>;
    const batchSize = fields.find((f) => f.name === 'config.batch_size');

    expect(batchSize?.validation?.min).toBe(1);
    expect(DataSourceFormDefaultValues[key].config).toMatchObject({
      batch_size: 32,
    });
  });
});

describe('Azure DevOps data source', () => {
  it('registers its catalog entry and defaults', () => {
    const info = generateDataSourceInfo(translate)[DataSourceKey.AZURE_DEVOPS];
    const defaults = DataSourceFormDefaultValues[DataSourceKey.AZURE_DEVOPS];

    expect(info.name).toBe('Azure DevOps');
    expect(info.description).toBe('setting.azure_devopsDescription');
    expect(defaults).toMatchObject({
      name: '',
      source: DataSourceKey.AZURE_DEVOPS,
      config: {
        base_url: '',
        organization: '',
        index_mode: 'organization',
        projects: '',
        repositories: '',
        content_types: 'both',
        credentials: { azure_devops_pat: '' },
      },
    });
  });

  it('validates organization requires collection path when empty', () => {
    const fields = getDataSourceFieldsWithExtras(
      translate,
      DataSourceKey.AZURE_DEVOPS,
    ) as Array<{
      name: string;
      customValidate?: (val: string, formValues?: any) => boolean | string;
    }>;
    const orgField = fields.find((f) => f.name === 'config.organization');
    expect(orgField).toBeDefined();
    const validate = orgField!.customValidate!;

    // Valid when organization is explicitly provided
    expect(validate('contoso', { config: { base_url: '' } })).toBe(true);
    expect(
      validate('contoso', { config: { base_url: 'https://dev.azure.com' } }),
    ).toBe(true);

    // Valid when base_url includes collection path
    expect(
      validate('', {
        config: {
          base_url: 'http://tfs.corp.local:8080/tfs/DefaultCollection',
        },
      }),
    ).toBe(true);
    expect(
      validate('', {
        config: { base_url: 'https://dev.azure.com/myorg' },
      }),
    ).toBe(true);

    // Invalid when both are empty
    expect(validate('', { config: { base_url: '' } })).toBe(
      'setting.dataSourceValidationFieldRequired',
    );

    // Invalid when base_url is root-only
    expect(
      validate('', { config: { base_url: 'https://dev.azure.com' } }),
    ).toBe('setting.dataSourceValidationFieldRequired');
    expect(
      validate('', { config: { base_url: 'https://dev.azure.com/' } }),
    ).toBe('setting.dataSourceValidationFieldRequired');
    expect(
      validate('', { config: { base_url: 'http://tfs.corp.local:8080' } }),
    ).toBe('setting.dataSourceValidationFieldRequired');
  });

  it('validates base_url rejects malformed URLs and URLs with query or fragment', () => {
    const fields = getDataSourceFieldsWithExtras(
      translate,
      DataSourceKey.AZURE_DEVOPS,
    ) as Array<{
      name: string;
      customValidate?: (val: string, formValues?: any) => boolean | string;
    }>;
    const baseUrlField = fields.find((f) => f.name === 'config.base_url');
    expect(baseUrlField).toBeDefined();
    const validate = baseUrlField!.customValidate!;

    expect(validate('')).toBe(true);
    expect(validate('https://dev.azure.com')).toBe(true);
    expect(validate('http://tfs.corp.local:8080/tfs')).toBe(true);

    // Invalid scheme
    expect(validate('ftp://dev.azure.com')).toBe(
      'setting.azureDevOpsBaseUrlTip',
    );

    // Triple-slash input parses to host=DefaultCollection per WHATWG URL
    // semantics, so it is accepted as a (single-label) host.
    expect(validate('https:///DefaultCollection')).toBe(true);

    // Query or fragment
    expect(validate('https://dev.azure.com/myorg?test=1')).toBe(
      'setting.azureDevOpsBaseUrlTip',
    );
    expect(validate('https://dev.azure.com/myorg#frag')).toBe(
      'setting.azureDevOpsBaseUrlTip',
    );

    // Credentials
    expect(validate('https://user:pass@dev.azure.com')).toBe(
      'setting.azureDevOpsPatTip',
    );
  });

  it('validates organization rejects query and fragment URLs', () => {
    const fields = getDataSourceFieldsWithExtras(
      translate,
      DataSourceKey.AZURE_DEVOPS,
    ) as Array<{
      name: string;
      customValidate?: (val: string, formValues?: any) => boolean | string;
    }>;
    const orgField = fields.find((f) => f.name === 'config.organization');
    expect(orgField).toBeDefined();
    const validate = orgField!.customValidate!;

    expect(validate('myorg?test=1', { config: { base_url: '' } })).toBe(
      'setting.azureDevOpsOrganizationTip',
    );
    expect(validate('myorg#frag', { config: { base_url: '' } })).toBe(
      'setting.azureDevOpsOrganizationTip',
    );
    // Triple-slash input parses to host=DefaultCollection per WHATWG URL
    // semantics, so it is accepted as a (single-label) host.
    expect(
      validate('https:///DefaultCollection', { config: { base_url: '' } }),
    ).toBe(true);
    expect(
      validate('https://user:pass@dev.azure.com/myorg', {
        config: { base_url: '' },
      }),
    ).toBe('setting.azureDevOpsPatTip');
  });
});

describe.each([
  ['MySQL', DataSourceKey.MYSQL],
  ['PostgreSQL', DataSourceKey.POSTGRESQL],
])('%s data source', (_label, key) => {
  it('exposes a bounded batch size for large table syncs', () => {
    const fields = getDataSourceFieldsWithExtras(translate, key) as Array<{
      name: string;
      validation?: { min?: number };
    }>;
    const batchSize = fields.find((f) => f.name === 'config.batch_size');

    expect(batchSize?.validation?.min).toBe(1);
    expect(DataSourceFormDefaultValues[key].config).toMatchObject({
      batch_size: 32,
    });
  });
});

describe('GitHub data source folder sync & CI/CD workflow guide', () => {
  it('exposes unique fields for include_code_files, branch, sync_scope, and folder_path with conditional rendering', () => {
    const fields = getDataSourceFieldsWithExtras(
      translate,
      DataSourceKey.GITHUB,
    ) as Array<{
      name: string;
      shouldRender?: (values: any) => boolean;
      customValidate?: (val: any, values: any) => boolean | string;
    }>;

    const fieldNames = fields.map((f) => f.name);
    expect(new Set(fieldNames).size).toBe(fieldNames.length);

    const includeCodeFiles = fields.find(
      (f) => f.name === 'config.include_code_files',
    );
    const branch = fields.find((f) => f.name === 'config.branch');
    const syncScope = fields.find((f) => f.name === 'config.sync_scope');
    const folderPath = fields.find((f) => f.name === 'config.folder_path');

    expect(includeCodeFiles).toBeDefined();
    expect(
      branch?.shouldRender?.({ config: { include_code_files: false } }),
    ).toBe(false);
    expect(
      branch?.shouldRender?.({ config: { include_code_files: true } }),
    ).toBe(true);
    expect(
      syncScope?.shouldRender?.({ config: { include_code_files: true } }),
    ).toBe(true);
    expect(
      folderPath?.shouldRender?.({
        config: { include_code_files: true, sync_scope: 'repository' },
      }),
    ).toBe(false);
    expect(
      folderPath?.shouldRender?.({
        config: { include_code_files: true, sync_scope: 'directory' },
      }),
    ).toBe(true);

    // Folder path required when sync_scope === 'directory'
    expect(
      folderPath?.customValidate?.('', {
        config: { include_code_files: true, sync_scope: 'directory' },
      }),
    ).toBe('setting.dataSourceValidationFieldRequired');
    expect(
      folderPath?.customValidate?.('docs', {
        config: { include_code_files: true, sync_scope: 'directory' },
      }),
    ).toBe(true);
  });

  it('builds an idempotent GitHub Actions workflow YAML pre-filled with repo, branch, folder_path, and baseUrl', () => {
    const yaml = buildGithubActionsWorkflowYaml({
      repositoryOwner: 'hung2604',
      repositoryName: 'BA-Demego',
      branch: 'main',
      folderPath: '/docs/',
      baseUrl: 'https://faq.demego.vn/',
    });

    expect(yaml).toContain('Sync Docs to RAGFlow Dataset (hung2604/BA-Demego)');
    expect(yaml).toContain('- main');
    expect(yaml).toContain("- 'docs/**'");
    expect(yaml).toContain("RAGFLOW_BASE_URL: 'https://faq.demego.vn'");
    expect(yaml).toContain('/api/v1/datasets/${RAGFLOW_DATASET_ID}/documents');
    expect(yaml).toContain('/api/v1/datasets/${RAGFLOW_DATASET_ID}/chunks');
  });
});
