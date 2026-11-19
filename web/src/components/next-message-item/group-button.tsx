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
import { AgentChatContext } from '@/pages/agent/context';
import { downloadAgentFile } from '@/services/file-manager-service';
import { removeThinkSection } from '@/utils/chat';
import { downloadFileFromBlob } from '@/utils/file-util';
import {
  DeleteOutlined,
  DislikeFilled,
  DislikeOutlined,
  LikeFilled,
  LikeOutlined,
  PauseCircleOutlined,
  SoundOutlined,
  SyncOutlined,
} from '@ant-design/icons';
import { Download, NotebookText, Star } from 'lucide-react';
import { useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import FeedbackDialog from '../feedback-dialog';
import { PromptDialog } from '../prompt-dialog';
import { Button } from '../ui/button';
import { ToggleGroup, ToggleGroupItem } from '../ui/toggle-group';
import { useRemoveMessage, useSendFeedback, useSpeech } from './hooks';

interface IProps {
  messageId: string;
  content: string;
  prompt?: string;
  showLikeButton: boolean;
  audioBinary?: string;
  showLoudspeaker?: boolean;
  showLog?: boolean;
  attachment?: {
    file_name: string;
    doc_id: string;
    format: string;
  };
  isShare?: boolean;
  thumbup?: boolean;
  feedback?: string;
  feedbackDetail?: IFeedbackDetail;
  feedbackConfig?: IFeedbackConfig;
  sessionId?: string;
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
  showLog = true,
  attachment,
  isShare,
  thumbup,
  feedback,
  feedbackDetail,
  feedbackConfig,
  sessionId,
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
      isShared: isShare,
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

  const isMultiCriteria =
    feedbackConfig?.mode === 'multi_criteria' &&
    Array.isArray(feedbackConfig?.criteria) &&
    feedbackConfig.criteria.length > 0;

  const isFeedbackEnabled =
    feedbackConfig?.enabled !== undefined
      ? Boolean(feedbackConfig.enabled)
      : showLikeButton;

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

  const { showLogSheet } = useContext(AgentChatContext);

  const copyText = useMemo(() => removeThinkSection(content), [content]);

  const handleShowLogSheet = useCallback(() => {
    showLogSheet(messageId);
  }, [messageId, showLogSheet]);

  return (
    <>
      <div className="inline-flex items-center gap-1">
        <ToggleGroup
          type={'single'}
          size="sm"
          variant="outline"
          className="space-x-1"
        >
          <ToggleGroupItem value="a">
            <CopyToClipboard
              text={copyText}
              className="border-none hover:!bg-transparent"
              avoidButtonWrapper
            ></CopyToClipboard>
          </ToggleGroupItem>
          {showLoudspeaker && (
            <ToggleGroupItem value="b" onClick={handleRead}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span>
                    {isPlaying ? <PauseCircleOutlined /> : <SoundOutlined />}
                  </span>
                </TooltipTrigger>
                <TooltipContent>{t('chat.read')}</TooltipContent>
              </Tooltip>
              <audio src="" ref={ref}></audio>
            </ToggleGroupItem>
          )}
          {prompt && (
            <ToggleGroupItem value="e" onClick={showPromptModal}>
              <PromptIcon style={{ fontSize: '16px' }} />
            </ToggleGroupItem>
          )}
          {showLog && (
            <ToggleGroupItem value="f" onClick={handleShowLogSheet}>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span>
                    <NotebookText className="size-4" />
                  </span>
                </TooltipTrigger>
                <TooltipContent>{t('flow.log')}</TooltipContent>
              </Tooltip>
            </ToggleGroupItem>
          )}
          {!!attachment?.doc_id && (
            <ToggleGroupItem
              value="g"
              onClick={async () => {
                try {
                  const response = await downloadAgentFile({
                    docId: attachment.doc_id,
                    ext: attachment.format,
                  });
                  const blob = new Blob([response.data], {
                    type: response.data.type,
                  });
                  downloadFileFromBlob(blob, attachment.file_name);
                } catch (error) {
                  console.error('Download failed:', error);
                }
              }}
            >
              <Download size={16} />
            </ToggleGroupItem>
          )}
        </ToggleGroup>

        {isFeedbackEnabled && (
          <div className="inline-flex items-center gap-1">
            {isMultiCriteria ? (
              <Button
                type="button"
                variant="outline"
                size="sm"
                className={cn(
                  'h-8 px-2.5 gap-1 text-xs',
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
                <Star
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
            ) : (
              <>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className={cn(
                    'h-8 w-8 p-0',
                    localThumbup === true &&
                      'border-emerald-500 bg-emerald-500/15 text-emerald-500 hover:bg-emerald-500/20',
                  )}
                  onClick={handleLike}
                >
                  {localThumbup === true ? <LikeFilled /> : <LikeOutlined />}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className={cn(
                    'h-8 w-8 p-0',
                    localThumbup === false &&
                      'border-rose-500 bg-rose-500/15 text-rose-500 hover:bg-rose-500/20',
                  )}
                  onClick={handleDislike}
                >
                  {localThumbup === false ? (
                    <DislikeFilled />
                  ) : (
                    <DislikeOutlined />
                  )}
                </Button>
              </>
            )}
          </div>
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
}

export const UserGroupButton = ({
  content,
  messageId,
  sendLoading,
  removeMessageById,
  regenerateMessage,
}: UserGroupButtonProps) => {
  const { onRemoveMessage, loading } = useRemoveMessage(
    messageId,
    removeMessageById,
  );
  const { t } = useTranslation();

  return (
    <ToggleGroup
      type="single"
      size="sm"
      variant="outline"
      className="space-x-1"
    >
      <ToggleGroupItem value="a">
        <CopyToClipboard text={content} avoidButtonWrapper></CopyToClipboard>
      </ToggleGroupItem>
      {regenerateMessage && (
        <ToggleGroupItem
          value="b"
          onClick={regenerateMessage}
          disabled={sendLoading}
        >
          <Tooltip>
            <TooltipTrigger asChild>
              <SyncOutlined spin={sendLoading} />
            </TooltipTrigger>
            <TooltipContent>{t('chat.regenerate')}</TooltipContent>
          </Tooltip>
        </ToggleGroupItem>
      )}
      {removeMessageById && (
        <ToggleGroupItem value="c" onClick={onRemoveMessage} disabled={loading}>
          <Tooltip>
            <TooltipTrigger asChild>
              <DeleteOutlined spin={loading} />
            </TooltipTrigger>
            <TooltipContent>{t('common.delete')}</TooltipContent>
          </Tooltip>
        </ToggleGroupItem>
      )}
    </ToggleGroup>
  );
};
