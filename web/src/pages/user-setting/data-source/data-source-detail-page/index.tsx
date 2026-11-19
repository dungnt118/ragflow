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

import {
  DynamicForm,
  DynamicFormRef,
  FormFieldConfig,
  FormFieldType,
} from '@/components/dynamic-form';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Separator } from '@/components/ui/separator';
import { RunningStatus, RunningStatusOld } from '@/constants/knowledge';
import { isEqual } from 'lodash';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { FieldValues } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router';
import {
  DataSourceFormDefaultValues,
  DataSourceKey,
  getCommonExtraDefaultValues,
  getDataSourceFormBaseFields,
  getDataSourceFieldsWithExtras,
  mergeDataSourceFormValues,
  useDataSourceInfo,
} from '../constant';
import { GithubWorkflowGuideModal } from '../component/github-workflow-guide-modal';
import {
  DataSourceKeys,
  useAddDataSource,
  useFetchDataSourceDetail,
  useTestDataSource,
  useUpdateDataSourceStatus,
} from '../hooks';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { DataSourceLogsTable } from './log-table';
import BackButton from '@/components/back-button';
import message from '@/components/ui/message';
import { dataSourceUpdate } from '@/services/data-source-service';
import { useQueryClient } from '@tanstack/react-query';
import { AlertCircle, FileCode2, RotateCw } from 'lucide-react';

