import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';

import {
  LucideBookText,
  LucideFolderOpen,
  LucideLogs,
  LucideSettings,
  LucideTextSearch,
} from 'lucide-react';

import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import { Button } from '@/components/ui/button';
import { useSecondPathName } from '@/hooks/route-hook';
import { cn, formatBytes } from '@/lib/utils';
import { Routes } from '@/routes';
import { formatPureDate } from '@/utils/date';

import { RunningStatus, RunningStatusOld } from '@/constants/knowledge';
import { IDataset } from '@/interfaces/database/dataset';
import { useParams } from 'react-router';

type PropType = {
  refreshCount?: number;
  dataset: IDataset;
};

export function SideBar({ dataset: data }: PropType) {
  const pathName = useSecondPathName();
  const { id } = useParams();
  const { t } = useTranslation();

  const connectors = data?.connectors ?? [];
  const hasFailedConnector = connectors.some(
    (c) =>
      c.status === RunningStatus.FAIL ||
      (c.status as string) === RunningStatusOld.FAIL,
  );
  const hasStoppedConnector = connectors.some(
    (c) =>
      c.status === RunningStatus.CANCEL ||
      (c.status as string) === RunningStatusOld.CANCEL,
  );

  const items = useMemo(() => {
    const list = [
      {
        icon: <LucideFolderOpen className="size-[1em]" />,
        label: t(`knowledgeDetails.subbarFiles`),
        key: Routes.Files,
      },
      {
        icon: <LucideTextSearch className="size-[1em]" />,
        label: t(`knowledgeDetails.testing`),
        key: Routes.DatasetTesting,
      },
      {
        icon: <LucideLogs className="size-[1em]" />,
        label: t(`knowledgeDetails.overview`),
        key: Routes.DataSetOverview,
      },
      {
        icon: <LucideSettings className="size-[1em]" />,
        label: t(`knowledgeDetails.configuration`),
        key: Routes.DataSetSetting,
      },
      {
        icon: <LucideBookText className="size-[1em]" />,
        label: 'Artifacts',
        key: Routes.Compilation,
      },
    ];

    return list;
  }, [t]);

  return (
    <aside className="w-full md:w-64 shrink-0 flex flex-col relative border-b md:border-b-0 border-border-button">
      <header
        className="px-3 md:px-5 pb-2 md:pb-4 flex items-center md:grid md:grid-cols-[auto_1fr] md:grid-rows-[auto_auto] gap-x-3"
        style={{
          gridTemplateAreas: '"avatar title" "avatar stats"',
        }}
      >
        <RAGFlowAvatar
          avatar={data.avatar}
          name={data.name}
          className="size-10 md:size-16 shrink-0"
          style={{ gridArea: 'avatar' }}
        />

        <div className="flex-1 min-w-0 md:contents">
          <h3
            className="text-base md:text-lg font-semibold truncate text-text-primary overflow-hidden"
            style={{ gridArea: 'title' }}
          >
            {data.name}
          </h3>

          <div
            className="self-end text-text-secondary text-xs overflow-hidden flex gap-2 md:block"
            style={{ gridArea: 'stats' }}
          >
            <div className="flex justify-between gap-2">
              <span>
                {data.document_count} {t('knowledgeDetails.files')}
              </span>
              <span>{data.size ? formatBytes(data.size) : ''}</span>
            </div>

            <div className="hidden md:block mt-0.5">
              {t('knowledgeDetails.created')} {formatPureDate(data.create_time)}
            </div>
          </div>
        </div>
      </header>

      <nav className="px-3 md:px-5 pt-1 pb-2 md:pb-5 overflow-x-auto md:overflow-y-auto scrollbar-none">
        <ul className="flex flex-row md:flex-col gap-2 md:space-y-5">
          {items.map((item) => {
            const active = '/' + pathName === item.key;

            return (
              <li key={item.key} className="shrink-0">
                <Button
                  asLink
                  block
                  variant="ghost"
                  className={cn(
                    'justify-start gap-2 px-3 relative h-8 md:h-10 text-xs md:text-base whitespace-nowrap rounded-full md:rounded-md',
                    active
                      ? 'bg-bg-card text-text-primary font-medium'
                      : 'text-text-secondary hover:text-text-primary',
                  )}
                  to={`${Routes.DatasetBase}${item.key}/${id}`}
                >
                  {item.icon}
                  <span>{item.label}</span>
                  {(item.key === Routes.DataSetOverview ||
                    item.key === Routes.DataSetSetting) &&
                    (hasFailedConnector ? (
                      <span
                        className="ml-auto size-2 rounded-full bg-state-error shrink-0"
                        aria-label="Sync failed"
                      />
                    ) : hasStoppedConnector ? (
                      <span
                        className="ml-auto size-2 rounded-full bg-state-warning shrink-0"
                        aria-label="Sync stopped"
                      />
                    ) : null)}
                </Button>
              </li>
            );
          })}
        </ul>
      </nav>
    </aside>
  );
}
