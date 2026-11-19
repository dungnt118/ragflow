import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Sheet, SheetContent } from '@/components/ui/sheet';
import { useNavigatePage } from '@/hooks/logic-hooks/navigate-hooks';
import {
  useFetchSessionList,
  useFetchSessionManually,
  useGetChatSearchParams,
} from '@/hooks/use-chat-request';
import { IClientConversation } from '@/interfaces/database/chat';
import { RootLayoutContainer } from '@/layouts/root-layout';
import { cn } from '@/lib/utils';
import { isEmpty } from 'lodash';
import {
  History,
  Logs,
  LucideArrowBigLeft,
  LucideArrowUpRight,
} from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router';
import { useHandleClickConversationCard } from '../hooks/use-click-card';
import { ChatSettings } from './app-settings/chat-settings';
import { MultipleChatBox } from './chat-box/next-multiple-chat-box';
import { SingleChatBox } from './chat-box/single-chat-box';
import { Sessions } from './sessions';
import { useAddChatBox } from './use-add-box';
import { useSwitchDebugMode } from './use-switch-debug-mode';

export default function Chat() {
  const { t } = useTranslation();
  const { id: chatId } = useParams();
  const { navigateToChatLogs } = useNavigatePage();
  const [currentConversation, setCurrentConversation] =
    useState<IClientConversation>({} as IClientConversation);

  const { fetchSessionManually } = useFetchSessionManually();

  const { handleConversationCardClick, controller, stopOutputMessage } =
    useHandleClickConversationCard();

  const { isDebugMode, switchDebugMode } = useSwitchDebugMode();
  const { removeChatBox, addChatBox, chatBoxIds, hasSingleChatBox } =
    useAddChatBox(isDebugMode);

  const { conversationId, isNew } = useGetChatSearchParams();

  const { data: dialogList } = useFetchSessionList();

  const currentConversationName = useMemo(() => {
    return (
      dialogList.find((x) => x.id === conversationId)?.name ||
      t('chat.newConversation')
    );
  }, [conversationId, dialogList, t]);

  // The URL is the single source of truth for which conversation is open:
  // card clicks, "+" and the temp→real id swap after the first send all land
  // here. Clear first so the previous conversation's messages and references
  // can never leak into the newly opened one while the fetch is in flight.
  useEffect(() => {
    setCurrentConversation((previous) =>
      isEmpty(previous) ? previous : ({} as IClientConversation),
    );
    if (!conversationId || isNew === 'true') return;

    let cancelled = false;
    fetchSessionManually(conversationId).then((conversation) => {
      if (!cancelled && !isEmpty(conversation)) {
        setCurrentConversation(conversation);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [conversationId, isNew, fetchSessionManually]);

  if (isDebugMode) {
    return (
      <section
        className="pt-5 pb-14 h-[100vh] flex flex-col"
        data-testid="chat-detail-multimodel-root"
      >
        <header className="px-10 pb-5">
          <div className="mb-5">
            <Button
              variant="outline"
              onClick={switchDebugMode}
              data-testid="chat-detail-multimodel-back"
            >
              <LucideArrowBigLeft />
              <span>{t('common.back')}</span>
            </Button>
          </div>

          <span className="text-2xl">
            {t('chat.multipleModels')} ({chatBoxIds.length}/3)
          </span>
        </header>

        <MultipleChatBox
          chatBoxIds={chatBoxIds}
          controller={controller}
          removeChatBox={removeChatBox}
          addChatBox={addChatBox}
          stopOutputMessage={stopOutputMessage}
          conversation={currentConversation}
        ></MultipleChatBox>
      </section>
    );
  }

  const [mobileSessionsOpen, setMobileSessionsOpen] = useState(false);

  return (
    <RootLayoutContainer>
      <section className="h-full flex flex-col" data-testid="chat-detail">
        <article className="flex flex-1 min-h-0 pb-2 md:pb-9">
          <div className="hidden md:flex shrink-0">
            <Sessions
              handleConversationCardClick={handleConversationCardClick}
            />
          </div>

          <Sheet open={mobileSessionsOpen} onOpenChange={setMobileSessionsOpen}>
            <SheetContent
              side="left"
              className="w-[85vw] max-w-sm p-0 flex flex-col"
              closeIcon={false}
            >
              <Sessions
                handleConversationCardClick={(id, isNew) => {
                  handleConversationCardClick(id, isNew);
                  setMobileSessionsOpen(false);
                }}
                onClose={() => setMobileSessionsOpen(false)}
                className="w-full h-full p-4"
              />
            </SheetContent>
          </Sheet>

          <Card className="flex-1 min-w-0 bg-transparent border-none shadow-none h-full">
            <CardContent className="flex p-0 h-full">
              <Card className="flex flex-col flex-1 bg-transparent min-w-0 overflow-hidden">
                <CardHeader
                  className={cn('p-3 sm:p-5', {
                    'border-b-0.5 border-border-button': hasSingleChatBox,
                  })}
                >
                  <CardTitle className="flex justify-between items-center text-base gap-2">
                    <div className="flex items-center gap-1.5 min-w-0">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="md:hidden shrink-0"
                        onClick={() => setMobileSessionsOpen(true)}
                        aria-label="Danh sách hội thoại"
                      >
                        <History className="size-4" />
                      </Button>
                      <div className="truncate">{currentConversationName}</div>
                    </div>

                    <div className="flex items-center gap-1 sm:gap-2">
                      {chatId && (
                        <Button
                          variant="outline"
                          size="sm"
                          className="hidden sm:inline-flex"
                          onClick={navigateToChatLogs(chatId)}
                          data-testid="chat-header-feedback-logs-btn"
                        >
                          <Logs className="size-4 mr-1" />
                          Nhật ký & Đánh giá
                        </Button>
                      )}
                      <Button
                        variant="ghost"
                        onClick={switchDebugMode}
                        data-testid="chat-detail-multimodel-toggle"
                        className="text-xs sm:text-sm px-2 sm:px-3"
                      >
                        <LucideArrowUpRight className="size-4 sm:size-auto" />
                        <span className="hidden sm:inline">
                          {t('chat.multipleModels')}
                        </span>
                      </Button>
                    </div>
                  </CardTitle>
                </CardHeader>
                <CardContent className="flex-1 p-0 min-h-0">
                  <SingleChatBox conversation={currentConversation} />
                </CardContent>
              </Card>

              <ChatSettings hasSingleChatBox={hasSingleChatBox}></ChatSettings>
            </CardContent>
          </Card>
        </article>
      </section>
    </RootLayoutContainer>
  );
}
