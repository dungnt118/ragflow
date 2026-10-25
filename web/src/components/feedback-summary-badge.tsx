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
  IFeedbackConfig,
  IFeedbackDetail,
} from '@/interfaces/request/chat';
import { cn } from '@/lib/utils';
import { MessageSquareText, Star, ThumbsDown, ThumbsUp } from 'lucide-react';
import { memo, useMemo } from 'react';

interface IFeedbackSummaryBadgeProps {
  thumbup?: boolean;
  feedback?: string;
  feedbackDetail?: IFeedbackDetail;
  feedbackConfig?: IFeedbackConfig;
  onClick?: () => void;
  className?: string;
}

export const FeedbackSummaryBadge = memo(
  ({
    thumbup,
    feedback,
    feedbackDetail,
    feedbackConfig,
    onClick,
    className,
  }: IFeedbackSummaryBadgeProps) => {
    const hasRating =
      thumbup !== undefined ||
      Boolean(feedback) ||
      Boolean(feedbackDetail && (feedbackDetail.csat_score || feedbackDetail.criteria));

    const criteriaLabelsMap = useMemo(() => {
      const map: Record<string, string> = {};
      feedbackConfig?.criteria?.forEach((c) => {
        map[c.key] = c.label;
      });
      return map;
    }, [feedbackConfig?.criteria]);

    if (!hasRating) {
      return null;
    }

    const commentText = feedback || feedbackDetail?.comment;
    const csatScore = feedbackDetail?.csat_score;
    const channel = feedbackDetail?.channel;
    const criteriaEntries = Object.entries(feedbackDetail?.criteria || {});

    return (
      <div
        onClick={onClick}
        className={cn(
          'mt-1.5 flex flex-wrap items-center gap-1.5 text-xs',
          onClick && 'cursor-pointer hover:opacity-90',
          className,
        )}
      >
        {thumbup !== undefined && (
          <span
            className={cn(
              'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 font-medium',
              thumbup
                ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-500'
                : 'border-rose-500/40 bg-rose-500/10 text-rose-500',
            )}
          >
            {thumbup ? (
              <>
                <ThumbsUp className="size-3" />
                <span>Đã thích</span>
              </>
            ) : (
              <>
                <ThumbsDown className="size-3" />
                <span>Chưa hài lòng</span>
              </>
            )}
          </span>
        )}

        {typeof csatScore === 'number' && csatScore > 0 && (
          <span className="inline-flex items-center gap-1 rounded-full border border-amber-500/40 bg-amber-500/10 px-2 py-0.5 font-medium text-amber-500">
            <Star className="size-3 fill-amber-400 text-amber-400" />
            <span>CSAT {csatScore}/5</span>
          </span>
        )}

        {criteriaEntries.map(([key, val]) => {
          const label = criteriaLabelsMap[key] || key;
          const isPositive =
            typeof val === 'boolean' ? val : typeof val === 'number' && val >= 4;
          return (
            <span
              key={key}
              className={cn(
                'inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px]',
                isPositive
                  ? 'border-border-button bg-bg-card text-text-secondary'
                  : 'border-rose-500/30 bg-rose-500/5 text-rose-400',
              )}
            >
              <span className="opacity-80">{label}:</span>
              <span className="font-medium">
                {typeof val === 'boolean'
                  ? val
                    ? 'Đúng'
                    : 'Chưa chuẩn'
                  : `${val}/5 ⭐`}
              </span>
            </span>
          );
        })}

        {channel && (
          <span className="inline-flex items-center rounded border border-border-button bg-bg-input px-1.5 py-0.5 text-[10px] uppercase tracking-wider text-text-secondary">
            {channel}
          </span>
        )}

        {commentText && (
          <span
            title={commentText}
            className="inline-flex max-w-[320px] items-center gap-1 truncate rounded-md border border-border-button bg-bg-card px-2 py-0.5 text-text-secondary"
          >
            <MessageSquareText className="size-3 shrink-0 text-accent-primary" />
            <span className="truncate">{commentText}</span>
          </span>
        )}
      </div>
    );
  },
);

FeedbackSummaryBadge.displayName = 'FeedbackSummaryBadge';
