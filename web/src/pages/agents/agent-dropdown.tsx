import {
  ConfirmDeleteDialog,
  ConfirmDeleteDialogNode,
} from '@/components/confirm-delete-dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { useNavigatePage } from '@/hooks/logic-hooks/navigate-hooks';
import { useDeleteAgent } from '@/hooks/use-agent-request';
import { IFlow } from '@/interfaces/database/agent';
import { Logs, PenLine, Tag, Trash2 } from 'lucide-react';
import {
  MouseEventHandler,
  PropsWithChildren,
  useCallback,
  useState,
} from 'react';
import { useTranslation } from 'react-i18next';
import { AgentTagEditor } from './agent-tag-editor';
import { useRenameAgent } from './use-rename-agent';

export function AgentDropdown({
  children,
  showAgentRenameModal,
  agent: agent,
}: PropsWithChildren &
  Pick<ReturnType<typeof useRenameAgent>, 'showAgentRenameModal'> & {
    agent: IFlow;
  }) {
  const { t } = useTranslation();
  const { deleteAgent } = useDeleteAgent();
  const { navigateToAgentLogs } = useNavigatePage();
  const [tagEditorOpen, setTagEditorOpen] = useState(false);

  const handleShowAgentRenameModal: MouseEventHandler<HTMLDivElement> =
    useCallback(
      (e) => {
        e.stopPropagation();
        showAgentRenameModal(agent);
      },
      [agent, showAgentRenameModal],
    );

  const handleOpenAgentLogs: MouseEventHandler<HTMLDivElement> = useCallback(
    (e) => {
      e.stopPropagation();
      navigateToAgentLogs(agent.id)();
    },
    [agent.id, navigateToAgentLogs],
  );

  const handleEditTags: MouseEventHandler<HTMLDivElement> = useCallback((e) => {
    e.stopPropagation();
    setTagEditorOpen(true);
  }, []);

  const handleDelete: MouseEventHandler<HTMLDivElement> = useCallback(() => {
    deleteAgent(agent.id);
  }, [agent.id, deleteAgent]);

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
        <DropdownMenuContent>
          <DropdownMenuItem onClick={handleShowAgentRenameModal}>
            {t('common.rename')} <PenLine />
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={handleOpenAgentLogs}
            data-testid={`agent-dropdown-logs-${agent.id}`}
          >
            {t('flow.log', 'Nhật ký & Đánh giá')} <Logs />
          </DropdownMenuItem>
          <DropdownMenuItem onClick={handleEditTags}>
            {t('flow.editTags')} <Tag />
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <ConfirmDeleteDialog
            onOk={handleDelete}
            title={t('deleteModal.delAgent')}
            content={{
              node: (
                <ConfirmDeleteDialogNode
                  avatar={{ avatar: agent.avatar, name: agent.title }}
                  name={agent.title}
                />
              ),
            }}
          >
            <DropdownMenuItem
              className="text-state-error"
              onSelect={(e) => {
                e.preventDefault();
              }}
              onClick={(e) => {
                e.stopPropagation();
              }}
            >
              {t('common.delete')} <Trash2 />
            </DropdownMenuItem>
          </ConfirmDeleteDialog>
        </DropdownMenuContent>
      </DropdownMenu>
      <AgentTagEditor
        agent={agent}
        open={tagEditorOpen}
        onOpenChange={setTagEditorOpen}
      />
    </>
  );
}
