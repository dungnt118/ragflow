import { Modal } from '@/components/ui/modal/modal';
import {
  confirmLargeDownloadIfNeeded,
  FolderDownloadWarningSizeBytes,
  normalizeArchiveFilename,
} from '../util';

jest.mock('@/components/ui/modal/modal', () => ({
  Modal: {
    confirm: jest.fn(),
  },
}));

describe('file manager download utils', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('normalizeArchiveFilename', () => {
    it('appends .zip to folder names and strips leading dots', () => {
      expect(normalizeArchiveFilename('CMS-QLTN', true)).toBe('CMS-QLTN.zip');
      expect(normalizeArchiveFilename('.knowledgebase', true)).toBe(
        'knowledgebase.zip',
      );
      expect(normalizeArchiveFilename('v1.2', true)).toBe('v1.2.zip');
    });

    it('replaces file extension with .zip when archiving a single file', () => {
      expect(normalizeArchiveFilename('report.md', false)).toBe('report.zip');
      expect(normalizeArchiveFilename('archive.zip', false)).toBe(
        'archive.zip',
      );
    });

    it('falls back to files.zip when name is empty', () => {
      expect(normalizeArchiveFilename('', true)).toBe('files.zip');
      expect(normalizeArchiveFilename(undefined, true)).toBe('files.zip');
    });
  });

  describe('confirmLargeDownloadIfNeeded', () => {
    const mockT = ((key: string, opts?: Record<string, unknown>) =>
      opts?.size ? `${key}:${opts.size}` : key) as any;

    it('executes immediately when size is within the 15MB threshold', () => {
      const onConfirm = jest.fn();
      confirmLargeDownloadIfNeeded(
        FolderDownloadWarningSizeBytes,
        mockT,
        onConfirm,
      );

      expect(onConfirm).toHaveBeenCalledTimes(1);
      expect(Modal.confirm).not.toHaveBeenCalled();
    });

    it('shows confirmation modal when size exceeds 15MB', () => {
      const onConfirm = jest.fn();
      confirmLargeDownloadIfNeeded(
        FolderDownloadWarningSizeBytes + 1024,
        mockT,
        onConfirm,
      );

      expect(onConfirm).not.toHaveBeenCalled();
      expect(Modal.confirm).toHaveBeenCalledTimes(1);

      const modalArgs = (Modal.confirm as jest.Mock).mock.calls[0][0];
      expect(modalArgs.title).toBe('fileManager.largeDownloadWarningTitle');
      modalArgs.onOk();
      expect(onConfirm).toHaveBeenCalledTimes(1);
    });
  });
});
