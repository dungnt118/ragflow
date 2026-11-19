import { useContext, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { NavLink, Outlet, useNavigate } from 'react-router';

import { useMutation, useQuery } from '@tanstack/react-query';

import {
  LucideMenu,
  LucideMonitor,
  LucideServerCrash,
  LucideSquareUserRound,
  LucideUserCog,
  LucideUserStar,
  LucideZap,
} from 'lucide-react';

import { Button } from '@/components/ui/button';
import { Sheet, SheetContent } from '@/components/ui/sheet';
import { cn } from '@/lib/utils';
import { Routes } from '@/routes';
import { getSystemVersion, logout } from '@/services/admin-service';

import { adminStorage } from '@/utils/authorization-util';

import ThemeSwitch from '../../../components/theme-switch';
import { IS_ENTERPRISE } from '../utils';
import { CurrentUserInfoContext } from './root-layout';

const AdminNavigationLayout = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [, setCurrentUserInfo] = useContext(CurrentUserInfoContext);

  const { data: version } = useQuery({
    queryKey: ['admin/version'],
    queryFn: async () => (await getSystemVersion())?.data?.data?.version,
  });

  const navItems = useMemo(
    () => [
      {
        path: Routes.AdminServices,
        name: t('admin.serviceStatus'),
        icon: <LucideServerCrash className="size-[1em]" />,
      },
      {
        path: Routes.AdminUserManagement,
        name: t('admin.userManagement'),
        icon: <LucideUserCog className="size-[1em]" />,
      },
      {
        path: Routes.AdminSandboxSettings,
        name: t('admin.sandboxSettings'),
        icon: <LucideZap className="size-[1em]" />,
      },
      ...(IS_ENTERPRISE
        ? [
            {
              path: Routes.AdminWhitelist,
              name: t('admin.registrationWhitelist'),
              icon: <LucideUserStar className="size-[1em]" />,
            },
            {
              path: Routes.AdminRoles,
              name: t('admin.roles'),
              icon: <LucideSquareUserRound className="size-[1em]" />,
            },
            {
              path: Routes.AdminMonitoring,
              name: t('admin.monitoring'),
              icon: <LucideMonitor className="size-[1em]" />,
            },
          ]
        : []),
    ],
    [t],
  );

  const logoutMutation = useMutation({
    mutationKey: ['adminLogout'],
    mutationFn: async () => {
      await logout();
      adminStorage.removeAll();
      navigate(Routes.Admin);
      setCurrentUserInfo({
        userInfo: null,
        source: null,
      });
    },
    retry: false,
  });

  const [mobileOpen, setMobileOpen] = useState(false);

  const renderNavContent = (onItemClick?: () => void) => (
    <>
      <div className="flex items-center mb-6">
        <img className="size-8 mr-5" src="/logo.svg" alt="logo" />
        <span className="text-xl font-bold">{t('admin.title')}</span>
      </div>

      <nav>
        <ul className="space-y-4">
          {navItems.map((it) => (
            <li key={it.path}>
              <NavLink
                to={it.path}
                onClick={onItemClick}
                className={({ isActive }) =>
                  cn(
                    'px-4 py-3 rounded-lg',
                    'text-base w-full flex items-center justify-start text-text-secondary',
                    'hover:bg-bg-card focus:bg-bg-card focus-visible:bg-bg-card',
                    'hover:text-text-primary focus:text-text-primary focus-visible:text-text-primary',
                    'active:text-text-primary',
                    'transition-colors',
                    {
                      'bg-bg-card text-text-primary': isActive,
                    },
                  )
                }
              >
                {it.icon}
                <span className="ml-3">{it.name}</span>
              </NavLink>
            </li>
          ))}
        </ul>
      </nav>

      <div className="mt-auto space-y-4">
        <div className="flex justify-between items-center">
          <span className="leading-none text-xs text-accent-primary">
            {version}
          </span>

          <ThemeSwitch />
        </div>

        <Button
          size="lg"
          variant="transparent"
          block
          onClick={() => {
            onItemClick?.();
            logoutMutation.mutate();
          }}
        >
          {t('header.logout')}
        </Button>
      </div>
    </>
  );

  return (
    <main className="w-screen h-screen flex flex-col md:flex-row px-3 md:px-6 pt-3 md:pt-12 pb-3 md:pb-6 dark:*:focus-visible:ring-white overflow-hidden">
      {/* Mobile Top Header */}
      <div className="flex items-center justify-between md:hidden pb-3 border-b border-border-button mb-3 shrink-0">
        <div className="flex items-center gap-3">
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setMobileOpen(true)}
            aria-label="Admin Menu"
          >
            <LucideMenu className="size-5" />
          </Button>
          <img className="size-7" src="/logo.svg" alt="logo" />
          <span className="text-lg font-bold">{t('admin.title')}</span>
        </div>
      </div>

      {/* Desktop Sidebar */}
      <aside className="hidden md:flex w-72 mr-6 shrink-0 flex-col gap-6">
        {renderNavContent()}
      </aside>

      {/* Mobile Sheet Drawer */}
      <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
        <SheetContent
          side="left"
          className="w-[85vw] max-w-xs p-5 flex flex-col gap-6"
        >
          {renderNavContent(() => setMobileOpen(false))}
        </SheetContent>
      </Sheet>

      <section className="flex-1 h-full min-w-0 overflow-auto">
        <Outlet />
      </section>
    </main>
  );
};

export default AdminNavigationLayout;
