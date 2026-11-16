import { cn } from '@/lib/utils';
import { Routes } from '@/routes';
import { Outlet, useLocation } from 'react-router';
import { Header } from './components/header';
import { MobileBottomNav } from './components/mobile-bottom-nav';

export function RootLayoutContainer({ children }: React.PropsWithChildren) {
  const { pathname } = useLocation();
  const hasBottomNav =
    !pathname.startsWith(`${Routes.Chat}/`) &&
    !pathname.startsWith(`${Routes.Agent}/`);

  return (
    <div className="size-full min-w-0 grid grid-flow-col grid-cols-1 grid-rows-[auto_1fr] relative">
      <Header className="px-3 py-2.5 sm:px-5 sm:py-4" />

      <main
        className={cn(
          'size-full min-w-0 overflow-hidden',
          hasBottomNav && 'pb-14 md:pb-0',
        )}
      >
        {children}
      </main>
      <MobileBottomNav />
    </div>
  );
}

export default function RootLayout() {
  return (
    <RootLayoutContainer>
      <Outlet />
    </RootLayoutContainer>
  );
}
