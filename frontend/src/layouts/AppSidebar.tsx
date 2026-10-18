import { useCallback, useMemo, useState } from 'react';
import type { ComponentType, CSSProperties } from 'react';
import { useLocation, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { Drawer, Layout, Menu } from 'antd';
import type { MenuProps } from 'antd';
import {
  ApartmentOutlined,
  CloseOutlined,
  CloudServerOutlined,
  CodeOutlined,
  DashboardOutlined,
  ExportOutlined,
  GithubOutlined,
  ImportOutlined,
  LogoutOutlined,
  MenuOutlined,
  MoonFilled,
  MoonOutlined,
  ReadOutlined,
  SafetyOutlined,
  SettingOutlined,
  SunOutlined,
  SwapOutlined,
  TeamOutlined,
  ToolOutlined,
} from '@ant-design/icons';

import { HttpUtil } from '@/utils';
import { formatPanelVersion } from '@/lib/panel-version';
import { pauseAnimationsUntilLeave, useTheme } from '@/hooks/useTheme';
import { useAllSettings } from '@/api/queries/useAllSettings';
import './AppSidebar.css';
const DOCS_URL = 'https://github.com/8bit-warrior/Miniray';
const REPO_URL = 'https://github.com/8bit-warrior/Miniray';
const LOGOUT_KEY = '__logout__';
const RAIL_WIDTH = 72;
const SIDER_WIDTH = 220;

type IconName =
  | 'dashboard'
  | 'inbound'
  | 'team'
  | 'setting'
  | 'tool'
  | 'logout'
  | 'outbound'
  | 'routing'
  | 'security'
  | 'subscription'
  | 'code'
  | 'balancer';

const iconByName: Record<IconName, ComponentType> = {
  dashboard: DashboardOutlined,
  inbound: ImportOutlined,
  team: TeamOutlined,
  setting: SettingOutlined,
  tool: ToolOutlined,
  logout: LogoutOutlined,
  outbound: ExportOutlined,
  routing: SwapOutlined,
  security: SafetyOutlined,
  subscription: CloudServerOutlined,
  code: CodeOutlined,
  balancer: ApartmentOutlined,
};

function DocsButton({ ariaLabel }: { ariaLabel: string }) {
  return (
    <a
      href={DOCS_URL}
      target="_blank"
      rel="noopener noreferrer"
      className="sidebar-docs"
      aria-label={ariaLabel}
      title={ariaLabel}
    >
      <ReadOutlined />
    </a>
  );
}

function VersionBadge({ version }: { version: string }) {
  if (!version) return null;
  const label = formatPanelVersion(version);
  return (
    <a
      href={REPO_URL}
      target="_blank"
      rel="noopener noreferrer"
      className="sider-version"
      aria-label={`GitHub ${label}`}
      title={label}
    >
      <GithubOutlined />
      <span className="sider-version-text">{label}</span>
    </a>
  );
}

function ThemeCycleButton({
  id,
  isDark,
  isUltra,
  onCycle,
  ariaLabel,
}: {
  id: string;
  isDark: boolean;
  isUltra: boolean;
  onCycle: () => void;
  ariaLabel: string;
}) {
  const icon = !isDark ? <SunOutlined /> : !isUltra ? <MoonOutlined /> : <MoonFilled />;
  return (
    <button
      id={id}
      type="button"
      className="sidebar-theme-cycle"
      aria-label={ariaLabel}
      title={ariaLabel}
      onClick={onCycle}
    >
      {icon}
    </button>
  );
}

export default function AppSidebar() {
  const { t } = useTranslation();
  const { isDark, isUltra, toggleTheme, toggleUltra } = useTheme();
  const navigate = useNavigate();
  const { pathname, hash } = useLocation();
  const { allSetting } = useAllSettings();
  const showSubFormats = !!(allSetting.subJsonEnable || allSetting.subClashEnable);
  const showSubBalancers = !!allSetting.subJsonEnable;

  const [drawerOpen, setDrawerOpen] = useState(false);
  const railCollapsed = false;
  const railStyle = { '--sider-rail': `${SIDER_WIDTH}px` } as CSSProperties;

  const currentTheme: 'light' | 'dark' = isDark ? 'dark' : 'light';
  const panelVersion = window.MINIRAY_CUR_VER || '';

  const tabs = useMemo<{ key: string; icon: IconName; title: string }[]>(() => {
    const items: { key: string; icon: IconName; title: string }[] = [
      { key: '/', icon: 'dashboard', title: t('menu.dashboard') },
      { key: '/inbounds', icon: 'inbound', title: t('menu.inbounds') },
      { key: '/clients', icon: 'team', title: t('menu.clients') },
      { key: '/outbound', icon: 'outbound', title: t('menu.outbounds') },
      { key: '/routing', icon: 'routing', title: t('menu.routing') },
      { key: '/settings#general', icon: 'setting', title: t('pages.settings.panelSettings') },
      { key: '/settings#security', icon: 'security', title: t('pages.settings.securitySettings') },
      {
        key: '/settings#subscription',
        icon: 'subscription',
        title: t('pages.settings.subSettings'),
      },
    ];
    if (showSubFormats) {
      items.push({
        key: '/settings#subscription-formats',
        icon: 'code',
        title: t('menu.subFormats'),
      });
    }
    if (showSubBalancers) {
      items.push({
        key: '/settings#subscription-balancers',
        icon: 'balancer',
        title: t('pages.settings.subBalancers.menu'),
      });
    }
    items.push(
      { key: '/xray#basic', icon: 'tool', title: t('pages.xray.basicTemplate') },
      { key: '/xray#advanced', icon: 'code', title: t('pages.xray.advancedTemplate') },
      { key: LOGOUT_KEY, icon: 'logout', title: t('logout') },
    );
    return items;
  }, [t, showSubFormats, showSubBalancers]);

  const navItems = useMemo(() => tabs.filter((tab) => tab.icon !== 'logout'), [tabs]);
  const utilItems = useMemo(() => tabs.filter((tab) => tab.icon === 'logout'), [tabs]);

  const settingsActive = pathname === '/settings';
  const xrayActive = pathname === '/xray';
  const xrayMenuHash = hash === '#balancer' || hash === '#dns' ? '#advanced' : hash || '#basic';
  const selectedKey = settingsActive
    ? `/settings${hash || '#general'}`
    : xrayActive
      ? `/xray${xrayMenuHash}`
      : pathname === ''
        ? '/'
        : pathname;

  const toMenuItems = useCallback(
    (items: typeof tabs): MenuProps['items'] =>
      items.map((tab) => {
        const Icon = iconByName[tab.icon];
        return { key: tab.key, icon: <Icon />, label: tab.title, title: '' };
      }),
    [],
  );

  const openLink = useCallback(
    async (key: string) => {
      if (key === LOGOUT_KEY) {
        await HttpUtil.post('/logout');
        window.location.href = window.MINIRAY_BASE_PATH || '/';
        return;
      }
      navigate(key);
    },
    [navigate],
  );

  const onMenuClick = useCallback<NonNullable<MenuProps['onClick']>>(
    ({ key }) => {
      openLink(String(key));
    },
    [openLink],
  );

  const cycleTheme = useCallback(
    (id: string) => {
      pauseAnimationsUntilLeave(id);
      if (!isDark) {
        toggleTheme();
        if (isUltra) toggleUltra();
      } else if (!isUltra) {
        toggleUltra();
      } else {
        toggleUltra();
        toggleTheme();
      }
    },
    [isDark, isUltra, toggleTheme, toggleUltra],
  );

  return (
    <div className="ant-sidebar" style={railStyle}>
      <Layout.Sider
        theme={currentTheme}
        width={SIDER_WIDTH}
        collapsedWidth={RAIL_WIDTH}
        collapsed={railCollapsed}
      >
        <div className="sider-brand">
          <div className="brand-block brand-identity">
            <span className="brand-text">Miniray</span>
            <VersionBadge version={panelVersion} />
          </div>
          {!railCollapsed && (
            <div className="brand-actions">
              <DocsButton ariaLabel={t('menu.docs') || 'Documentation'} />
              <ThemeCycleButton
                id="theme-cycle"
                isDark={isDark}
                isUltra={isUltra}
                onCycle={() => cycleTheme('theme-cycle')}
                ariaLabel={t('menu.theme')}
              />
            </div>
          )}
        </div>
        <Menu
          theme={currentTheme}
          mode="inline"
          selectedKeys={[selectedKey]}
          className="sider-nav"
          items={toMenuItems(navItems)}
          onClick={onMenuClick}
        />
        <Menu
          theme={currentTheme}
          mode="inline"
          selectedKeys={[selectedKey]}
          className="sider-utility"
          items={toMenuItems(utilItems)}
          onClick={onMenuClick}
        />
      </Layout.Sider>

      <Drawer
        placement="left"
        closable={false}
        open={drawerOpen}
        rootClassName={currentTheme}
        size="min(82vw, 320px)"
        styles={{
          wrapper: { padding: 0 },
          body: { padding: 0, display: 'flex', flexDirection: 'column', height: '100%' },
          header: { display: 'none' },
        }}
        onClose={() => setDrawerOpen(false)}
      >
        <div className="drawer-header">
          <div className="brand-block brand-identity">
            <span className="drawer-brand">Miniray</span>
            <VersionBadge version={panelVersion} />
          </div>
          <div className="drawer-header-actions">
            <DocsButton ariaLabel={t('menu.docs') || 'Documentation'} />
            <ThemeCycleButton
              id="theme-cycle-drawer"
              isDark={isDark}
              isUltra={isUltra}
              onCycle={() => cycleTheme('theme-cycle-drawer')}
              ariaLabel={t('menu.theme')}
            />
            <button
              className="drawer-close"
              type="button"
              aria-label={t('close')}
              onClick={() => setDrawerOpen(false)}
            >
              <CloseOutlined />
            </button>
          </div>
        </div>
        <Menu
          theme={currentTheme}
          mode="inline"
          selectedKeys={[selectedKey]}
          className="drawer-menu drawer-nav"
          items={toMenuItems(navItems)}
          onClick={(info) => {
            onMenuClick(info);
            setDrawerOpen(false);
          }}
        />
        <Menu
          theme={currentTheme}
          mode="inline"
          selectedKeys={[selectedKey]}
          className="drawer-menu drawer-utility"
          items={toMenuItems(utilItems)}
          onClick={(info) => {
            onMenuClick(info);
            setDrawerOpen(false);
          }}
        />
      </Drawer>

      {!drawerOpen && (
        <button
          className="drawer-handle"
          type="button"
          aria-label={t('menu.openMenu')}
          onClick={() => setDrawerOpen(true)}
        >
          <MenuOutlined />
        </button>
      )}
    </div>
  );
}
