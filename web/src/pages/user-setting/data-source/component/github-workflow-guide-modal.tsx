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

import { Button } from '@/components/ui/button';
import { Modal } from '@/components/ui/modal/modal';
import { Check, Copy, FileCode2, Info } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { buildGithubActionsWorkflowYaml } from '../constant/github-workflow-template';

export { buildGithubActionsWorkflowYaml };

export interface GithubWorkflowGuideModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  repositoryOwner?: string;
  repositoryName?: string;
  branch?: string;
  folderPath?: string;
}

export const GithubWorkflowGuideModal = ({
  open,
  onOpenChange,
  repositoryOwner,
  repositoryName,
  branch,
  folderPath,
}: GithubWorkflowGuideModalProps) => {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);

  const baseUrl = useMemo(() => {
    if (typeof window !== 'undefined' && window.location?.origin) {
      return window.location.origin;
    }
    return 'https://your-ragflow-domain.com';
  }, []);

  const workflowYaml = useMemo(
    () =>
      buildGithubActionsWorkflowYaml({
        repositoryOwner,
        repositoryName,
        branch,
        folderPath,
        baseUrl,
      }),
    [repositoryOwner, repositoryName, branch, folderPath, baseUrl],
  );

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(workflowYaml);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Fallback copy if clipboard API is restricted
      const textarea = document.createElement('textarea');
      textarea.value = workflowYaml;
      document.body.appendChild(textarea);
      textarea.select();
      document.execCommand('copy');
      document.body.removeChild(textarea);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  }, [workflowYaml]);

  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      className="!max-w-[820px]"
      title={
        <div className="flex items-center gap-2 text-lg font-medium text-text-primary">
          <FileCode2 className="size-5 text-accent-primary" />
          <span>{t('setting.githubWorkflowGuideModalTitle')}</span>
        </div>
      }
      footer={
        <div className="flex items-center justify-end gap-2 px-6 py-4">
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
          >
            {t('common.close', 'Close')}
          </Button>
          <Button type="button" onClick={handleCopy} className="gap-1.5">
            {copied ? (
              <>
                <Check className="size-4" />
                {t('setting.githubWorkflowGuideCopied')}
              </>
            ) : (
              <>
                <Copy className="size-4" />
                {t('setting.githubWorkflowGuideCopyButton')}
              </>
            )}
          </Button>
        </div>
      }
    >
      <div className="flex flex-col gap-4 text-sm text-text-secondary max-h-[70vh] overflow-y-auto pr-1">
        <p className="text-text-secondary leading-relaxed">
          {t('setting.githubWorkflowGuideModalSubtitle')}
        </p>

        <div className="flex items-start gap-2.5 rounded-md border border-border-button bg-bg-card p-3 text-xs text-text-primary">
          <Info className="size-4 shrink-0 mt-0.5 text-accent-primary" />
          <span>{t('setting.githubWorkflowGuideNativeSyncNote')}</span>
        </div>

        <div className="flex flex-col gap-2 rounded-md border border-border-button p-3.5">
          <h4 className="font-medium text-text-primary">
            {t('setting.githubWorkflowGuideStep1Title')}
          </h4>
          <p className="text-xs text-text-secondary">
            {t('setting.githubWorkflowGuideStep1Desc')}
          </p>
          <ul className="list-disc pl-5 text-xs space-y-1 text-text-primary">
            <li>
              <code className="px-1.5 py-0.5 rounded bg-bg-card font-mono">
                RAGFLOW_API_KEY
              </code>{' '}
              — {t('setting.githubWorkflowGuideSecretApiKeyDesc')}
            </li>
            <li>
              <code className="px-1.5 py-0.5 rounded bg-bg-card font-mono">
                RAGFLOW_DATASET_ID
              </code>{' '}
              — {t('setting.githubWorkflowGuideSecretDatasetIdDesc')}
            </li>
          </ul>
        </div>

        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <h4 className="font-medium text-text-primary">
              {t('setting.githubWorkflowGuideStep2Title')}
            </h4>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={handleCopy}
              className="h-7 px-2.5 text-xs gap-1"
            >
              {copied ? (
                <>
                  <Check className="size-3.5" />
                  {t('setting.githubWorkflowGuideCopied')}
                </>
              ) : (
                <>
                  <Copy className="size-3.5" />
                  {t('setting.githubWorkflowGuideCopyButton')}
                </>
              )}
            </Button>
          </div>
          <p className="text-xs text-text-secondary">
            {t('setting.githubWorkflowGuideStep2Desc')}
          </p>
          <pre className="rounded-md border border-border-button bg-bg-card p-3.5 text-xs font-mono text-text-primary overflow-x-auto leading-relaxed select-all">
            <code>{workflowYaml}</code>
          </pre>
        </div>
      </div>
    </Modal>
  );
};
