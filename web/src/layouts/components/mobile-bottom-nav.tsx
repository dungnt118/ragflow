import { cn } from '@/lib/utils';
import { Routes } from '@/routes';
import {
  LucideDatabase,
  LucideHouse,
  LucideMessageSquare,
  LucideSearch,
  LucideUser,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Link, useLocation } from 'react-router';

export function MobileBottomNav() {
  const { pathname } = useLocation();
  const { t } = useTranslation();

  // Hide bottom navigation in full-screen work surfaces (active chat conversation, agent canvas)
  if (
    pathname.startsWith(`${Routes.Chat}/`) ||
    pathname.startsWith(`${Routes.Agent}/`)
  ) {
    return null;
  }

  const navItems = [
    {
      to: Routes.Root,
      label: t('header.home'),
      icon: LucideHouse,
      isActive: pathname === Routes.Root,
    },
    {
      to: Routes.Chats,
      label: t('header.chat'),
      icon: LucideMessageSquare,
      isActive:
        pathname === Routes.Chats ||
        pathname.startsWith(`${Routes.Chats}/`) ||
        pathname.startsWith(`${Routes.Chat}/`),
    },
    {
      to: Routes.Searches,
      label: t('header.search'),
      icon: LucideSearch,
      isActive:
        pathname === Routes.Searches ||
        pathname.startsWith(`${Routes.Searches}/`) ||
        pathname.startsWith(`${Routes.Search}/`),
    },
    {
      to: Routes.Datasets,
      label: t('header.dataset'),
      icon: LucideDatabase,
      isActive:
        pathname === Routes.Datasets ||
        pathname.startsWith(`${Routes.Datasets}/`) ||
        pathname.startsWith(`${Routes.DatasetBase}/`),
    },
    {
      to: Routes.UserSetting,
      label: t('header.profile', { defaultValue: 'Cá nhân' }),
      icon: LucideUser,
      isActive:
        pathname === Routes.UserSetting ||
        pathname.startsWith(`${Routes.UserSetting}/`),
    },
  ];

  return (
    <nav
      className={cn(
        'md:hidden fixed bottom-0 inset-x-0 z-40',
        'bg-bg-card/95 backdrop-blur-md border-t border-border-button pb-safe',
      )}
      aria-label="Mobile Navigation"
    >
      <div className="flex h-14 items-center justify-around px-2">
        {navItems.map((item) => {
          const Icon = item.icon;
          return (
            <Link
              key={item.to}
              to={item.to}
              className={cn(
                'flex flex-1 flex-col items-center justify-center gap-1 py-1 text-2xl transition-colors',
                item.isActive
                  ? 'text-accent-primary font-medium'
                  : 'text-text-secondary hover:text-text-primary',
              )}
            >
              <Icon className="size-5 shrink-0" />
              <span className="text-[11px] leading-tight truncate max-w-[64px]">
                {item.label}
              </span>
            </Link>
          );
        })}
      </div>
    </nav>
  );
}
