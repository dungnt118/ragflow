import { useSelectedIds } from '@/hooks/logic-hooks/use-row-selection';
import { useBatchDownloadFile } from '@/hooks/use-file-request';
import { IFile } from '@/interfaces/database/file-manager';
import { OnChangeFn, RowSelectionState } from '@tanstack/react-table';
import { ArrowDownToLine, FolderInput, Link2, Trash2 } from 'lucide-react';
import { useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { UseHandleConnectToKnowledgeReturnType } from './hooks';
import { useHandleDeleteFile } from './use-delete-file';
import { UseMoveDocumentShowType } from './use-move-file';
import {
  confirmLargeDownloadIfNeeded,
  isFolderType,
  isKnowledgeBaseType,
  normalizeArchiveFilename,
} from './util';

export function useBulkOperateFile({
  files,
  rowSelection,
  showMoveFileModal,
  setRowSelection,
  showConnectToKnowledgeModal,
}: {
  files: IFile[];
  rowSelection: RowSelectionState;
  setRowSelection: OnChangeFn<RowSelectionState>;
} & UseMoveDocumentShowType & {
    showConnectToKnowledgeModal: UseHandleConnectToKnowledgeReturnType['showConnectToKnowledgeModal'];
  }) {
  const { t } = useTranslation();

  const { selectedIds } = useSelectedIds(rowSelection, files);

  const { handleRemoveFile } = useHandleDeleteFile();
  const { batchDownloadFile } = useBatchDownloadFile();

  const selectedFiles = useMemo(
    () => (files ?? []).filter((x) => selectedIds.includes(x.id)),
    [files, selectedIds],
  );

  const hasKnowledgeBaseItem = useMemo(
    () => selectedFiles.some((x) => isKnowledgeBaseType(x.source_type)),
    [selectedFiles],
  );

  const handleBatchDownload = useCallback(() => {
    if (selectedIds.length === 0) return;

    const totalSize = selectedFiles.reduce(
      (acc, item) => acc + (item.size || 0),
      0,
    );
    const filename =
      selectedFiles.length === 1
        ? normalizeArchiveFilename(
            selectedFiles[0].name,
            isFolderType(selectedFiles[0].type),
          )
        : 'files.zip';

    confirmLargeDownloadIfNeeded(totalSize, t, () => {
      batchDownloadFile({
        ids: selectedIds,
        filename,
      });
    });
  }, [batchDownloadFile, selectedFiles, selectedIds, t]);

  const list = [
    {
      id: 'download',
      label: t('common.download'),
      icon: <ArrowDownToLine />,
      onClick: handleBatchDownload,
    },
    ...(hasKnowledgeBaseItem
      ? []
      : [
          {
            id: 'move',
            label: t('common.move'),
            icon: <FolderInput />,
            onClick: () => {
              showMoveFileModal(selectedIds, true);
            },
          },
          {
            id: 'link',
            label: t('fileManager.addToKnowledge'),
            icon: <Link2 />,
            onClick: async () => {
              showConnectToKnowledgeModal(selectedIds);
            },
          },
          {
            id: 'delete',
            label: t('common.delete'),
            icon: <Trash2 />,
            onClick: async () => {
              const code = await handleRemoveFile(selectedIds);
              if (code === 0) {
                setRowSelection({});
              }
            },
          },
        ]),
  ];

  return { list };
}
