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

import { PromptIcon } from '@/assets/icon/next-icon';
import CopyToClipboard from '@/components/copy-to-clipboard';
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { useSetModalState } from '@/hooks/common-hooks';
import { IRemoveMessageById } from '@/hooks/logic-hooks';
import { IFeedbackConfig, IFeedbackDetail } from '@/interfaces/request/chat';
import { cn } from '@/lib/utils';
import { removeThinkSection } from '@/utils/chat';
import {
  LucidePauseCircle,
  LucideRefreshCw,
  LucideStar,
  LucideThumbsDown,
  LucideThumbsUp,
  LucideTrash2,
  LucideVolume2,
} from 'lucide-react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import FeedbackDialog from '../feedback-dialog';
import { PromptDialog } from '../prompt-dialog';
import { Button } from '../ui/button';
import { useRemoveMessage, useSendFeedback, useSpeech } from './hooks';

interface IProps {
  messageId: string;
  content: string;
  prompt?: string;
  showLikeButton: boolean;
  audioBinary?: string;
  showLoudspeaker?: boolean;
  thumbup?: boolean;
  feedback?: string;
  feedbackDetail?: IFeedbackDetail;
  feedbackConfig?: IFeedbackConfig;
  sessionId?: string;
  isShared?: boolean;
  isAgent?: boolean;
  targetId?: string;
  channel?: string;
  onFeedbackSuccess?: (
    messageId: string,
    patch: {
      thumbup?: boolean;
      feedback?: string;
      feedback_detail?: IFeedbackDetail;
    },
  ) => void;
}

