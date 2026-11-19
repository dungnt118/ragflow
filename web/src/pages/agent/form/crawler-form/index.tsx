import { SelectWithSearch } from '@/components/originui/select-with-search';
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { useTranslate } from '@/hooks/common-hooks';
import { zodResolver } from '@hookform/resolvers/zod';
import { memo, useMemo } from 'react';
import { useForm, useFormContext } from 'react-hook-form';
import { z } from 'zod';
import { initialCrawlerValues } from '../../constant';
import { useWatchFormChange } from '../../hooks/use-watch-form-change';
import { INextOperatorForm } from '../../interface';
import { CrawlerResultOptions } from '../../options';
import { QueryVariable } from '../components/query-variable';

export function CrawlerProxyFormField() {
  const { t } = useTranslate('flow');
  const form = useFormContext();

  return (
    <FormField
      control={form.control}
      name="proxy"
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('proxy')}</FormLabel>
          <FormControl>
            <Input placeholder="like: http://127.0.0.1:8888" {...field} />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  );
}

export function CrawlerExtractTypeFormField() {
  const { t } = useTranslate('flow');
  const form = useFormContext();
  const crawlerResultOptions = useMemo(() => {
    return CrawlerResultOptions.map((x) => ({
      value: x,
      label: t(`crawlerResultOptions.${x}`),
    }));
  }, [t]);

  return (
    <FormField
      control={form.control}
      name="extract_type"
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('extractType')}</FormLabel>
          <FormControl>
            <SelectWithSearch {...field} options={crawlerResultOptions} />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  );
}

export const CrawlerFormSchema = {
  proxy: z.string().url().optional().or(z.literal('')),
  extract_type: z.string(),
  max_depth: z.coerce.number().min(0).max(3).optional(),
  max_pages: z.coerce.number().min(1).max(50).optional(),
};

const FormSchema = z.object({
  query: z.string().optional(),
  ...CrawlerFormSchema,
});

export function CrawlerDepthFormFields() {
  const { t } = useTranslate('flow');
  const form = useFormContext();

  return (
    <>
      <FormField
        control={form.control}
        name="max_depth"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('crawlerMaxDepth', { defaultValue: 'Độ sâu quét liên kết (Max Depth: 0-3)' })}</FormLabel>
            <FormControl>
              <Input type="number" min={0} max={3} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name="max_pages"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('crawlerMaxPages', { defaultValue: 'Số trang tối đa (Max Pages: 1-50)' })}</FormLabel>
            <FormControl>
              <Input type="number" min={1} max={50} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
}

function CrawlerForm({ node }: INextOperatorForm) {
  const form = useForm<z.infer<typeof FormSchema>>({
    resolver: zodResolver(FormSchema),
    defaultValues: initialCrawlerValues,
    mode: 'onChange',
  });

  useWatchFormChange(node?.id, form);

  return (
    <Form {...form}>
      <form
        className="space-y-6 p-4"
        onSubmit={(e) => {
          e.preventDefault();
        }}
      >
        <QueryVariable></QueryVariable>
        <CrawlerExtractTypeFormField></CrawlerExtractTypeFormField>
        <CrawlerDepthFormFields></CrawlerDepthFormFields>
        <CrawlerProxyFormField></CrawlerProxyFormField>
      </form>
    </Form>
  );
}

export default memo(CrawlerForm);
