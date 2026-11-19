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

import { useSetModalState } from '@/hooks/common-hooks';
import { IRemoveMessageById, useSpeechWithSse } from '@/hooks/logic-hooks';
import {
  IUseFeedbackOptions,
  useDeleteMessage,
  useFeedback,
  useGetChatSearchParams,
} from '@/hooks/use-chat-request';
import { IFeedbackDetail, IFeedbackRequestBody } from '@/interfaces/request/chat';
import { useChatStreamStore } from '@/pages/next-chats/chat-stream/store';
import { hexStringToUint8Array } from '@/utils/common-util';
import { SpeechPlayer } from 'openai-speech-stream-player';
import { useCallback, useEffect, useRef, useState } from 'react';

export interface IUseSendFeedbackParams extends IUseFeedbackOptions {
  onFeedbackSuccess?: (
    messageId: string,
    patch: {
      thumbup?: boolean;
      feedback?: string;
      feedback_detail?: IFeedbackDetail;
    },
  ) => void;
}

export const useSendFeedback = (
  messageId: string,
  options?: IUseSendFeedbackParams,
) => {
  const { visible, hideModal, showModal } = useSetModalState();
  const { feedback, loading } = useFeedback(options);
  const { conversationId } = useGetChatSearchParams();
  const updateMessageFeedback = useChatStreamStore(
    (state) => state.updateMessageFeedback,
  );

  const onFeedbackOk = useCallback(
    async (params: IFeedbackRequestBody) => {
      const ret = await feedback({
        ...params,
        messageId: messageId,
      });

      const code = typeof ret === 'number' ? ret : ret?.code;
      if (code === 0) {
        const msgList =
          typeof ret === 'object'
            ? ret?.data?.messages || ret?.data?.message || []
            : [];
        const matchedMsg = Array.isArray(msgList)
          ? msgList.find(
              (m: any) => m?.id === messageId && m?.role === 'assistant',
            )
          : undefined;
        const serverDetail =
          matchedMsg?.feedback_detail ??
          (typeof ret === 'object' ? ret?.data?.feedback_detail : undefined);
        const serverThumbup =
          matchedMsg?.thumbup !== undefined
            ? Boolean(matchedMsg.thumbup)
            : typeof ret === 'object' && ret?.data?.thumbup !== undefined
              ? Boolean(ret.data.thumbup)
              : params.thumbup;
        const patch = {
          thumbup: serverThumbup,
          feedback: params.feedback,
          feedback_detail:
            serverDetail ||
            (params.csat_score !== undefined || params.criteria !== undefined
              ? {
                  csat_score: params.csat_score,
                  criteria: params.criteria,
                  comment: params.feedback,
                  channel: params.channel || options?.channel || 'web',
                }
              : undefined),
        };
        const targetSessionId = options?.sessionId || conversationId;
        if (targetSessionId) {
          updateMessageFeedback(targetSessionId, messageId, patch);
        }
        options?.onFeedbackSuccess?.(messageId, patch);
        hideModal();
      }
    },
    [feedback, hideModal, messageId, options, conversationId, updateMessageFeedback],
  );

  return {
    loading,
    onFeedbackOk,
    visible,
    hideModal,
    showModal,
  };
};

export const useRemoveMessage = (
  messageId: string,
  removeMessageById?: IRemoveMessageById['removeMessageById'],
) => {
  const { deleteMessage, loading } = useDeleteMessage();

  const onRemoveMessage = useCallback(async () => {
    if (messageId) {
      const code = await deleteMessage(messageId);
      if (code === 0) {
        removeMessageById?.(messageId);
      }
    }
  }, [deleteMessage, messageId, removeMessageById]);

  return { onRemoveMessage, loading };
};

export const useSpeech = (content: string, audioBinary?: string) => {
  const ref = useRef<HTMLAudioElement>(null);
  const { read } = useSpeechWithSse();
  const player = useRef<SpeechPlayer>();
  const [isPlaying, setIsPlaying] = useState<boolean>(false);

  const initialize = useCallback(async () => {
    player.current = new SpeechPlayer({
      audio: ref.current!,
      onPlaying: () => {
        setIsPlaying(true);
      },
      onPause: () => {
        setIsPlaying(false);
      },
      onChunkEnd: () => {},
      mimeType: MediaSource.isTypeSupported('audio/mpeg')
        ? 'audio/mpeg'
        : 'audio/mp4; codecs="mp4a.40.2"', // https://stackoverflow.com/questions/64079424/cannot-replay-mp3-in-firefox-using-mediasource-even-though-it-works-in-chrome
    });
    await player.current.init();
  }, []);

  const pause = useCallback(() => {
    player.current?.pause();
  }, []);

  const speech = useCallback(async () => {
    const response = await read({ text: content });
    if (response) {
      player?.current?.feedWithResponse(response);
    }
  }, [read, content]);

  const handleRead = useCallback(async () => {
    if (isPlaying) {
      setIsPlaying(false);
      pause();
    } else {
      setIsPlaying(true);
      speech();
    }
  }, [setIsPlaying, speech, isPlaying, pause]);

  useEffect(() => {
    if (audioBinary) {
      const units = hexStringToUint8Array(audioBinary);
      if (units) {
        try {
          player.current?.feed(units);
        } catch (error) {
          console.warn(error);
        }
      }
    }
  }, [audioBinary]);

  useEffect(() => {
    initialize();
  }, [initialize]);

  return { ref, handleRead, isPlaying };
};
