import { DataSourceKey } from './constant';

describe('Available data sources search filter logic', () => {
  const sampleTemplates = [
    {
      id: DataSourceKey.CONFLUENCE,
      name: 'Confluence',
      description:
        'Integrate your Confluence workspace to search documentation.',
    },
    {
      id: DataSourceKey.WEB_CRAWLER,
      name: 'Web Crawler (Spider)',
      description:
        'Intelligent multi-page web crawler and CMS extractor for HTML & SSR/SPA sites.',
    },
    {
      id: DataSourceKey.SITEMAP,
      name: 'Sitemap',
      description: 'Import pages and documents from XML sitemaps.',
    },
    {
      id: DataSourceKey.GITHUB,
      name: 'GitHub',
      description: 'Sync files and folders from GitHub repositories.',
    },
  ];

  const filterTemplates = (
    templates: typeof sampleTemplates,
    searchString: string,
  ) => {
    const query = searchString.trim().toLowerCase();
    if (!query) {
      return templates;
    }
    return templates.filter((item) => {
      const name = (item.name || '').toLowerCase();
      const desc = (item.description || '').toLowerCase();
      const id = (item.id || '').toLowerCase();
      const idWithoutUnderscore = id.replace(/_/g, ' ');
      return (
        name.includes(query) ||
        desc.includes(query) ||
        id.includes(query) ||
        idWithoutUnderscore.includes(query)
      );
    });
  };

  it('returns all templates when search query is empty', () => {
    expect(filterTemplates(sampleTemplates, '')).toHaveLength(4);
    expect(filterTemplates(sampleTemplates, '   ')).toHaveLength(4);
  });

  it('filters templates by name (case-insensitive)', () => {
    const result = filterTemplates(sampleTemplates, 'crawler');
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe(DataSourceKey.WEB_CRAWLER);
  });

  it('filters templates by description keyword (e.g. spider)', () => {
    const result = filterTemplates(sampleTemplates, 'spider');
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe(DataSourceKey.WEB_CRAWLER);
  });

  it('filters templates by id with underscore or spaces', () => {
    const result1 = filterTemplates(sampleTemplates, 'web_crawler');
    expect(result1).toHaveLength(1);
    expect(result1[0].id).toBe(DataSourceKey.WEB_CRAWLER);

    const result2 = filterTemplates(sampleTemplates, 'web crawler');
    expect(result2).toHaveLength(1);
    expect(result2[0].id).toBe(DataSourceKey.WEB_CRAWLER);
  });

  it('returns empty array when query matches nothing', () => {
    const result = filterTemplates(sampleTemplates, 'nonexistent_source_xyz');
    expect(result).toHaveLength(0);
  });
});
