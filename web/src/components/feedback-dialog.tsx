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
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Textarea } from '@/components/ui/textarea';
import { IModalProps } from '@/interfaces/common';
import {
  IFeedbackConfig,
  IFeedbackCriterionConfig,
  IFeedbackDetail,
  IFeedbackRequestBody,
} from '@/interfaces/request/chat';
import { cn } from '@/lib/utils';
import { zodResolver } from '@hookform/resolvers/zod';
import { Check, Star, ThumbsDown, ThumbsUp, X } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

const CSAT_LABELS: Record<number, string> = {
  1: 'Rất không hài lòng',
  2: 'Không hài lòng',
  3: 'Bình thường',
  4: 'Hài lòng',
  5: 'Rất hài lòng',
};

const FormSchema = z.object({
  feedback: z.string().optional(),
});

export type FeedbackFormType = z.infer<typeof FormSchema>;

interface IFeedbackDialogProps extends IModalProps<IFeedbackRequestBody> {
  feedbackConfig?: IFeedbackConfig;
  initialFeedback?: string;
  initialThumbup?: boolean;
  initialDetail?: IFeedbackDetail;
}

const FeedbackDialog = ({
  visible,
  hideModal,
  onOk,
  loading,
  feedbackConfig,
  initialFeedback,
  initialThumbup,
  initialDetail,
}: IFeedbackDialogProps) => {
  const { t } = useTranslation();
  const isMultiCriteria =
    feedbackConfig?.mode === 'multi_criteria' &&
    Array.isArray(feedbackConfig?.criteria) &&
    feedbackConfig.criteria.length > 0;

  const criteriaList = useMemo<IFeedbackCriterionConfig[]>(() => {
    if (!isMultiCriteria) return [];
    return feedbackConfig?.criteria || [];
  }, [feedbackConfig?.criteria, isMultiCriteria]);

  const [criteriaValues, setCriteriaValues] = useState<
    Record<string, number | boolean>
  >({});
  const [validationError, setValidationError] = useState<string | null>(null);

  const form = useForm<FeedbackFormType>({
    resolver: zodResolver(FormSchema),
    defaultValues: {
      feedback: initialFeedback || initialDetail?.comment || '',
    },
  });

  useEffect(() => {
    if (!visible) return;
    form.reset({
      feedback: initialFeedback || initialDetail?.comment || '',
    });
    setValidationError(null);

    if (isMultiCriteria) {
      const seeded: Record<string, number | boolean> = {};
      if (initialDetail?.criteria) {
        Object.entries(initialDetail.criteria).forEach(([k, v]) => {
          seeded[k] = v;
        });
      } else if (initialThumbup !== undefined) {
        criteriaList.forEach((c) => {
          if (c.type === 'csat_5') {
            seeded[c.key] = initialThumbup ? 5 : 2;
          } else {
            seeded[c.key] = initialThumbup;
          }
        });
      }
      setCriteriaValues(seeded);
    }
  }, [
    visible,
    initialFeedback,
    initialDetail,
    initialThumbup,
    isMultiCriteria,
    criteriaList,
    form,
  ]);

  const handleCriterionChange = (key: string, val: number | boolean) => {
    setValidationError(null);
    setCriteriaValues((prev) => ({
      ...prev,
      [key]: val,
    }));
  };

  async function onSubmit(data: FeedbackFormType) {
    if (isMultiCriteria) {
      for (const crit of criteriaList) {
        if (crit.required && criteriaValues[crit.key] === undefined) {
          setValidationError(`Vui lòng đánh giá tiêu chí: "${crit.label}"`);
          return;
        }
      }

      const csatScores: number[] = [];
      let hasBinaryFalse = false;
      criteriaList.forEach((crit) => {
        const val = criteriaValues[crit.key];
        if (crit.type === 'csat_5' && typeof val === 'number') {
          csatScores.push(val);
        } else if (crit.type === 'binary' && val === false) {
          hasBinaryFalse = true;
        }
      });

      const avgCsat =
        csatScores.length > 0
          ? Math.round(
              csatScores.reduce((acc, s) => acc + s, 0) / csatScores.length,
            )
          : undefined;
      const avgFloat =
        csatScores.length > 0
          ? csatScores.reduce((acc, s) => acc + s, 0) / csatScores.length
          : 5;
      const derivedThumbup = !hasBinaryFalse && avgFloat >= 4.0;

      await onOk?.({
        thumbup: derivedThumbup,
        feedback: data.feedback?.trim() || '',
        csat_score: avgCsat,
        criteria: criteriaValues,
      });
      return;
    }

    await onOk?.({
      thumbup: false,
      feedback: data.feedback?.trim() || '',
    });
  }

  return (
    <Dialog open={visible} onOpenChange={hideModal}>
      <DialogContent className="sm:max-w-[500px]">
        <DialogHeader>
          <DialogTitle>
            {isMultiCriteria
              ? t('chat.multiCriteriaFeedbackTitle', {
                  defaultValue: 'Đánh giá chất lượng phản hồi',
                })
              : t('common.name', { defaultValue: 'Phản hồi chi tiết' })}
          </DialogTitle>
        </DialogHeader>
        <Form {...form}>
          <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
            {isMultiCriteria && (
              <div className="space-y-3">
                {criteriaList.map((crit) => {
                  const currentVal = criteriaValues[crit.key];
                  return (
                    <div
                      key={crit.key}
                      className="rounded-lg border border-border-button bg-bg-card/60 p-3 space-y-2"
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-sm font-medium text-text-primary">
                          {crit.label}
                          {crit.required && (
                            <span className="text-state-error ml-1">*</span>
                          )}
                        </span>
                        {crit.type === 'csat_5' &&
                          typeof currentVal === 'number' && (
                            <span className="text-xs font-medium text-accent-primary">
                              {CSAT_LABELS[currentVal] || `${currentVal}/5`}
                            </span>
                          )}
                      </div>

                      {crit.type === 'csat_5' ? (
                        <div className="grid grid-cols-5 gap-1.5">
                          {[1, 2, 3, 4, 5].map((star) => {
                            const active =
                              typeof currentVal === 'number' &&
                              currentVal >= star;
                            const selected = currentVal === star;
                            return (
                              <button
                                key={star}
                                type="button"
                                onClick={() =>
                                  handleCriterionChange(crit.key, star)
                                }
                                title={CSAT_LABELS[star]}
                                className={cn(
                                  'flex flex-col items-center justify-center rounded-md border py-2 px-1 text-xs transition-colors',
                                  selected
                                    ? 'border-accent-primary bg-accent-primary/15 text-accent-primary font-semibold'
                                    : active
                                      ? 'border-amber-400/50 bg-amber-500/10 text-amber-500'
                                      : 'border-border-button bg-bg-input text-text-secondary hover:border-accent-primary/50',
                                )}
                              >
                                <div className="flex items-center gap-0.5">
                                  <Star
                                    className={cn(
                                      'size-3.5',
                                      active
                                        ? 'fill-amber-400 text-amber-400'
                                        : 'text-text-secondary',
                                    )}
                                  />
                                  <span>{star}</span>
                                </div>
                                <span className="mt-0.5 text-[10px] leading-tight line-clamp-1">
                                  {star === 1
                                    ? 'Rất tệ'
                                    : star === 2
                                      ? 'Tệ'
                                      : star === 3
                                        ? 'Ổn'
                                        : star === 4
                                          ? 'Tốt'
                                          : 'Rất tốt'}
                                </span>
                              </button>
                            );
                          })}
                        </div>
                      ) : (
                        <div className="grid grid-cols-2 gap-2">
                          <button
                            type="button"
                            onClick={() =>
                              handleCriterionChange(crit.key, true)
                            }
                            className={cn(
                              'flex items-center justify-center gap-1.5 rounded-md border py-2 px-3 text-xs font-medium transition-colors',
                              currentVal === true
                                ? 'border-emerald-500 bg-emerald-500/15 text-emerald-500'
                                : 'border-border-button bg-bg-input text-text-secondary hover:border-emerald-500/50',
                            )}
                          >
                            <ThumbsUp className="size-3.5" />
                            <span>Chính xác / Hài lòng</span>
                            {currentVal === true && (
                              <Check className="size-3.5 ml-auto" />
                            )}
                          </button>
                          <button
                            type="button"
                            onClick={() =>
                              handleCriterionChange(crit.key, false)
                            }
                            className={cn(
                              'flex items-center justify-center gap-1.5 rounded-md border py-2 px-3 text-xs font-medium transition-colors',
                              currentVal === false
                                ? 'border-rose-500 bg-rose-500/15 text-rose-500'
                                : 'border-border-button bg-bg-input text-text-secondary hover:border-rose-500/50',
                            )}
                          >
                            <ThumbsDown className="size-3.5" />
                            <span>Chưa chính xác / Thiếu</span>
                            {currentVal === false && (
                              <X className="size-3.5 ml-auto" />
                            )}
                          </button>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            )}

            {validationError && (
              <div className="rounded-md bg-rose-500/10 border border-rose-500/30 px-3 py-2 text-xs text-rose-500">
                {validationError}
              </div>
            )}

            <FormField
              control={form.control}
              name="feedback"
              render={({ field }) => (
                <FormItem>
                  <FormLabel className="text-xs text-text-secondary">
                    {t('chat.feedbackCommentLabel', {
                      defaultValue:
                        'Nhận xét bổ sung (giúp Admin kiểm tra & cải thiện tài liệu)',
                    })}
                  </FormLabel>
                  <FormControl>
                    <Textarea
                      placeholder={t('feedbackPlaceholder', {
                        keyPrefix: 'chat',
                        defaultValue:
                          'Nhập góp ý hoặc thông tin còn thiếu/chưa chính xác...',
                      })}
                      {...field}
                      rows={4}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <DialogFooter>
              <Button type="button" variant="outline" onClick={hideModal}>
                {t('common.cancel')}
              </Button>
              <Button type="submit" disabled={loading}>
                {t('common.ok')}
              </Button>
            </DialogFooter>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  );
};

export default FeedbackDialog;
