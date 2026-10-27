import { ButtonLoading } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useSetAgent } from '@/hooks/use-agent-request';
import { IModalProps } from '@/interfaces/common';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useBuildDslData } from '../hooks/use-build-dsl';
import {
  AgentSettingId,
  SettingForm,
  SettingFormSchemaType,
} from './setting-form';

export function SettingDialog({ hideModal }: IModalProps<any>) {
  const { t } = useTranslation();
  const { setAgent } = useSetAgent();
  const { buildDslData } = useBuildDslData();

  const submit = useCallback(
    async (values: SettingFormSchemaType) => {
      const { feedback_config, avatar, ...rest } = values;
      const currentDsl = buildDslData();
      const ret = await setAgent({
        ...rest,
        avatar: avatar ?? undefined,
        dsl: {
          ...currentDsl,
          feedback_config,
        },
      });
      if (ret?.code === 0) {
        hideModal?.();
      }
    },
    [buildDslData, hideModal, setAgent],
  );

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('common.edit')}</DialogTitle>
        </DialogHeader>
        <SettingForm submit={submit}></SettingForm>
        <DialogFooter>
          <ButtonLoading type="submit" form={AgentSettingId} loading={false}>
            {t('common.save')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