const SourceDetailPage = () => {
  const { t } = useTranslation();
  const formRef = useRef<DynamicFormRef>(null);
  const [searchParams] = useSearchParams();
  const connectorId = searchParams.get('id')!;

  const { data: detail } = useFetchDataSourceDetail();
  const { updateStatus, loading: statusUpdateLoading } =
    useUpdateDataSourceStatus();
  const { dataSourceInfo } = useDataSourceInfo();
  const detailInfo = useMemo(() => {
    if (detail) {
      return dataSourceInfo[detail.source];
    }
  }, [detail, dataSourceInfo]);

  const [isDirty, setIsDirty] = useState(false);
  const [guideOpen, setGuideOpen] = useState(false);
  const [guideFormSnapshot, setGuideFormSnapshot] = useState<FieldValues>({});

  const isGithubSource = detail?.source === DataSourceKey.GITHUB;

  const handleOpenGithubGuide = useCallback(() => {
    const liveValues = formRef.current?.getValues?.() || {};
    setGuideFormSnapshot(liveValues);
    setGuideOpen(true);
  }, []);

  const customFields = useMemo(() => {
    return [
      {
        label: t('setting.dataSourcePruneFreq'),
        name: 'prune_freq',
        type: FormFieldType.Number,
        required: false,
        tooltip: t('setting.dataSourcePruneFreqTip'),
        shouldRender: (values: any) => !!values?.config?.sync_deleted_files,
        render: (fieldProps: FormFieldConfig) => {
          return (
            <Input
              {...fieldProps}
              type={FormFieldType.Number}
              suffix={
                <span className="px-2 text-text-secondary italic">
                  {t('setting.minutes')}
                </span>
              }
            />
          );
        },
      },
      {
        label: t('setting.dataSourceRefreshFreq'),
        name: 'refresh_freq',
        type: FormFieldType.Number,
        required: false,
        tooltip: t('setting.dataSourceRefreshFreqTip'),
        render: (fieldProps: FormFieldConfig) => (
          <Input
            {...fieldProps}
            type={FormFieldType.Number}
            suffix={
              <span className="px-2 text-text-secondary italic">
                {t('setting.minutes')}
              </span>
            }
          />
        ),
      },
      {
        label: t('setting.dataSourceTimeoutSecs'),
        name: 'timeout_secs',
        type: FormFieldType.Number,
        required: false,
        tooltip: t('setting.dataSourceTimeoutSecsTip'),
        render: (fieldProps: FormFieldConfig) => (
          <div className="flex items-center gap-1 w-full relative">
            <div className="flex-1">
              <Input
                {...fieldProps}
                type={FormFieldType.Number}
                suffix={
                  <span className="px-2 text-text-secondary italic">
                    {t('setting.seconds')}
                  </span>
                }
              />
            </div>
          </div>
        ),
      },
    ];
  }, [t]);

  const fields = useMemo<FormFieldConfig[]>(() => {
    if (!detail) {
      return [];
    }
    const baseFields = getDataSourceFormBaseFields(t).map((field) =>
      field.name === 'name' ? { ...field, disabled: true } : { ...field },
    );
    const allFields = [
      ...baseFields,
      ...getDataSourceFieldsWithExtras(t, detail.source as any),
      ...customFields,
    ] as FormFieldConfig[];
    return allFields.map((field) => ({
      ...field,
      horizontal: true,
      onChange: undefined,
    }));
  }, [detail, customFields, t]);

  const defaultValues = useMemo<FieldValues>(() => {
    if (!detail) {
      return {};
    }
    const isCrawler =
      detail.source === DataSourceKey.WEB_CRAWLER ||
      detail.source === DataSourceKey.SITEMAP;

    const merged = mergeDataSourceFormValues(
      DataSourceFormDefaultValues[
        detail.source as keyof typeof DataSourceFormDefaultValues
      ] as FieldValues,
      getCommonExtraDefaultValues(),
      detail as FieldValues,
    );

    if (isCrawler && (!merged.refresh_freq || merged.refresh_freq === 5)) {
      merged.refresh_freq = 360;
    }

    return merged;
  }, [detail]);

  const queryClient = useQueryClient();
  const [fetchLoading, setFetchLoading] = useState(false);

  const handleManualFetch = useCallback(async () => {
    if (!connectorId) return;
    if (isDirty) {
      message.warning(t('setting.saveBeforeFetch'));
      return;
    }
    setFetchLoading(true);
    try {
      const { data } = await dataSourceUpdate(connectorId, {
        reschedule: true,
        status: RunningStatus.SCHEDULE,
      });
      if (data.code === 0) {
        message.success(t('setting.fetchNowSuccess'));
        await Promise.all([
          queryClient.invalidateQueries({
            queryKey: DataSourceKeys.detail(connectorId),
          }),
          queryClient.invalidateQueries({
            queryKey: DataSourceKeys.logsPrefix(connectorId),
          }),
          queryClient.invalidateQueries({
            queryKey: DataSourceKeys.list(),
          }),
        ]);
      }
    } finally {
      setFetchLoading(false);
    }
  }, [connectorId, isDirty, queryClient, t]);

  const { addLoading, handleAddOk } = useAddDataSource({ isEdit: true });
  const { loading: testLoading, handleTest } = useTestDataSource(
    formRef,
    connectorId,
    fields,
  );

  const [conflictModalOpen, setConflictModalOpen] = useState(false);
  const [conflictApplyMode, setConflictApplyMode] = useState<
    'next_cycle' | 'restart_now'
  >('next_cycle');
  const pendingRescheduleRef = useRef<boolean>(true);

  const isConnectorRunning = useMemo(
    () =>
      detail?.status === RunningStatus.RUNNING ||
      detail?.status === RunningStatusOld.RUNNING,
    [detail?.status],
  );

  const isConnectorActive = useMemo(
    () =>
      detail?.status === RunningStatus.RUNNING ||
      detail?.status === RunningStatus.SCHEDULE ||
      detail?.status === RunningStatusOld.RUNNING ||
      detail?.status === RunningStatusOld.SCHEDULE,
    [detail?.status],
  );

  const handleSaveClick = useCallback(async () => {
    if (!formRef.current) return;
    const isValid = await formRef.current.trigger?.();
    if (isValid === false) {
      formRef.current.submit();
      return;
    }
    if (isConnectorRunning) {
      setConflictApplyMode('next_cycle');
      setConflictModalOpen(true);
      return;
    }
    pendingRescheduleRef.current = true;
    formRef.current.submit();
  }, [isConnectorRunning]);

  const handleConfirmConflictSave = useCallback(() => {
    setConflictModalOpen(false);
    pendingRescheduleRef.current = conflictApplyMode === 'restart_now';
    formRef.current?.submit();
  }, [conflictApplyMode]);

  const handleToggleProcess = useCallback(() => {
    updateStatus(
      isConnectorActive ? RunningStatus.CANCEL : RunningStatus.SCHEDULE,
    );
  }, [isConnectorActive, updateStatus]);

  useEffect(() => {
    setIsDirty(false);
  }, [detail]);

  useEffect(() => {
    const instance = formRef.current;
    if (!instance) return;

    setIsDirty(!isEqual(instance.getValues(), defaultValues));
    return instance.watchDirty((_nextIsDirty, values) => {
      setIsDirty(!isEqual(values, defaultValues));
    });
  }, [defaultValues, fields]);

  return (
    <div className="px-10 py-5">
      {/* <BackButton /> */}
      <Card className="bg-transparent border border-border-button px-5 pt-[10px] pb-5 rounded-md mt-5">
        <CardHeader className="flex flex-row items-center justify-between space-y-0 p-0 pb-3">
          <CardTitle className="text-2xl text-text-primary flex gap-1 items-center font-normal pb-3">
            <BackButton className="border-none">
              <></>
            </BackButton>
            {detailInfo?.icon}
            {detail?.name}
          </CardTitle>
          {isGithubSource && (
            <div className="pb-3">
              <Button
                type="button"
                variant="outline"
                onClick={handleOpenGithubGuide}
                className="gap-1.5"
              >
                <FileCode2 className="size-4" />
                {t('setting.githubWorkflowGuideButton')}
              </Button>
            </div>
          )}
        </CardHeader>
        <Separator className="border-border-button bg-border-button w-[calc(100%+2rem)] -translate-x-4 -translate-y-4" />
        <CardContent className="p-2 flex flex-col gap-10 max-h-[calc(100vh-230px)] overflow-y-auto scrollbar-auto">
          <div className="max-w-[1200px]">
            <DynamicForm.Root
              ref={formRef}
              fields={fields}
              onSubmit={(data) =>
                handleAddOk(data, { reschedule: pendingRescheduleRef.current })
              }
              defaultValues={defaultValues}
            />
          </div>
          <div className="max-w-[1200px] flex justify-end gap-2">
            {isGithubSource && (
              <Button
                type="button"
                variant="outline"
                onClick={handleOpenGithubGuide}
                className="gap-1.5"
              >
                <FileCode2 className="size-4" />
                {t('setting.githubWorkflowGuideButton')}
              </Button>
            )}
            <Button
              type="button"
              variant="outline"
              onClick={handleTest}
              disabled={testLoading}
              loading={testLoading}
            >
              {t('setting.dataSourceTestConnection')}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={handleManualFetch}
              disabled={fetchLoading || addLoading || statusUpdateLoading}
              loading={fetchLoading}
              className="gap-1.5"
            >
              <RotateCw className="size-4" />
              {t('setting.fetchNow')}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={handleToggleProcess}
              disabled={statusUpdateLoading || addLoading || fetchLoading}
              loading={statusUpdateLoading}
            >
              {isConnectorActive ? t('common.stop') : t('common.resume')}
            </Button>
            <Button
              type="button"
              onClick={handleSaveClick}
              disabled={
                !isDirty || addLoading || statusUpdateLoading || fetchLoading
              }
              loading={addLoading}
            >
              {t('setting.saveChanges')}
            </Button>
          </div>
          <section className="flex flex-col gap-2">
            <div className="text-2xl text-text-primary mb-2">
              {t('setting.log')}
            </div>
            <DataSourceLogsTable autoRefresh={isConnectorActive} />
          </section>
        </CardContent>
      </Card>
      {isGithubSource && (
        <GithubWorkflowGuideModal
          open={guideOpen}
          onOpenChange={setGuideOpen}
          repositoryOwner={
            guideFormSnapshot?.config?.repository_owner ||
            (detail?.config as any)?.repository_owner
          }
          repositoryName={
            guideFormSnapshot?.config?.repository_name ||
            (detail?.config as any)?.repository_name
          }
          branch={
            guideFormSnapshot?.config?.branch || (detail?.config as any)?.branch
          }
          folderPath={
            guideFormSnapshot?.config?.folder_path ||
            (detail?.config as any)?.folder_path
          }
        />
      )}
      <Dialog open={conflictModalOpen} onOpenChange={setConflictModalOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-lg">
              <AlertCircle className="size-5 text-state-warning" />
              {t('setting.runningTaskConflictTitle')}
            </DialogTitle>
            <DialogDescription className="pt-2 text-text-secondary">
              {t('setting.runningTaskConflictDesc')}
            </DialogDescription>
          </DialogHeader>
          <div className="py-3">
            <RadioGroup
              value={conflictApplyMode}
              onValueChange={(val) =>
                setConflictApplyMode(val as 'next_cycle' | 'restart_now')
              }
              className="gap-4"
            >
              <label
                htmlFor="apply-next-cycle"
                className="flex items-start gap-3 p-3 rounded-lg border border-border-button hover:border-border-default cursor-pointer transition-colors bg-bg-component/50"
              >
                <RadioGroupItem
                  value="next_cycle"
                  id="apply-next-cycle"
                  className="mt-1"
                />
                <div className="flex flex-col gap-1">
                  <span className="font-medium text-text-primary text-sm">
                    {t('setting.applyNextCycleOption')}
                  </span>
                  <span className="text-xs text-text-secondary leading-relaxed">
                    {t('setting.applyNextCycleDesc')}
                  </span>
                </div>
              </label>

              <label
                htmlFor="restart-now"
                className="flex items-start gap-3 p-3 rounded-lg border border-border-button hover:border-border-default cursor-pointer transition-colors bg-bg-component/50"
              >
                <RadioGroupItem
                  value="restart_now"
                  id="restart-now"
                  className="mt-1"
                />
                <div className="flex flex-col gap-1">
                  <span className="font-medium text-text-primary text-sm">
                    {t('setting.restartNowOption')}
                  </span>
                  <span className="text-xs text-text-secondary leading-relaxed">
                    {t('setting.restartNowDesc')}
                  </span>
                </div>
              </label>
            </RadioGroup>
          </div>
          <DialogFooter className="gap-2 sm:gap-0">
            <Button
              type="button"
              variant="outline"
              onClick={() => setConflictModalOpen(false)}
            >
              {t('setting.conflictModalCancel')}
            </Button>
            <Button
              type="button"
              onClick={handleConfirmConflictSave}
              loading={addLoading}
            >
              {t('setting.conflictModalConfirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
};
export default SourceDetailPage;
