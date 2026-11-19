import { Modal } from '@/components/ui/modal/modal';
import { formatFileSize } from '@/utils/common-util';
import { TFunction } from 'i18next';

export const FolderDownloadWarningSizeBytes = 15 * 1024 * 1024;

export function isFolderType(type: string) {
  return type === 'folder';
}

export function isKnowledgeBaseType(sourceType: string) {
  return sourceType === 'knowledgebase';
}

export function normalizeArchiveFilename(name?: string, isFolder = true) {
  let cleaned = (name ?? '').trim().replace(/^\.+/, '');
  if (!isFolder && cleaned.includes('.')) {
    cleaned = cleaned.replace(/\.[^/.]+$/, '');
  }
  const baseName = cleaned || 'files';
  return baseName.toLowerCase().endsWith('.zip') ? baseName : `${baseName}.zip`;
}

export function confirmLargeDownloadIfNeeded(
  sizeBytes: number,
  t: TFunction,
  onConfirm: () => void,
) {
  if (sizeBytes > FolderDownloadWarningSizeBytes) {
    Modal.confirm({
      title: t('fileManager.largeDownloadWarningTitle'),
      content: t('fileManager.largeDownloadWarningContent', {
        size: formatFileSize(sizeBytes),
      }),
      okText: t('common.ok'),
      cancelText: t('common.cancel'),
      onOk: onConfirm,
    });
    return;
  }
  onConfirm();
}