export const AssistantGroupButton = ({
  messageId,
  content,
  prompt,
  audioBinary,
  showLikeButton,
  showLoudspeaker = true,
  thumbup,
  feedback,
  feedbackDetail,
  feedbackConfig,
  sessionId,
  isShared,
  isAgent,
  targetId,
  channel,
  onFeedbackSuccess,
}: IProps) => {
  const [localThumbup, setLocalThumbup] = useState<boolean | undefined>(thumbup);
  const [localFeedback, setLocalFeedback] = useState<string | undefined>(feedback);
  const [localDetail, setLocalDetail] = useState<IFeedbackDetail | undefined>(
    feedbackDetail,
  );
  const [pendingThumbup, setPendingThumbup] = useState<boolean | undefined>(
    thumbup,
  );

  useEffect(() => {
    setLocalThumbup(thumbup);
  }, [thumbup]);

  useEffect(() => {
    setLocalFeedback(feedback);
  }, [feedback]);

  useEffect(() => {
    setLocalDetail(feedbackDetail);
  }, [feedbackDetail]);

  const handleFeedbackUpdated = useCallback(
    (
      msgId: string,
      patch: {
        thumbup?: boolean;
        feedback?: string;
        feedback_detail?: IFeedbackDetail;
      },
    ) => {
      setLocalThumbup(patch.thumbup);
      setLocalFeedback(patch.feedback);
      if (patch.feedback_detail) {
        setLocalDetail(patch.feedback_detail);
      }
      onFeedbackSuccess?.(msgId, patch);
    },
    [onFeedbackSuccess],
  );

  const { visible, hideModal, showModal, onFeedbackOk, loading } =
    useSendFeedback(messageId, {
      sessionId,
      isShared,
      isAgent,
      targetId,
      channel,
      onFeedbackSuccess: handleFeedbackUpdated,
    });
  const {
    visible: promptVisible,
    hideModal: hidePromptModal,
    showModal: showPromptModal,
  } = useSetModalState();
  const { t } = useTranslation();
  const { handleRead, ref, isPlaying } = useSpeech(content, audioBinary);

  const copyText = useMemo(() => removeThinkSection(content), [content]);

  const isMultiCriteria =
    feedbackConfig?.mode === 'multi_criteria' &&
    Array.isArray(feedbackConfig?.criteria) &&
    feedbackConfig.criteria.length > 0;

  const isFeedbackEnabled =
    feedbackConfig?.enabled !== undefined
      ? Boolean(feedbackConfig.enabled)
      : showLikeButton;

  const hasRating =
    localThumbup !== undefined ||
    Boolean(localFeedback) ||
    Boolean(localDetail?.csat_score);

  const handleLike = useCallback(() => {
    if (isMultiCriteria) {
      setPendingThumbup(true);
      showModal();
      return;
    }
    onFeedbackOk({ thumbup: true });
  }, [isMultiCriteria, onFeedbackOk, showModal]);

  const handleDislike = useCallback(() => {
    setPendingThumbup(false);
    showModal();
  }, [showModal]);

  return (
    <>
      <div
        className={cn(
          'flex items-center gap-1 transition-opacity',
          hasRating ? 'opacity-100' : 'opacity-0 group-hover:opacity-100',
        )}
        role="toolbar"
      >
        <CopyToClipboard
          text={copyText}
          className="border-0"
          size="icon-xs"
          avoidButtonWrapper
        />

        {showLoudspeaker && (
          <>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="transparent"
                  size="icon-xs"
                  className="border-0"
                  onClick={handleRead}
                >
                  <span>
                    {isPlaying ? <LucidePauseCircle /> : <LucideVolume2 />}
                  </span>
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t('chat.read')}</TooltipContent>
            </Tooltip>

            <audio src="" ref={ref}></audio>
          </>
        )}
        {isFeedbackEnabled && (
          <>
            {isMultiCriteria ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="transparent"
                    size="xs"
                    className={cn(
                      'h-6 px-2 gap-1 text-xs border rounded-full',
                      localThumbup === true
                        ? 'border-emerald-500/50 bg-emerald-500/15 text-emerald-500'
                        : localThumbup === false
                          ? 'border-rose-500/50 bg-rose-500/15 text-rose-500'
                          : 'border-border-button text-text-secondary hover:text-text-primary',
                    )}
                    onClick={() => {
                      setPendingThumbup(localThumbup);
                      showModal();
                    }}
                  >
                    <LucideStar
                      className={cn(
                        'size-3.5',
                        localDetail?.csat_score
                          ? 'fill-amber-400 text-amber-400'
                          : '',
                      )}
                    />
                    <span>
                      {localDetail?.csat_score
                        ? `${localDetail.csat_score}/5`
                        : 'Đánh giá'}
                    </span>
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  {t('chat.multiCriteriaFeedbackTitle', {
                    defaultValue: 'Đánh giá chất lượng phản hồi',
                  })}
                </TooltipContent>
              </Tooltip>
            ) : (
              <>
                <Button
                  variant="transparent"
                  size="icon-xs"
                  className={cn(
                    'border-0',
                    localThumbup === true &&
                      'text-emerald-500 bg-emerald-500/15 hover:bg-emerald-500/20',
                  )}
                  onClick={handleLike}
                >
                  <LucideThumbsUp
                    className={cn(localThumbup === true && 'fill-emerald-500/20')}
                  />
                </Button>

                <Button
                  variant="transparent"
                  size="icon-xs"
                  className={cn(
                    'border-0',
                    localThumbup === false &&
                      'text-rose-500 bg-rose-500/15 hover:bg-rose-500/20',
                  )}
                  onClick={handleDislike}
                >
                  <LucideThumbsDown
                    className={cn(localThumbup === false && 'fill-rose-500/20')}
                  />
                </Button>
              </>
            )}
          </>
        )}
        {prompt && (
          <Button
            onClick={showPromptModal}
            variant="transparent"
            size="icon-xs"
            className="border-0"
          >
            <PromptIcon style={{ fontSize: '16px' }} />
          </Button>
        )}
      </div>
      {visible && (
        <FeedbackDialog
          visible={visible}
          hideModal={hideModal}
          onOk={onFeedbackOk}
          loading={loading}
          feedbackConfig={feedbackConfig}
          initialFeedback={localFeedback}
          initialThumbup={pendingThumbup}
          initialDetail={localDetail}
        ></FeedbackDialog>
      )}
      {promptVisible && (
        <PromptDialog
          visible={promptVisible}
          hideModal={hidePromptModal}
          prompt={prompt}
        ></PromptDialog>
      )}
    </>
  );
};

interface UserGroupButtonProps extends Partial<IRemoveMessageById> {
  messageId: string;
  content: string;
  regenerateMessage?: () => void;
  sendLoading: boolean;
  deleteDisabled?: boolean;
}

export const UserGroupButton = ({
  content,
  messageId,
  sendLoading,
  deleteDisabled = false,
  removeMessageById,
  regenerateMessage,
}: UserGroupButtonProps) => {
  const { onRemoveMessage, loading } = useRemoveMessage(
    messageId,
    removeMessageById,
  );
  const { t } = useTranslation();

  return (
    <div className="flex gap-1 opacity-0 transition-opacity group-hover:opacity-100">
      <CopyToClipboard
        text={content}
        className="border-0"
        size="icon-xs"
        avoidButtonWrapper
      />

      {regenerateMessage && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="transparent"
              size="icon-xs"
              className="border-0"
              onClick={regenerateMessage}
              disabled={sendLoading}
            >
              <LucideRefreshCw className={cn(sendLoading && 'animate-spin')} />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t('chat.regenerate')}</TooltipContent>
        </Tooltip>
      )}
      {removeMessageById && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="transparent"
              size="icon-xs"
              className="border-0"
              onClick={onRemoveMessage}
              disabled={loading || deleteDisabled}
            >
              <LucideTrash2 />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t('common.delete')}</TooltipContent>
        </Tooltip>
      )}
    </div>
  );
};
